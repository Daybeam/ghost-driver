package predict

import (
	"context"
	"testing"
)

func TestRulePredictor(t *testing.T) {
	et := NewElementTable()
	et.Add("button", "button", "Search", "#search-btn", 100, 200, 80, 30)
	et.Add("input", "textbox", "Username", "#username", 100, 300, 150, 30)
	et.Add("button", "button", "Submit Form", "#submit", 100, 400, 100, 30)

	tableStr := et.Format()

	p := NewRulePredictor()
	ctx := context.Background()

	t.Run("Exact Match Search Button", func(t *testing.T) {
		action, err := p.Predict(ctx, tableStr, "search")
		if err != nil {
			t.Fatalf("Predict failed: %v", err)
		}
		if action.TargetIdx != 1 {
			t.Errorf("got TargetIdx = %d, want 1", action.TargetIdx)
		}
		if action.Operation != "CLICK" {
			t.Errorf("got Operation = %s, want CLICK", action.Operation)
		}
	})

	t.Run("Exact Match Username Input", func(t *testing.T) {
		action, err := p.Predict(ctx, tableStr, "username")
		if err != nil {
			t.Fatalf("Predict failed: %v", err)
		}
		if action.TargetIdx != 2 {
			t.Errorf("got TargetIdx = %d, want 2", action.TargetIdx)
		}
		if action.Operation != "TYPE_TEXT" {
			t.Errorf("got Operation = %s, want TYPE_TEXT", action.Operation)
		}
	})

	t.Run("Substring Only Delegates", func(t *testing.T) {
		_, err := p.Predict(ctx, tableStr, "click search")
		if err != ErrDelegateToCaller {
			t.Errorf("got err = %v, want ErrDelegateToCaller for substring-only match", err)
		}
	})

	t.Run("No Match Delegates", func(t *testing.T) {
		_, err := p.Predict(ctx, tableStr, "nonexistent featurexyz")
		if err != ErrDelegateToCaller {
			t.Errorf("got err = %v, want ErrDelegateToCaller", err)
		}
	})
}
