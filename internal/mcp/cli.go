package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/Daybeam/ghost-driver/internal/browser"
	"github.com/Daybeam/ghost-driver/internal/cdp"
)

func (s *State) InvokeCLI(ctx context.Context, action string, args map[string]any) (string, error) {
	// Auto-connect if not connected
	if b, _ := s.getActive(); b == nil && action != "connect_browser" {
		if s.lastCfg != nil {
			_, _, err := s.ensureConnected(ctx)
			if err != nil {
				return "", err
			}
		} else {
			headless, _ := args["headless"].(bool)
			browserPath, _ := args["browser_path"].(string)
			devPort, _ := args["dev_port"].(int)
			if devPort == 0 {
				devPort = 9222
			}
			devToolsHTTP := fmt.Sprintf("http://127.0.0.1:%d", devPort)
			cfg := browser.Config{
				Mode:         browser.ModeAttached,
				DevToolsHTTP: devToolsHTTP,
				Headless:     headless,
			}
			// Optionally open a new tab instead of grabbing the first existing one
			if nt, ok := args["new_tab"].(bool); ok && nt {
				cfg.NewTab = true
			}

			// Optionally attach to a specific existing tab by URL substring.
			// Each stateless CLI call re-resolves the match independently, so
			// repeated calls keep targeting the same tab without state.
			if tu, ok := args["tab_url"].(string); ok && tu != "" {
				if targets, terr := cdp.ListTargets(devToolsHTTP); terr == nil {
					for _, t := range targets {
						if t.Type == "page" && t.WebSocketDebuggerURL != "" && strings.Contains(t.URL, tu) {
							if nb, nerr := browser.New(ctx, devToolsHTTP, t.WebSocketDebuggerURL, t.ID, cfg.StealthEnabled, s.registry, false); nerr == nil {
								s.mu.Lock()
								s.mgr = browser.NewManager(cfg, s.registry)
								s.browsers[t.ID] = nb
								s.inputs[t.ID] = browser.NewInput(nb, browser.InputModeL2)
								s.activeID = t.ID
								s.lastCfg = &cfg
								s.mu.Unlock()
								break
							}
						}
					}
				}
			}

			// Fallback: connect via the manager (grab first tab, or open a new one).
			if s.activeID == "" {
				// If port is closed, fallback to Managed mode
				if !browser.CheckPortOpen(cfg.DevToolsHTTP) {
					cfg.Mode = browser.ModeManaged
					if browserPath != "" {
						cfg.BrowserPath = browserPath
					} else {
						cfg.BrowserPath = browser.DefaultBrowserPath()
					}
				}

				s.mu.Lock()
				s.mgr = browser.NewManager(cfg, s.registry)
				s.lastCfg = &cfg
				s.mu.Unlock()
				b, err := s.mgr.Start(ctx)
				if err != nil {
					return "", fmt.Errorf("failed to auto-connect to browser: %w", err)
				}
				targetID := b.TargetID()
				if targetID == "" {
					targetID = "default"
				}
				s.mu.Lock()
				s.browsers[targetID] = b
				s.inputs[targetID] = browser.NewInput(b, browser.InputModeL2)
				s.activeID = targetID
				s.mu.Unlock()
			}
		}
	} else if action != "connect_browser" {
		// Even if connected, ensure it's healthy
		_, _, err := s.ensureConnected(ctx)
		if err != nil {
			return "", err
		}
	}

	b, input := s.getActive()

	switch action {
	case "navigate":
		urlStr, ok := args["url"].(string)
		if !ok {
			return "", fmt.Errorf("missing url")
		}
		if err := b.Navigate(ctx, urlStr); err != nil {
			return "", err
		}
		u, title, _ := b.PageInfo(ctx)
		return fmt.Sprintf("Navigated to %s (%s)", u, title), nil
	case "click":
		x, _ := args["x"].(float64)
		y, _ := args["y"].(float64)
		if err := input.Click(ctx, x, y); err != nil {
			return "", err
		}
		return fmt.Sprintf("Clicked at %.0f, %.0f", x, y), nil
	case "type":
		text, _ := args["text"].(string)
		if err := input.TypeText(ctx, text); err != nil {
			return "", err
		}
		return "Typed text", nil
	case "screenshot":
		out, ok := args["out"].(string)
		if ok {
			png, err := b.Screenshot(ctx, browser.ScreenshotOptions{Format: "png"})
			if err != nil {
				return "", err
			}
			if err := os.WriteFile(out, png, 0644); err != nil {
				return "", fmt.Errorf("failed to save screenshot: %w", err)
			}
			return "Screenshot saved to " + out, nil
		}
		b64, err := b.ScreenshotBase64(ctx, browser.ScreenshotOptions{Format: "png"})
		if err != nil {
			return "", err
		}
		return b64, nil
	case "get_text":
		text, err := b.GetAllText(ctx)
		if err != nil {
			return "", err
		}
		return text, nil
	case "get_content":
		format, _ := args["format"].(string)
		if format == "" {
			format = "markdown"
		}
		sel, _ := args["selector"].(string)
		switch format {
		case "text":
			if sel != "" {
				return b.InnerText(ctx, sel)
			}
			return b.GetAllText(ctx)
		case "html":
			return b.OuterHTML(ctx, sel)
		default:
			mode, _ := args["mode"].(string)
			return b.HTMLToMarkdown(ctx, sel, mode)
		}
	case "list_tabs":
		s.mu.RLock()
		devHTTP := s.mgr.DevToolsHTTP()
		s.mu.RUnlock()
		targets, err := cdp.ListTargets(devHTTP)
		if err != nil {
			return "", err
		}
		var lines []string
		for _, t := range targets {
			lines = append(lines, fmt.Sprintf("- %s: %s (%s)", t.ID, t.Title, t.URL))
		}
		return fmt.Sprintf("Tabs found (%d):\n%s", len(targets), joinLines(lines)), nil
	case "eval":
		js, _ := args["js"].(string)
		res, err := b.EvalJS(ctx, js)
		if err != nil {
			return "", err
		}
		return string(res), nil
	case "page_info":
		u, title, err := b.PageInfo(ctx)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("url=%s\ntitle=%s", u, title), nil
	case "get_markdown":
		selector, _ := args["selector"].(string)
		mode, _ := args["mode"].(string)
		md, err := b.HTMLToMarkdown(ctx, selector, mode)
		if err != nil {
			return "", err
		}
		return md, nil
	case "get_html":
		selector, _ := args["selector"].(string)
		html, err := b.OuterHTML(ctx, selector)
		if err != nil {
			return "", err
		}
		return html, nil
	case "smart_click":
		desc, ok := args["description"].(string)
		if !ok {
			return "", fmt.Errorf("missing description")
		}
		selRaw, _ := args["selectors"].(string)
		var strategies []browser.SelectorStrategy
		for _, sel := range splitComma(selRaw) {
			strategies = append(strategies, browser.SelectorStrategy{Name: "css", Selector: sel})
		}
		if err := b.SelfHealingClick(ctx, input, nil, desc, strategies); err != nil {
			return "", err
		}
		return fmt.Sprintf("smart_clicked: %s", desc), nil
	case "scroll":
		x, _ := args["x"].(float64)
		y, _ := args["y"].(float64)
		dx, _ := args["dx"].(float64)
		dy, _ := args["dy"].(float64)
		if err := input.Scroll(ctx, x, y, dx, dy); err != nil {
			return "", err
		}
		return fmt.Sprintf("scrolled (%.0f, %.0f) by (%.0f, %.0f)", x, y, dx, dy), nil
	case "navigate_back":
		if err := b.NavigateBack(ctx); err != nil {
			return "", err
		}
		return "navigated back", nil
	case "navigate_forward":
		if err := b.NavigateForward(ctx); err != nil {
			return "", err
		}
		return "navigated forward", nil
	case "reload":
		if err := b.Reload(ctx); err != nil {
			return "", err
		}
		return "reloaded", nil
	case "hover":
		x, _ := args["x"].(float64)
		y, _ := args["y"].(float64)
		if err := input.Hover(ctx, x, y); err != nil {
			return "", err
		}
		return fmt.Sprintf("hovered at %.0f, %.0f", x, y), nil
	case "wait_smart":
		if err := b.SmartWait(ctx, browser.DefaultWaitConfig()); err != nil {
			return "", err
		}
		return "page settled", nil
	case "upload_files":
		filesRaw, _ := args["files"].(string)
		if filesRaw == "" {
			return "", fmt.Errorf("missing files")
		}
		selector, _ := args["selector"].(string)
		if b == nil {
			return "", fmt.Errorf("no browser connected")
		}
		files := splitSemicolon(filesRaw)
		if err := b.UploadFiles(ctx, files, selector); err != nil {
			return "", err
		}
		return fmt.Sprintf("Provided %d file(s) to the file chooser.", len(files)), nil
	case "read_storage":
		scope, _ := args["scope"].(string)
		if scope == "" {
			scope = "local"
		}
		var items map[string]string
		var rErr error
		if scope == "session" {
			items, rErr = b.GetSessionStorage(ctx)
		} else {
			items, rErr = b.GetLocalStorage(ctx)
		}
		if rErr != nil {
			return "", rErr
		}
		data, _ := json.Marshal(items)
		return string(data), nil
	case "write_storage":
		scope, _ := args["scope"].(string)
		if scope == "" {
			scope = "local"
		}
		key, _ := args["key"].(string)
		val, _ := args["value"].(string)
		var wErr error
		if scope == "session" {
			wErr = b.SetSessionStorage(ctx, key, val)
		} else {
			wErr = b.SetLocalStorage(ctx, key, val)
		}
		if wErr != nil {
			return "", wErr
		}
		return fmt.Sprintf("set %sStorage[%s]=%s", scope, key, val), nil
	case "remove_storage":
		scope, _ := args["scope"].(string)
		if scope == "" {
			scope = "local"
		}
		key, _ := args["key"].(string)
		var dErr error
		if scope == "session" {
			dErr = b.RemoveSessionStorage(ctx, key)
		} else {
			dErr = b.RemoveLocalStorage(ctx, key)
		}
		if dErr != nil {
			return "", dErr
		}
		return fmt.Sprintf("removed %sStorage[%s]", scope, key), nil
	default:
		return "", fmt.Errorf("unknown action: %s", action)
	}
}
