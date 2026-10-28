package browser_test

import (
	"context"
	"testing"
	"time"

	"github.com/Daybeam/ghost-driver/internal/browser"
	"github.com/Daybeam/ghost-driver/internal/profile"
)

// TestReconnectNoSelfDeadlock is a regression test for the self-deadlock bug
// fixed 2026-08-22.
//
// BUG: reconnect() held b.mu (line 52: b.mu.Lock), then called
// setupDialogHandler() → b.On() → b.mu.Lock(). Go's sync.Mutex is NOT
// reentrant, so this deadlocked instantly — connect_browser hung forever
// on every call, even on a fresh server process.
//
// FIX: moved setupDialogHandler() out of reconnect() into New(), where
// b.mu is not held.
//
// This test calls browser.New() with a short timeout. If the deadlock
// regresses, New() will hang and the context deadline will fire → test
// fails with "context deadline exceeded" instead of hanging the suite.
func TestReconnectNoSelfDeadlock(t *testing.T) {
	devtoolsHTTP := "http://localhost:9222"
	if !browser.CheckPortOpen(devtoolsHTTP) {
		t.Skip("Chrome on port 9222 not running, skipping browser regression test")
	}
	// 10s is generous — a healthy connect completes in ~50ms (see debug log).
	// If it takes longer, something is wrong.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	reg, _ := profile.NewRegistry("./profiles")

	done := make(chan error, 1)
	go func() {
		b, err := browser.New(ctx, devtoolsHTTP, "", "", false, reg, false)
		if err != nil {
			done <- err
			return
		}
		b.Close()
		done <- nil
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("browser.New failed (deadlock regression?): %v", err)
		}
	case <-ctx.Done():
		t.Fatal("browser.New did not complete within 10s — self-deadlock likely regressed (setupDialogHandler called inside reconnect while b.mu is held)")
	}

	t.Log("OK: browser.New completed without deadlock")
}

// TestCloseReturnsQuickly is a regression test for the Close() forever-block
// bug fixed 2026-08-22.
//
// BUG: cdp.Client.Close() did `<-c.done` with no timeout. If the WebSocket
// readLoop was stuck on a zombie/half-open connection, done was never closed
// and Close() blocked forever. This held the server mutex s.mu, deadlocking
// all subsequent connect_browser calls.
//
// FIX: replaced `<-c.done` with a select that abandons after 3s.
//
// This test verifies that browser.Close() returns within a bounded time
// on a healthy connection (it should be near-instant). The real regression
// scenario (stuck readLoop) requires a mock WS conn, but this at least
// ensures Close doesn't hang on the normal path.
func TestCloseReturnsQuickly(t *testing.T) {
	devtoolsHTTP := "http://localhost:9222"
	if !browser.CheckPortOpen(devtoolsHTTP) {
		t.Skip("Chrome on port 9222 not running, skipping browser regression test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	reg, _ := profile.NewRegistry("./profiles")
	b, err := browser.New(ctx, devtoolsHTTP, "", "", false, reg, false)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	done := make(chan struct{}, 1)
	go func() {
		b.Close()
		close(done)
	}()

	select {
	case <-done:
		t.Log("OK: Close returned promptly")
	case <-time.After(5 * time.Second):
		t.Fatal("b.Close() did not return within 5s — Close() forever-block likely regressed (<-c.done without timeout)")
	}
}
