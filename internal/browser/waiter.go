package browser

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Daybeam/ghost-driver/internal/resources"
)

// WaitConfig controls how SmartWait decides "stable enough".
type WaitConfig struct {
	// NetworkIdleTimeout: how long with no in-flight XHR/fetch before we declare network idle.
	NetworkIdleTimeout time.Duration
	// DOMQuietTimeout: how long with no MutationObserver mutations before DOM is quiet.
	DOMQuietTimeout time.Duration
	// VisualHashTimeout: if two consecutive screenshots have the same SHA-256, consider visually stable.
	VisualHashTimeout time.Duration
	// MaxWait: hard ceiling regardless of conditions.
	MaxWait time.Duration
}

func DefaultWaitConfig() WaitConfig {
	return WaitConfig{
		NetworkIdleTimeout: 500 * time.Millisecond,
		DOMQuietTimeout:    300 * time.Millisecond,
		VisualHashTimeout:  400 * time.Millisecond,
		MaxWait:            15 * time.Second,
	}
}

// SmartWait blocks until the page is considered stable for SPA interactions.
// It runs three checks in parallel and requires ALL to pass:
//  1. Network idle (no in-flight XHR/fetch for NetworkIdleTimeout)
//  2. DOM quiet (no MutationObserver events for DOMQuietTimeout)
//  3. Visual stable (two identical screenshot hashes DOMQuietTimeout apart)
//
// This is the correct approach for SPAs like TradingView where a navigation
// event doesn't fire a Page.loadEventFired but the DOM still changes heavily.
func (b *Browser) SmartWait(ctx context.Context, cfg WaitConfig) error {
	tctx, cancel := context.WithTimeout(ctx, cfg.MaxWait)
	defer cancel()

	// Inject monitoring harness into the page.
	if err := b.injectWaitHarness(tctx); err != nil {
		// Non-fatal — fall through to visual-only wait.
		return b.visualStableWait(tctx, cfg.VisualHashTimeout)
	}

	deadline := time.Now().Add(cfg.MaxWait)
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-tctx.Done():
			return nil // best-effort, don't fail the caller
		case <-ticker.C:
			if time.Now().After(deadline) {
				return nil
			}
			ok, err := b.checkWaitConditions(tctx, cfg)
			if err != nil {
				return nil // best-effort
			}
			if ok {
				return nil
			}
		}
	}
}

func (b *Browser) FlushPopupLog(ctx context.Context) ([]string, error) {
	raw, err := b.EvalJS(ctx, `
(function(){
  var w = window.__ghostWait;
  if (!w || !w.popupLog) return [];
  var logs = w.popupLog.map(l => l.text);
  w.popupLog = []; // 清空缓冲区
  return logs;
})()`)
	if err != nil {
		return nil, err
	}
	var logs []string
	if err := json.Unmarshal(raw, &logs); err != nil {
		return nil, err
	}
	return logs, nil
}

func (b *Browser) injectWaitHarness(ctx context.Context) error {
	_, err := b.EvalJS(ctx, resources.WaitHarnessJS)
	return err
}

func (b *Browser) checkWaitConditions(ctx context.Context, cfg WaitConfig) (bool, error) {
	raw, err := b.EvalJS(ctx, `
(function(){
  var w = window.__ghostWait;
  if (!w) return {ready: false, reason: "no harness"};
  var now = Date.now();
  var domQuiet   = (now - w.lastMutation)    > `+fmt.Sprintf("%d", cfg.DOMQuietTimeout.Milliseconds())+`;
  var netIdle    = w.inFlight === 0 && (w.lastNetworkEnd === 0 || (now - w.lastNetworkEnd) > `+fmt.Sprintf("%d", cfg.NetworkIdleTimeout.Milliseconds())+`);
  return {ready: domQuiet && netIdle, inFlight: w.inFlight,
          domAge: now - w.lastMutation, netAge: now - w.lastNetworkEnd};
})()`)
	if err != nil {
		return false, err
	}
	var result struct {
		Ready bool `json:"ready"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return false, err
	}
	return result.Ready, nil
}

// visualStableWait takes two screenshots separated by interval and compares their hashes.
func (b *Browser) visualStableWait(ctx context.Context, interval time.Duration) error {
	png1, err := b.Screenshot(ctx, ScreenshotOptions{Format: "png"})
	if err != nil {
		return nil // best-effort
	}
	h1 := sha256.Sum256(png1)

	select {
	case <-ctx.Done():
		return nil
	case <-time.After(interval):
	}

	png2, err := b.Screenshot(ctx, ScreenshotOptions{Format: "png"})
	if err != nil {
		return nil
	}
	h2 := sha256.Sum256(png2)

	if h1 == h2 {
		return nil // visually identical
	}
	// Still changing — wait one more round.
	select {
	case <-ctx.Done():
	case <-time.After(interval):
	}
	return nil
}

// WaitForSelector polls until an element matching selector appears in the DOM
// (or in any nested Shadow DOM). Useful after SPA route changes.
func (b *Browser) WaitForSelector(ctx context.Context, selector string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	js := fmt.Sprintf(`
(function(){
  function deepQuerySelector(sel) {
    function find(node) {
      if (node.querySelector) {
        const res = node.querySelector(sel);
        if (res) return res;
      }
      const all = node.querySelectorAll ? node.querySelectorAll('*') : [];
      for (const el of all) {
        if (el.shadowRoot) {
          const res = find(el.shadowRoot);
          if (res) return res;
        }
      }
      return null;
    }
    return find(document);
  }
  return !!deepQuerySelector(%q);
})()`, selector)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("timeout waiting for selector %q", selector)
			}
			raw, err := b.EvalJS(ctx, js)
			if err != nil {
				continue
			}
			var found bool
			if err := json.Unmarshal(raw, &found); err != nil {
				continue
			}
			if found {
				return nil
			}
		}
	}
}

// WaitForNavigation watches for URL changes (SPA client-side routing).
// Returns when the URL differs from the one at call time.
func (b *Browser) WaitForNavigation(ctx context.Context, timeout time.Duration) error {
	currentURL, _, err := b.PageInfo(ctx)
	if err != nil {
		return err
	}

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return nil // not an error — URL just didn't change
			}
			newURL, _, _ := b.PageInfo(ctx)
			if newURL != currentURL {
				// Brief settle time for the new route to render.
				select {
				case <-ctx.Done():
				case <-time.After(200 * time.Millisecond):
				}
				return nil
			}
		}
	}
}

// WaitForText polls until text appears anywhere in the document body.
func (b *Browser) WaitForText(ctx context.Context, text string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()

	js := fmt.Sprintf(`document.body.innerText.includes(%q)`, text)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("timeout waiting for text %q", text)
			}
			raw, _ := b.EvalJS(ctx, js)
			var ok bool
			if err := json.Unmarshal(raw, &ok); err != nil {
				continue
			}
			if ok {
				return nil
			}
		}
	}
}
