package browser_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Daybeam/ghost-driver/internal/browser"
	"github.com/Daybeam/ghost-driver/internal/profile"
)

// TestRegression_ChromeMultiTabAnd405Fix verifies:
// 1. Target selection skips internal chrome://new-tab-page/ when attaching to a busy Chrome.
// 2. CreateNewTab uses CDP Target.createTarget over WebSocket, bypassing legacy HTTP /json/new 405 errors.
// Issue ref: Chrome M111+ /json/new 405 & chrome:// NTP navigation deadlock.
func TestRegression_ChromeMultiTabAnd405Fix(t *testing.T) {
	devtoolsHTTP := "http://localhost:9222"
	if !browser.CheckPortOpen(devtoolsHTTP) {
		t.Skip("Chrome port 9222 not running, skipping live browser regression test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	reg, _ := profile.NewRegistry("./profiles")
	b, err := browser.New(ctx, devtoolsHTTP, "", "", false, reg, false)
	if err != nil {
		t.Fatalf("failed to attach to Chrome: %v", err)
	}
	defer b.Close()

	u, title, err := b.PageInfo(ctx)
	if err != nil {
		t.Fatalf("PageInfo failed: %v", err)
	}
	t.Logf("Attached active target URL: %s (%s)", u, title)

	if strings.HasPrefix(u, "chrome://") || strings.HasPrefix(u, "chrome-untrusted://") {
		t.Errorf("Target selection selected internal page %s instead of filtering it out", u)
	}

	// Verify CreateNewTab creates new tab via CDP Target.createTarget without HTTP 405
	newB, err := b.CreateNewTab(ctx, "about:blank")
	if err != nil {
		t.Fatalf("CreateNewTab failed (likely 405 / json/new issue): %v", err)
	}
	defer newB.Close()

	newURL, _, err := newB.PageInfo(ctx)
	if err != nil {
		t.Fatalf("new tab PageInfo failed: %v", err)
	}
	if !strings.Contains(newURL, "about:blank") {
		t.Errorf("expected new tab URL about:blank, got %s", newURL)
	}
}
