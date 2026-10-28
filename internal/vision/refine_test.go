package vision

import (
	"testing"
)

func TestRefinePoint_InvalidImage(t *testing.T) {
	coarse := Coord{X: 500, Y: 500}
	refined := RefinePoint([]byte("invalid-png-data"), coarse, 1000, 1000)
	if refined != coarse {
		t.Errorf("expected coarse coordinate on error, got %v", refined)
	}
}
