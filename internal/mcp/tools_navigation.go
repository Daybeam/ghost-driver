package mcp

import (
	"context"
	"fmt"
	"strings"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/Daybeam/ghost-driver/internal/browser"
)

func (s *State) registerNavigationTools(srv *server.MCPServer) {
	srv.AddTool(mcpgo.NewTool("navigate",
		mcpgo.WithDescription(`Navigate to URL or a known site name (e.g. 'chatgpt').
ANTI-BOT TIP: For public sites, prefer starting at the homepage and clicking through. Direct deep-linking is a strong bot signal.`),
		mcpgo.WithString("url", mcpgo.Required()),
	), s.auditWrapper("navigate", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}
		dest, err := req.RequireString("url")
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		return s.withTopology(ctx, b, "navigate", map[string]string{"url": dest}, func() (*mcpgo.CallToolResult, error) {
			if prof := s.registry.GetByName(dest); prof != nil {
				dest = prof.EntryURL
			}
		if err := b.Navigate(ctx, dest); err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		u, title, _ := b.PageInfo(ctx)
		if strings.HasPrefix(u, "chrome-error://") {
			return mcpgo.NewToolResultError(fmt.Sprintf("navigation error: %s (%s)", u, title)), nil
		}
		return mcpgo.NewToolResultText(fmt.Sprintf("→ %s (%s)", u, title)), nil
		})
	}))

	// ── navigate_back ────────────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("navigate_back",
		mcpgo.WithDescription("Navigate back in history."),
	), s.auditWrapper("navigate_back", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}
		if err := b.NavigateBack(ctx); err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		return mcpgo.NewToolResultText("navigated back"), nil
	}))

	// ── navigate_forward ─────────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("navigate_forward",
		mcpgo.WithDescription("Navigate forward in history."),
	), s.auditWrapper("navigate_forward", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}
		if err := b.NavigateForward(ctx); err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		return mcpgo.NewToolResultText("navigated forward"), nil
	}))

	// ── reload ───────────────────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("reload",
		mcpgo.WithDescription("Reload current page."),
	), s.auditWrapper("reload", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}
		if err := b.Reload(ctx); err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		return mcpgo.NewToolResultText("reloaded"), nil
	}))

	// ── screenshot ────────────────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("screenshot",
		mcpgo.WithDescription("Capture viewport or element as base64 image."),
		mcpgo.WithString("format", mcpgo.Enum("png", "jpeg", "webp")),
		mcpgo.WithNumber("quality"),
		mcpgo.WithBoolean("full_page"),
		mcpgo.WithString("selector"),
	), s.auditWrapper("screenshot", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}

		format := req.GetString("format", "png")
		quality := int(req.GetFloat("quality", 0))
		fullPage := req.GetBool("full_page", false)
		selector := req.GetString("selector", "")

		var b64 string

		if selector != "" {
			b64, err = b.CaptureElementBase64(ctx, selector)
		} else {
			b64, err = b.ScreenshotBase64(ctx, browser.ScreenshotOptions{
				Format:                format,
				Quality:               quality,
				CaptureBeyondViewport: fullPage,
			})
		}

		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}

		mime := "image/png"
		if format == "jpeg" {
			mime = "image/jpeg"
		} else if format == "webp" {
			mime = "image/webp"
		}

		res := mcpgo.NewToolResultText("screenshot captured")
		res.Content = append(res.Content, mcpgo.ImageContent{
			Type:     "image",
			Data:     b64,
			MIMEType: mime,
		})
		return res, nil
	}))
}
