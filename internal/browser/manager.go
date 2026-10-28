package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Daybeam/ghost-driver/internal/cdp"
	"github.com/Daybeam/ghost-driver/internal/profile"
)

type Mode string

const (
	ModeAttached Mode = "attached"
	ModeManaged  Mode = "managed"
)

type Config struct {
	Mode             Mode
	DevToolsHTTP     string // e.g. "http://localhost:9222"
	BrowserPath      string // path to chrome/chromium/electron/webview2-app binary
	ProxyServer      string // e.g. "http://proxy:8080"
	UserDataDir      string // path to user data directory for session persistence
	StealthEnabled   bool
	Headless         bool
	CDPProxyUpstream string // host.docker.internal:9222
	CDPProxyPort     int    // 9223
	AppType          string // "chrome" (default), "electron", "webview2"
	NewTab           bool   // create a new tab instead of grabbing the first existing one
}

type Manager struct {
	cfg      Config
	mu       sync.RWMutex
	cmd      *exec.Cmd
	browser  *Browser
	registry *profile.Registry
	cdpProxy interface{}
}

func NewManager(cfg Config, registry *profile.Registry) *Manager {
	if cfg.Mode == ModeManaged && cfg.BrowserPath == "" {
		cfg.BrowserPath = DefaultBrowserPath()
	}
	if cfg.DevToolsHTTP == "" {
		cfg.DevToolsHTTP = "http://127.0.0.1:9222"
	}
	return &Manager{cfg: cfg, registry: registry}
}

func (m *Manager) Start(ctx context.Context) (*Browser, error) {
	m.cfg.DetectAndApplyCDPProxy()

	if m.cfg.CDPProxyUpstream != "" {
		port := m.cfg.CDPProxyPort
		if port == 0 {
			port = 9223
		}
		proxy := cdp.NewProxy(cdp.ProxyConfig{
			ListenAddr: fmt.Sprintf("127.0.0.1:%d", port),
			Upstream:   m.cfg.CDPProxyUpstream,
			Logf: func(f string, a ...any) {
				fmt.Printf("[ghost-driver] "+f+"\n", a...)
			},
		})
		m.mu.Lock()
		m.cdpProxy = proxy
		m.mu.Unlock()
		go func() {
			if err := proxy.Start(); err != nil {
				fmt.Printf("[ghost-driver] CDP proxy failed: %v\n", err)
			}
		}()
		// Redirect devtools HTTP to proxy
		m.cfg.DevToolsHTTP = fmt.Sprintf("http://127.0.0.1:%d", port)
	}

	if m.cfg.Mode == ModeAttached {
		return m.startAttached(ctx)
	}
	return m.startManaged(ctx)
}

func (m *Manager) startAttached(ctx context.Context) (*Browser, error) {
	// In attached mode, we assume the browser is already running.
	// We just need to connect to it.
	return New(ctx, m.cfg.DevToolsHTTP, "", "", m.cfg.StealthEnabled, m.registry, m.cfg.NewTab)
}

func (m *Manager) startManaged(ctx context.Context) (*Browser, error) {
	appType := m.cfg.AppType
	if appType == "" {
		appType = detectAppType(m.cfg.BrowserPath)
	}
	m.cfg.AppType = appType

	var args []string
	switch appType {
	case "webview2":
		// Tauri/Wails on Windows: native shell, no CLI args.
		// CDP enabled via WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS env var.
		args = nil
	case "electron":
		// Electron: --remote-debugging-port passes through to embedded Chromium.
		// Skip Chrome-specific flags (headless, stealth) — desktop apps want a visible window.
		args = []string{"--remote-debugging-port=9222"}
	case "firefox":
		// Extract port from DevToolsHTTP, default to 9333 for Firefox.
		port := 9333
		if parsed, err := url.Parse(m.cfg.DevToolsHTTP); err == nil {
			if _, p, err := net.SplitHostPort(parsed.Host); err == nil && p != "" {
				fmt.Sscanf(p, "%d", &port)
			}
		}
		args = []string{
			fmt.Sprintf("--remote-debugging-port=%d", port),
			"--no-remote",
		}
		m.cfg.DevToolsHTTP = fmt.Sprintf("http://127.0.0.1:%d", port)
		if m.cfg.Headless {
			args = append(args, "--headless")
		}
		if m.cfg.ProxyServer != "" {
			args = append(args, fmt.Sprintf("--proxy-server=%s", m.cfg.ProxyServer))
		}
		if m.cfg.UserDataDir != "" {
			args = append(args, "-profile", m.cfg.UserDataDir)
		}
	default: // "chrome"
		args = []string{
			"--remote-debugging-port=9222",
			"--no-first-run",
			"--no-default-browser-check",
		}
		if m.cfg.Headless {
			args = append(args, "--headless=new")
		} else {
			args = append(args, "--headless=false")
		}
		if m.cfg.ProxyServer != "" {
			args = append(args, fmt.Sprintf("--proxy-server=%s", m.cfg.ProxyServer))
		}
		if m.cfg.UserDataDir != "" {
			args = append(args, fmt.Sprintf("--user-data-dir=%s", m.cfg.UserDataDir))
		}
		args = append(args, "--disable-blink-features=AutomationControlled")
	}

	cmd := exec.CommandContext(ctx, m.cfg.BrowserPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if appType == "webview2" {
		cmd.Env = append(os.Environ(),
			"WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS=--remote-debugging-port=9222",
		)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start browser: %w", err)
	}
	m.mu.Lock()
	m.cmd = cmd
	m.mu.Unlock()
	go func() { _ = cmd.Wait() }()

	// Wait for CDP port to be ready
	var wsURL string
	err := retry(5, 1*time.Second, func() error {
		u := m.cfg.DevToolsHTTP + "/json/version"
		req, err := http.NewRequest("GET", u, nil)
		if err != nil {
			return err
		}

		// Bypass Chrome's Host header validation.
		req.Host = "localhost"
		if parsed, err := url.Parse(m.cfg.DevToolsHTTP); err == nil {
			if strings.Contains(parsed.Host, ":") {
				_, port, _ := net.SplitHostPort(parsed.Host)
				if port != "" {
					req.Host = "localhost:" + port
				}
			}
		}

		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		// Firefox BiDi: /json/version returns 404. Just check the port is alive.
		if appType == "firefox" {
			if resp.StatusCode == 404 || resp.StatusCode == 200 {
				return nil
			}
			return fmt.Errorf("unexpected status %d", resp.StatusCode)
		}

		var version struct {
			WebsocketDebuggerUrl string `json:"webSocketDebuggerUrl"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&version); err != nil {
			return err
		}
		if version.WebsocketDebuggerUrl == "" {
			return fmt.Errorf("no websockets url found")
		}
		wsURL = version.WebsocketDebuggerUrl
		return nil
	})

	if err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("browser failed to start CDP port: %w", err)
	}

	b, err := New(ctx, m.cfg.DevToolsHTTP, wsURL, "", m.cfg.StealthEnabled, m.registry, false)
	if err != nil {
		_ = cmd.Process.Kill()
		return nil, err
	}

	m.mu.Lock()
	m.browser = b
	m.mu.Unlock()
	return b, nil
}

func (m *Manager) NewTab(ctx context.Context, urlStr string) (*Browser, error) {
	m.mu.RLock()
	b := m.browser
	m.mu.RUnlock()
	if b != nil && b.IsHealthy(ctx) {
		b2, err := b.CreateNewTab(ctx, urlStr)
		if err == nil {
			return b2, nil
		}
	}

	u := m.cfg.DevToolsHTTP + "/json/new"
	if urlStr != "" {
		u += "?url=" + url.QueryEscape(urlStr)
	}

	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}

	// Bypass Chrome's Host header validation.
	req.Host = "localhost"
	if parsed, err := url.Parse(m.cfg.DevToolsHTTP); err == nil {
		if strings.Contains(parsed.Host, ":") {
			_, port, _ := net.SplitHostPort(parsed.Host)
			if port != "" {
				req.Host = "localhost:" + port
			}
		}
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("new_tab: unexpected HTTP %d from Chrome (modern Chrome M111+ deprecates /json/new; connect_browser first)", resp.StatusCode)
	}

	var t struct {
		Id                   string `json:"id"`
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&t); err != nil {
		return nil, fmt.Errorf("new_tab: Chrome returned non-JSON response (url may have redirected): %w", err)
	}
	if t.WebSocketDebuggerURL == "" || t.Id == "" {
		return nil, fmt.Errorf("new_tab: Chrome returned empty target info")
	}
	newB, err := New(ctx, m.cfg.DevToolsHTTP, t.WebSocketDebuggerURL, t.Id, m.cfg.StealthEnabled, m.registry, false)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.browser = newB
	m.mu.Unlock()
	return newB, nil
}

func (m *Manager) CloseTab(targetID string) error {
	u := m.cfg.DevToolsHTTP + "/json/close/" + targetID
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return err
	}

	// Bypass Chrome's Host header validation.
	req.Host = "localhost"
	if parsed, err := url.Parse(m.cfg.DevToolsHTTP); err == nil {
		if strings.Contains(parsed.Host, ":") {
			_, port, _ := net.SplitHostPort(parsed.Host)
			if port != "" {
				req.Host = "localhost:" + port
			}
		}
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (m *Manager) Stop() {
	m.mu.RLock()
	b := m.browser
	cmd := m.cmd
	proxy := m.cdpProxy
	m.mu.RUnlock()
	if b != nil {
		b.Close()
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	if proxy != nil {
		if p, ok := proxy.(*cdp.ReverseProxy); ok {
			p.Stop()
		}
	}
}

func (m *Manager) IsHealthy(ctx context.Context) bool {
	m.mu.RLock()
	b := m.browser
	m.mu.RUnlock()
	if b == nil {
		return false
	}
	return b.IsHealthy(ctx)
}

func (c *Config) DetectAndApplyCDPProxy() {
	if c.CDPProxyUpstream != "" {
		return // Already set explicitly
	}

	u, err := url.Parse(c.DevToolsHTTP)
	if err != nil {
		return
	}

	host := u.Hostname()
	// If host is not localhost/127.0.0.1/::1, it's likely a remote/docker host
	if host != "" && host != "localhost" && host != "127.0.0.1" && host != "::1" {
		c.CDPProxyUpstream = u.Host
		if c.CDPProxyPort == 0 {
			c.CDPProxyPort = 9223 // Default proxy port
		}
	}
}

func (m *Manager) DevToolsHTTP() string {
	return m.cfg.DevToolsHTTP
}

func DefaultBrowserPath() string {
	switch runtime.GOOS {
	case "linux":
		paths := []string{
			"/usr/bin/google-chrome",
			"/usr/bin/chromium-browser",
			"/usr/bin/chromium",
			"/usr/bin/microsoft-edge",
			"/usr/bin/brave-browser",
			"/usr/bin/brave",
			"/usr/bin/opera",
			"/usr/bin/vivaldi",
			"/usr/bin/vivaldi-stable",
			"/usr/bin/firefox",
			"/usr/bin/zen-browser",
		}
		for _, p := range paths {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	case "darwin":
		paths := []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
			"/Applications/Opera.app/Contents/MacOS/Opera",
			"/Applications/Vivaldi.app/Contents/MacOS/Vivaldi",
			"/Applications/Arc.app/Contents/MacOS/Arc",
			"/Applications/Comet.app/Contents/MacOS/Comet",
			"/Applications/Firefox.app/Contents/MacOS/firefox",
			"/Applications/Zen Browser.app/Contents/MacOS/zen",
		}
		for _, p := range paths {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	case "windows":
		paths := []string{
			`C:\Program Files\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
			`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
			`C:\Program Files\BraveSoftware\Brave-Browser\Application\brave.exe`,
			`C:\Program Files (x86)\BraveSoftware\Brave-Browser\Application\brave.exe`,
			`C:\Program Files\Mozilla Firefox\firefox.exe`,
			`C:\Program Files (x86)\Mozilla Firefox\firefox.exe`,
			`C:\Program Files\Zen Browser\zen.exe`,
			`C:\Program Files (x86)\Zen Browser\zen.exe`,
		}
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData != "" {
			paths = append(paths,
				filepath.Join(localAppData, `Google\Chrome\Application\chrome.exe`),
				filepath.Join(localAppData, `Microsoft\Edge\Application\msedge.exe`),
				filepath.Join(localAppData, `BraveSoftware\Brave-Browser\Application\brave.exe`),
				filepath.Join(localAppData, `Programs\Opera\opera.exe`),
				filepath.Join(localAppData, `Vivaldi\Application\vivaldi.exe`),
				filepath.Join(localAppData, `Programs\Arc\Arc.exe`),
				filepath.Join(localAppData, `Programs\Comet\Comet.exe`),
				filepath.Join(localAppData, `zen\zen.exe`),
			)
		}
		for _, p := range paths {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return "google-chrome"
}

// CheckPortOpen checks if the DevTools HTTP port is responding.
func CheckPortOpen(devtoolsHTTP string) bool {
	u, err := url.Parse(devtoolsHTTP)
	if err != nil {
		return false
	}
	// Use /json/version to verify it's a real CDP endpoint
	target := fmt.Sprintf("%s://%s/json/version", u.Scheme, u.Host)
	req, err := http.NewRequest("GET", target, nil)
	if err != nil {
		return false
	}

	// Bypass Chrome's Host header validation.
	req.Host = "localhost"
	if strings.Contains(u.Host, ":") {
		_, port, _ := net.SplitHostPort(u.Host)
		if port != "" {
			req.Host = "localhost:" + port
		}
	}

	client := http.Client{
		Timeout: 2 * time.Second, // Increased timeout
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	// Any HTTP response means the port is open and a debugger is listening.
	// Chrome returns 200 on /json/version; Firefox BiDi returns 404.
	return resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotFound
}

func retry(attempts int, sleep time.Duration, f func() error) error {
	var err error
	for i := 0; i < attempts; i++ {
		if err = f(); err == nil {
			return nil
		}
		time.Sleep(sleep)
	}
	return fmt.Errorf("after %d attempts, last error: %w", attempts, err)
}

// detectAppType inspects a binary's embedded strings to determine its framework.
// Returns "electron", "webview2" (Tauri/Wails), or "chrome" (default/unknown).
// Scans the file in 1MB chunks with a 32-byte overlap so patterns that straddle
// chunk boundaries are still found — avoids loading a 200MB binary into RAM.
func detectAppType(path string) string {
	// Quick path-name check for Firefox and Firefox forks (Zen)
	lowerPath := strings.ToLower(path)
	if strings.Contains(lowerPath, "firefox") || strings.Contains(lowerPath, "zen") {
		return "firefox"
	}
	electronSigs := [][]byte{[]byte("chrome_elf.dll"), []byte("libffmpeg.dll")}
	webview2Sigs := [][]byte{[]byte("WebView2"), []byte("WEBVIEW2_ADDITIONAL")}

	f, err := os.Open(path)
	if err != nil {
		return "chrome"
	}
	defer f.Close()

	const chunkSize = 1 << 20
	const overlap = 32 // > len("WEBVIEW2_ADDITIONAL")
	buf := make([]byte, chunkSize)
	var tail []byte

	for {
		n, readErr := f.Read(buf)
		if n > 0 {
			chunk := make([]byte, 0, len(tail)+n)
			chunk = append(chunk, tail...)
			chunk = append(chunk, buf[:n]...)
			for _, sig := range electronSigs {
				if bytes.Contains(chunk, sig) {
					return "electron"
				}
			}
			for _, sig := range webview2Sigs {
				if bytes.Contains(chunk, sig) {
					return "webview2"
				}
			}
			tail = tail[:0]
			if n >= overlap {
				tail = append(tail, buf[n-overlap:n]...)
			} else {
				tail = append(tail, chunk...)
			}
		}
		if readErr != nil {
			break
		}
	}
	return "chrome"
}
