package browser

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Daybeam/ghost-driver/internal/cdp"
)

func TestActionRecorder(t *testing.T) {
	mockCDP := &MockCDPClientForRecorder{
		handlers: make(map[string][]handlerEntry),
	}
	b := &Browser{
		client:   mockCDP,
		handlers: make(map[string][]handlerEntry),
	}

	recorder := NewActionRecorder(b)

	ctx := context.Background()
	if err := recorder.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if !recorder.active {
		t.Error("expected recorder to be active")
	}

	if mockCDP.bindingName != "ghostRecordAction" {
		t.Errorf("expected binding ghostRecordAction, got %s", mockCDP.bindingName)
	}

	// Simulate a binding call
	payload := `{"type":"click","x":100,"y":200,"timestamp":123456}`
	bindingParams := map[string]string{
		"name":    "ghostRecordAction",
		"payload": payload,
	}
	data, _ := json.Marshal(bindingParams)

	// Trigger the handler
	for _, entry := range mockCDP.handlers["Runtime.bindingCalled"] {
		entry.fn(data)
	}

	actions := recorder.Actions()
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Type != "click" || actions[0].X != 100 {
		t.Errorf("unexpected action: %+v", actions[0])
	}

	recorder.Stop()
	if recorder.active {
		t.Error("expected recorder to be inactive after stop")
	}
}

type MockCDPClientForRecorder struct {
	bindingName string
	handlers    map[string][]handlerEntry
}

func (m *MockCDPClientForRecorder) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return json.RawMessage("{}"), nil
}
func (m *MockCDPClientForRecorder) CallSession(ctx context.Context, sessionID, method string, params any) (json.RawMessage, error) {
	return nil, nil
}
func (m *MockCDPClientForRecorder) SetSessionID(sid string) {}
func (m *MockCDPClientForRecorder) SessionID() string        { return "" }
func (m *MockCDPClientForRecorder) On(method string, fn cdp.EventHandler) func() {
	m.handlers[method] = append(m.handlers[method], handlerEntry{0, fn})
	return func() {}
}
func (m *MockCDPClientForRecorder) Off(method string, fn cdp.EventHandler) {}
func (m *MockCDPClientForRecorder) AddBinding(ctx context.Context, name string) error {
	m.bindingName = name
	return nil
}
func (m *MockCDPClientForRecorder) Close() {}
