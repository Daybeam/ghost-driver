package browser

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

type ScenarioStep struct {
	Tool string         `json:"tool"`
	Args map[string]any `json:"args"`
}

type Scenario struct {
	ID          string         `json:"id"`
	Goal        string         `json:"goal"`
	URLPattern  string         `json:"url_pattern"`
	Steps       []ScenarioStep `json:"steps"`
	CreatedAt   time.Time      `json:"created_at"`
	SuccessRuns int            `json:"success_runs"`
	FailRuns    int            `json:"fail_runs"`
	Verified    bool           `json:"verified"`
}

type ScenarioStore struct {
	mu    sync.RWMutex
	path  string
	items map[string]*Scenario
}

func NewScenarioStore(path string) *ScenarioStore {
	ss := &ScenarioStore{path: path, items: make(map[string]*Scenario)}
	ss.load()
	return ss
}

func (ss *ScenarioStore) load() {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	data, err := os.ReadFile(ss.path)
	if err != nil {
		return
	}
	if err := json.Unmarshal(data, &ss.items); err != nil {
		fmt.Printf("[ghost-driver] Warning: failed to parse scenario store: %v\n", err)
	}
}

func (ss *ScenarioStore) Get(id string) (*Scenario, bool) {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	s, ok := ss.items[id]
	if !ok {
		return nil, false
	}
	cp := *s
	cp.Steps = append([]ScenarioStep(nil), s.Steps...) // deep copy: caller owns the slice
	return &cp, true
}

func (ss *ScenarioStore) Put(s *Scenario) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.items[s.ID] = s
	ss.persistLocked()
}

func (ss *ScenarioStore) List() []*Scenario {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	result := make([]*Scenario, 0, len(ss.items))
	for _, s := range ss.items {
		cp := *s
		cp.Steps = append([]ScenarioStep(nil), s.Steps...) // deep copy
		result = append(result, &cp)
	}
	return result
}

func (ss *ScenarioStore) MarkRun(id string, success bool) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	s, ok := ss.items[id]
	if !ok {
		return
	}
	if success {
		s.SuccessRuns++
		if s.SuccessRuns >= 2 {
			s.Verified = true
		}
	} else {
		s.FailRuns++
		if s.FailRuns >= 3 {
			s.Verified = false
		}
	}
	ss.persistLocked()
}

func (ss *ScenarioStore) persistLocked() {
	data, err := json.MarshalIndent(ss.items, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(ss.path, data, 0644); err != nil {
		fmt.Printf("[ghost-driver] Warning: failed to persist scenario store: %v\n", err)
	}
}

func ScenarioID(goal, urlPattern string) string {
	h := sha256.Sum256([]byte(goal + "\x00" + urlPattern))
	return hex.EncodeToString(h[:])[:12]
}
