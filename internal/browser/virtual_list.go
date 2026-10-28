package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// VirtualListConfig controls extraction behaviour.
type VirtualListConfig struct {
	// ContainerSelector: the scrollable container.
	// If empty, defaults to the first element with overflow:auto/scroll.
	ContainerSelector string
	// ItemSelector: CSS selector for individual items within the container.
	ItemSelector string
	// MaxItems: stop after collecting this many unique items (0 = unlimited).
	MaxItems int
	// ScrollStep: pixels to scroll per step (default 400).
	ScrollStep float64
	// SettleDelay: time to wait after each scroll for the renderer to populate.
	SettleDelay time.Duration
	// StopIfNoNew: stop scrolling when this many consecutive scrolls yield 0 new items.
	StopIfNoNew int
}

func DefaultVirtualListConfig(containerSel, itemSel string) VirtualListConfig {
	return VirtualListConfig{
		ContainerSelector: containerSel,
		ItemSelector:      itemSel,
		MaxItems:          0,
		ScrollStep:        400,
		SettleDelay:       400 * time.Millisecond,
		StopIfNoNew:       3,
	}
}

// VirtualListItem is one extracted row.
type VirtualListItem struct {
	Index int               `json:"index"`
	Text  string            `json:"text"`
	HTML  string            `json:"html"`
	Attrs map[string]string `json:"attrs"`
}

// ExtractVirtualList scrolls a virtual/windowed list, collecting each batch
// of rendered items until:
//   - MaxItems is reached
//   - StopIfNoNew consecutive scrolls yield nothing new
//   - bottom of container is reached
//
// Deduplication is key-based: items are keyed on their innerText fingerprint
// so re-rendered items don't produce duplicates.
//
// This covers React-Window, TanStack Virtual, AG-Grid virtual rows,
// TradingView's pine-editor symbol list, etc.
func (b *Browser) ExtractVirtualList(ctx context.Context, cfg VirtualListConfig) ([]VirtualListItem, error) {
	if cfg.ScrollStep <= 0 {
		cfg.ScrollStep = 400
	}
	if cfg.SettleDelay <= 0 {
		cfg.SettleDelay = 400 * time.Millisecond
	}
	if cfg.StopIfNoNew <= 0 {
		cfg.StopIfNoNew = 3
	}

	// Auto-detect container if not specified.
	container := cfg.ContainerSelector
	if container == "" {
		detected, err := b.detectScrollContainer(ctx)
		if err != nil || detected == "" {
			container = "body"
		} else {
			container = detected
		}
	}

	seen := make(map[string]bool)
	var results []VirtualListItem
	noNewCount := 0
	scrollPos := 0.0
	maxIterations := 100
	if cfg.MaxItems > 0 {
		maxIterations = 1000
	}

	for iter := 0; iter < maxIterations; iter++ {
		if ctx.Err() != nil {
			return results, ctx.Err()
		}

		// Collect currently visible items.
		batch, err := b.collectVisibleItems(ctx, container, cfg.ItemSelector)
		if err != nil {
			return results, err
		}

		newInBatch := 0
		for _, item := range batch {
			key := fingerprintItem(item)
			if seen[key] {
				continue
			}
			seen[key] = true
			item.Index = len(results)
			results = append(results, item)
			newInBatch++

			if cfg.MaxItems > 0 && len(results) >= cfg.MaxItems {
				return results, nil
			}
		}

		if newInBatch == 0 {
			noNewCount++
		} else {
			noNewCount = 0
		}

		if noNewCount >= cfg.StopIfNoNew {
			break
		}

		// Check if we've reached the bottom.
		atBottom, err := b.isScrolledToBottom(ctx, container)
		if err == nil && atBottom {
			break
		}

		// Scroll down.
		scrollPos += cfg.ScrollStep
		err = b.scrollContainerTo(ctx, container, scrollPos)
		if err != nil {
			break
		}

		// Wait for virtual renderer to populate new items.
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		case <-time.After(cfg.SettleDelay):
		}
	}

	return results, nil
}

// collectVisibleItems reads all currently rendered items matching itemSelector
// inside the container. Returns HTML + text + key attributes.
func (b *Browser) collectVisibleItems(ctx context.Context, containerSel, itemSel string) ([]VirtualListItem, error) {
	if itemSel == "" {
		// Generic heuristic: rows, list items, or data cells.
		itemSel = "[role='row'], [role='listitem'], li, tr, .row, .item, .list-item"
	}

	js := fmt.Sprintf(`
(function(){
  var container = document.querySelector(%q);
  if (!container) return [];
  var items = Array.from(container.querySelectorAll(%q));
  return items.map(function(el){
    var attrs = {};
    Array.from(el.attributes).forEach(function(a){ attrs[a.name] = a.value; });
    return {
      text: (el.innerText || el.textContent || '').trim().substring(0,500),
      html: el.innerHTML.substring(0,2000),
      attrs: attrs
    };
  });
})()`, containerSel, itemSel)

	raw, err := b.EvalJS(ctx, js)
	if err != nil {
		return nil, err
	}

	var items []VirtualListItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func (b *Browser) scrollContainerTo(ctx context.Context, containerSel string, y float64) error {
	js := fmt.Sprintf(`
(function(){
  var el = document.querySelector(%q);
  if (!el) { window.scrollTo(0, %f); return; }
  el.scrollTop = %f;
})()`, containerSel, y, y)
	_, err := b.EvalJS(ctx, js)
	return err
}

func (b *Browser) isScrolledToBottom(ctx context.Context, containerSel string) (bool, error) {
	js := fmt.Sprintf(`
(function(){
  var el = document.querySelector(%q);
  if (!el) return (window.innerHeight + window.scrollY) >= document.body.scrollHeight - 10;
  return el.scrollTop + el.clientHeight >= el.scrollHeight - 10;
})()`, containerSel)

	raw, err := b.EvalJS(ctx, js)
	if err != nil {
		return false, err
	}
	var ok bool
	if err := json.Unmarshal(raw, &ok); err != nil {
		return false, err
	}
	return ok, nil
}

// detectScrollContainer finds the most prominent scrollable container.
func (b *Browser) detectScrollContainer(ctx context.Context) (string, error) {
	raw, err := b.EvalJS(ctx, `
(function(){
  var best = null, bestArea = 0;
  document.querySelectorAll('*').forEach(function(el){
    var s = window.getComputedStyle(el);
    if (s.overflow === 'auto' || s.overflow === 'scroll' ||
        s.overflowY === 'auto' || s.overflowY === 'scroll') {
      var r = el.getBoundingClientRect();
      var area = r.width * r.height;
      if (area > bestArea && el.scrollHeight > el.clientHeight) {
        bestArea = area;
        best = el;
      }
    }
  });
  if (!best) return '';
  // Build a selector from id or class
  if (best.id) return '#' + best.id;
  if (best.className) return '.' + best.className.trim().split(/\s+/)[0];
  return best.tagName.toLowerCase();
})()`)
	if err != nil {
		return "", err
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", err
	}
	return s, nil
}

func fingerprintItem(item VirtualListItem) string {
	// Use first 100 chars of text as dedup key — robust to attribute churn.
	t := strings.TrimSpace(item.Text)
	if len(t) > 100 {
		t = t[:100]
	}
	return t
}

// ─── TradingView-specific helpers ─────────────────────────────────────────────

// TVChartData extracts OHLCV data from a TradingView chart by reading the
// internal series data attached to the chart widget — no screenshot needed.
//
// This works because TradingView exposes chart state on window._tvWidget or
// similar globals. The JS path is fragile across versions; treat as best-effort.
func (b *Browser) TVChartData(ctx context.Context) ([]map[string]any, error) {
	raw, err := b.EvalJS(ctx, `
(function(){
  // TradingView attaches chart data to various globals depending on version.
  // Try the most common paths.
  var sources = [
    window.TradingView,
    window._tvWidget,
    window.tvWidget,
  ];
  for (var src of sources) {
    if (!src) continue;
    try {
      var chart = src.chart ? src.chart() : src;
      var series = chart.getAllStudies ? chart.getAllStudies() : null;
      if (series) return series;
    } catch(e) {}
  }
  // Last resort: read from the DOM data attributes TradingView uses for tooltips.
  var rows = [];
  document.querySelectorAll('[data-value]').forEach(function(el){
    rows.push({value: el.getAttribute('data-value'), text: el.innerText});
  });
  return rows;
})()`)
	if err != nil {
		return nil, err
	}
	var result []map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// TVGetSymbol reads the currently displayed symbol from TradingView's URL
// or DOM without a screenshot.
func (b *Browser) TVGetSymbol(ctx context.Context) (string, error) {
	raw, err := b.EvalJS(ctx, `
(function(){
  // URL path: /chart/XXXXX/EXCHANGE:SYMBOL
  var m = window.location.pathname.match(/([A-Z]+:[A-Z0-9]+)$/);
  if (m) return m[1];
  // Fallback: title tag usually contains "SYMBOL — TradingView"
  var title = document.title.split('—')[0].trim();
  return title || '';
})()`)
	if err != nil {
		return "", err
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", err
	}
	return s, nil
}
