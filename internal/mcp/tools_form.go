package mcp

import (
	"context"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/Daybeam/ghost-driver/internal/browser"
)

func (s *State) registerFormTools(srv *server.MCPServer) {
	// ── enum_options ─────────────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("enum_options",
		mcpgo.WithDescription(`Extract available options from a SPA dropdown (React/Vue/ElementUI).
It clicks the trigger, waits for the dropdown to render, and enumerates all options.`),
		mcpgo.WithString("trigger_selector", mcpgo.Required(), mcpgo.Description("CSS selector of the dropdown trigger.")),
		mcpgo.WithString("container_selector", mcpgo.Description("Optional: CSS selector of the dropdown panel.")),
		mcpgo.WithString("item_selector", mcpgo.Description("Optional: CSS selector of the individual options.")),
		mcpgo.WithBoolean("virtual", mcpgo.Description("Whether the list is virtualized (scrolling required).")),
		mcpgo.WithNumber("max_items", mcpgo.Description("Max items to collect.")),
		mcpgo.WithString("match", mcpgo.Description("Fuzzy match string to find specific options.")),
	), s.auditWrapper("enum_options", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, input, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}

		cfg := browser.EnumOptionsConfig{
			TriggerSelector:   req.GetString("trigger_selector", ""),
			ContainerSelector: req.GetString("container_selector", ""),
			ItemSelector:      req.GetString("item_selector", ""),
			Virtual:           req.GetBool("virtual", false),
			MaxItems:          int(req.GetFloat("max_items", 0)),
			Match:             req.GetString("match", ""),
		}

		res, err := b.EnumOptions(ctx, cfg, input)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}

		out, err := mcpgo.NewToolResultJSON(res)
		return out, err
	}))

	// ── get_form_snapshot ────────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("get_form_snapshot",
		mcpgo.WithDescription("Get current values of all visible form fields."),
	), s.auditWrapper("get_form_snapshot", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		fields, err := b.SnapshotForm(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		out, err := mcpgo.NewToolResultJSON(fields)
		return out, err
	}))
}
