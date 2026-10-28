package predict

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"
)

// PacerConfig defines timing and behavioral thresholds for Anti-Bot pacing.
type PacerConfig struct {
	MinInterval   time.Duration // Minimum interval between automated actions
	HoverDelayMin time.Duration // Minimum hover delay before clicking
	HoverDelayMax time.Duration // Maximum hover delay before clicking
	MaxCoordShift float64       // Maximum allowed coordinate shift in pixels
}

// DefaultPacerConfig returns recommended default Anti-Bot pacing settings.
func DefaultPacerConfig() PacerConfig {
	return PacerConfig{
		MinInterval:   400 * time.Millisecond,
		HoverDelayMin: 200 * time.Millisecond,
		HoverDelayMax: 600 * time.Millisecond,
		MaxCoordShift: 50.0,
	}
}

// Pacer enforces human-like pacing and behavioral risk mitigation to avoid bot detection.
type Pacer struct {
	mu       sync.Mutex
	cfg      PacerConfig
	lastExec time.Time
	rand     *rand.Rand
}

// NewPacer creates a new Pacer with the specified configuration.
func NewPacer(cfg PacerConfig) *Pacer {
	return &Pacer{
		cfg:  cfg,
		rand: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// Pace injects rate-limiting pauses and random hover jitter before action execution.
// The mutex is only held to compute/reserve the pacing slot — sleeps happen
// outside the lock so concurrent actions don't serialize behind each other.
func (p *Pacer) Pace(ctx context.Context) error {
	p.mu.Lock()
	now := time.Now()
	elapsed := now.Sub(p.lastExec)
	var sleepTime time.Duration
	if elapsed < p.cfg.MinInterval {
		sleepTime = p.cfg.MinInterval - elapsed
	}

	// Calculate random hover delay with jitter
	jitterRange := p.cfg.HoverDelayMax - p.cfg.HoverDelayMin
	hoverDelay := p.cfg.HoverDelayMin
	if jitterRange > 0 {
		hoverDelay += time.Duration(p.rand.Int63n(int64(jitterRange)))
	}

	// Reserve the slot: subsequent callers pace against the end time of this
	// pause, preserving the >= MinInterval anti-bot spacing guarantee.
	p.lastExec = now.Add(sleepTime + hoverDelay)
	p.mu.Unlock()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(sleepTime + hoverDelay):
	}
	return nil
}

// CheckFreshness checks if the element's position shifted significantly since snapshot capture.
func (p *Pacer) CheckFreshness(origX, origY, newX, newY float64) error {
	dx := newX - origX
	dy := newY - origY
	distSq := dx*dx + dy*dy
	maxSq := p.cfg.MaxCoordShift * p.cfg.MaxCoordShift

	if distSq > maxSq {
		return fmt.Errorf("element shifted too far: original (%.0f,%.0f) -> current (%.0f,%.0f)", origX, origY, newX, newY)
	}
	return nil
}
