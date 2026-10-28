package browser

import (
	"context"
	"fmt"
	"time"
)

type PostActionConfig struct {
	TargetSelector string
	ExpectedState  string // "value_set", "dom_changed", "class_added"
	ExpectedClass  string
	Timeout        time.Duration
}

func (b *Browser) VerifyPostAction(ctx context.Context, cfg PostActionConfig) (bool, string, error) {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 2 * time.Second
	}

	start := time.Now()
	for time.Since(start) < cfg.Timeout {
		var js string
		switch cfg.ExpectedState {
		case "value_set":
			js = fmt.Sprintf(`(function(){
				var el = document.querySelector(%q);
				if (!el) return "not_found";
				return (el.value && el.value !== "") ? "ok" : "empty";
			})()`, cfg.TargetSelector)
		case "class_added":
			js = fmt.Sprintf(`(function(){
				var el = document.querySelector(%q);
				if (!el) return "not_found";
				return el.classList.contains(%q) ? "ok" : "missing";
			})()`, cfg.TargetSelector, cfg.ExpectedClass)
		default:
			return false, "unsupported_state", nil
		}

		resRaw, err := b.EvalJS(ctx, js)
		if err == nil {
			var status string
			fmt.Sscanf(string(resRaw), "%q", &status)
			if status == "ok" {
				return true, "verified", nil
			}
		}

		select {
		case <-ctx.Done():
			return false, "timeout", ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}

	return false, "assertion_failed", nil
}

func (b *Browser) DispatchEvents(ctx context.Context, selector string, events []string) error {
	for _, ev := range events {
		js := fmt.Sprintf(`
(function(){
  var el = document.querySelector(%q);
  if (el) {
    var e = new Event(%q, {bubbles: true, cancelable: true});
    el.dispatchEvent(e);
  }
})()`, selector, ev)
		_, err := b.EvalJS(ctx, js)
		if err != nil {
			return err
		}
	}
	return nil
}
