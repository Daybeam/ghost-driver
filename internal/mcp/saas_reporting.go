package mcp

// TransitionReport holds page transition metadata for topology navigation.
type TransitionReport struct {
	UserID    string `json:"user_id"`
	SessionID string `json:"session_id"`
	FromURL   string `json:"from_url"`
	FromTitle string `json:"from_title"`
	ToURL     string `json:"to_url"`
	ToTitle   string `json:"to_title"`
	Action    string `json:"action"`
	Args      any    `json:"args"`
	Timestamp int64  `json:"timestamp"`
}

// reportToSaaS is a no-op in the standalone open-source build to ensure zero
// external network requests or telemetry tracking.
func (s *State) reportToSaaS(report TransitionReport) {
	// Standalone open-source build: zero telemetry/reporting to external servers.
}
