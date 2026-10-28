package cdp

import (
	"encoding/json"
	"testing"
)

func TestRequestMessageMarshal(t *testing.T) {
	tests := []struct {
		name   string
		msg    requestMessage
		expect string
	}{
		{
			"nil params omitted",
			requestMessage{ID: 1, Method: "Page.navigate"},
			`{"id":1,"method":"Page.navigate"}`,
		},
		{
			"map params",
			requestMessage{ID: 2, Method: "Page.navigate", Params: map[string]string{"url": "https://example.com"}},
			`{"id":2,"method":"Page.navigate","params":{"url":"https://example.com"}}`,
		},
		{
			"with sessionID",
			requestMessage{ID: 3, Method: "Runtime.evaluate", Params: map[string]string{"expression": "1+1"}, SessionID: "ABC"},
			`{"id":3,"method":"Runtime.evaluate","params":{"expression":"1+1"},"sessionId":"ABC"}`,
		},
		{
			"nil params with sessionID",
			requestMessage{ID: 4, Method: "Page.enable", SessionID: "DEF"},
			`{"id":4,"method":"Page.enable","sessionId":"DEF"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.msg)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(data) != tt.expect {
				t.Errorf("got %s, want %s", data, tt.expect)
			}
		})
	}
}

func TestRequestMessageMatchesOldDoubleMarshal(t *testing.T) {
	// Verify that the single-marshal requestMessage produces the same JSON
	// as the old double-marshal approach (marshal params → RawMessage → Message).
	params := map[string]any{"url": "https://example.com", "transition": "reload"}

	// Old approach: double marshal
	oldParams, _ := json.Marshal(params)
	oldMsg := Message{ID: 42, Method: "Page.navigate", Params: oldParams, SessionID: "SID"}
	oldData, _ := json.Marshal(oldMsg)

	// New approach: single marshal
	newMsg := requestMessage{ID: 42, Method: "Page.navigate", Params: params, SessionID: "SID"}
	newData, _ := json.Marshal(newMsg)

	if string(oldData) != string(newData) {
		t.Errorf("marshal mismatch:\n  old: %s\n  new: %s", oldData, newData)
	}
}

func TestOnCleanupRemovesHandler(t *testing.T) {
	c := &Client{
		handlers: make(map[string][]handlerEntry),
	}

	called := false
	cancel := c.On("Test.event", func(_ json.RawMessage) {
		called = true
	})

	// Handler should be registered
	entries := c.handlers["Test.event"]
	if len(entries) != 1 {
		t.Fatalf("expected 1 handler, got %d", len(entries))
	}

	// Simulate dispatch
	entries[0].fn(nil)
	if !called {
		t.Error("handler was not called")
	}

	// Cleanup removes the handler
	cancel()
	entries = c.handlers["Test.event"]
	if len(entries) != 0 {
		t.Fatalf("expected 0 handlers after cleanup, got %d", len(entries))
	}
}

func TestOnCleanupMultipleSafe(t *testing.T) {
	c := &Client{
		handlers: make(map[string][]handlerEntry),
	}

	cancel := c.On("Test.event", func(_ json.RawMessage) {})

	// Double cleanup should be safe (no panic)
	cancel()
	cancel()

	if len(c.handlers["Test.event"]) != 0 {
		t.Error("expected 0 handlers after double cleanup")
	}
}

func TestOnMultipleHandlersIndependentCleanup(t *testing.T) {
	c := &Client{
		handlers: make(map[string][]handlerEntry),
	}

	cancel1 := c.On("Test.event", func(_ json.RawMessage) {})
	cancel2 := c.On("Test.event", func(_ json.RawMessage) {})

	if len(c.handlers["Test.event"]) != 2 {
		t.Fatalf("expected 2 handlers, got %d", len(c.handlers["Test.event"]))
	}

	// Remove first handler only
	cancel1()
	if len(c.handlers["Test.event"]) != 1 {
		t.Fatalf("expected 1 handler after cancel1, got %d", len(c.handlers["Test.event"]))
	}

	// Remove second handler
	cancel2()
	if len(c.handlers["Test.event"]) != 0 {
		t.Fatalf("expected 0 handlers after cancel2, got %d", len(c.handlers["Test.event"]))
	}
}
