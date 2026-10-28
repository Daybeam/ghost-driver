package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os/exec"
	"runtime"
	"time"
	"unicode/utf8"

	"github.com/Daybeam/ghost-driver/internal/cdp"
)

// InputMode controls how events are injected.
type InputMode int

const (
	// InputModeL1 uses DOM-level event dispatching (e.g. element.click()).
	// Fastest, but easiest to detect and bypasses some UI listeners.
	InputModeL1 InputMode = iota

	// InputModeL2 uses Chrome DevTools Protocol Input domain.
	// Events arrive as isTrusted=false.
	InputModeL2

	// InputModeL3 uses OS-level injection (xdotool on Linux).
	// Events originate from the kernel input stack → isTrusted=true.
	InputModeL3
)

// BrowserInterface defines the methods Input needs from Browser.
type BrowserInterface interface {
	SetIndicatorLabel(ctx context.Context, label string) error
	SetIndicatorColor(ctx context.Context, color string) error
	CDPClient() cdp.CDPClient
	EvalJS(ctx context.Context, js string) (json.RawMessage, error)
	MarkDirty() // Add this
	IsSystemDialogOpen() bool // Add this
}

// Input handles physical-level event injection for a browser session.
type Input struct {
	b          BrowserInterface
	mode       InputMode
	lastX      float64
	lastY      float64
	hasLastPos bool
}

// NewInput creates an Input injector.
func NewInput(b BrowserInterface, mode InputMode) *Input {
	// Fallback check for L3
	if mode == InputModeL3 && !xdotoolAvailable() {
		mode = InputModeL2
	}
	return &Input{b: b, mode: mode}
}

// Click moves to (x,y) via a smooth arc, then presses and releases.
func (in *Input) Click(ctx context.Context, x, y float64) error {
	_ = in.b.SetIndicatorLabel(ctx, "● clicking")

	// Pre-action check
	if err := in.checkAvailability(ctx, x, y); err != nil {
		return err
	}

	// Bounds check and scroll into view if needed
	if err := in.ensureVisible(ctx, x, y); err != nil {
		// Non-fatal, but log if possible.
	}

	// L1 Logic: Try to find element at point and click it via JS
	if in.mode == InputModeL1 {
		_ = in.b.SetIndicatorColor(ctx, "#00ff88") // Green for L1
		js := fmt.Sprintf(`document.elementFromPoint(%f, %f)?.click()`, x, y)
		_, err := in.b.EvalJS(ctx, js)
		_ = in.b.SetIndicatorLabel(ctx, "● Ghost-Driver")
		_ = in.b.SetIndicatorColor(ctx, "#00d4ff")
		return err
	}

	// Add a small random offset (jitter) to mimic human imperfection for L2/L3.
	jx := x + (rand.Float64()*4 - 2)
	jy := y + (rand.Float64()*4 - 2)

	_ = in.b.SetIndicatorColor(ctx, "#ffcc00") // yellow while moving
	if err := in.MoveTo(ctx, jx, jy); err != nil {
		return err
	}
	_ = in.b.SetIndicatorColor(ctx, "#ff4444") // red flash on press
	err := in.mouseButton(ctx, jx, jy, "down", "up")
	_ = in.b.SetIndicatorColor(ctx, "#00d4ff") // back to blue
	_ = in.b.SetIndicatorLabel(ctx, "● Ghost-Driver")
	return err
}

// LongClick moves to (x,y), presses, waits duration, then releases.
func (in *Input) LongClick(ctx context.Context, x, y float64, duration time.Duration) error {
	_ = in.b.SetIndicatorLabel(ctx, "● long clicking")

	if err := in.ensureVisible(ctx, x, y); err != nil {
		// Non-fatal
	}

	if in.mode == InputModeL1 {
		_ = in.b.SetIndicatorColor(ctx, "#00ff88")
		js := fmt.Sprintf(`
			(function(){
				var el = document.elementFromPoint(%f, %f);
				if (el) {
					var ev = new MouseEvent('contextmenu', {
						bubbles: true,
						cancelable: true,
						view: window,
						clientX: %f,
						clientY: %f,
						button: 2
					});
					el.dispatchEvent(ev);
				}
			})()
		`, x, y, x, y)
		_, err := in.b.EvalJS(ctx, js)
		_ = in.b.SetIndicatorLabel(ctx, "● Ghost-Driver")
		_ = in.b.SetIndicatorColor(ctx, "#00d4ff")
		return err
	}

	jx := x + (rand.Float64()*4 - 2)
	jy := y + (rand.Float64()*4 - 2)

	_ = in.b.SetIndicatorColor(ctx, "#ffcc00")
	if err := in.MoveTo(ctx, jx, jy); err != nil {
		return err
	}

	_ = in.b.SetIndicatorColor(ctx, "#ff4444")
	if in.mode == InputModeL3 {
		if err := xdotoolMouseDown(); err != nil {
			return err
		}
		defer xdotoolMouseUp()
		if !sleepCtx(ctx, duration) {
			return ctx.Err()
		}
		if err := xdotoolMouseUp(); err != nil {
			return err
		}
	} else {
		// L2 (CDP)
		if err := in.mouseButton(ctx, jx, jy, "down"); err != nil {
			return err
		}
		defer func() {
			upCtx, upCancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer upCancel()
			in.mouseButton(upCtx, jx, jy, "up")
		}()
		if !sleepCtx(ctx, duration) {
			return ctx.Err()
		}
		if err := in.mouseButton(ctx, jx, jy, "up"); err != nil {
			return err
		}
	}

	_ = in.b.SetIndicatorColor(ctx, "#00d4ff")
	_ = in.b.SetIndicatorLabel(ctx, "● Ghost-Driver")
	return nil
}

// Hover moves the cursor to (x,y) without clicking.
func (in *Input) Hover(ctx context.Context, x, y float64) error {
	_ = in.b.SetIndicatorLabel(ctx, "● hovering")
	_ = in.b.SetIndicatorColor(ctx, "#ffcc00") // yellow while moving
	err := in.MoveTo(ctx, x, y)
	_ = in.b.SetIndicatorColor(ctx, "#00d4ff")
	_ = in.b.SetIndicatorLabel(ctx, "● Ghost-Driver")
	return err
}

// MoveTo animates the cursor from its current position to (x,y) in small steps.
func (in *Input) MoveTo(ctx context.Context, x, y float64) error {
	if in.mode == InputModeL3 {
		return xdotoolMove(int(x), int(y))
	}
	return in.cdpMoveSmooth(ctx, x, y)
}

// TypeText types a string character by character with realistic key timings.
func (in *Input) TypeText(ctx context.Context, text string) error {
	if in.b != nil {
		in.b.MarkDirty()
	}

	// For TypeText, we usually target the currently focused element.
	// But we can check if a system dialog is blocking.
	if err := in.checkAvailability(ctx, -1, -1); err != nil {
		return err
	}

	_ = in.b.SetIndicatorLabel(ctx, "● typing")
	_ = in.b.SetIndicatorColor(ctx, "#aa44ff") // purple while typing
	var err error
	if in.mode == InputModeL3 {
		err = xdotoolType(text)
	} else {
		err = in.cdpType(ctx, text)
	}
	_ = in.b.SetIndicatorColor(ctx, "#00d4ff")
	_ = in.b.SetIndicatorLabel(ctx, "● Ghost-Driver")
	return err
}

// Scroll scrolls the page by (dx, dy) pixels at position (x, y).
func (in *Input) Scroll(ctx context.Context, x, y, dx, dy float64) error {
	if in.mode == InputModeL3 {
		return xdotoolScroll(int(x), int(y), dy)
	}
	return in.cdpScroll(ctx, x, y, dx, dy)
}

// ─── CDP path ────────────────────────────────────────────────────────────────

func (in *Input) ensureVisible(ctx context.Context, x, y float64) error {
	js := fmt.Sprintf(`
(function(){
  var w = window.innerWidth;
  var h = window.innerHeight;
  if (%f < 0 || %f > w || %f < 0 || %f > h) {
    window.scrollTo({left: %f - w/2, top: %f - h/2, behavior: 'instant'});
    return true;
  }
  return false;
})()`, x, x, y, y, x, y)
	_, err := in.b.EvalJS(ctx, js)
	return err
}

func (in *Input) cdpMoveSmooth(ctx context.Context, targetX, targetY float64) error {
	startX, startY := in.lastX, in.lastY
	if !in.hasLastPos {
		raw, err := in.b.EvalJS(ctx, `[window.innerWidth/2, window.innerHeight/2]`)
		if err == nil {
			var center [2]float64
			if json.Unmarshal(raw, &center) == nil {
				startX, startY = center[0], center[1]
			} else {
				startX, startY = 640, 360
			}
		} else {
			startX, startY = 640, 360
		}
		in.hasLastPos = true
	}
	dist := math.Hypot(targetX-startX, targetY-startY)
	steps := int(dist / 10)
	if steps < 4 {
		steps = 4
	} else if steps > 40 {
		steps = 40
	}
	for i := 1; i <= steps; i++ {
		t := float64(i) / float64(steps)
		// Ease-in-out cubic for human-like acceleration.
		ease := t * t * (3 - 2*t)
		cx := startX + (targetX-startX)*ease
		cy := startY + (targetY-startY)*ease

		_, err := in.b.CDPClient().Call(ctx, "Input.dispatchMouseEvent", map[string]any{
			"type": "mouseMoved",
			"x":    math.Round(cx),
			"y":    math.Round(cy),
		})
		if err != nil {
			return err
		}
		if !sleepCtx(ctx, 8*time.Millisecond) {
			return ctx.Err()
		}
	}
	in.lastX, in.lastY = targetX, targetY
	return nil
}

func (in *Input) mouseButton(ctx context.Context, x, y float64, actions ...string) error {
	for _, action := range actions {
		evType := "mousePressed"
		if action == "up" {
			evType = "mouseReleased"
		}
		_, err := in.b.CDPClient().Call(ctx, "Input.dispatchMouseEvent", map[string]any{
			"type":       evType,
			"x":          x,
			"y":          y,
			"button":     "left",
			"clickCount": 1,
		})
		if err != nil {
			return err
		}
		// Click hold time: 20-60ms
		if !humanSleep(ctx, 20, 60) {
			return ctx.Err()
		}
	}
	return nil
}

// typeBatchThreshold is the rune count above which TypeText switches from
// per-character dispatchKeyEvent (stealth + per-keystroke handlers) to a
// single Input.insertText call (speed). Short/interactive text (search boxes,
// login forms) stays per-char; long content (chat prompts, article bodies)
// is inserted in one round-trip.
const typeBatchThreshold = 50

func (in *Input) cdpType(ctx context.Context, text string) error {
	// Fast path: long text → one Input.insertText call. This avoids per-char
	// CDP round-trips and per-char humanSleep (the dominant cost for long input).
	// insertText fires an `input` event but not per-keystroke keydown/keyup,
	// so it is only used when speed outweighs per-keystroke handler fidelity.
	if utf8.RuneCountInString(text) > typeBatchThreshold {
		_, err := in.b.CDPClient().Call(ctx, "Input.insertText", map[string]any{
			"text": text,
		})
		return err
	}
	for _, ch := range text {
		if ch > 127 {
			_, err := in.b.CDPClient().Call(ctx, "Input.insertText", map[string]any{
				"text": string(ch),
			})
			if err != nil {
				return err
			}
		} else {
			_, err := in.b.CDPClient().Call(ctx, "Input.dispatchKeyEvent", map[string]any{
				"type": "keyDown",
				"text": string(ch),
				"key":  string(ch),
			})
			if err != nil {
				return err
			}
			_, err = in.b.CDPClient().Call(ctx, "Input.dispatchKeyEvent", map[string]any{
				"type": "keyUp",
				"key":  string(ch),
			})
			if err != nil {
				return err
			}
		}
		// Human typing rhythm: 60–120 WPM ≈ 40–120ms per char.
		if !humanSleep(ctx, 40, 120) {
			return ctx.Err()
		}
	}
	return nil
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func humanSleep(ctx context.Context, min, max int) bool {
	if max <= min {
		return sleepCtx(ctx, time.Duration(min)*time.Millisecond)
	}
	d := time.Duration(min+rand.Intn(max-min)) * time.Millisecond
	return sleepCtx(ctx, d)
}

func (in *Input) cdpScroll(ctx context.Context, x, y, dx, dy float64) error {
	_, err := in.b.CDPClient().Call(ctx, "Input.dispatchMouseEvent", map[string]any{
		"type":      "mouseWheel",
		"x":         x,
		"y":         y,
		"deltaX":    dx,
		"deltaY":    dy,
		"modifiers": 0,
	})
	return err
}

func (in *Input) checkAvailability(ctx context.Context, x, y float64) error {
	// 1. Check System Dialog
	if in.b.IsSystemDialogOpen() {
		return fmt.Errorf("action blocked: a system dialog (file picker/alert) is currently open")
	}

	// 2. Check DOM Disabled (only if x,y provided)
	if x >= 0 && y >= 0 {
		js := fmt.Sprintf(`document.elementFromPoint(%f, %f)?.disabled || false`, x, y)
		raw, err := in.b.EvalJS(ctx, js)
		if err == nil {
			var isDisabled bool
			json.Unmarshal(raw, &isDisabled)
			if isDisabled {
				return fmt.Errorf("action blocked: the target element at (%f, %f) is disabled", x, y)
			}
		}
	}

	return nil
}

// ─── OS path (Linux xdotool) ─────────────────────────────────────────────────

func xdotoolAvailable() bool {
	if runtime.GOOS == "windows" {
		return true // We'll use native Windows API
	}
	if runtime.GOOS != "linux" {
		return false
	}
	_, err := exec.LookPath("xdotool")
	return err == nil
}

// xdotoolRun executes an xdotool command with a 10s timeout to prevent
// orphan processes if the caller's context is already cancelled.
func xdotoolRun(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "xdotool", args...).Run()
}

func xdotoolMove(x, y int) error {
	if runtime.GOOS == "windows" {
		return winMove(x, y)
	}
	return xdotoolRun("mousemove", fmt.Sprintf("%d", x), fmt.Sprintf("%d", y))
}

func xdotoolMouseDown() error {
	if runtime.GOOS == "windows" {
		return winMouseDown()
	}
	return xdotoolRun("mousedown", "1")
}

func xdotoolMouseUp() error {
	if runtime.GOOS == "windows" {
		return winMouseUp()
	}
	return xdotoolRun("mouseup", "1")
}

func xdotoolType(text string) error {
	if runtime.GOOS == "windows" {
		return winType(text)
	}
	return xdotoolRun("type", "--delay", "50", "--", text)
}

func xdotoolScroll(x, y int, dy float64) error {
	if runtime.GOOS == "windows" {
		return winScroll(int(dy))
	}
	btn := "4" // scroll up
	if dy > 0 {
		btn = "5" // scroll down
	}
	clicks := int(math.Abs(dy) / 120)
	if clicks < 1 {
		clicks = 1
	}
	for i := 0; i < clicks; i++ {
		if err := xdotoolRun("click", "--clearmodifiers", btn); err != nil {
			return err
		}
	}
	return nil
}
