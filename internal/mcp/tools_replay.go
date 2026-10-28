package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/Daybeam/ghost-driver/internal/browser"
)

func (s *State) registerReplayTools(srv *server.MCPServer) {
	srv.AddTool(mcpgo.NewTool("replay_record_start",
		mcpgo.WithDescription(`Start recording a replayable scenario. All subsequent tool calls are captured as steps. Call replay_record_end to save.`),
		mcpgo.WithString("goal", mcpgo.Required(), mcpgo.Description("The LLM intent this scenario achieves, e.g. 'checkout flow'.")),
		mcpgo.WithString("url_pattern", mcpgo.Description("Glob pattern for matching URLs, e.g. 'https://shop.example.com/*'.")),
	), s.auditWrapper("replay_record_start", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		goal := req.GetString("goal", "")
		urlPattern := req.GetString("url_pattern", "")
		if goal == "" {
			return mcpgo.NewToolResultError("goal is required"), nil
		}
		scen := &browser.Scenario{
			ID:         browser.ScenarioID(goal, urlPattern),
			Goal:       goal,
			URLPattern: urlPattern,
			CreatedAt:  time.Now(),
		}
		s.mu.Lock()
		s.recordingScen = scen
		s.mu.Unlock()
		return mcpgo.NewToolResultText(fmt.Sprintf(`{"status":"recording","scenario_id":"%s","goal":"%s"}`, scen.ID, goal)), nil
	}))

	srv.AddTool(mcpgo.NewTool("replay_record_end",
		mcpgo.WithDescription(`Stop recording and save the scenario. Requires a prior replay_record_start.`),
	), s.auditWrapper("replay_record_end", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		s.mu.Lock()
		scen := s.recordingScen
		s.recordingScen = nil
		if scen != nil && len(scen.Steps) > 0 {
			stepsCopy := make([]browser.ScenarioStep, len(scen.Steps))
			copy(stepsCopy, scen.Steps)
			scen.Steps = stepsCopy
		}
		s.mu.Unlock()
		if scen == nil {
			return mcpgo.NewToolResultError("no active recording session"), nil
		}
		if len(scen.Steps) == 0 {
			return mcpgo.NewToolResultError("no steps captured"), nil
		}
		s.scenarioStore.Put(scen)
		resp, _ := json.MarshalIndent(map[string]any{
			"status":      "saved",
			"scenario_id": scen.ID,
			"steps":       len(scen.Steps),
		}, "", "  ")
		return mcpgo.NewToolResultText(string(resp)), nil
	}))

	srv.AddTool(mcpgo.NewTool("replay_run",
		mcpgo.WithDescription(`Return the recorded steps of a scenario for deterministic replay. The client executes each step in sequence, skipping LLM planning.`),
		mcpgo.WithString("scenario_id", mcpgo.Required(), mcpgo.Description("Scenario ID from replay_record_end or replay_list.")),
	), s.auditWrapper("replay_run", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		id := req.GetString("scenario_id", "")
		scen, ok := s.scenarioStore.Get(id)
		if !ok {
			return mcpgo.NewToolResultError(fmt.Sprintf("scenario %s not found", id)), nil
		}
		s.scenarioStore.MarkRun(id, true)
		resp, _ := json.MarshalIndent(map[string]any{
			"status": "replaying",
			"goal":   scen.Goal,
			"steps":  scen.Steps,
		}, "", "  ")
		return mcpgo.NewToolResultText(string(resp)), nil
	}))

	srv.AddTool(mcpgo.NewTool("replay_list",
		mcpgo.WithDescription(`List all recorded scenarios with their stats.`),
	), s.auditWrapper("replay_list", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		list := s.scenarioStore.List()
		type summary struct {
			ID          string `json:"id"`
			Goal        string `json:"goal"`
			URLPattern  string `json:"url_pattern"`
			Steps       int    `json:"steps"`
			SuccessRuns int    `json:"success_runs"`
			Verified    bool   `json:"verified"`
		}
		summaries := make([]summary, 0, len(list))
		for _, sc := range list {
			summaries = append(summaries, summary{
				ID: sc.ID, Goal: sc.Goal, URLPattern: sc.URLPattern,
				Steps: len(sc.Steps), SuccessRuns: sc.SuccessRuns, Verified: sc.Verified,
			})
		}
		data, _ := json.MarshalIndent(summaries, "", "  ")
		return mcpgo.NewToolResultText(string(data)), nil
	}))
}
