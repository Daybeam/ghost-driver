package browser

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// FormState is a snapshot of all visible form field values.
type FormState struct {
	ID        string            `json:"id"`
	FieldHash map[string]string `json:"field_hash"`
	Timestamp int64             `json:"timestamp"`
}

// FormAction represents a transition between states.
type FormAction struct {
	FromState string `json:"from_state"`
	ToState   string `json:"to_state"`
	Field     string `json:"field"`
	Action    string `json:"action"`
	Value     string `json:"value"`
	Result    string `json:"result"`
	ErrorHint string `json:"error_hint,omitempty"`
}

// FormStateGraph is an in-memory tracker of form interactions during a task.
type FormStateGraph struct {
	mu      sync.RWMutex
	states  map[string]*FormState
	actions []FormAction
	current string
}

func NewFormStateGraph() *FormStateGraph {
	return &FormStateGraph{
		states:  make(map[string]*FormState),
		actions: make([]FormAction, 0),
	}
}

func (g *FormStateGraph) UpdateCurrent(fields map[string]string) string {
	g.mu.Lock()
	defer g.mu.Unlock()

	id := hashFields(fields)
	if _, ok := g.states[id]; !ok {
		g.states[id] = &FormState{
			ID:        id,
			FieldHash: fields,
			Timestamp: time.Now().Unix(),
		}
	}
	g.current = id
	return id
}

func (g *FormStateGraph) RecordAction(field, action, value, result, errorHint string, nextFields map[string]string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	from := g.current
	to := hashFields(nextFields)

	if _, ok := g.states[to]; !ok {
		g.states[to] = &FormState{
			ID:        to,
			FieldHash: nextFields,
			Timestamp: time.Now().Unix(),
		}
	}

	g.actions = append(g.actions, FormAction{
		FromState: from,
		ToState:   to,
		Field:     field,
		Action:    action,
		Value:     value,
		Result:    result,
		ErrorHint: errorHint,
	})
	g.current = to
}

func hashFields(fields map[string]string) string {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(k)
		sb.WriteString("=")
		sb.WriteString(fields[k])
		sb.WriteString(";")
	}
	sum := sha256.Sum256([]byte(sb.String()))
	return fmt.Sprintf("%x", sum)[:12]
}

func DiffFormStates(before, after map[string]string) string {
	var changes []string
	for k, v := range after {
		if old, ok := before[k]; !ok || old != v {
			changes = append(changes, fmt.Sprintf("%s:%s->%s", k, old, v))
		}
	}
	return strings.Join(changes, ", ")
}
