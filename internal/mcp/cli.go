package mcp

import (
	"context"
	"fmt"
	"os"

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
			// Try to connect to existing browser on the port
			cfg := browser.Config{
				Mode:         browser.ModeAttached,
				DevToolsHTTP: devToolsHTTP,
				Headless:     headless,
			}
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
	default:
		return "", fmt.Errorf("unknown action: %s", action)
	}
}
