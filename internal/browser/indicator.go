package browser

import (
	"context"
	"fmt"
)

// IndicatorStyle controls the visual appearance of the control overlay.
type IndicatorStyle struct {
	Color     string // CSS color, e.g. "#00ff88" or "rgba(0,200,120,0.9)"
	Width     int    // border thickness in px
	Label     string // text shown in the top-left corner badge
	Animated  bool   // pulse animation on the border
}

func DefaultIndicatorStyle() IndicatorStyle {
	return IndicatorStyle{
		Color:    "#00d4ff",
		Width:    3,
		Label:    "● Ghost-Driver",
		Animated: true,
	}
}

// ShowIndicator injects a fixed border + badge into the page to signal that
// Ghost-Driver is actively controlling this tab. No extension needed —
// it's a plain DOM overlay injected via CDP.
//
// The overlay:
//   - sits at z-index 2147483647 (max), pointer-events:none so it doesn't
//     interfere with clicks
//   - survives React/Vue re-renders (injected into <body>, not a shadow root)
//   - is removed cleanly by HideIndicator without leaving any trace
func (b *Browser) ShowIndicator(ctx context.Context, style IndicatorStyle) error {
	pulseCSS := ""
	if style.Animated {
		pulseCSS = fmt.Sprintf(`
@keyframes __ghost_pulse {
  0%%,100%% { box-shadow: inset 0 0 0 %dpx %s, 0 0 12px %s; }
  50%%      { box-shadow: inset 0 0 0 %dpx %s, 0 0 28px %s; }
}`, style.Width, style.Color, style.Color,
			style.Width, style.Color, style.Color)
	}

	animRule := ""
	if style.Animated {
		animRule = "animation: __ghost_pulse 2s ease-in-out infinite;"
	} else {
		animRule = fmt.Sprintf("box-shadow: inset 0 0 0 %dpx %s;", style.Width, style.Color)
	}

	js := fmt.Sprintf(`
(function(){
  // Idempotent: remove existing indicator first.
  var old = document.getElementById('__ghost_indicator');
  if (old) old.remove();
  var oldStyle = document.getElementById('__ghost_indicator_style');
  if (oldStyle) oldStyle.remove();

  // Inject keyframe CSS.
  var styleEl = document.createElement('style');
  styleEl.id = '__ghost_indicator_style';
  styleEl.textContent = %q + %q;
  document.head.appendChild(styleEl);

  // Border overlay — full viewport, pointer-events:none.
  var overlay = document.createElement('div');
  overlay.id = '__ghost_indicator';
  overlay.style.cssText = [
    'position:fixed',
    'top:0','left:0','right:0','bottom:0',
    'pointer-events:none',
    'z-index:2147483647',
    %q,
    'border-radius:0',
    'transition:opacity 0.3s',
  ].join(';');

  // Badge in top-left corner.
  var badge = document.createElement('div');
  badge.style.cssText = [
    'position:absolute',
    'top:0','left:0',
    'background:' + %q,
    'color:#000',
    'font:bold 11px/1 monospace',
    'padding:3px 8px',
    'border-radius:0 0 6px 0',
    'letter-spacing:0.04em',
    'opacity:0.92',
    'pointer-events:none',
  ].join(';');
  badge.textContent = %q;

  overlay.appendChild(badge);
  document.body.appendChild(overlay);
})()`,
		pulseCSS,
		fmt.Sprintf(`.ghost-overlay { %s }`, animRule),
		animRule,
		style.Color,
		style.Label,
	)

	_, err := b.EvalJS(ctx, js)
	return err
}

// HideIndicator removes the overlay injected by ShowIndicator.
func (b *Browser) HideIndicator(ctx context.Context) error {
	_, err := b.EvalJS(ctx, `
(function(){
  var el = document.getElementById('__ghost_indicator');
  if (el) el.remove();
  var s = document.getElementById('__ghost_indicator_style');
  if (s) s.remove();
})()`)
	return err
}

// SetIndicatorLabel updates only the badge text without re-injecting the overlay.
// Useful for showing current action: "● clicking", "● typing", "● recording".
func (b *Browser) SetIndicatorLabel(ctx context.Context, label string) error {
	js := fmt.Sprintf(`
(function(){
  var overlay = document.getElementById('__ghost_indicator');
  if (!overlay) return;
  var badge = overlay.querySelector('div');
  if (badge) badge.textContent = %q;
})()`, label)
	_, err := b.EvalJS(ctx, js)
	return err
}

// SetIndicatorColor swaps the border color at runtime — e.g. red during
// recording, yellow while waiting, green after success.
func (b *Browser) SetIndicatorColor(ctx context.Context, color string) error {
	js := fmt.Sprintf(`
(function(){
  var s = document.getElementById('__ghost_indicator_style');
  if (s) s.remove();
  var overlay = document.getElementById('__ghost_indicator');
  if (!overlay) return;
  var newStyle = document.createElement('style');
  newStyle.id = '__ghost_indicator_style';
  newStyle.textContent = [
    '@keyframes __ghost_pulse {',
    '  0%%,100%% { box-shadow: inset 0 0 0 3px '+%q+', 0 0 12px '+%q+'; }',
    '  50%%      { box-shadow: inset 0 0 0 3px '+%q+', 0 0 28px '+%q+'; }',
    '}'
  ].join('');
  document.head.appendChild(newStyle);
  overlay.style.boxShadow = 'inset 0 0 0 3px ' + %q;
  var badge = overlay.querySelector('div');
  if (badge) badge.style.background = %q;
})()`,
		color, color, color, color, color, color)
	_, err := b.EvalJS(ctx, js)
	return err
}
