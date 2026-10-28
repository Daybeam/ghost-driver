package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// SelectorStrategy describes a single attempt in the healing chain.
type SelectorStrategy struct {
	Name     string
	Selector string
}

// SelfHealingClick tries each strategy in order until one works, then clicks
// the first match. This is the correct approach for React/Vue apps where
// class names are hashed and change every build.
//
// Precedence:
//  1. Caller-supplied stable selectors (data-testid, aria-label, etc.)
//  2. DOM semantic search (FindElement — no VLM)
//  3. VLM visual search (screenshot → Claude Vision → coordinates)
func (b *Browser) SelfHealingClick(ctx context.Context, inp *Input, finder interface {
	Locate(context.Context, []byte, string) (interface{ GetXY() (float64, float64) }, error)
}, description string, strategies []SelectorStrategy) error {

	// ── Layer 1: explicit CSS selectors ──────────────────────────────────────
	for _, s := range strategies {
		result, err := b.clickBySelector(ctx, inp, s.Selector)
		if err == nil && result {
			return nil
		}
	}

	// ── Layer 2: semantic DOM (no VLM, very fast) ─────────────────────────────
	domResult, err := b.FindElement(ctx, description)
	if err == nil && domResult.Found {
		return inp.Click(ctx, domResult.X, domResult.Y)
	}

	// ── Layer 3: VLM (screenshot + Claude Vision) ────────────────────────────
	// Only reaches here if both CSS and DOM semantic failed.
	// Implemented in mcp/server.go to avoid circular imports.
	return fmt.Errorf("element %q not found by any strategy", description)
}

// clickBySelector attempts to click the element at selector.
// Returns (true, nil) on success, (false, nil) if not found, (false, err) on error.
func (b *Browser) clickBySelector(ctx context.Context, inp *Input, selector string) (bool, error) {
	js := fmt.Sprintf(`
(function(){
  var el = document.querySelector(%q);
  if (!el) {
    // Try Shadow DOM
    var hosts = document.querySelectorAll('*');
    for (var i=0; i<hosts.length; i++) {
      if (hosts[i].shadowRoot) {
        el = hosts[i].shadowRoot.querySelector(%q);
        if (el) break;
      }
    }
  }
  if (!el) return null;
  var r = el.getBoundingClientRect();
  // Scroll into view if needed
  if (r.top < 0 || r.bottom > window.innerHeight) {
    el.scrollIntoView({block:'center', behavior:'instant'});
    r = el.getBoundingClientRect();
  }
  return {x: r.left + r.width/2, y: r.top + r.height/2};
})()`, selector, selector)

	raw, err := b.EvalJS(ctx, js)
	if err != nil {
		return false, err
	}

	if string(raw) == "null" {
		return false, nil
	}

	var coord struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	}
	if err := json.Unmarshal(raw, &coord); err != nil {
		return false, err
	}

	return true, inp.Click(ctx, coord.X, coord.Y)
}

// ─── Common stable selectors for known frameworks ────────────────────────────

// TradingViewSelectors returns common TradingView selectors.
// TV uses obfuscated class names but has stable data-name/aria-label attributes.
func TradingViewSelectors(element string) []SelectorStrategy {
	selectors := map[string][]SelectorStrategy{
		"symbol_search": {
			{Name: "data-name", Selector: `[data-name="symbol-search-items-dialog"]`},
			{Name: "header-input", Selector: `#header-toolbar-symbol-search`},
			{Name: "aria", Selector: `[aria-label="Symbol Search"]`},
		},
		"chart_container": {
			{Name: "data-name", Selector: `[data-name="legend"]`},
			{Name: "canvas", Selector: `canvas.chart-markup-table`},
		},
		"timeframe_selector": {
			{Name: "data-name", Selector: `[data-name="resolution-periods"]`},
		},
		"drawing_toolbar": {
			{Name: "data-name", Selector: `[data-name="drawing-toolbar"]`},
		},
	}
	if s, ok := selectors[element]; ok {
		return s
	}
	return nil
}

// WaitForSPARoute waits for a React/Vue/Next.js client-side route to complete.
// It combines URL change detection with DOM settle.
func (b *Browser) WaitForSPARoute(ctx context.Context, expectedURLContains string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				// Don't fail — the URL check is best-effort.
				return b.SmartWait(ctx, DefaultWaitConfig())
			}
			url, _, _ := b.PageInfo(ctx)
			if expectedURLContains == "" || strings.Contains(url, expectedURLContains) {
				// URL matched — now wait for DOM to settle.
				return b.SmartWait(ctx, DefaultWaitConfig())
			}
		}
	}
}
