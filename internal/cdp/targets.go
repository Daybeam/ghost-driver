package cdp

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Target represents a Chrome debugging target (tab, worker, etc.)
type Target struct {
	ID                   string `json:"id"`
	Type                 string `json:"type"`
	Title                string `json:"title"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

// ListTargets queries the /json/list endpoint and returns all targets.
// devtoolsHTTP is the HTTP base, e.g. "http://localhost:9222".
func ListTargets(devtoolsHTTP string) ([]Target, error) {
	tr := &http.Transport{
		Proxy: func(req *http.Request) (*url.URL, error) {
			return nil, nil
		},
	}
	client := &http.Client{Transport: tr}
	req, err := http.NewRequest("GET", devtoolsHTTP+"/json/list", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	// Bypass Chrome's Host header validation.
	req.Host = "localhost"
	if u, err := url.Parse(devtoolsHTTP); err == nil {
		if strings.Contains(u.Host, ":") {
			_, port, _ := net.SplitHostPort(u.Host)
			if port != "" {
				req.Host = "localhost:" + port
			}
		}
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/148.0.0.0 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list targets: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	var targets []Target
	if err := json.Unmarshal(body, &targets); err != nil {
		return nil, fmt.Errorf("decode targets: %w", err)
	}
	return targets, nil
}

// FirstPageTarget returns the ID and WebSocket URL of the first valid "page" target,
// preferring normal web pages over internal chrome:// or extension pages.
func FirstPageTarget(devtoolsHTTP string) (string, string, error) {
	targets, err := ListTargets(devtoolsHTTP)
	if err != nil {
		return "", "", err
	}
	// 1. Prefer normal web pages
	for _, t := range targets {
		if t.Type == "page" && t.WebSocketDebuggerURL != "" {
			isInternal := strings.HasPrefix(t.URL, "chrome://") ||
				strings.HasPrefix(t.URL, "chrome-untrusted://") ||
				strings.HasPrefix(t.URL, "chrome-extension://") ||
				strings.HasPrefix(t.URL, "devtools://")
			if !isInternal {
				return t.ID, t.WebSocketDebuggerURL, nil
			}
		}
	}
	// 2. Fallback to any page target
	for _, t := range targets {
		if t.Type == "page" && t.WebSocketDebuggerURL != "" {
			return t.ID, t.WebSocketDebuggerURL, nil
		}
	}
	return "", "", fmt.Errorf("no page target found at %s", devtoolsHTTP)
}

// BrowserWebSocketURL returns the browser-level WebSocket URL from /json/version.
func BrowserWebSocketURL(devtoolsHTTP string) (string, error) {
	tr := &http.Transport{
		Proxy: func(req *http.Request) (*url.URL, error) {
			return nil, nil
		},
	}
	client := &http.Client{Transport: tr}
	req, err := http.NewRequest("GET", devtoolsHTTP+"/json/version", nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	// Bypass Chrome's Host header validation.
	req.Host = "localhost"
	if u, err := url.Parse(devtoolsHTTP); err == nil {
		if strings.Contains(u.Host, ":") {
			_, port, _ := net.SplitHostPort(u.Host)
			if port != "" {
				req.Host = "localhost:" + port
			}
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("get version: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}

	var version struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.Unmarshal(body, &version); err != nil {
		return "", fmt.Errorf("decode version: %w", err)
	}
	if version.WebSocketDebuggerURL == "" {
		return "", fmt.Errorf("no browser websocket URL found at %s", devtoolsHTTP)
	}
	return version.WebSocketDebuggerURL, nil
}

// DetectBrowserType fetches /json/version and parses the "Browser" field
// to auto-detect "firefox" vs "chrome". Returns "" on error.
func DetectBrowserType(devtoolsHTTP string) string {
	tr := &http.Transport{
		Proxy: func(req *http.Request) (*url.URL, error) { return nil, nil },
	}
	client := &http.Client{Transport: tr, Timeout: 3 * time.Second}
	req, err := http.NewRequest("GET", devtoolsHTTP+"/json/version", nil)
	if err != nil {
		return ""
	}
	req.Host = "localhost"
	if u, err := url.Parse(devtoolsHTTP); err == nil {
		if strings.Contains(u.Host, ":") {
			_, port, _ := net.SplitHostPort(u.Host)
			if port != "" {
				req.Host = "localhost:" + port
			}
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	// Firefox BiDi: /json/version returns 404 (no CDP endpoint).
	// Chrome always serves /json/version. So 404 => Firefox/BiDi.
	if resp.StatusCode == 404 {
		return "firefox"
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}
	var version struct {
		Browser string `json:"Browser"`
	}
	if json.Unmarshal(body, &version) != nil {
		return ""
	}
	if strings.HasPrefix(version.Browser, "firefox") || strings.HasPrefix(version.Browser, "Zen") {
		return "firefox"
	}
	return "chrome"
}
