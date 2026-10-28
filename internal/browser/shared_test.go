package browser

import (
	"context"
	"encoding/json"

	"github.com/Daybeam/ghost-driver/internal/cdp"
)

// MockCDPClient is the single shared mock for all browser-package tests.
type MockCDPClient struct {
	LastMethod string
	Result     json.RawMessage
	Err        error
	CallCount  int
	Methods    []string
}

func (m *MockCDPClient) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	m.LastMethod = method
	m.CallCount++
	m.Methods = append(m.Methods, method)
	return m.Result, m.Err
}

func (m *MockCDPClient) CallSession(ctx context.Context, sessionID, method string, params any) (json.RawMessage, error) {
	return m.Call(ctx, method, params)
}

func (m *MockCDPClient) SetSessionID(sid string)                    {}
func (m *MockCDPClient) SessionID() string                           { return "" }
func (m *MockCDPClient) On(method string, fn cdp.EventHandler) func()  { return func() {} }
func (m *MockCDPClient) Off(method string, fn cdp.EventHandler)      {}
func (m *MockCDPClient) AddBinding(ctx context.Context, name string) error { return nil }
func (m *MockCDPClient) Close()                                      {}
