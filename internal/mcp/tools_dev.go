package mcp

import (
	"context"
	"encoding/json"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/Daybeam/ghost-driver/internal/browser"
)

// registerDevTools registers UI measurement/inspection tools for frontend
// development assistance. These are completely unrelated to browser automation
// — they measure and inspect UI properties (design tokens, accessibility,
// visual regression, content structure, telemetry).
//
// Gated behind the "dev" toolset. Enable with GHOST_TOOLSETS=dev or
// GHOST_TOOLSETS=all.
func (s *State) registerDevTools(srv *server.MCPServer) {
	// ── get_accessibility_tree ───────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("get_accessibility_tree",
		mcpgo.WithDescription("Get the full accessibility tree of the page."),
	), s.auditWrapper("get_accessibility_tree", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}
		tree, err := b.GetAccessibilityTree(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		return mcpgo.NewToolResultText(string(tree)), nil
	}))

	// ── get_content_anchors ───────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("get_content_anchors",
		mcpgo.WithDescription(`Extract or retrieve deterministic content anchors (headings, tables count, links, first paragraph snippet) without extracting full page content.`),
		mcpgo.WithString("selector"),
	), s.auditWrapper("get_content_anchors", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}
		sel := req.GetString("selector", "")
		anchors, err := b.ExtractContentAnchors(ctx, sel)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		currentURL, title, _ := b.PageInfo(ctx)
		if s.topology != nil {
			s.topology.SaveContentAnchors(currentURL, title, anchors)
		}
		out, err := json.MarshalIndent(anchors, "", "  ")
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		return mcpgo.NewToolResultText(string(out)), nil
	}))

	// ── get_design_tokens ─────────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("get_design_tokens",
		mcpgo.WithDescription(`Extract design system tokens (colors, fonts, variables) for a selector.`),
		mcpgo.WithString("selector"),
	), s.auditWrapper("get_design_tokens", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}
		sel := req.GetString("selector", "")
		tokens, err := b.GetDesignTokens(ctx, sel)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		data, _ := json.MarshalIndent(tokens, "", "  ")
		return mcpgo.NewToolResultText(string(data)), nil
	}))

	// ── scan_interactions ─────────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("scan_interactions",
		mcpgo.WithDescription(`Analyze how styles change when an element is hovered.`),
		mcpgo.WithString("selector", mcpgo.Required()),
	), s.auditWrapper("scan_interactions", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, input, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}
		sel, err := req.RequireString("selector")
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}

		// 1. Capture Base State
		base, err := b.GetDesignTokens(ctx, sel)
		if err != nil {
			return mcpgo.NewToolResultError("base state capture failed: " + err.Error()), nil
		}

		// 2. Perform Hover
		var x, y float64
		ok, err := getCoordBySelector(ctx, b, sel, &x, &y)
		if err != nil || !ok {
			return mcpgo.NewToolResultError("could not locate element for hover"), nil
		}
		if err := input.Hover(ctx, x, y); err != nil {
			return mcpgo.NewToolResultError("hover failed: " + err.Error()), nil
		}

		// Wait for animations
		time.Sleep(300 * time.Millisecond)

		// 3. Capture Hover State
		hovered, err := b.GetDesignTokens(ctx, sel)
		if err != nil {
			return mcpgo.NewToolResultError("hover state capture failed: " + err.Error()), nil
		}

		// 4. Diff
		diff := browser.DiffDesignTokens(base, hovered)
		data, _ := json.MarshalIndent(diff, "", "  ")
		return mcpgo.NewToolResultText(string(data)), nil
	}))

	// ── compare_visual_state ──────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("compare_visual_state",
		mcpgo.WithDescription(`Return the visual hash (SHA-256) of the current viewport.
If a reference_hash is provided, it also returns if they match.`),
		mcpgo.WithString("reference_hash"),
	), s.auditWrapper("compare_visual_state", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}
		ref := req.GetString("reference_hash", "")
		hash, match, err := b.CompareVisualState(ctx, ref)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}

		res := struct {
			Hash  string `json:"hash"`
			Match bool   `json:"match"`
		}{Hash: hash, Match: match}
		data, _ := json.Marshal(res)
		return mcpgo.NewToolResultText(string(data)), nil
	}))

	// ── get_tool_analytics ────────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("get_tool_analytics",
		mcpgo.WithDescription("Get tool usage statistics and latency telemetry."),
	), s.auditWrapper("get_tool_analytics", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		stats := s.auditor.GetStats()
		data, _ := json.MarshalIndent(stats, "", "  ")
		return mcpgo.NewToolResultText(string(data)), nil
	}))
}
