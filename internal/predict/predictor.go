package predict

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrDelegateToCaller signifies that action prediction failed or is ambiguous,
// and execution should delegate to the standard LLM decision loop.
var ErrDelegateToCaller = errors.New("action prediction failed: delegate to standard LLM loop")

// Action represents a predicted user interface action.
type Action struct {
	Operation string `json:"operation"`  // "CLICK", "TYPE_TEXT", "SCROLL", "WAIT", "DONE"
	TargetIdx int    `json:"target_idx"` // 1-based index from element table
	Text      string `json:"text"`       // populated for TYPE_TEXT
}

// Predictor predicts the next UI action based on a concise element table and user goal.
type Predictor interface {
	Predict(ctx context.Context, elementTable string, goal string) (Action, error)
}

// RulePredictor is a fast, lightweight predictor that matches user goals against element table items.
type RulePredictor struct{}

func NewRulePredictor() *RulePredictor {
	return &RulePredictor{}
}

func (p *RulePredictor) Predict(ctx context.Context, elementTableStr string, goal string) (Action, error) {
	if elementTableStr == "" || goal == "" {
		return Action{}, ErrDelegateToCaller
	}

	goalLower := strings.ToLower(goal)
	lines := strings.Split(elementTableStr, "\n")

	type candidate struct {
		idx   int
		text  string
		tag   string
		score int
	}

	var candidates []candidate

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "[") {
			continue
		}

		// Line format: "[1] button 'Search' (x=100, y=200)"
		var idx int
		var tag, label string
		_, err := fmt.Sscanf(line, "[%d]", &idx)
		if err != nil {
			continue
		}

		// Extract label inside quotes if present
		firstQuote := strings.Index(line, "'")
		lastQuote := strings.LastIndex(line, "'")
		if firstQuote != -1 && lastQuote > firstQuote {
			label = line[firstQuote+1 : lastQuote]
		}

		// Extract tag
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			tag = parts[1]
		}

		if label == "" {
			continue
		}

		labelLower := strings.ToLower(label)
		score := 0

		if strings.Contains(goalLower, labelLower) || strings.Contains(labelLower, goalLower) {
			score += 10
		}

		// Bonus for exact or strong match
		if goalLower == labelLower {
			score += 20
		}

		if score >= 30 {
			candidates = append(candidates, candidate{
				idx:   idx,
				text:  label,
				tag:   tag,
				score: score,
			})
		}
	}

	if len(candidates) == 0 {
		return Action{}, ErrDelegateToCaller
	}

	// Pick highest scoring candidate
	best := candidates[0]
	for _, c := range candidates[1:] {
		if c.score > best.score {
			best = c
		}
	}

	// Determine operation based on tag or goal
	op := "CLICK"
	if strings.Contains(goalLower, "type") || strings.Contains(goalLower, "input") || strings.Contains(best.tag, "input") || strings.Contains(best.tag, "textbox") {
		if strings.Contains(goalLower, "click") {
			op = "CLICK"
		} else {
			op = "TYPE_TEXT"
		}
	}

	return Action{
		Operation: op,
		TargetIdx: best.idx,
		Text:      goal,
	}, nil
}
