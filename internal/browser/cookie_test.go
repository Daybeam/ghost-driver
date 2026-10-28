package browser

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Daybeam/ghost-driver/internal/cdp"
)

type MockCookieCDPClient struct {
	lastMethod string
	lastParams map[string]any
}

func (m *MockCookieCDPClient) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	m.lastMethod = method
	if params != nil {
		if p, ok := params.(map[string]any); ok {
			m.lastParams = p
		}
	}
	if method == "Network.getCookies" {
		return json.RawMessage(`{"cookies": [{"name": "test", "value": "val"}]}`), nil
	}
	return nil, nil
}

func (m *MockCookieCDPClient) CallSession(ctx context.Context, sessionID, method string, params any) (json.RawMessage, error) {
	return m.Call(ctx, method, params)
}
func (m *MockCookieCDPClient) SetSessionID(sid string)                    {}
func (m *MockCookieCDPClient) SessionID() string                           { return "" }
func (m *MockCookieCDPClient) On(method string, fn cdp.EventHandler) func()  { return func() {} }
func (m *MockCookieCDPClient) Off(method string, fn cdp.EventHandler)      {}
func (m *MockCookieCDPClient) AddBinding(ctx context.Context, name string) error { return nil }
func (m *MockCookieCDPClient) Close()                                      {}

func TestGetCookies(t *testing.T) {
	mockCDP := &MockCookieCDPClient{}
	b := &Browser{client: mockCDP}

	cookies, err := b.GetCookies(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cookies) != 1 {
		t.Errorf("expected 1 cookie, got %d", len(cookies))
	}
	if cookies[0]["name"] != "test" {
		t.Errorf("expected cookie name 'test', got %v", cookies[0]["name"])
	}

	if mockCDP.lastMethod != "Network.getCookies" {
		t.Errorf("expected Network.getCookies, got %s", mockCDP.lastMethod)
	}
}

func TestSetCookie(t *testing.T) {
	mockCDP := &MockCookieCDPClient{}
	b := &Browser{client: mockCDP}

	err := b.SetCookie(context.Background(), "name", "val", "domain", "/", true, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mockCDP.lastMethod != "Network.setCookie" {
		t.Errorf("expected Network.setCookie, got %s", mockCDP.lastMethod)
	}
	if mockCDP.lastParams["name"] != "name" {
		t.Errorf("expected name 'name', got %v", mockCDP.lastParams["name"])
	}
}
