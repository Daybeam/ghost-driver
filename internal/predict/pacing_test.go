package predict

import (
	"context"
	"testing"
	"time"
)

func TestPacer(t *testing.T) {
	cfg := PacerConfig{
		MinInterval:   50 * time.Millisecond,
		HoverDelayMin: 10 * time.Millisecond,
		HoverDelayMax: 20 * time.Millisecond,
		MaxCoordShift: 20.0,
	}

	pacer := NewPacer(cfg)
	ctx := context.Background()

	t.Run("Pace Delay", func(t *testing.T) {
		start := time.Now()
		if err := pacer.Pace(ctx); err != nil {
			t.Fatalf("Pace failed: %v", err)
		}
		duration := time.Since(start)
		if duration < 10*time.Millisecond {
			t.Errorf("Pace duration too short: %v", duration)
		}
	})

	t.Run("Freshness Within Limits", func(t *testing.T) {
		err := pacer.CheckFreshness(100, 100, 105, 105)
		if err != nil {
			t.Errorf("CheckFreshness failed unexpectedly: %v", err)
		}
	})

	t.Run("Freshness Exceeded", func(t *testing.T) {
		err := pacer.CheckFreshness(100, 100, 200, 200)
		if err == nil {
			t.Errorf("CheckFreshness expected error for shifted element")
		}
	})
}
