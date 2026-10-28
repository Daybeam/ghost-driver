package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/Daybeam/ghost-driver/internal/browser"
	"github.com/Daybeam/ghost-driver/internal/cdp"
)

func (s *State) registerContentTools(srv *server.MCPServer) {
	// ── type_text ─────────────────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("type_text",
		mcpgo.WithDescription(`Type text into the focused element.
STEALTH HINT: Use randomized pauses (3-5s) between form fields on sensitive platforms.`),
		mcpgo.WithString("text", mcpgo.Required()),
	), s.auditWrapper("type_text", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, input, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}
		return s.withRecording(ctx, b, func() (*mcpgo.CallToolResult, error) {
			text, err := req.RequireString("text")
			if err != nil {
				return mcpgo.NewToolResultError(err.Error()), nil
			}
			if err := input.TypeText(ctx, text); err != nil {
				return mcpgo.NewToolResultError(err.Error()), nil
			}
			return mcpgo.NewToolResultText("typed"), nil
		})
	}))

	// ── scroll ────────────────────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("scroll",
		mcpgo.WithDescription("Scroll page."),
		mcpgo.WithNumber("dy", mcpgo.Required()),
		mcpgo.WithNumber("x"),
		mcpgo.WithNumber("y"),
		mcpgo.WithString("container_selector"),
	), s.auditWrapper("scroll", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, input, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}
		dy, err := req.RequireFloat("dy")
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		x := req.GetFloat("x", 0)
		y := req.GetFloat("y", 0)
		containerSel := req.GetString("container_selector", "")

		if containerSel != "" {
			js := fmt.Sprintf(`(function(){ var el = document.querySelector(%q); if (el) { el.scrollTop += %f; return "container"; } return "not found"; })()`, containerSel, dy)
			raw, err := b.EvalJS(ctx, js)
			if err != nil {
				return mcpgo.NewToolResultError(err.Error()), nil
			}
			var resStr string
			json.Unmarshal(raw, &resStr)
			return mcpgo.NewToolResultText("scrolled " + resStr + fmt.Sprintf(" dy=%.0f", dy)), nil
		}

		if err := input.Scroll(ctx, x, y, 0, dy); err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		return mcpgo.NewToolResultText(fmt.Sprintf("scrolled dy=%.0f", dy)), nil
	}))

	// ── read_storage (consolidated: local/session) ──────────────────────────
	srv.AddTool(mcpgo.NewTool("read_storage",
		mcpgo.WithDescription(`Read all items from localStorage or sessionStorage.`),
		mcpgo.WithString("scope", mcpgo.Enum("local", "session"), mcpgo.Description("Storage scope. Default: local")),
	), s.auditWrapper("read_storage", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}
		var items map[string]string
		if req.GetString("scope", "local") == "session" {
			items, err = b.GetSessionStorage(ctx)
		} else {
			items, err = b.GetLocalStorage(ctx)
		}
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		data, _ := json.Marshal(items)
		return mcpgo.NewToolResultText(string(data)), nil
	}))

	// ── write_storage (consolidated: local/session) ─────────────────────────
	srv.AddTool(mcpgo.NewTool("write_storage",
		mcpgo.WithDescription(`Set an item in localStorage or sessionStorage.`),
		mcpgo.WithString("scope", mcpgo.Enum("local", "session"), mcpgo.Description("Storage scope. Default: local")),
		mcpgo.WithString("key", mcpgo.Required()),
		mcpgo.WithString("value", mcpgo.Required()),
	), s.auditWrapper("write_storage", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}
		key, _ := req.RequireString("key")
		val, _ := req.RequireString("value")
		scope := req.GetString("scope", "local")
		if scope == "session" {
			err = b.SetSessionStorage(ctx, key, val)
		} else {
			err = b.SetLocalStorage(ctx, key, val)
		}
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		return mcpgo.NewToolResultText(fmt.Sprintf("set %sStorage[%s]=%s", scope, key, val)), nil
	}))

	// ── remove_storage (consolidated: local/session) ────────────────────────
	srv.AddTool(mcpgo.NewTool("remove_storage",
		mcpgo.WithDescription(`Remove an item from localStorage or sessionStorage.`),
		mcpgo.WithString("scope", mcpgo.Enum("local", "session"), mcpgo.Description("Storage scope. Default: local")),
		mcpgo.WithString("key", mcpgo.Required()),
	), s.auditWrapper("remove_storage", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}
		key, _ := req.RequireString("key")
		scope := req.GetString("scope", "local")
		if scope == "session" {
			err = b.RemoveSessionStorage(ctx, key)
		} else {
			err = b.RemoveLocalStorage(ctx, key)
		}
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		return mcpgo.NewToolResultText(fmt.Sprintf("removed %sStorage[%s]", scope, key)), nil
	}))

	if os.Getenv("GHOST_LEGACY_TOOLS") != "" {
	// ── get_local_storage (legacy) ──────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("get_local_storage",
		mcpgo.WithDescription("Get all items in localStorage."),
	), s.auditWrapper("get_local_storage", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}
		items, err := b.GetLocalStorage(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		data, _ := json.Marshal(items)
		return mcpgo.NewToolResultText(string(data)), nil
	}))

	// ── set_local_storage ────────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("set_local_storage",
		mcpgo.WithDescription("Set an item in localStorage."),
		mcpgo.WithString("key", mcpgo.Required()),
		mcpgo.WithString("value", mcpgo.Required()),
	), s.auditWrapper("set_local_storage", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}
		key, err := req.RequireString("key")
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		val, err := req.RequireString("value")
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if err := b.SetLocalStorage(ctx, key, val); err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		return mcpgo.NewToolResultText(fmt.Sprintf("set localStorage[%s]=%s", key, val)), nil
	}))

	// ── remove_local_storage ─────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("remove_local_storage",
		mcpgo.WithDescription("Remove an item from localStorage."),
		mcpgo.WithString("key", mcpgo.Required()),
	), s.auditWrapper("remove_local_storage", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}
		key, err := req.RequireString("key")
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if err := b.RemoveLocalStorage(ctx, key); err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		return mcpgo.NewToolResultText(fmt.Sprintf("removed localStorage[%s]", key)), nil
	}))

	// ── get_session_storage ──────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("get_session_storage",
		mcpgo.WithDescription("Get all items in sessionStorage."),
	), s.auditWrapper("get_session_storage", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}
		items, err := b.GetSessionStorage(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		data, _ := json.Marshal(items)
		return mcpgo.NewToolResultText(string(data)), nil
	}))

	// ── set_session_storage ──────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("set_session_storage",
		mcpgo.WithDescription("Set an item in sessionStorage."),
		mcpgo.WithString("key", mcpgo.Required()),
		mcpgo.WithString("value", mcpgo.Required()),
	), s.auditWrapper("set_session_storage", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}
		key, err := req.RequireString("key")
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		val, err := req.RequireString("value")
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if err := b.SetSessionStorage(ctx, key, val); err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		return mcpgo.NewToolResultText(fmt.Sprintf("set sessionStorage[%s]=%s", key, val)), nil
	}))

	// ── remove_session_storage ───────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("remove_session_storage",
		mcpgo.WithDescription("Remove an item from sessionStorage."),
		mcpgo.WithString("key", mcpgo.Required()),
	), s.auditWrapper("remove_session_storage", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}
		key, err := req.RequireString("key")
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if err := b.RemoveSessionStorage(ctx, key); err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		return mcpgo.NewToolResultText(fmt.Sprintf("removed sessionStorage[%s]", key)), nil
	}))
	} // end legacy tools

	// ── list_tabs ────────────────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("list_tabs",
		mcpgo.WithDescription("List all open tabs."),
	), s.auditWrapper("list_tabs", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		_, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		s.mu.RLock()
		mgr := s.mgr
		activeID := s.activeID
		s.mu.RUnlock()
		if mgr == nil {
			return mcpgo.NewToolResultError("no browser manager"), nil
		}
		targets, err := cdp.ListTargets(mgr.DevToolsHTTP())
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		var lines []string
		for _, t := range targets {
			status := ""
			if t.ID == activeID {
				status = " [ACTIVE]"
			}
			lines = append(lines, fmt.Sprintf("- %s: %s (%s)%s", t.ID, t.Title, t.URL, status))
		}
		return mcpgo.NewToolResultText(fmt.Sprintf("tabs (%d):\n%s", len(targets), joinLines(lines))), nil
	}))

	// ── new_tab ──────────────────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("new_tab",
		mcpgo.WithDescription("Open a new tab."),
		mcpgo.WithString("url"),
	), s.auditWrapper("new_tab", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		_, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.mgr == nil {
			return mcpgo.NewToolResultError("no browser manager"), nil
		}
		urlStr := req.GetString("url", "")
		b, err := s.mgr.NewTab(ctx, urlStr)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if !b.IsHealthy(ctx) {
			b.Close()
			return mcpgo.NewToolResultError("new tab created but is unresponsive to JS execution (zombie)"), nil
		}

		targetID := b.TargetID()
		s.browsers[targetID] = b
		s.inputs[targetID] = browser.NewInput(b, browser.InputModeL2)
		s.activeID = targetID

		return mcpgo.NewToolResultText(fmt.Sprintf("opened new tab: %s", targetID)), nil
	}))

	// ── switch_tab ───────────────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("switch_tab",
		mcpgo.WithDescription("Switch focus to a specific tab."),
		mcpgo.WithString("target_id", mcpgo.Required()),
	), s.auditWrapper("switch_tab", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		_, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		targetID, err := req.RequireString("target_id")
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}

		if existing, ok := s.browsers[targetID]; ok {
			if !existing.IsHealthy(ctx) {
				return mcpgo.NewToolResultError(fmt.Sprintf("target %s is a zombie (connected but unresponsive to JS execution)", targetID)), nil
			}
		} else {
			if s.mgr == nil {
				return mcpgo.NewToolResultError("browser manager not initialized (call connect_browser first)"), nil
			}
			targets, err := cdp.ListTargets(s.mgr.DevToolsHTTP())
			if err != nil {
				return mcpgo.NewToolResultError(err.Error()), nil
			}
			var wsURL string
			for _, t := range targets {
				if t.ID == targetID {
					wsURL = t.WebSocketDebuggerURL
					break
				}
			}
			if wsURL == "" {
				return mcpgo.NewToolResultError("target not found: " + targetID), nil
			}
			b, err := browser.New(ctx, s.mgr.DevToolsHTTP(), wsURL, targetID, s.cfg.StealthEnabled, s.registry, false)
			if err != nil {
				return mcpgo.NewToolResultError(err.Error()), nil
			}
			if !b.IsHealthy(ctx) {
				b.Close()
				return mcpgo.NewToolResultError(fmt.Sprintf("target %s is a zombie (connected but unresponsive to JS execution)", targetID)), nil
			}
			s.browsers[targetID] = b
			s.inputs[targetID] = browser.NewInput(b, browser.InputModeL2)
		}

		s.activeID = targetID
		return mcpgo.NewToolResultText("switched to tab: " + targetID), nil
	}))

	// ── close_tab ────────────────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("close_tab",
		mcpgo.WithDescription("Close a specific tab."),
		mcpgo.WithString("target_id"),
	), s.auditWrapper("close_tab", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		_, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		targetID := req.GetString("target_id", s.activeID)
		if targetID == "" {
			return mcpgo.NewToolResultError("no active tab to close"), nil
		}

		if err := s.mgr.CloseTab(targetID); err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}

		if b, ok := s.browsers[targetID]; ok {
			b.Close()
			delete(s.browsers, targetID)
			delete(s.inputs, targetID)
		}
		if s.activeID == targetID {
			s.activeID = ""
			for id := range s.browsers {
				s.activeID = id
				break
			}
		}

		return mcpgo.NewToolResultText("closed tab: " + targetID), nil
	}))
}
