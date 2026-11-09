package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
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
		mcpgo.WithDescription(`Return the visual hash (SHA-256) of the current viewport or a specific element.
If a reference_hash is provided, it also returns if they match.
If a selector is provided, only the element region is hashed (enables region-specific visual regression).`),
		mcpgo.WithString("reference_hash"),
		mcpgo.WithString("selector"),
	), s.auditWrapper("compare_visual_state", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}
		ref := req.GetString("reference_hash", "")
		sel := req.GetString("selector", "")

		var hash string
		var match bool
		if sel != "" {
			png, err := b.CaptureElement(ctx, sel)
			if err != nil {
				return mcpgo.NewToolResultError(err.Error()), nil
			}
			hash = fmt.Sprintf("%x", sha256.Sum256(png))
			match = ref != "" && hash == ref
		} else {
			hash, match, err = b.CompareVisualState(ctx, ref)
			if err != nil {
				return mcpgo.NewToolResultError(err.Error()), nil
			}
		}

		res := struct {
			Hash  string `json:"hash"`
			Match bool   `json:"match"`
		}{Hash: hash, Match: match}
		data, _ := json.Marshal(res)
		return mcpgo.NewToolResultText(string(data)), nil
	}))

	// ── pixel_diff ────────────────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("pixel_diff",
		mcpgo.WithDescription(`Compare current screenshot against a reference image pixel-by-pixel.
Returns diff bounding box (where), diff percentage, max color distance, and current hash.
If reference_image (base64 PNG) is not provided, just returns the current screenshot hash.
Use selector to limit capture to a specific element region.
threshold (0.0-1.0) sets the diff percentage above which match=false (default 0.01 = 1%).`),
		mcpgo.WithString("reference_image"),
		mcpgo.WithString("selector"),
		mcpgo.WithNumber("threshold"),
	), s.auditWrapper("pixel_diff", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}
		refImg := req.GetString("reference_image", "")
		sel := req.GetString("selector", "")
		threshold := req.GetFloat("threshold", 0.01)

		result, err := b.PixelDiff(ctx, refImg, sel, threshold)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		data, _ := json.MarshalIndent(result, "", "  ")
		return mcpgo.NewToolResultText(string(data)), nil
	}))

	// ── diff_dom_tree ─────────────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("diff_dom_tree",
		mcpgo.WithDescription(`Compare current accessibility tree against a reference tree (JSON from get_accessibility_tree).
Returns node-level differences: added nodes, removed nodes, and changed properties (role, name, focusable, etc.).
If reference_tree is not provided, returns the current AX tree with no diffs.`),
		mcpgo.WithString("reference_tree"),
	), s.auditWrapper("diff_dom_tree", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}
		refTree := req.GetString("reference_tree", "")

		result, err := b.DiffDOMTree(ctx, json.RawMessage(refTree))
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		data, _ := json.MarshalIndent(result, "", "  ")
		return mcpgo.NewToolResultText(string(data)), nil
	}))

	// ── sample_pixels ─────────────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("sample_pixels",
		mcpgo.WithDescription(`Sample pixels at grid intervals within a region and compute color distance from a reference color.
Returns RGB values and color distance for each sample point.
selector limits sampling to an element region. grid sets samples per axis (default 10 = 100 samples).
reference_color is a hex string like "#FF0000".`),
		mcpgo.WithString("selector"),
		mcpgo.WithNumber("grid"),
		mcpgo.WithString("reference_color"),
	), s.auditWrapper("sample_pixels", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}
		sel := req.GetString("selector", "")
		grid := int(req.GetFloat("grid", 10))
		refColor := req.GetString("reference_color", "#FFFFFF")

		samples, err := b.SamplePixels(ctx, sel, nil, grid, refColor)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		data, _ := json.MarshalIndent(samples, "", "  ")
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
