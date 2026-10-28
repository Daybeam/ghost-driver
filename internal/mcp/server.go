package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/Daybeam/ghost-driver/internal/audit"
	"github.com/Daybeam/ghost-driver/internal/browser"
	"github.com/Daybeam/ghost-driver/internal/cdp"
	"github.com/Daybeam/ghost-driver/internal/config"
	"github.com/Daybeam/ghost-driver/internal/predict"
	"github.com/Daybeam/ghost-driver/internal/profile"
	"github.com/Daybeam/ghost-driver/internal/vision"
)

type State struct {
	mu              sync.RWMutex
	connectMu       sync.Mutex // serializes connect/reconnect; heavy I/O happens without stalling all tools
	mgr             *browser.Manager
	browsers        map[string]*browser.Browser
	inputs          map[string]*browser.Input
	activeID        string
	recorder        *browser.Recorder
	visionFinder    *vision.Finder
	actionPredictor predict.Predictor
	pacer           *predict.Pacer
	registry        *profile.Registry
	cfg             *config.Config
	lastCfg         *browser.Config
	lastInputMode   browser.InputMode
	autoRecord      bool
	recordFrames    int
	topology        *browser.Topology
	auditor         *audit.Auditor
	actionCache     *browser.ActionCache
	scenarioStore   *browser.ScenarioStore
	recordingScen   *browser.Scenario
	stopWatch       chan struct{}
}

func (s *State) getActive() (*browser.Browser, *browser.Input) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.activeID == "" {
		return nil, nil
	}
	return s.browsers[s.activeID], s.inputs[s.activeID]
}

func (s *State) ensureConnected(ctx context.Context) (*browser.Browser, *browser.Input, error) {
	b, i := s.getActive()
	if b != nil && b.IsHealthy(ctx) {
		return b, i, nil
	}

	// Serialize connect/reconnect attempts against each other (not against
	// every other tool call — the heavy Start I/O runs without s.mu held).
	s.connectMu.Lock()
	defer s.connectMu.Unlock()

	// Re-check: another goroutine may have reconnected while we waited.
	b, i = s.getActive()
	if b != nil && b.IsHealthy(ctx) {
		return b, i, nil
	}

	// 15s budget covers stale-connection cleanup + reconnect.
	rctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	s.mu.Lock()
	if s.lastCfg == nil {
		s.mu.Unlock()
		return nil, nil, fmt.Errorf("no browser connected. please call connect_browser first")
	}
	cfg := *s.lastCfg
	inputMode := s.lastInputMode
	fmt.Printf("[ghost-driver] connection lost or unhealthy, attempting auto-reconnect...\n")

	// Release stale connections concurrently (each Close() has ~3s internal
	// timeout — previously sequential, so N stale conns could eat the entire
	// reconnect budget before mgr.Start even began).
	if s.mgr != nil {
		s.mgr.Stop()
	}
	var wg sync.WaitGroup
	for _, old := range s.browsers {
		if c := old.CDPClient(); c != nil {
			wg.Add(1)
			go func(c cdp.CDPClient) {
				defer wg.Done()
				c.Close()
			}(c)
		}
	}
	s.browsers = make(map[string]*browser.Browser)
	s.inputs = make(map[string]*browser.Input)
	s.activeID = ""
	s.mu.Unlock()

	// Wait for stale connections to close, bounded at 5s.
	cleanupDone := make(chan struct{})
	go func() { wg.Wait(); close(cleanupDone) }()
	select {
	case <-cleanupDone:
	case <-time.After(5 * time.Second):
		fmt.Printf("[ghost-driver] stale connection cleanup timed out after 5s, proceeding\n")
	case <-rctx.Done():
		return nil, nil, fmt.Errorf("auto-reconnect failed: cleanup exceeded total reconnect budget")
	}

	mgr := browser.NewManager(cfg, s.registry)
	newB, err := mgr.Start(rctx)
	if err != nil {
		return nil, nil, fmt.Errorf("auto-reconnect failed: %w", err)
	}

	targetID := newB.TargetID()
	if targetID == "" {
		targetID = "default"
	}
	s.mu.Lock()
	s.mgr = mgr
	s.browsers[targetID] = newB
	newInput := browser.NewInput(newB, inputMode)
	s.inputs[targetID] = newInput
	s.activeID = targetID
	s.mu.Unlock()

	return newB, newInput, nil
}

// Close releases held resources (audit log file handle, topology DB,
// active recorder). Intended to be called via defer when the server loop exits.
func (s *State) Close() {
	s.mu.Lock()
	for _, b := range s.browsers {
		b.Close()
	}
	s.browsers = nil
	rec := s.recorder
	s.recorder = nil
	stopWatch := s.stopWatch
	s.stopWatch = nil
	s.mu.Unlock()

	if rec != nil {
		rec.Stop()
		rec.Cleanup()
	}

	s.auditor.Close()
	if s.topology != nil {
		s.topology.Close()
	}
	if stopWatch != nil {
		close(stopWatch)
	}
}

func (s *State) auditWrapper(toolName string, handler func(context.Context, mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error)) func(context.Context, mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		start := time.Now()
		res, err := handler(ctx, req)
		duration := time.Since(start)

		args, _ := req.Params.Arguments.(map[string]any)
		s.auditor.Record(toolName, duration, args, err)

		s.mu.RLock()
		rec := s.recordingScen
		s.mu.RUnlock()
		if rec != nil && err == nil {
			s.mu.Lock()
			rec.Steps = append(rec.Steps, browser.ScenarioStep{Tool: toolName, Args: args})
			s.mu.Unlock()
		}

		return res, err
	}
}

func NewState() *State {
	conf := config.LoadConfig()
	reg, regErr := profile.NewRegistry("./profiles")
	if regErr != nil {
		fmt.Printf("[ghost-driver] Warning: failed to load profile registry: %v\n", regErr)
	}

	s := &State{
		cfg:             conf,
		autoRecord:      false, // disabled by default — enable explicitly to avoid FFmpeg overhead per action
		recordFrames:    conf.DefaultRecordFrames,
		browsers:        make(map[string]*browser.Browser),
		inputs:          make(map[string]*browser.Input),
		registry:        reg,
		auditor:         audit.NewAuditor(),
		actionPredictor: predict.NewRulePredictor(),
		pacer:           predict.NewPacer(predict.DefaultPacerConfig()),
	}

	s.actionCache = browser.NewActionCache("./memory/action_cache.json")
	s.scenarioStore = browser.NewScenarioStore("./memory/scenarios.json")

	s.initAdvancedFeatures()

	if reg != nil {
		s.stopWatch = make(chan struct{})
		go reg.Watch(s.stopWatch)
	}
	if s.cfg.CDPHostMap != "" {
		cdp.DefaultHostMap = s.cfg.CDPHostMap
	}
	return s
}

func NewServer() (*server.MCPServer, *State) {
	s := NewState()
	srv := server.NewMCPServer("ghost-driver", "1.1.0")

	enabled := parseToolsets(s.cfg.Toolsets)
	registrations := []struct {
		name string
		fn   func(*server.MCPServer)
	}{
		{"basic", s.registerBasicTools},
		{"navigation", s.registerNavigationTools},
		{"interaction", s.registerInteractionTools},
		{"content", s.registerContentTools},
		{"extract", s.registerExtractTools},
		{"page", s.registerPageTools},
		{"advanced", s.registerAdvancedTools},
		{"auth", s.registerAuthTools},
		{"form", s.registerFormTools},
		{"replay", s.registerReplayTools},
		{"dev", s.registerDevTools},
	}
	for _, r := range registrations {
		if enabled[r.name] {
			r.fn(srv)
		}
	}

	return srv, s
}

// parseToolsets returns a map of enabled toolset names.
//
// Edition presets:
//   "" / "base"  — automation tools only (default, no UI measurement)
//   "dev" / "all" — base + UI measurement/inspection tools for frontend dev
//   "minimal"    — smallest viable set (basic,navigation,interaction,extract,page)
//   custom       — comma-separated list of toolset names
func parseToolsets(s string) map[string]bool {
	// Base toolsets: all browser automation tools (no UI measurement).
	base := []string{"basic", "navigation", "interaction", "content", "extract", "page", "advanced", "auth", "form", "replay"}
	// Dev toolset adds UI measurement/inspection tools for frontend dev assistance.
	all := append(base, "dev")

	if s == "" || s == "base" {
		m := make(map[string]bool, len(base))
		for _, t := range base {
			m[t] = true
		}
		return m
	}
	if s == "dev" || s == "all" {
		m := make(map[string]bool, len(all))
		for _, t := range all {
			m[t] = true
		}
		return m
	}
	if s == "minimal" {
		s = "basic,navigation,interaction,extract,page"
	}

	// Custom comma-separated list.
	m := make(map[string]bool)
	for _, t := range strings.Split(s, ",") {
		t = strings.TrimSpace(t)
		if t != "" {
			m[t] = true
		}
	}
	return m
}

func (s *State) registerAuthHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/auth/callback", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")

		if r.Method == "OPTIONS" {
			return
		}

		token := r.URL.Query().Get("token")
		userID := r.URL.Query().Get("user_id")
		if token == "" {
			http.Error(w, "missing token", 400)
			return
		}

		// Save to local memory/config
		s.mu.Lock()
		s.cfg.APIKey = token
		s.cfg.UserID = userID
		s.mu.Unlock()

		// Persist for next restart
		authData := map[string]string{"token": token, "user_id": userID}
		data, _ := json.Marshal(authData)
		_ = os.WriteFile("./memory/auth.json", data, 0600)

		fmt.Fprintf(os.Stderr, "\n✨ Device authorized successfully for user: %s\n", userID)
		w.Write([]byte("OK: Authorized"))
	})
}

func (s *State) RegisterAuthHandlers(mux *http.ServeMux) {
	s.registerAuthHandlers(mux)
}
