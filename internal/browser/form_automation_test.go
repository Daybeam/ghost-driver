package browser

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestFormStateGraph(t *testing.T) {
	g := NewFormStateGraph()

	f1 := map[string]string{"#name": "alice", "#age": "25"}
	id1 := g.UpdateCurrent(f1)
	if len(id1) != 12 {
		t.Errorf("expected 12 char ID, got %s", id1)
	}

	f2 := map[string]string{"#name": "alice", "#age": "26"}
	g.RecordAction("#age", "fill", "26", "success", "", f2)

	if g.current == id1 {
		t.Errorf("current state should have changed")
	}

	if len(g.actions) != 1 {
		t.Errorf("expected 1 action, got %d", len(g.actions))
	}

	diff := DiffFormStates(f1, f2)
	if !strings.Contains(diff, "#age:25->26") {
		t.Errorf("diff incorrect: %s", diff)
	}
}

func TestCausalityTraceStack(t *testing.T) {
	stack := NewCausalityTraceStack(2)

	stack.Push(TraceEntry{ActionType: "fill", Selector: "#field1", ValueFilled: "val1"})
	stack.Push(TraceEntry{ActionType: "fill", Selector: "#field2", ValueFilled: "val2"})
	stack.Push(TraceEntry{ActionType: "fill", Selector: "#field3", ValueFilled: "val3"}) // Evicts #field1

	last := stack.Last()
	if last.Selector != "#field3" {
		t.Errorf("expected #field3, got %s", last.Selector)
	}

	if len(stack.entries) != 2 {
		t.Errorf("expected size 2, got %d", len(stack.entries))
	}
}

func TestFuzzyMatchOptions(t *testing.T) {
	b := &Browser{}
	opts := []EnumOption{
		{Value: "1", Text: "Apple"},
		{Value: "2", Text: "Banana"},
		{Value: "3", Text: "Cherry"},
		{Value: "4", Text: "Apricot"},
	}

	matched := b.fuzzyMatchOptions(opts, "Ap")
	if len(matched) != 2 {
		t.Errorf("expected 2 matches, got %d", len(matched))
	}

	if matched[0].Text != "Apple" {
		t.Errorf("expected Apple as first match, got %s", matched[0].Text)
	}
}

func TestErrorSentinelAttribution(t *testing.T) {
	stack := NewCausalityTraceStack(5)
	stack.Push(TraceEntry{ActionType: "fill", Selector: "#age", ValueFilled: "abc"})

	// Mock browser for sentinel
	// ErrorSentinel.Check calls EvalJS, which expects CDP Runtime.evaluate response format
	mock := &MockCDPClient{
		Result: json.RawMessage(`{"result": {"value": {"found": true, "text": "invalid age"}}}`),
	}
	b := &Browser{client: mock}

	sentinel := NewErrorSentinel(b, stack)
	msg, found := sentinel.Check(context.Background())

	if !found {
		t.Fatal("expected error to be found")
	}
	if !strings.Contains(msg, "Likely caused by Step 1") {
		t.Errorf("attribution failed: %s", msg)
	}
}

func TestVerifyPostAction(t *testing.T) {
	// VerifyPostAction calls EvalJS, which expects CDP Runtime.evaluate response format
	mock := &MockCDPClient{
		Result: json.RawMessage(`{"result": {"value": "ok"}}`),
	}
	b := &Browser{client: mock}

	ok, status, err := b.VerifyPostAction(context.Background(), PostActionConfig{
		TargetSelector: "#test",
		ExpectedState:  "value_set",
		Timeout:        100 * time.Millisecond,
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !ok || status != "verified" {
		t.Errorf("expected verified, got ok=%v status=%s", ok, status)
	}
}
