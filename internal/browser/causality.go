package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sync"
	"time"
)

// TraceEntry records one action in the causality stack.
type TraceEntry struct {
	StepID      int    `json:"step_id"`
	Timestamp   int64  `json:"timestamp"`
	ActionType  string `json:"action_type"`
	Selector    string `json:"selector"`
	FieldLabel  string `json:"field_label,omitempty"`
	ValueFilled string `json:"value_filled,omitempty"`
}

// CausalityTraceStack maintains a history of recent actions for error attribution.
type CausalityTraceStack struct {
	mu       sync.Mutex
	entries  []TraceEntry
	capacity int
	nextStep int // monotonic counter, never resets on ring shift
}

func NewCausalityTraceStack(capacity int) *CausalityTraceStack {
	if capacity <= 0 {
		capacity = 20
	}
	return &CausalityTraceStack{
		entries:  make([]TraceEntry, 0, capacity),
		capacity: capacity,
		nextStep: 1,
	}
}

func (s *CausalityTraceStack) Push(entry TraceEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.entries) >= s.capacity {
		// Shift left in place (copy) so the backing array is reused and the
		// dropped entry becomes garbage-collectable. Reslicing via entries[1:]
		// leaked the first element's backing memory forever.
		copy(s.entries, s.entries[1:])
		s.entries = s.entries[:s.capacity-1]
	}
	entry.Timestamp = time.Now().Unix()
	entry.StepID = s.nextStep
	s.nextStep++
	s.entries = append(s.entries, entry)
}

func (s *CausalityTraceStack) Last() *TraceEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.entries) == 0 {
		return nil
	}
	e := s.entries[len(s.entries)-1]
	return &e
}

// ErrorPattern defines what an async error looks like.
type ErrorPattern struct {
	Name      string
	Selector  string
	TextMatch string
}

// ErrorSentinel watches for async error indicators.
type ErrorSentinel struct {
	browser  *Browser
	stack    *CausalityTraceStack
	patterns []ErrorPattern
}

func NewErrorSentinel(b *Browser, stack *CausalityTraceStack) *ErrorSentinel {
	return &ErrorSentinel{
		browser: b,
		stack:   stack,
		patterns: []ErrorPattern{
			{Name: "General Error", Selector: ".error, .invalid, .errorMessage, [role='alert'], .ant-form-item-explain-error, .el-form-item__error"},
			{Name: "HTML5 Invalid", Selector: "input:invalid"},
		},
	}
}

func (e *ErrorSentinel) Check(ctx context.Context) (string, bool) {
	for _, p := range e.patterns {
		js := fmt.Sprintf(`
(function(){
  var el = document.querySelector(%q);
  if (el) {
    return {found: true, text: el.innerText || el.placeholder || "unknown error"};
  }
  return {found: false};
})()`, p.Selector)
		raw, err := e.browser.EvalJS(ctx, js)
		if err != nil {
			continue
		}
		var res struct {
			Found bool   `json:"found"`
			Text  string `json:"text"`
		}
		json.Unmarshal(raw, &res)
		if res.Found {
			if p.TextMatch != "" {
				if matched, _ := regexp.MatchString(p.TextMatch, res.Text); !matched {
					continue
				}
			}
			return e.formatAttribution(res.Text), true
		}
	}
	return "", false
}

func (e *ErrorSentinel) formatAttribution(errorText string) string {
	last := e.stack.Last()
	if last == nil {
		return fmt.Sprintf("Detected error: %q (No recent actions in stack)", errorText)
	}
	return fmt.Sprintf("Detected error: %q. Likely caused by Step %d (%s %s with %q)",
		errorText, last.StepID, last.ActionType, last.Selector, last.ValueFilled)
}
