package mcp

import (
	"context"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

func TestNewServer(t *testing.T) {
	srv, state := NewServer()
	if srv == nil || state == nil {
		t.Fatal("failed to create server or state")
	}

	if state.auditor == nil {
		t.Error("auditor not initialized")
	}
}

func TestToolRegistration(t *testing.T) {
	srv, _ := NewServer()
	tools := srv.ListTools()

	foundEnum := false
	foundAnalytics := false
	for _, tool := range tools {
		// Use tool.Tool.Name for mcp-go MCPServer
		if tool.Tool.Name == "enum_options" {
			foundEnum = true
		}
		if tool.Tool.Name == "get_tool_analytics" {
			foundAnalytics = true
		}
	}

	if !foundEnum {
		t.Error("enum_options tool not registered")
	}
	// get_tool_analytics is a dev-only UI measurement tool; it must NOT be
	// registered in the default base edition (automation only).
	if foundAnalytics {
		t.Error("get_tool_analytics should not be registered in base edition")
	}
}

func TestAuditWrapper(t *testing.T) {
	s := NewState()

	called := false
	handler := func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		called = true
		return mcpgo.NewToolResultText("ok"), nil
	}

	wrapped := s.auditWrapper("test_tool", handler)

	req := mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{
			Name: "test_tool",
		},
	}

	_, err := wrapped(context.Background(), req)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !called {
		t.Error("handler not called")
	}

	stats := s.auditor.GetStats()
	if stats["test_tool"].InvocationCount != 1 {
		t.Errorf("expected 1 invocation, got %d", stats["test_tool"].InvocationCount)
	}
}
