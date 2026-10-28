package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// Entry represents a single structured audit log entry.
type Entry struct {
	RequestID        string         `json:"request_id"`
	Timestamp        string         `json:"timestamp"`
	ToolName         string         `json:"tool_name"`
	Status           string         `json:"status"`
	DurationMS       int64          `json:"duration_ms"`
	ArgumentsSummary map[string]any `json:"arguments_summary,omitempty"`
	Error            string         `json:"error,omitempty"`
}

// Stats holds usage telemetry for a specific tool.
type Stats struct {
	InvocationCount int64 `json:"invocation_count"`
	TotalLatencyMS  int64 `json:"total_latency_ms"`
	ErrorCount      int64 `json:"error_count"`
	ErrorRate       float64 `json:"error_rate"`
	Healthy         bool   `json:"healthy"`
}

// Auditor handles structured logging and in-memory telemetry for tool calls.
type Auditor struct {
	mu       sync.RWMutex
	stats    map[string]*Stats
	logFile  *os.File
	reqCount uint64
}

// NewAuditor creates a new Auditor instance.
// Audit entries are written to GHOST_AUDIT_LOG (if set) or ./memory/audit.jsonl by default.
func NewAuditor() *Auditor {
	a := &Auditor{stats: make(map[string]*Stats)}
	path := os.Getenv("GHOST_AUDIT_LOG")
	if path == "" {
		path = "./memory/audit.jsonl"
	}
	os.MkdirAll(filepath.Dir(path), 0755)
	if f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err == nil {
		a.logFile = f
	}
	return a
}

// Record logs a tool execution and updates telemetry.
func (a *Auditor) Record(toolName string, duration time.Duration, args map[string]any, err error) {
	reqID := fmt.Sprintf("req-%d", atomic.AddUint64(&a.reqCount, 1))
	entry := Entry{
		RequestID:        reqID,
		Timestamp:        time.Now().UTC().Format(time.RFC3339),
		ToolName:         toolName,
		Status:           "success",
		DurationMS:       duration.Milliseconds(),
		ArgumentsSummary: args,
	}
	if err != nil {
		entry.Status = "error"
		entry.Error = err.Error()
	}

	data, _ := json.Marshal(entry)
	line := fmt.Sprintf("[AUDIT] %s\n", string(data))

	// 1. Stderr (always)
	fmt.Fprint(os.Stderr, line)

	// 2. In-memory telemetry + file (both under mu so concurrent Records
	// don't interleave writes on Windows)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.logFile != nil {
		fmt.Fprint(a.logFile, line)
	}
	s, ok := a.stats[toolName]
	if !ok {
		s = &Stats{}
		a.stats[toolName] = s
	}
	s.InvocationCount++
	s.TotalLatencyMS += entry.DurationMS
	if err != nil {
		s.ErrorCount++
	}
}

// GetStats returns a copy of current telemetry with computed health flags.
func (a *Auditor) GetStats() map[string]Stats {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make(map[string]Stats)
	for name, s := range a.stats {
		copy := *s
		if copy.InvocationCount > 0 {
			copy.ErrorRate = float64(copy.ErrorCount) / float64(copy.InvocationCount)
		}
		copy.Healthy = copy.InvocationCount == 0 || copy.ErrorRate < 0.5
		out[name] = copy
	}
	return out
}

// Close closes the audit log file if open. Thread-safe against concurrent Record.
func (a *Auditor) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.logFile != nil {
		a.logFile.Close()
		a.logFile = nil
	}
}
