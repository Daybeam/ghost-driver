package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func (s *State) registerPageTools(srv *server.MCPServer) {
	srv.AddTool(mcpgo.NewTool("eval_js",
		mcpgo.WithDescription("Execute JavaScript. If script_path is provided, injects the JS file first, then evaluates expression (if provided)."),
		mcpgo.WithString("expression", mcpgo.Description("JS expression to evaluate. Required unless script_path is used alone.")),
		mcpgo.WithString("script_path", mcpgo.Description("Path to a local JS file to inject before evaluating expression.")),
	), s.auditWrapper("eval_js", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}
		scriptPath := req.GetString("script_path", "")
		if scriptPath != "" {
			if err := b.InjectScript(ctx, scriptPath); err != nil {
				return mcpgo.NewToolResultError(err.Error()), nil
			}
		}
		expr := req.GetString("expression", "")
		if expr == "" {
			if scriptPath != "" {
				return mcpgo.NewToolResultText("Script injected successfully"), nil
			}
			return mcpgo.NewToolResultError("either expression or script_path is required"), nil
		}
		result, err := b.EvalJS(ctx, expr)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		return mcpgo.NewToolResultText(string(result)), nil
	}))

	// ── page_info ─────────────────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("page_info",
		mcpgo.WithDescription("Return current URL, title, and SOP context hints (记忆 & 直觉)."),
	), s.auditWrapper("page_info", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}
		u, title, err := b.PageInfo(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}

		var hints []string

		// 1. Dynamic Safety Hints (Rule 1)
		if b.Dirty() {
			hints = append(hints, "⚠️ WARNING (Rule 1): Form data has been modified. DO NOT NAVIGATE in this tab. Use 'new_tab' or finish submission first.")
		}

		// 2. Static Profile Hints
		if prof := s.registry.Match(u); prof != nil {
			for _, h := range prof.Hints {
				hints = append(hints, "💡 PRO-TIP: "+h)
			}
		}

		// 3. Topological Insights (Dead End Memory)
		if s.topology != nil {
			if s.topology.IsDeadEnd(u, title) {
				hints = append(hints, "🛑 DANGER: This state was previously marked as a DEAD END. You should probably back off.")
			}
		}

		resp := struct {
			URL   string   `json:"url"`
			Title string   `json:"title"`
			Hints []string `json:"hints"`
		}{URL: u, Title: title, Hints: hints}

		data, _ := json.MarshalIndent(resp, "", "  ")
		return mcpgo.NewToolResultText(string(data)), nil
	}))

	// ── capture_api_calls ────────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("capture_api_calls",
		mcpgo.WithDescription(`Intercept and return the response body of the next API call matching the pattern.`),
		mcpgo.WithString("url_pattern", mcpgo.Required(), mcpgo.Description("Substring or regex to match the request URL.")),
		mcpgo.WithNumber("timeout_ms", mcpgo.Description("Wait time in ms. Default 10000.")),
	), s.auditWrapper("capture_api_calls", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}
		pattern, err := req.RequireString("url_pattern")
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		timeout := time.Duration(req.GetFloat("timeout_ms", 10000)) * time.Millisecond

		body, err := b.CaptureNetworkCall(ctx, pattern, timeout)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		return mcpgo.NewToolResultText(body), nil
	}))

	// ── handle_dialog ────────────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("handle_dialog",
		mcpgo.WithDescription(`Manually accept or dismiss a JavaScript dialog (alert, confirm, prompt, beforeunload).`),
		mcpgo.WithBoolean("accept", mcpgo.Required(), mcpgo.Description("Whether to accept/OK the dialog.")),
		mcpgo.WithString("prompt_text", mcpgo.Description("Text to enter into a prompt dialog.")),
	), s.auditWrapper("handle_dialog", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}
		accept, err := req.RequireBool("accept")
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		promptText := req.GetString("prompt_text", "")

		if err := b.HandleDialog(ctx, accept, promptText); err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		return mcpgo.NewToolResultText("dialog handled"), nil
	}))

	// ── browser_fetch ────────────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("browser_fetch",
		mcpgo.WithDescription(`Execute a fetch() request within the browser context.
Leverages existing session cookies/auth. Perfect for Rule 2 (API > Form filling).`),
		mcpgo.WithString("url", mcpgo.Required()),
		mcpgo.WithString("method", mcpgo.Description("GET, POST, PUT, DELETE, etc. Default GET.")),
		mcpgo.WithString("body", mcpgo.Description("Request body string.")),
		mcpgo.WithString("headers", mcpgo.Description("JSON string of headers.")),
		mcpgo.WithBoolean("is_form", mcpgo.Description("If true, treats body as URLSearchParams (form-encoded).")),
	), s.auditWrapper("browser_fetch", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}
		urlStr, err := req.RequireString("url")
		if err != nil {
			return mcpgo.NewToolResultError("missing required parameter: url"), nil
		}
		method := req.GetString("method", "GET")
		body := req.GetString("body", "")
		headersJSON := req.GetString("headers", "{}")

		isForm := false
		if args, ok := req.Params.Arguments.(map[string]any); ok {
			if val, ok := args["is_form"]; ok {
				if b, ok := val.(bool); ok {
					isForm = b
				}
			}
		}

		js := fmt.Sprintf(`
(async () => {
	const controller = new AbortController();
	const timeoutId = setTimeout(() => controller.abort(), 15000);
	const headers = %s;
	const options = {
		method: %q,
		headers: headers,
		credentials: 'include',
		signal: controller.signal
	};
	if (%v && %q !== "") {
		options.body = new URLSearchParams(%q).toString();
		options.headers['Content-Type'] = 'application/x-www-form-urlencoded';
	} else if (%q !== "") {
		options.body = %q;
	}

	try {
		const resp = await fetch(%q, options);
		clearTimeout(timeoutId);
		const text = await resp.text();
		return JSON.stringify({
			status: resp.status,
			ok: resp.ok,
			body: text
		});
	} catch (e) {
		clearTimeout(timeoutId);
		return JSON.stringify({error: e.toString()});
	}
})()`, headersJSON, method, isForm, body, body, body, body, urlStr)

		resRaw, err := b.EvalJS(ctx, js)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		return mcpgo.NewToolResultText(string(resRaw)), nil
	}))
}
