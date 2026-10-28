package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/Daybeam/ghost-driver/internal/browser"
)

func (s *State) registerExtractTools(srv *server.MCPServer) {
	srv.AddTool(mcpgo.NewTool("extract_virtual_list",
		mcpgo.WithDescription(`Collect all items from a virtual list.`),
		mcpgo.WithString("container_selector"),
		mcpgo.WithString("item_selector", mcpgo.Required()),
		mcpgo.WithNumber("max_items"),
		mcpgo.WithNumber("scroll_step"),
		mcpgo.WithNumber("settle_ms"),
	), s.auditWrapper("extract_virtual_list", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}

		containerSel := req.GetString("container_selector", "")
		itemSel, err := req.RequireString("item_selector")
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		maxItems := int(req.GetFloat("max_items", 0))
		scrollStep := req.GetFloat("scroll_step", 400.0)
		settleMs := req.GetFloat("settle_ms", 400.0)

		cfg := browser.VirtualListConfig{
			ContainerSelector: containerSel,
			ItemSelector:      itemSel,
			MaxItems:          maxItems,
			ScrollStep:        scrollStep,
			SettleDelay:       time.Duration(settleMs) * time.Millisecond,
			StopIfNoNew:       3,
		}

		items, err := b.ExtractVirtualList(ctx, cfg)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}

		var lines []string
		for _, item := range items {
			lines = append(lines, fmt.Sprintf("[%d] %s", item.Index, item.Text))
		}
		return mcpgo.NewToolResultText(
			fmt.Sprintf("extracted %d items:\n%s", len(items), joinLines(lines))), nil
	}))

	// ── get_content (consolidated: text/markdown/html) ─────────────────────
	srv.AddTool(mcpgo.NewTool("get_content",
		mcpgo.WithDescription(`Retrieve page content. Default format: markdown (rendered DOM → Markdown).`),
		mcpgo.WithString("format", mcpgo.Enum("text", "markdown", "html"), mcpgo.Description("text: plain text; markdown: HTML→Markdown (default); html: outer HTML")),
		mcpgo.WithString("selector"),
		mcpgo.WithString("mode", mcpgo.Enum("full", "article", "controls"), mcpgo.Description("Markdown mode (format=markdown only). Default: full")),
	), s.auditWrapper("get_content", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}
		format := req.GetString("format", "markdown")
		sel := req.GetString("selector", "")
		var content string
		switch format {
		case "text":
			if sel != "" {
				content, err = b.InnerText(ctx, sel)
			} else {
				content, err = b.GetAllText(ctx)
			}
		case "html":
			content, err = b.OuterHTML(ctx, sel)
		default:
			content, err = b.HTMLToMarkdown(ctx, sel, req.GetString("mode", "full"))
		}
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if format != "html" {
			currentURL, _, _ := b.PageInfo(ctx)
			budget := browser.DefaultBrowserOutputBudget()
			content = budget.Apply(content, currentURL)
		}
		return mcpgo.NewToolResultText(content), nil
	}))

	if os.Getenv("GHOST_LEGACY_TOOLS") != "" {
	// ── get_text (legacy) ───────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("get_text",
		mcpgo.WithDescription(`Extract page text.`),
		mcpgo.WithString("selector"),
	), s.auditWrapper("get_text", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}
		sel := req.GetString("selector", "")
		var text string
		if sel != "" {
			text, err = b.InnerText(ctx, sel)
		} else {
			text, err = b.GetAllText(ctx)
		}
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		currentURL, _, _ := b.PageInfo(ctx)
		budget := browser.DefaultBrowserOutputBudget()
		return mcpgo.NewToolResultText(budget.Apply(text, currentURL)), nil
	}))

	// ── get_markdown ──────────────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("get_markdown",
		mcpgo.WithDescription(`Convert page HTML to Markdown. Supported modes: "full" (default), "article" (content only, strips nav/footer/aside), "controls" (list of interactive controls for navigation).`),
		mcpgo.WithString("selector"),
		mcpgo.WithString("mode", mcpgo.Enum("full", "article", "controls")),
	), s.auditWrapper("get_markdown", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}
		sel := req.GetString("selector", "")
		mode := req.GetString("mode", "full")
		md, err := b.HTMLToMarkdown(ctx, sel, mode)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		currentURL, _, _ := b.PageInfo(ctx)
		budget := browser.DefaultBrowserOutputBudget()
		return mcpgo.NewToolResultText(budget.Apply(md, currentURL)), nil
	}))

	// ── get_html ──────────────────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("get_html",
		mcpgo.WithDescription(`Extract page HTML.`),
		mcpgo.WithString("selector"),
	), s.auditWrapper("get_html", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}
		sel := req.GetString("selector", "")
		html, err := b.OuterHTML(ctx, sel)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		return mcpgo.NewToolResultText(html), nil
	}))
	} // end legacy tools

	// ── get_metadata ──────────────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("get_metadata",
		mcpgo.WithDescription(`Extract page metadata (OG tags, etc.).`),
	), s.auditWrapper("get_metadata", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}
		meta, err := b.GetMetadata(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		data, _ := json.Marshal(meta)
		return mcpgo.NewToolResultText(string(data)), nil
	}))

	// ── wait_smart ────────────────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("wait_smart",
		mcpgo.WithDescription(`Wait for SPA page to stabilise.`),
		mcpgo.WithNumber("max_ms"),
	), s.auditWrapper("wait_smart", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}
		cfg := browser.DefaultWaitConfig()
		if v := req.GetFloat("max_ms", 0); v > 0 {
			cfg.MaxWait = time.Duration(v) * time.Millisecond
		}
		b.SmartWait(ctx, cfg)
		return mcpgo.NewToolResultText("stable"), nil
	}))

	// ── wait_for_selector ────────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("wait_for_selector",
		mcpgo.WithDescription(`Wait for CSS selector to appear.`),
		mcpgo.WithString("selector", mcpgo.Required()),
		mcpgo.WithNumber("timeout_ms"),
	), s.auditWrapper("wait_for_selector", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, _, err := s.ensureConnected(ctx)
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
		timeout := time.Duration(req.GetFloat("timeout_ms", 10000)) * time.Millisecond
		if err := b.WaitForSelector(ctx, sel, timeout); err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		return mcpgo.NewToolResultText("selector found"), nil
	}))

	// ── wait_for_text ────────────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("wait_for_text",
		mcpgo.WithDescription(`Wait for text to appear on page.`),
		mcpgo.WithString("text", mcpgo.Required()),
		mcpgo.WithNumber("timeout_ms"),
	), s.auditWrapper("wait_for_text", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}
		text, err := req.RequireString("text")
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		timeout := time.Duration(req.GetFloat("timeout_ms", 10000)) * time.Millisecond
		if err := b.WaitForText(ctx, text, timeout); err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		return mcpgo.NewToolResultText("text found"), nil
	}))
}
