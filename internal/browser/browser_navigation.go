package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Daybeam/ghost-driver/internal/cdp"
)

// Navigate goes to a URL and waits for DOMContentLoaded / Page.loadEventFired with a safety timeout.
func (b *Browser) Navigate(ctx context.Context, url string) error {
	// Escape chrome:// internal page sandbox if currently attached to an internal page target
	currentURL, _, _ := b.PageInfo(ctx)
	if strings.HasPrefix(currentURL, "chrome://") || strings.HasPrefix(currentURL, "chrome-untrusted://") {
		// Navigation on internal chrome:// pages is blocked by Chromium CDP security policy.
		// Escape by creating/attaching to a clean target via Target.createTarget.
		b.mu.RLock()
		client := b.client
		b.mu.RUnlock()
		if client != nil {
			res, err := client.Call(ctx, "Target.createTarget", map[string]any{"url": url})
			if err == nil {
				var created struct {
					TargetID string `json:"targetId"`
				}
				if json.Unmarshal(res, &created) == nil && created.TargetID != "" {
					attRes, err := client.Call(ctx, "Target.attachToTarget", map[string]any{
						"targetId": created.TargetID,
						"flatten":  true,
					})
					if err == nil {
						var resp struct {
							SessionID string `json:"sessionId"`
						}
						if json.Unmarshal(attRes, &resp) == nil && resp.SessionID != "" {
							b.mu.Lock()
							b.targetID = created.TargetID
							b.mu.Unlock()
							client.SetSessionID(resp.SessionID)
							for _, domain := range []string{"Page", "Runtime", "DOM", "Network", "Accessibility"} {
								if _, err := client.Call(ctx, domain+".enable", nil); err != nil {
									dbg("enable %s: %v", domain, err)
								}
							}
							return nil
						}
					}
				}
			}
		}
	}

	// Rule 1 Prevention: Force clear beforeunload handlers to prevent dialogs bricking CDP.
	if _, err := b.EvalJS(ctx, `window.onbeforeunload = null; window.addEventListener('beforeunload', (e) => { delete e['returnValue']; }, true);`); err != nil {
		dbg("clear beforeunload: %v", err)
	}

	b.mu.Lock()
	b.IsDirty = false // Reset dirty state on hard navigation
	b.mu.Unlock()

	domLoaded := make(chan struct{}, 1)
	pageLoaded := make(chan struct{}, 1)

	var domCancel func()
	var domHandler cdp.EventHandler
	domHandler = func(_ json.RawMessage) {
		select {
		case domLoaded <- struct{}{}:
			if domCancel != nil {
				domCancel()
			}
		default:
		}
	}

	var loadCancel func()
	var loadHandler cdp.EventHandler
	loadHandler = func(_ json.RawMessage) {
		select {
		case pageLoaded <- struct{}{}:
			if loadCancel != nil {
				loadCancel()
			}
		default:
		}
	}

	domCancel = b.On("Page.domContentEventFired", domHandler)
	defer domCancel()

	loadCancel = b.On("Page.loadEventFired", loadHandler)
	defer loadCancel()

	navCtx, navCancel := context.WithTimeout(ctx, 12*time.Second)
	defer navCancel()

	_, err := b.Call(navCtx, "Page.navigate", map[string]string{"url": url})
	if err != nil {
		select {
		case <-domLoaded:
		case <-pageLoaded:
		default:
			return fmt.Errorf("navigate: %w", err)
		}
	}

	select {
	case <-pageLoaded:
		// Full page load completed
	case <-domLoaded:
		// DOM Content loaded (SPA structure or main content ready). Give up to 1.5s for loadEventFired before proceeding.
		select {
		case <-pageLoaded:
		case <-time.After(1500 * time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
	case <-navCtx.Done():
		// Safety timeout hit (12s) or parent context done
		if ctx.Err() != nil {
			return ctx.Err()
		}
		currentURL, _, _ := b.PageInfo(ctx)
		if currentURL == "" || currentURL == "about:blank" {
			return fmt.Errorf("navigate: timeout waiting for page load (12s)")
		}
	}

	// Auto-inject scripts from profile if matched
	currentURL, title, _ := b.PageInfo(ctx)
	if prof := b.registry.Match(currentURL); prof != nil {
		for _, scriptPath := range prof.PreloadScripts {
			_ = b.InjectScript(ctx, scriptPath)
		}
	}
	if ban := detectBan(title); ban != "" {
		return fmt.Errorf("ban signal: page title %q matches %q — back off and retry later", title, ban)
	}

	return nil
}

// detectBan checks the page title for known anti-bot block patterns.
func detectBan(title string) string {
	lower := strings.ToLower(title)
	for _, p := range []string{"access denied", "just a moment", "attention required", "403 forbidden", "429 too many"} {
		if strings.Contains(lower, p) {
			return p
		}
	}
	return ""
}

// NavigateBack goes back in history.
func (b *Browser) NavigateBack(ctx context.Context) error {
	_, err := b.Call(ctx, "Page.goBack", nil)
	return err
}

// NavigateForward goes forward in history.
func (b *Browser) NavigateForward(ctx context.Context) error {
	_, err := b.Call(ctx, "Page.goForward", nil)
	return err
}

// Reload refreshes the current page.
func (b *Browser) Reload(ctx context.Context) error {
	_, err := b.Call(ctx, "Page.reload", nil)
	return err
}
