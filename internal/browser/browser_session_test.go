package browser_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/Daybeam/ghost-driver/internal/browser"
	"github.com/Daybeam/ghost-driver/internal/profile"
)

func TestBrowserSessionSurvivesNavigation(t *testing.T) {
	devtoolsHTTP := "http://localhost:9222"
	if !browser.CheckPortOpen(devtoolsHTTP) {
		t.Skip("Chrome on port 9222 not running, skipping browser integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	reg, _ := profile.NewRegistry("./profiles")

	b, err := browser.New(ctx, devtoolsHTTP, "", "", false, reg, false)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer b.Close()

	t.Log("Connected. Checking browser session mode...")

	url1, title1, err := b.PageInfo(ctx)
	if err != nil {
		t.Fatalf("PageInfo before nav: %v", err)
	}
	t.Logf("Before nav: url=%s title=%s", url1, title1)

	err = b.Navigate(ctx, "https://example.com")
	if err != nil {
		t.Fatalf("Navigate to example.com: %v", err)
	}

	url2, title2, err := b.PageInfo(ctx)
	if err != nil {
		t.Fatalf("PageInfo after nav: %v", err)
	}
	t.Logf("After nav: url=%s title=%s", url2, title2)

	if url2 == url1 {
		t.Errorf("URL did not change after navigation")
	}

	err = b.Navigate(ctx, "https://httpbin.org/get")
	if err != nil {
		t.Fatalf("Navigate to httpbin: %v", err)
	}

	url3, title3, err := b.PageInfo(ctx)
	if err != nil {
		t.Fatalf("PageInfo after second nav: %v", err)
	}
	t.Logf("After second nav: url=%s title=%s", url3, title3)

	md, err := b.GetCleanMarkdown(ctx)
	if err != nil {
		t.Fatalf("GetMarkdown after navigation: %v", err)
	}
	if len(md) == 0 {
		t.Errorf("Markdown is empty after navigation")
	}
	t.Logf("Markdown length: %d", len(md))

	t.Log("SUCCESS: Browser session survived multiple navigations")
}

func TestBrowserSessionClickNavigation(t *testing.T) {
	devtoolsHTTP := "http://localhost:9222"
	if !browser.CheckPortOpen(devtoolsHTTP) {
		t.Skip("Chrome on port 9222 not running, skipping browser integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	reg, _ := profile.NewRegistry("./profiles")

	b, err := browser.New(ctx, devtoolsHTTP, "", "", false, reg, false)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer b.Close()

	err = b.Navigate(ctx, "https://example.com")
	if err != nil {
		t.Fatalf("Navigate: %v", err)
	}

	js := `document.querySelector('a').href`
	result, err := b.EvalJS(ctx, js)
	if err != nil {
		t.Fatalf("EvalJS to find link: %v", err)
	}
	var href string
	json.Unmarshal(result, &href)
	t.Logf("Found link: %s", href)

	jsClick := fmt.Sprintf(`document.querySelector('a').click()`)
	_, err = b.EvalJS(ctx, jsClick)
	if err != nil {
		t.Fatalf("Click link via JS: %v", err)
	}

	time.Sleep(2 * time.Second)

	url, title, err := b.PageInfo(ctx)
	if err != nil {
		t.Fatalf("PageInfo after click-nav: %v", err)
	}
	t.Logf("After click-nav: url=%s title=%s", url, title)

	if url == "https://example.com" {
		t.Errorf("URL did not change after clicking link")
	}

	t.Log("SUCCESS: Browser session survived click-triggered navigation")
}
