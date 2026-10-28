package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/Daybeam/ghost-driver/internal/browser"
	"github.com/Daybeam/ghost-driver/internal/cdp"
)

func (s *State) registerBasicTools(srv *server.MCPServer) {
	srv.AddTool(mcpgo.NewTool("pixel_click",
		mcpgo.WithDescription("Raw click at screen coordinates (x,y). Use for L2 Vision fallback. Pass duration_ms > 0 for long click. Set is_image_pixel=true if passing raw screenshot pixel coordinates to auto-convert via devicePixelRatio."),
		mcpgo.WithNumber("x", mcpgo.Required()),
		mcpgo.WithNumber("y", mcpgo.Required()),
		mcpgo.WithNumber("duration_ms", mcpgo.Description("Duration in milliseconds. 0 = normal click (default), >0 = long click.")),
		mcpgo.WithBoolean("is_image_pixel", mcpgo.Description("If true, coordinates are in image/screen pixels and will be auto-scaled by window.devicePixelRatio.")),
	), s.auditWrapper("pixel_click", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, input, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}
		x, err := req.RequireFloat("x")
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		y, err := req.RequireFloat("y")
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}

		// Auto DPR Conversion logic
		isImagePixel := req.GetBool("is_image_pixel", false)
		dprJS := `({dpr: window.devicePixelRatio || 1, w: window.innerWidth, h: window.innerHeight})`
		if rawInfo, err := b.EvalJS(ctx, dprJS); err == nil {
			var info struct {
				DPR float64 `json:"dpr"`
				W   float64 `json:"w"`
				H   float64 `json:"h"`
			}
			if json.Unmarshal(rawInfo, &info) == nil && info.DPR > 1.0 {
				// Auto-detect if coordinates are out of CSS bounds or explicitly flagged as image pixels
				if isImagePixel || (info.W > 0 && x > info.W) || (info.H > 0 && y > info.H) {
					x = x / info.DPR
					y = y / info.DPR
				}
			}
		}

		durMS := req.GetFloat("duration_ms", 0)
		if durMS > 0 {
			duration := time.Duration(durMS) * time.Millisecond
			if err := input.LongClick(ctx, x, y, duration); err != nil {
				return mcpgo.NewToolResultError(err.Error()), nil
			}
			return mcpgo.NewToolResultText(fmt.Sprintf("pixel_long_clicked (%.0f, %.0f) for %v", x, y, duration)), nil
		}
		if err := input.Click(ctx, x, y); err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		return mcpgo.NewToolResultText(fmt.Sprintf("pixel_clicked (%.0f, %.0f)", x, y)), nil
	}))

	// ── get_cookies ───────────────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("get_cookies",
		mcpgo.WithDescription("Get all cookies for current page."),
	), s.auditWrapper("get_cookies", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}
		cookies, err := b.GetCookies(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		data, _ := json.Marshal(cookies)
		return mcpgo.NewToolResultText(string(data)), nil
	}))

	// ── set_cookie ───────────────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("set_cookie",
		mcpgo.WithDescription("Set a cookie."),
		mcpgo.WithString("name", mcpgo.Required()),
		mcpgo.WithString("value", mcpgo.Required()),
		mcpgo.WithString("domain", mcpgo.Required()),
		mcpgo.WithString("path"),
		mcpgo.WithString("httpOnly"),
		mcpgo.WithString("secure"),
	), s.auditWrapper("set_cookie", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}
		name, err := req.RequireString("name")
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		value, err := req.RequireString("value")
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		domain, err := req.RequireString("domain")
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		path := req.GetString("path", "/")
		httpOnly := req.GetString("httpOnly", "false") == "true"
		secure := req.GetString("secure", "false") == "true"

		if err := b.SetCookie(ctx, name, value, domain, path, httpOnly, secure); err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		return mcpgo.NewToolResultText(fmt.Sprintf("set cookie %s=%s", name, value)), nil
	}))

	// ── debug ────────────────────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("debug",
		mcpgo.WithDescription("Capture browser state for debugging."),
	), s.auditWrapper("debug", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}

		url, title, err := b.PageInfo(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}

		b64, err := b.ScreenshotBase64(ctx, browser.ScreenshotOptions{Format: "png"})
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}

		res := mcpgo.NewToolResultText(fmt.Sprintf("url=%s title=%s", url, title))
		res.Content = append(res.Content, mcpgo.ImageContent{
			Type:     "image",
			Data:     b64,
			MIMEType: "image/png",
		})
		return res, nil
	}))

	// ── connect_browser ───────────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("connect_browser",
		mcpgo.WithDescription(`Connect to Chrome via CDP.
If devtools_http is not provided, it defaults to http://localhost:9222.
If the browser is not running, it will attempt to start it automatically using detected paths.
DOCKER TIP: Just pass devtools_http='http://host.docker.internal:9222', the internal proxy will be mounted automatically.
STEALTH TIP: Use input_mode='l3' for sites with heavy bot protection (Vue/React apps, CSDN, Reddit, TradingView).
RECOMMENDED: Connect to an existing real Chrome profile on port 9222 to leverage existing session/cookies.
DESKTOP APPS: Set app_type='electron' or app_type='webview2' to automate Electron/Tauri/Wails desktop apps via CDP. Requires browser_path to the app executable. For WebView2 apps (Tauri/Wails on Windows), the debug port is enabled via the WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS env var. For already-running apps with a debug port, use mode='attached' (works for any CDP-speaking target).`),
		mcpgo.WithString("mode", mcpgo.Description("Managed (auto-start) or Attached (existing). Auto-detected if empty.")),
		mcpgo.WithString("devtools_http", mcpgo.Description("CDP endpoint, default http://localhost:9222")),
		mcpgo.WithString("browser_path"),
		mcpgo.WithString("app_type", mcpgo.Description("Target type: 'chrome' (default), 'electron', or 'webview2' (Tauri/Wails on Windows). Electron passes --remote-debugging-port through to embedded Chromium. WebView2 enables CDP via WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS env var.")),
		mcpgo.WithString("proxy"),
		mcpgo.WithString("user_data_dir"),
		mcpgo.WithString("stealth", mcpgo.Description("Set to 'true' to enable anti-bot evasions.")),
		mcpgo.WithString("headless", mcpgo.Description("Set to 'true' to run in background. Default 'false'.")),
		mcpgo.WithString("ws_url"),
		mcpgo.WithString("input_mode", mcpgo.Description("l1: DOM-level (fast), l2: CDP-level (default), l3: OS-level physical injection (REQUIRED for Vue/React click handlers and bypassing bot detection).")),
		mcpgo.WithString("ffmpeg_path"),
		mcpgo.WithString("cdp_host_map"),
		mcpgo.WithString("cdp_proxy_upstream", mcpgo.Description("host.docker.internal:9222")),
		mcpgo.WithNumber("cdp_proxy_port", mcpgo.Description("Local proxy port (default 9223)")),
		mcpgo.WithString("tab", mcpgo.Description("Set to 'new' to create a dedicated new tab instead of grabbing the first existing one. Use this when multiple agents share the same browser to avoid hijacking each other's active tab.")),
	), s.auditWrapper("connect_browser", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		// Serialize against ensureConnected/other connects. The heavy
		// mgr.Start() runs WITHOUT s.mu held — holding the global lock here
		// previously stalled every other tool call for the full start time.
		s.connectMu.Lock()
		defer s.connectMu.Unlock()

		devtoolsHTTP := req.GetString("devtools_http", "http://127.0.0.1:9222")

		// Idempotency: if already connected to the same endpoint and healthy,
		// skip teardown/reconnect. This prevents destroying another SSE client's
		// active connection (which would block its in-flight tool calls).
		s.mu.RLock()
		alreadyOK := s.lastCfg != nil &&
			s.lastCfg.DevToolsHTTP == devtoolsHTTP &&
			s.activeID != ""
		if alreadyOK {
			b := s.browsers[s.activeID]
			alreadyOK = b != nil && b.IsHealthy(ctx)
		}
		s.mu.RUnlock()
		if alreadyOK {
			return mcpgo.NewToolResultText("already connected to " + devtoolsHTTP), nil
		}

		s.mu.Lock()
		if hostMap := req.GetString("cdp_host_map", ""); hostMap != "" {
			cdp.DefaultHostMap = hostMap
		}

		if s.mgr != nil {
			s.mgr.Stop()
		}
		// Release stale connections before replacing the maps (previously leaked).
		for _, old := range s.browsers {
			if c := old.CDPClient(); c != nil {
				c.Close()
			}
		}
		s.browsers = make(map[string]*browser.Browser)
		s.inputs = make(map[string]*browser.Input)
		s.activeID = ""
		s.mu.Unlock()

		modeStr := req.GetString("mode", "")
		if modeStr == "" {
			// Auto-detection logic: if port is closed, default to Managed mode
			if !browser.CheckPortOpen(devtoolsHTTP) {
				modeStr = string(browser.ModeManaged)
			} else {
				modeStr = string(browser.ModeAttached)
			}
		}

		stealthStr := req.GetString("stealth", "")
		stealthEnabled := s.cfg.StealthEnabled
		if stealthStr == "false" {
			stealthEnabled = false
		} else if stealthStr == "true" {
			stealthEnabled = true
		}

		headlessStr := req.GetString("headless", "false")
		headless := (headlessStr == "true")

		browserPath := req.GetString("browser_path", "")
		proxyServer := req.GetString("proxy", "")
		userDataDir := req.GetString("user_data_dir", "")

		cfg := browser.Config{
			Mode:             browser.Mode(modeStr),
			DevToolsHTTP:     devtoolsHTTP,
			BrowserPath:      browserPath,
			ProxyServer:      proxyServer,
			UserDataDir:      userDataDir,
			StealthEnabled:   stealthEnabled,
			Headless:         headless,
			CDPProxyUpstream: req.GetString("cdp_proxy_upstream", ""),
			CDPProxyPort:     int(req.GetFloat("cdp_proxy_port", 0)),
			AppType:          req.GetString("app_type", ""),
			NewTab:           req.GetString("tab", "") == "new",
		}

		if ffmpeg := req.GetString("ffmpeg_path", ""); ffmpeg != "" {
			browser.FFmpegPath = ffmpeg
		}

		mgr := browser.NewManager(cfg, s.registry)

		s.mu.Lock()
		s.mgr = mgr
		s.lastCfg = &cfg
		s.mu.Unlock()

		b, err := mgr.Start(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}

		inputMode := browser.InputModeL2
		modeStrInput := req.GetString("input_mode", "")
		switch modeStrInput {
		case "l1":
			inputMode = browser.InputModeL1
		case "l3":
			inputMode = browser.InputModeL3
		}

		targetID := b.TargetID()
		if targetID == "" {
			targetID = "default"
		}
		s.mu.Lock()
		s.lastInputMode = inputMode
		s.browsers[targetID] = b
		s.inputs[targetID] = browser.NewInput(b, inputMode)
		s.activeID = targetID
		s.mu.Unlock()

		targets, _ := cdp.ListTargets(cfg.DevToolsHTTP)
		return mcpgo.NewToolResultText(fmt.Sprintf(
			"connected (auto_mode=%s, app_type=%s) — %d target(s), active_tab=%s, input_mode=%s", modeStr, cfg.AppType, len(targets), targetID, modeStrInput)), nil
	}))

	// ── check_system_ready ───────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("check_system_ready",
		mcpgo.WithDescription("Check if the system environment is ready for Ghost-Driver."),
	), s.auditWrapper("check_system_ready", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		status := make(map[string]any)

		// 1. Browser Detection
		browserPath := browser.DefaultBrowserPath()
		status["detected_browser_path"] = browserPath
		browserInstalled := false
		if _, err := os.Stat(browserPath); err == nil {
			browserInstalled = true
		}
		status["browser_installed"] = browserInstalled

		// 2. Port Check
		devtoolsHTTP := "http://localhost:9222"
		status["default_devtools_http"] = devtoolsHTTP
		portOpen := browser.CheckPortOpen(devtoolsHTTP)
		status["port_9222_open"] = portOpen

		// 3. Active MCP Connection Check (the real readiness gate)
		mcpHealthy := false
		b, _ := s.getActive()
		if b != nil {
			healthy := b.IsHealthy(ctx)
			status["mcp_connection_healthy"] = healthy
			if !healthy {
				status["mcp_connection_error"] = "WebSocket is open but browser is not responding to JS commands (zombie state)."
			}
			mcpHealthy = healthy
		} else if portOpen {
			status["mcp_connection_status"] = "Port open but Ghost-Driver not connected. Call connect_browser."
		} else {
			status["mcp_connection_status"] = "No browser detected on port 9222."
		}

		// 4. Synthesized readiness — only true when MCP layer can actually execute commands
		status["ready"] = mcpHealthy
		if !mcpHealthy {
			switch {
			case !browserInstalled:
				status["recommended_action"] = "Install Chrome/Chromium browser."
			case !portOpen && b == nil:
				status["recommended_action"] = "Call connect_browser to start a managed browser."
				var launchCmd string
				switch runtime.GOOS {
				case "windows":
					launchCmd = `start chrome.exe --remote-debugging-port=9222 --user-data-dir="C:\temp\ghost-profile"`
				case "darwin":
					launchCmd = `/Applications/Google\ Chrome.app/Contents/MacOS/Google\ Chrome --remote-debugging-port=9222 --user-data-dir="/tmp/ghost-profile"`
				default:
					launchCmd = `google-chrome --remote-debugging-port=9222 --user-data-dir="/tmp/ghost-profile"`
				}
				status["launch_command"] = launchCmd
			case portOpen && b == nil:
				status["recommended_action"] = "Call connect_browser to attach to the running browser on port 9222."
			case b != nil:
				status["recommended_action"] = "Connection is unhealthy. Call connect_browser to reconnect (auto-reconnect will also trigger on next tool call)."
			}
		}

		// 5. FFmpeg Check
		_, err := exec.LookPath("ffmpeg")
		status["ffmpeg_installed"] = (err == nil)

		// 6. OS Info
		status["os"] = runtime.GOOS
		status["arch"] = runtime.GOARCH

		// 7. Topology ROI counters
		if s.topology != nil {
			status["topology_stats"] = s.topology.Stats()
		}

		data, _ := json.MarshalIndent(status, "", "  ")
		return mcpgo.NewToolResultText(string(data)), nil
	}))
}
