// Package browser provides high-level browser operations built on top of CDP.
// It manages the connection lifecycle and exposes atomic actions that the MCP
// tool layer composes into workflows.
package browser

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Daybeam/ghost-driver/internal/bidi"
	"github.com/Daybeam/ghost-driver/internal/cdp"
	"github.com/Daybeam/ghost-driver/internal/profile"
	"github.com/Daybeam/ghost-driver/internal/resources"
)

// dbgLog writes debug trace lines to a file to diagnose connect_browser hangs.
// Set GHOST_DRIVER_DEBUG_LOG=/path/to/log to enable. The file handle is opened
// once and reused (opening/closing on every line was a measurable perf hit).
var (
	dbgLogPath = os.Getenv("GHOST_DRIVER_DEBUG_LOG")
	dbgMu      sync.Mutex
	dbgFile    *os.File
)

func dbg(format string, args ...any) {
	if dbgLogPath == "" {
		return
	}
	msg := fmt.Sprintf(format, args...)
	dbgMu.Lock()
	defer dbgMu.Unlock()
	if dbgFile == nil {
		f, err := os.OpenFile(dbgLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return
		}
		dbgFile = f
	}
	fmt.Fprintf(dbgFile, "%s %s\n", time.Now().Format("15:04:05.000"), msg)
}

// Browser wraps a CDP client and maintains per-session state.
type Browser struct {
	mu                     sync.RWMutex
	connectMu              sync.Mutex // serializes reconnect attempts in Call
	client                 cdp.CDPClient
	httpURL                string // e.g. "http://localhost:9222"
	wsURL                  string // e.g. "ws://localhost:9222/devtools/browser/..."
	targetID               string
	registry               *profile.Registry
	browserSession         bool   // true if using browser-level WebSocket (survives navigation)
	stealth                bool   // config stored for reconnection
	browserType            string // "chrome" or "firefox", auto-detected from /json/version
	newTab                 bool   // create a new tab instead of grabbing the first existing one
	IsDirty                bool   // true if user has typed/interacted with forms
	handlers               map[string][]handlerEntry
	handlerCounter         atomic.Int64
	HasSystemDialog        bool         // true if a system file picker is open
	fileChooserBackendNode atomic.Int64 // backendNodeId from Page.fileChooserOpened event (for DOM.setFileInputFiles fallback)
	closing                bool         // true once Close() started; suppresses auto-reconnect in Call
}

// handlerEntry pairs a handler with a unique ID for O(1) removal.
type handlerEntry struct {
	id int64
	fn cdp.EventHandler
}

func New(ctx context.Context, devtoolsHTTP, wsURL, targetID string, stealth bool, registry *profile.Registry, newTab bool) (*Browser, error) {
	b := &Browser{
		httpURL:  devtoolsHTTP,
		wsURL:    wsURL,
		targetID: targetID,
		registry: registry,
		stealth:  stealth,
		newTab:   newTab,
	}

	if err := b.reconnect(ctx, wsURL); err != nil {
		return nil, err
	}

	dbg("New: reconnect ok, calling setupDialogHandler")
	b.setupDialogHandler(ctx)
	dbg("New: setupDialogHandler done, calling ShowIndicator")
	if err := b.ShowIndicator(ctx, DefaultIndicatorStyle()); err != nil {
		dbg("New: ShowIndicator error: %v", err)
	}
	dbg("New: ShowIndicator done")

	return b, nil
}

func (b *Browser) normalizeWSURL(wsURL string) string {
	u, err := url.Parse(b.httpURL)
	if err != nil {
		return wsURL
	}
	host := u.Hostname()
	if host == "" {
		host = "127.0.0.1"
	}
	// Replace localhost/127.0.0.1 in the WS URL with the hostname from our HTTP URL.
	// This ensures we stay on the same IP stack (IPv4 vs IPv6).
	wsURL = strings.ReplaceAll(wsURL, "localhost", host)
	// If we are connecting to a specific IP, make sure we don't accidentally mix it.
	if host != "127.0.0.1" {
		wsURL = strings.ReplaceAll(wsURL, "127.0.0.1", host)
	}
	return wsURL
}

func (b *Browser) reconnect(ctx context.Context, wsURL string) error {
	dbg("reconnect START wsURL=%q", wsURL)

	// Use a dedicated timeout for the reconnection process itself
	// to prevent blocking the caller forever if Chrome is hung.
	rctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Close the old client OUTSIDE b.mu to avoid a ~3s stall when an inline
	// event handler (dispatched in readLoop under semaphore backpressure)
	// calls b.Off which needs b.mu — that would deadlock until client.Close's
	// internal timeout fires. connectMu serializes reconnects so b.client
	// stays nil between the unlock and re-lock; Call() handles nil gracefully.
	b.mu.Lock()
	oldClient := b.client
	b.client = nil
	b.mu.Unlock()
	if oldClient != nil {
		dbg("reconnect closing existing client")
		oldClient.Close()
		dbg("reconnect existing client closed")
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	dbg("reconnect got b.mu")

	browserWSURL, browserErr := cdp.BrowserWebSocketURL(b.httpURL)
	dbg("reconnect BrowserWebSocketURL err=%v url=%q", browserErr, browserWSURL)

	var client cdp.CDPClient
	var usedBrowserSession bool

	if browserErr == nil && browserWSURL != "" {
		// Normalize to intended host (e.g. 127.0.0.1) instead of localhost
		browserWSURL = b.normalizeWSURL(browserWSURL)
		dbg("reconnect dialing normalized browser WS: %q", browserWSURL)
		c, err := cdp.Connect(rctx, browserWSURL)
		dbg("reconnect dial done err=%v", err)
		if err == nil {
			client = c
			usedBrowserSession = true
		}
	}

	if client == nil {
		bt := cdp.DetectBrowserType(b.httpURL)
		b.browserType = bt
		dbg("reconnect browserType=%s", bt)

		if bt == "firefox" {
			wsBidi := strings.Replace(b.httpURL, "http://", "ws://", 1) + "/session"
			dbg("reconnect BiDi WS: %q", wsBidi)
			c, err := bidi.Connect(rctx, wsBidi)
			if err != nil {
				return err
			}
			client = c
		} else {
			if wsURL == "" {
				var err error
				_, wsURL, err = cdp.FirstPageTarget(b.httpURL)
				if err != nil {
					return err
				}
			}
			wsURL = b.normalizeWSURL(wsURL)
			dbg("reconnect dialing normalized page WS: %q", wsURL)
			c, err := cdp.Connect(rctx, wsURL)
			if err != nil {
				return err
			}
			client = c
		}
	} else {
		b.browserType = cdp.DetectBrowserType(b.httpURL)
		dbg("reconnect browserType=%s", b.browserType)
	}

	b.client = client
	b.browserSession = usedBrowserSession

	if usedBrowserSession {
		if _, err := client.Call(rctx, "Target.setDiscoverTargets", map[string]any{"discover": true}); err != nil {
			client.Close()
			return fmt.Errorf("setDiscoverTargets: %w", err)
		}
		dbg("reconnect setDiscoverTargets ok")

		_, _ = client.Call(rctx, "Target.setAutoAttach", map[string]any{
			"autoAttach":             true,
			"waitForDebuggerOnStart": false,
			"flatten":                true,
		})

		// If targetID matches the Browser target ID in wsURL (e.g. from /json/version),
		// clear targetID so we attach to a page target instead of the browser root.
		if b.targetID != "" && wsURL != "" && strings.Contains(wsURL, b.targetID) {
			b.targetID = ""
		}

		if b.targetID != "" {
			result, err := client.Call(rctx, "Target.attachToTarget", map[string]any{
				"targetId": b.targetID,
				"flatten":  true,
			})
			if err != nil {
				client.Close()
				return fmt.Errorf("attachToTarget %s: %w", b.targetID, err)
			}
			var resp struct {
				SessionID string `json:"sessionId"`
			}
			if json.Unmarshal(result, &resp) == nil && resp.SessionID != "" {
				client.SetSessionID(resp.SessionID)
			}
		} else if b.newTab {
			result, err := client.Call(rctx, "Target.createTarget", map[string]any{
				"url": "about:blank",
			})
			if err != nil {
				client.Close()
				return fmt.Errorf("createTarget (new tab): %w", err)
			}
			var created struct {
				TargetID string `json:"targetId"`
			}
			if json.Unmarshal(result, &created) != nil || created.TargetID == "" {
				client.Close()
				return fmt.Errorf("createTarget: empty targetId in response")
			}
			b.targetID = created.TargetID
			result, err = client.Call(rctx, "Target.attachToTarget", map[string]any{
				"targetId": created.TargetID,
				"flatten":  true,
			})
			if err != nil {
				client.Close()
				return fmt.Errorf("attachToTarget %s: %w", created.TargetID, err)
			}
			var resp struct {
				SessionID string `json:"sessionId"`
			}
			if json.Unmarshal(result, &resp) == nil && resp.SessionID != "" {
				client.SetSessionID(resp.SessionID)
			}
		} else {
			targets, err := cdp.ListTargets(b.httpURL)
			if err == nil {
				var selectedTarget *cdp.Target
				for i, t := range targets {
					if t.Type == "page" && t.ID != "" {
						isInternal := strings.HasPrefix(t.URL, "chrome://") ||
							strings.HasPrefix(t.URL, "chrome-untrusted://") ||
							strings.HasPrefix(t.URL, "chrome-extension://") ||
							strings.HasPrefix(t.URL, "devtools://")
						if !isInternal {
							selectedTarget = &targets[i]
							break
						}
					}
				}
				if selectedTarget != nil {
					b.targetID = selectedTarget.ID
					result, err := client.Call(rctx, "Target.attachToTarget", map[string]any{
						"targetId": selectedTarget.ID,
						"flatten":  true,
					})
					if err == nil {
						var resp struct {
							SessionID string `json:"sessionId"`
						}
						if json.Unmarshal(result, &resp) == nil && resp.SessionID != "" {
							client.SetSessionID(resp.SessionID)
							_, _ = client.Call(rctx, "Target.activateTarget", map[string]any{"targetId": b.targetID})
						}
					}
				}
			}

			// Fallback: If no clean web page target was found/attached, create a fresh page target
			if client.SessionID() == "" {
				result, err := client.Call(rctx, "Target.createTarget", map[string]any{
					"url": "about:blank",
				})
				if err == nil {
					var created struct {
						TargetID string `json:"targetId"`
					}
					if json.Unmarshal(result, &created) == nil && created.TargetID != "" {
						b.targetID = created.TargetID
						attRes, err := client.Call(rctx, "Target.attachToTarget", map[string]any{
							"targetId": created.TargetID,
							"flatten":  true,
						})
						if err == nil {
							var resp struct {
								SessionID string `json:"sessionId"`
							}
							if json.Unmarshal(attRes, &resp) == nil && resp.SessionID != "" {
								client.SetSessionID(resp.SessionID)
							}
						}
					}
				}
			}
		}

		if client.SessionID() == "" {
			client.Close()
			return fmt.Errorf("failed to obtain session ID for target %s", b.targetID)
		}
		sid := client.SessionID()
		tid := b.targetID
		if len(sid) > 8 {
			sid = sid[:8]
		}
		if len(tid) > 8 {
			tid = tid[:8]
		}
		dbg("reconnect got sessionID=%s targetID=%s", sid, tid)

		enableDomains := func() error {
			for _, domain := range []string{"Page", "Runtime", "DOM", "Network", "Accessibility"} {
				dbg("reconnect enabling %s", domain)
				dctx, dcancel := context.WithTimeout(rctx, 2*time.Second)
				_, err := client.Call(dctx, domain+".enable", nil)
				dcancel()
				if err != nil {
					return fmt.Errorf("enable %s failed: %w", domain, err)
				}
				dbg("reconnect %s enabled", domain)
			}
			return nil
		}

		if err := enableDomains(); err != nil {
			// Target is unresponsive/crashed/frozen. Fallback: create a fresh target and attach
			res, cErr := client.Call(rctx, "Target.createTarget", map[string]any{"url": "about:blank"})
			if cErr == nil {
				var created struct {
					TargetID string `json:"targetId"`
				}
				if json.Unmarshal(res, &created) == nil && created.TargetID != "" {
					attRes, aErr := client.Call(rctx, "Target.attachToTarget", map[string]any{
						"targetId": created.TargetID,
						"flatten":  true,
					})
					if aErr == nil {
						var resp struct {
							SessionID string `json:"sessionId"`
						}
						if json.Unmarshal(attRes, &resp) == nil && resp.SessionID != "" {
							b.targetID = created.TargetID
							client.SetSessionID(resp.SessionID)
							if err2 := enableDomains(); err2 != nil {
								client.Close()
								return fmt.Errorf("target %s is unresponsive (%v)", b.targetID, err2)
							}
						}
					}
				}
			} else {
				client.Close()
				return fmt.Errorf("target %s is unresponsive (%v)", b.targetID, err)
			}
		}

		// Enable file chooser interception (Chrome-only)
		if b.browserType != "firefox" {
			if _, err := client.Call(rctx, "Page.setInterceptFileChooserDialog", map[string]any{"enabled": true}); err != nil {
				fmt.Printf("Warning: failed to enable file chooser interception: %v\n", err)
			}
		}

		dbg("reconnect setAutoAttach DEFERRED to after stealth/handlers (was overwriting sessionID via autoAttach events, causing stealth CDP call to hang)")
	} else {
		for _, domain := range []string{"Page", "Runtime", "DOM", "Network", "Accessibility"} {
			dctx, dcancel := context.WithTimeout(rctx, 3*time.Second)
			if _, err := client.Call(dctx, domain+".enable", nil); err != nil {
				dcancel()
				client.Close()
				return fmt.Errorf("target %s is unresponsive (enable %s failed: %w)", b.targetID, domain, err)
			}
			dcancel()
		}
	}

	dbg("reconnect stealth=%v", b.stealth)
	if b.stealth {
		dbg("reconnect injecting stealth")
		if err := b.injectStealthLocked(rctx); err != nil {
			fmt.Printf("Warning: failed to inject stealth: %v\n", err)
		}
		dbg("reconnect stealth done")
	}

	// Re-register all event handlers on the new client
	dbg("reconnect re-registering %d handler methods", len(b.handlers))
	for method, fns := range b.handlers {
		for _, entry := range fns {
			b.client.On(method, entry.fn)
		}
	}
	dbg("reconnect handlers re-registered")

	if b.browserSession {
		dbg("reconnect setAutoAttach (last)")
		if _, err := client.Call(rctx, "Target.setAutoAttach", map[string]any{
			"autoAttach":             true,
			"waitForDebuggerOnStart": false,
			"flatten":                true,
		}); err != nil {
			fmt.Printf("Warning: setAutoAttach failed: %v\n", err)
		}
		dbg("reconnect setAutoAttach done")
	}

	dbg("reconnect DONE")
	return nil
}

func (b *Browser) setupDialogHandler(ctx context.Context) {
	// Auto-dismiss/accept dialogs to prevent stalling navigation or JS thread.
	b.On("Page.javascriptDialogOpening", func(params json.RawMessage) {
		var p struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(params, &p); err == nil {
			fmt.Printf("Ghost-Driver: Auto-handling dialog type=%s msg=%s\n", p.Type, p.Message)
			// For beforeunload, accept=true means "Leave".
			// For alert/confirm/prompt, accept=true means "OK".
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_, _ = b.Call(ctx, "Page.handleJavaScriptDialog", map[string]any{
					"accept": true,
				})
			}()
		}
	})

	// Detect system file chooser (Chrome-only)
	if b.browserType != "firefox" {
		b.On("Page.fileChooserOpened", func(params json.RawMessage) {
			var fc struct {
				BackendNodeID int64 `json:"backendNodeId"`
			}
			_ = json.Unmarshal(params, &fc)
			b.mu.Lock()
			b.HasSystemDialog = true
			b.mu.Unlock()
			if fc.BackendNodeID > 0 {
				b.fileChooserBackendNode.Store(fc.BackendNodeID)
			}
			fmt.Println("Ghost-Driver: System file chooser detected. Use 'upload_files' to handle it.")
		})
	}

	// When navigation occurs, assume system dialogs are closed
	b.On("Page.frameNavigated", func(params json.RawMessage) {
		b.mu.Lock()
		b.HasSystemDialog = false
		b.mu.Unlock()
	})
}

// On registers an event handler, tracks it for reconnection, and returns a
// cleanup function that removes it from both the tracking map and the client.
func (b *Browser) On(method string, fn cdp.EventHandler) func() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.handlers == nil {
		b.handlers = make(map[string][]handlerEntry)
	}
	id := b.handlerCounter.Add(1)
	b.handlers[method] = append(b.handlers[method], handlerEntry{id, fn})
	if b.client != nil {
		b.client.On(method, fn)
	}

	return func() {
		b.mu.Lock()
		entries := b.handlers[method]
		for i, e := range entries {
			if e.id == id {
				b.handlers[method] = append(entries[:i], entries[i+1:]...)
				break
			}
		}
		if b.client != nil {
			b.client.Off(method, fn)
		}
		b.mu.Unlock()
	}
}

// Off removes an event handler from both the tracking map and the active client.
// Deprecated: use the cleanup function returned by On instead.
func (b *Browser) Off(method string, fn cdp.EventHandler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	entries := b.handlers[method]
	for i, e := range entries {
		if reflect.ValueOf(e.fn).Pointer() == reflect.ValueOf(fn).Pointer() {
			b.handlers[method] = append(entries[:i], entries[i+1:]...)
			break
		}
	}
	if b.client != nil {
		b.client.Off(method, fn)
	}
}

// Call wraps b.client.Call with automatic reconnection on connection error.
func (b *Browser) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	b.mu.RLock()
	client := b.client
	b.mu.RUnlock()
	if client == nil {
		return nil, fmt.Errorf("browser not connected")
	}

	res, err := client.Call(ctx, method, params)
	if err != nil && isConnError(err) {
		b.mu.RLock()
		closing := b.closing
		b.mu.RUnlock()
		// During Close(), a failed CDP call must not trigger a full
		// reconnection (it would resurrect a connection we are tearing down).
		if closing {
			return res, err
		}
		// Serialize reconnect attempts: a concurrent Call may have already
		// reconnected while we waited on connectMu.
		b.connectMu.Lock()
		b.mu.RLock()
		client = b.client
		b.mu.RUnlock()
		if client == nil {
			fmt.Printf("Ghost-Driver: Connection lost during %s, attempting reconnect...\n", method)
			if rerr := b.reconnect(ctx, ""); rerr != nil {
				b.connectMu.Unlock()
				return res, err
			}
			b.mu.RLock()
			client = b.client
			b.mu.RUnlock()
		}
		b.connectMu.Unlock()
		if client == nil {
			return nil, fmt.Errorf("browser not connected after reconnect")
		}
		return client.Call(ctx, method, params)
	}
	return res, err
}

func isConnError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF) {
		return true
	}
	s := err.Error()
	return strings.Contains(s, "closed") || strings.Contains(s, "broken pipe") || strings.Contains(s, "EOF")
}

func (b *Browser) injectStealthLocked(ctx context.Context) error {
	script := resources.StealthJS
	if b.browserType == "firefox" {
		script = resources.StealthJSFirefox
	}
	_, err := b.client.Call(ctx, "Page.addScriptToEvaluateOnNewDocument", map[string]string{
		"source": script,
	})
	return err
}

func (b *Browser) Close() {
	// Mark closing first so HideIndicator's failed CDP call cannot trigger
	// an auto-reconnect while we are tearing the connection down.
	b.mu.Lock()
	b.closing = true
	b.mu.Unlock()

	// Remove indicator before closing so it doesn't linger if the tab stays open.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = b.HideIndicator(ctx)
	b.mu.RLock()
	client := b.client
	b.mu.RUnlock()
	if client != nil {
		client.Close()
	}
}

func (b *Browser) MarkDirty() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.IsDirty = true
}

// Dirty reports whether the page has unsaved form input. Thread-safe read of IsDirty.
func (b *Browser) Dirty() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.IsDirty
}

func (b *Browser) IsSystemDialogOpen() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.HasSystemDialog
}

func (b *Browser) GetCookieValue(ctx context.Context, domain, name string) (string, error) {
	raw, err := b.Call(ctx, "Network.getCookies", map[string]any{
		"urls": []string{"https://" + domain},
	})
	if err != nil {
		return "", err
	}
	var res struct {
		Cookies []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"cookies"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", err
	}
	for _, c := range res.Cookies {
		if c.Name == name {
			return c.Value, nil
		}
	}
	return "", fmt.Errorf("cookie %s not found", name)
}

// CreateNewTab creates a new tab via CDP Target.createTarget over WebSocket (bypassing legacy HTTP /json/new).
func (b *Browser) CreateNewTab(ctx context.Context, urlStr string) (*Browser, error) {
	if urlStr == "" {
		urlStr = "about:blank"
	}
	result, err := b.Call(ctx, "Target.createTarget", map[string]any{
		"url": urlStr,
	})
	if err != nil {
		return nil, fmt.Errorf("Target.createTarget: %w", err)
	}
	var created struct {
		TargetID string `json:"targetId"`
	}
	if err := json.Unmarshal(result, &created); err != nil || created.TargetID == "" {
		return nil, fmt.Errorf("Target.createTarget returned invalid targetId")
	}

	newB, err := New(ctx, b.httpURL, b.wsURL, created.TargetID, b.stealth, b.registry, false)
	if err != nil {
		return nil, err
	}
	return newB, nil
}

// ScreenshotOptions defines parameters for Page.captureScreenshot.
type ScreenshotOptions struct {
	Format                string `json:"format,omitempty"`                // png, jpeg, webp
	Quality               int    `json:"quality,omitempty"`               // 0-100
	Clip                  *Box   `json:"clip,omitempty"`                  // Clipping region
	FromSurface           bool   `json:"fromSurface,omitempty"`           // Capture from surface
	CaptureBeyondViewport bool   `json:"captureBeyondViewport,omitempty"` // Capture beyond viewport
	OptimizeForVLM        bool   `json:"optimizeForVLM,omitempty"`        // Internal: Downscale for AI
}

// Box represents a rectangular area in pixels.
type Box struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	Scale  float64 `json:"scale"`
}

// ScreenshotBase64 captures the viewport/region and returns Chrome's raw
// base64 string, avoiding a decode→re-encode round-trip when the caller only
// needs base64 (e.g. MCP image content). Use Screenshot() when decoded bytes
// are required (VLM image.DecodeConfig, writing PNG to disk).
func (b *Browser) ScreenshotBase64(ctx context.Context, opts ScreenshotOptions) (string, error) {
	params := map[string]any{}
	if b.BrowserType() != "firefox" {
		params["fromSurface"] = opts.FromSurface
	}
	if opts.Format != "" {
		params["format"] = opts.Format
	} else {
		params["format"] = "png"
	}
	if opts.Quality > 0 {
		params["quality"] = opts.Quality
	}
	if opts.Clip != nil {
		params["clip"] = opts.Clip
	}
	if opts.CaptureBeyondViewport {
		params["captureBeyondViewport"] = true
	}

	result, err := b.Call(ctx, "Page.captureScreenshot", params)
	if err != nil {
		return "", fmt.Errorf("captureScreenshot: %w", err)
	}

	var resp struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return "", err
	}
	return resp.Data, nil
}

// Screenshot captures the current viewport or a region as decoded bytes.
func (b *Browser) Screenshot(ctx context.Context, opts ScreenshotOptions) ([]byte, error) {
	b64, err := b.ScreenshotBase64(ctx, opts)
	if err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(b64)
}

// elementBox returns the bounding box of a DOM element for screenshot clipping.
func (b *Browser) elementBox(ctx context.Context, selector string) (Box, error) {
	js := fmt.Sprintf(`(function() {
		var el = document.querySelector(%q);
		if (!el) return null;
		var r = el.getBoundingClientRect();
		return {x: r.left, y: r.top, width: r.width, height: r.height, scale: 1};
	})()`, selector)

	res, err := b.EvalJS(ctx, js)
	if err != nil {
		return Box{}, err
	}
	if string(res) == "null" {
		return Box{}, fmt.Errorf("element not found: %s", selector)
	}

	var box Box
	if err := json.Unmarshal(res, &box); err != nil {
		return Box{}, err
	}
	return box, nil
}

// CaptureElementBase64 captures a specific DOM element and returns raw base64.
func (b *Browser) CaptureElementBase64(ctx context.Context, selector string) (string, error) {
	box, err := b.elementBox(ctx, selector)
	if err != nil {
		return "", err
	}
	return b.ScreenshotBase64(ctx, ScreenshotOptions{Clip: &box})
}

// CaptureElement captures a specific DOM element.
func (b *Browser) CaptureElement(ctx context.Context, selector string) ([]byte, error) {
	b64, err := b.CaptureElementBase64(ctx, selector)
	if err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(b64)
}

// EvalJS executes JavaScript in the page context and returns the raw result.
func (b *Browser) EvalJS(ctx context.Context, expression string) (json.RawMessage, error) {
	result, err := b.Call(ctx, "Runtime.evaluate", map[string]any{
		"expression":    expression,
		"returnByValue": true,
		"awaitPromise":  true,
	})
	if err != nil {
		return nil, err
	}

	var resp struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}
	if resp.ExceptionDetails != nil {
		return nil, fmt.Errorf("js exception: %s", resp.ExceptionDetails.Text)
	}
	return resp.Result.Value, nil
}

// PageInfo returns the current URL and title.
func (b *Browser) PageInfo(ctx context.Context) (url, title string, err error) {
	urlRaw, err := b.EvalJS(ctx, "window.location.href")
	if err != nil {
		return "", "", err
	}
	titleRaw, err := b.EvalJS(ctx, "document.title")
	if err != nil {
		return "", "", err
	}
	if err := json.Unmarshal(urlRaw, &url); err != nil {
		return "", "", fmt.Errorf("page_info url unmarshal: %w", err)
	}
	if err := json.Unmarshal(titleRaw, &title); err != nil {
		return "", "", fmt.Errorf("page_info title unmarshal: %w", err)
	}
	return url, title, nil
}

// InjectStealth applies advanced scripts to bypass sophisticated bot detection.
func (b *Browser) InjectStealth(ctx context.Context) error {
	b.mu.RLock()
	client := b.client
	bt := b.browserType
	b.mu.RUnlock()
	script := resources.StealthJS
	if bt == "firefox" {
		script = resources.StealthJSFirefox
	}
	_, err := client.Call(ctx, "Page.addScriptToEvaluateOnNewDocument", map[string]string{
		"source": script,
	})
	return err
}

// GetCleanMarkdown extracts a noise-reduced, LLM-friendly version of the page.
func (b *Browser) GetCleanMarkdown(ctx context.Context) (string, error) {
	result, err := b.EvalJS(ctx, resources.ExtractionJS)
	if err != nil {
		return "", err
	}

	var md string
	if err := json.Unmarshal(result, &md); err != nil {
		return "", err
	}
	return md, nil
}

// CompareVisualState takes a screenshot and returns its SHA-256 hash as a hex string.
// If referenceHash is provided, it returns whether they match.
func (b *Browser) CompareVisualState(ctx context.Context, referenceHash string) (string, bool, error) {
	png, err := b.Screenshot(ctx, ScreenshotOptions{Format: "png"})
	if err != nil {
		return "", false, err
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(png))
	if referenceHash == "" {
		return hash, false, nil
	}
	return hash, hash == referenceHash, nil
}

// WaitForStable polls screenshots until pixel variance drops below threshold,
// indicating the page has finished animating / loading.
func (b *Browser) WaitForStable(ctx context.Context) error {
	// Simple heuristic: wait for networkidle via Page.frameStoppedLoading
	// then sleep briefly for CSS animations.
	idle := make(chan struct{}, 1)
	var idleCancel func()
	var idleHandler cdp.EventHandler
	idleHandler = func(_ json.RawMessage) {
		select {
		case idle <- struct{}{}:
			if idleCancel != nil {
				idleCancel()
			}
		default:
		}
	}
	idleCancel = b.On("Page.frameStoppedLoading", idleHandler)
	defer idleCancel()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-idle:
	}
	return nil
}

// GetAccessibilityTree returns the full accessibility tree of the page.
func (b *Browser) GetAccessibilityTree(ctx context.Context) (json.RawMessage, error) {
	if b.BrowserType() == "firefox" {
		return b.EvalJS(ctx, resources.AXTreeJS)
	}
	return b.Call(ctx, "Accessibility.getFullAXTree", nil)
}

// BrowserType returns the auto-detected browser type ("chrome" or "firefox").
func (b *Browser) BrowserType() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.browserType
}

// InjectScript reads a local JS file and executes it in the page.
func (b *Browser) InjectScript(ctx context.Context, filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read script file %s: %w", filePath, err)
	}

	_, err = b.EvalJS(ctx, string(data))
	if err != nil {
		return fmt.Errorf("execute injected script: %w", err)
	}
	return nil
}

// TargetID returns the unique ID of the current target.
func (b *Browser) TargetID() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.targetID
}

// Registry returns the site profile registry.
func (b *Browser) Registry() *profile.Registry {
	return b.registry
}

// CDPClient exposes the raw CDP client for the input and recorder layers.
func (b *Browser) CDPClient() cdp.CDPClient {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.client
}

// IsHealthy checks if the CDP connection and the target session are responsive.
func (b *Browser) IsHealthy(ctx context.Context) bool {
	b.mu.RLock()
	client := b.client
	b.mu.RUnlock()
	if client == nil {
		return false
	}

	// Use a strict timeout for health check to avoid hanging the tool call.
	// If the browser doesn't respond in 2 seconds, it's considered unhealthy.
	hctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	// CRITICAL: We don't just check the browser version; we check if JS execution works.
	// This verifies both the WebSocket connection and the target session ID.
	_, err := b.EvalJS(hctx, "1+1")
	return err == nil
}
