package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// CapturedAction represents a single interaction performed by a human.
type CapturedAction struct {
	Type      string `json:"type"`      // click, type, scroll
	Selector  string `json:"selector"`  // derived CSS selector
	Value     string `json:"value"`     // input text
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Timestamp int64   `json:"ts"`
}

// ActionRecorder manages the "Teaching Mode" session.
type ActionRecorder struct {
	mu       sync.Mutex
	b        *Browser
	videoRec *Recorder
	actions  []CapturedAction
	active   bool
	done     chan bool
}

func NewActionRecorder(b *Browser) *ActionRecorder {
	return &ActionRecorder{
		b:    b,
		done: make(chan bool, 1),
	}
}

// Start kicks off the teaching session: injects UI, starts video, and listens for bindings.
func (ar *ActionRecorder) Start(ctx context.Context) error {
	ar.mu.Lock()
	defer ar.mu.Unlock()

	if ar.active {
		return fmt.Errorf("teaching session already active")
	}

	// 1. Start video recording (reusing existing Recorder)
	vrec, err := NewRecorder(ar.b, 8)
	if err != nil {
		return err
	}
	ar.videoRec = vrec
	vrec.Start(ctx)

	// 2. Set up CDP Binding for human interactions
	// The JS side calls window.ghostRecordAction(payload)
	client := ar.b.CDPClient()
	if client == nil {
		vrec.Stop()
		return fmt.Errorf("browser not connected")
	}
	err = client.AddBinding(ctx, "ghostRecordAction")
	if err != nil {
		vrec.Stop()
		return fmt.Errorf("failed to add binding: %w", err)
	}

	ar.b.On("Runtime.bindingCalled", func(params json.RawMessage) {
		var p struct {
			Name    string `json:"name"`
			Payload string `json:"payload"`
		}
		if err := json.Unmarshal(params, &p); err == nil && p.Name == "ghostRecordAction" {
			if p.Payload == "FINISH" {
				select {
				case ar.done <- true:
				default:
				}
				return
			}
			var act CapturedAction
			if err := json.Unmarshal([]byte(p.Payload), &act); err == nil {
				ar.mu.Lock()
				ar.actions = append(ar.actions, act)
				ar.mu.Unlock()
				fmt.Printf("[teaching] Captured action: %s on %s\n", act.Type, act.Selector)
			}
		}
	})

	// 3. Inject Teaching UI and Listeners
	// (JS will be implemented in resources/js/teaching_mode.js)
	// For now, we use a simple injection
	ar.active = true
	return nil
}

func (ar *ActionRecorder) Stop() ([]CapturedAction, string, error) {
	ar.mu.Lock()
	defer ar.mu.Unlock()

	if !ar.active {
		return nil, "", fmt.Errorf("no active teaching session")
	}

	ar.videoRec.Stop()
	ar.active = false

	// Render video to temp file
	tmpPath := fmt.Sprintf("teaching_demo_%d.gif", time.Now().Unix())
	_ = ar.videoRec.Render(tmpPath)

	return ar.actions, tmpPath, nil
}

func (ar *ActionRecorder) Wait() <-chan bool {
	return ar.done
}

func (ar *ActionRecorder) Actions() []CapturedAction {
	ar.mu.Lock()
	defer ar.mu.Unlock()
	return ar.actions
}

// GenerateSOPProfile creates a skeleton SiteProfile from captured actions.
func (ar *ActionRecorder) GenerateSOPProfile(name string) map[string]any {
	ar.mu.Lock()
	defer ar.mu.Unlock()

	selectors := make(map[string]string)
	for _, act := range ar.actions {
		if act.Selector != "" {
			// Create a readable key
			key := fmt.Sprintf("%s_%s", act.Type, act.Selector)
			selectors[key] = act.Selector
		}
	}

	return map[string]any{
		"name":      name,
		"selectors": selectors,
		"hints":     []string{"Generated from human demonstration"},
	}
}
