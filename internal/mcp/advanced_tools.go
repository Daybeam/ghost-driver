//go:build !lite
package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/Daybeam/ghost-driver/internal/browser"
)

func (s *State) registerAdvancedTools(srv *server.MCPServer) {
	// ── get_topology_map ─────────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("get_topology_map",
		mcpgo.WithDescription("Returns the recorded state-action-state graph (Topological Map). Pass 'domain' to filter."),
		mcpgo.WithString("domain", mcpgo.Description("Filter by domain, e.g. 'github.com'")),
	), s.auditWrapper("get_topology_map", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		if s.topology == nil {
			return mcpgo.NewToolResultError("topology memory not initialized (check database permissions)"), nil
		}
		domain := req.GetString("domain", "")
		var res map[string]any
		var err error
		if domain != "" {
			res, err = s.topology.GetDomainMap(domain)
		} else {
			res, err = s.topology.GetFullMap()
		}
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if nodes, ok := res["nodes"].([]browser.Node); ok {
			for i := range nodes {
				nodes[i].ContentAnchors = ""
			}
			res["nodes"] = nodes
		}
		data, _ := json.MarshalIndent(res, "", "  ")
		return mcpgo.NewToolResultText(string(data)), nil
	}))

	// ── mark_dead_end ────────────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("mark_dead_end",
		mcpgo.WithDescription("Marks the current page as a 'Dead End' in the topological map to avoid future navigation to this state."),
	), s.auditWrapper("mark_dead_end", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		_, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if s.topology == nil {
			return mcpgo.NewToolResultError("topology memory not initialized"), nil
		}
		b, _ := s.getActive()
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}
		u, title, _ := b.PageInfo(ctx)
		s.topology.MarkDeadEnd(u, title)
		return mcpgo.NewToolResultText(fmt.Sprintf("State %s marked as Dead End", title)), nil
	}))

	// ── start_recording ──────────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("start_recording",
		mcpgo.WithDescription(`Start video recording.`),
		mcpgo.WithNumber("fps"),
	), s.auditWrapper("start_recording", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}
		fps := int(req.GetFloat("fps", 8))
		rec, err := browser.NewRecorder(b, fps)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		s.mu.Lock()
		old := s.recorder
		s.recorder = rec
		s.mu.Unlock()
		// Stop and clean up a replaced recorder so its frames dir doesn't leak.
		if old != nil {
			old.Stop()
			old.Cleanup()
		}
		rec.Start(ctx)
		return mcpgo.NewToolResultText("recording started"), nil
	}))

	// ── stop_recording ───────────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("stop_recording",
		mcpgo.WithDescription(`Stop recording and render video.`),
		mcpgo.WithString("out_path", mcpgo.Required()),
	), s.auditWrapper("stop_recording", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		s.mu.Lock()
		rec := s.recorder
		s.recorder = nil
		s.mu.Unlock()
		if rec == nil {
			return mcpgo.NewToolResultError("no active recording"), nil
		}
		rec.Stop()
		outPath, err := req.RequireString("out_path")
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if err := rec.Render(outPath); err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		abs, _ := filepath.Abs(outPath)
		return mcpgo.NewToolResultText("video rendered to: " + abs), nil
	}))

	// ── vision_analyze ───────────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("vision_analyze",
		mcpgo.WithDescription(`Use VLM to find coordinates of an element.`),
		mcpgo.WithString("description", mcpgo.Required()),
		mcpgo.WithString("image_base64"),
	), s.auditWrapper("vision_analyze", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}

		var png []byte
		if b64 := req.GetString("image_base64", ""); b64 != "" {
			png, err = base64.StdEncoding.DecodeString(b64)
		} else {
			// VLM likes WebP or low-quality JPEG for speed
			png, err = b.Screenshot(ctx, browser.ScreenshotOptions{
				Format:  "webp",
				Quality: 80,
			})
		}

		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}

		desc, err := req.RequireString("description")
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if s.visionFinder == nil {
			return mcpgo.NewToolResultError("vision/VLM not configured (set VLM_API_KEY or GEMINI_API_KEY)"), nil
		}
		coord, err := s.visionFinder.Locate(ctx, png, desc)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}

		data, _ := json.Marshal(coord)
		return mcpgo.NewToolResultText(string(data)), nil
	}))
}
