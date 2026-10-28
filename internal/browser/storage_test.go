package browser

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Daybeam/ghost-driver/internal/cdp"
)

type MockStorageCDPClient struct {
	lastMethod string
	lastParams map[string]any
	responses  map[string]json.RawMessage
}

func (m *MockStorageCDPClient) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	m.lastMethod = method
	if params != nil {
		m.lastParams = params.(map[string]any)
	}
	if resp, ok := m.responses[method]; ok {
		return resp, nil
	}
	// Default response for Runtime.evaluate
	if method == "Runtime.evaluate" {
		return json.RawMessage(`{"result": {"value": {}}}`), nil
	}
	return nil, nil
}

func (m *MockStorageCDPClient) CallSession(ctx context.Context, sessionID, method string, params any) (json.RawMessage, error) {
	return m.Call(ctx, method, params)
}
func (m *MockStorageCDPClient) SetSessionID(sid string)                    {}
func (m *MockStorageCDPClient) SessionID() string                           { return "" }
func (m *MockStorageCDPClient) On(method string, fn cdp.EventHandler) func()  { return func() {} }
func (m *MockStorageCDPClient) Off(method string, fn cdp.EventHandler)      {}
func (m *MockStorageCDPClient) AddBinding(ctx context.Context, name string) error { return nil }
func (m *MockStorageCDPClient) Close()                                      {}

func TestStorage(t *testing.T) {
	mockCDP := &MockStorageCDPClient{
		responses: make(map[string]json.RawMessage),
	}
	b := &Browser{client: mockCDP}

	// Test SetLocalStorage
	err := b.SetLocalStorage(context.Background(), "key", "val")
	if err != nil {
		t.Errorf("SetLocalStorage failed: %v", err)
	}
	if mockCDP.lastMethod != "Runtime.evaluate" {
		t.Errorf("expected Runtime.evaluate, got %s", mockCDP.lastMethod)
	}

	// Test GetLocalStorage
	mockCDP.responses["Runtime.evaluate"] = json.RawMessage(`{"result": {"value": {"k1": "v1"}}}`)
	items, err := b.GetLocalStorage(context.Background())
	if err != nil {
		t.Errorf("GetLocalStorage failed: %v", err)
	}
	if items["k1"] != "v1" {
		t.Errorf("expected v1, got %v", items["k1"])
	}

	// Test Navigation
	err = b.NavigateBack(context.Background())
	if err != nil {
		t.Errorf("NavigateBack failed: %v", err)
	}
	if mockCDP.lastMethod != "Page.goBack" {
		t.Errorf("expected Page.goBack, got %s", mockCDP.lastMethod)
	}
}
