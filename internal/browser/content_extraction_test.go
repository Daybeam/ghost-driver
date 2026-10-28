package browser_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Daybeam/ghost-driver/internal/browser"
	"github.com/Daybeam/ghost-driver/internal/profile"
)

func TestContentExtractionModesAndAnchors(t *testing.T) {
	devtoolsHTTP := "http://localhost:9222"
	if !browser.CheckPortOpen(devtoolsHTTP) {
		t.Skip("Chrome on port 9222 not running, skipping browser integration test")
	}

	html := `<!DOCTYPE html>
<html>
<head><title>Test Article Page</title></head>
<body>
  <header><nav><a href="/home">Home</a></nav></header>
  <main>
    <h1>Main Title</h1>
    <h2>Section 1</h2>
    <p>This is the first paragraph of main content.</p>
    <table><tr><th>Header</th></tr><tr><td>Data</td></tr></table>
    <form><input type="text" id="email" placeholder="Enter Email" /><button id="sub">Submit</button></form>
  </main>
  <footer><p>Copyright 2026</p></footer>
</body>
</html>`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(html))
	}))
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	reg, _ := profile.NewRegistry("./profiles")
	b, err := browser.New(ctx, devtoolsHTTP, "", "", false, reg, false)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer b.Close()

	if err := b.Navigate(ctx, ts.URL); err != nil {
		t.Fatalf("failed to navigate: %v", err)
	}

	t.Run("HTMLToMarkdown Modes", func(t *testing.T) {
		fullMd, err := b.HTMLToMarkdown(ctx, "body", "full")
		if err != nil {
			t.Fatalf("full mode failed: %v", err)
		}
		if !strings.Contains(fullMd, "Home") || !strings.Contains(fullMd, "Main Title") {
			t.Errorf("full mode missing elements: %s", fullMd)
		}

		articleMd, err := b.HTMLToMarkdown(ctx, "body", "article")
		if err != nil {
			t.Fatalf("article mode failed: %v", err)
		}
		if strings.Contains(articleMd, "Home") || strings.Contains(articleMd, "Copyright") {
			t.Errorf("article mode should have stripped nav/footer: %s", articleMd)
		}

		controlsMd, err := b.HTMLToMarkdown(ctx, "body", "controls")
		if err != nil {
			t.Fatalf("controls mode failed: %v", err)
		}
		if !strings.Contains(controlsMd, "[INPUT]") || !strings.Contains(controlsMd, "[BUTTON]") {
			t.Errorf("controls mode missing interactive controls: %s", controlsMd)
		}
	})

	t.Run("ExtractContentAnchors", func(t *testing.T) {
		anchors, err := b.ExtractContentAnchors(ctx, "")
		if err != nil {
			t.Fatalf("ExtractContentAnchors failed: %v", err)
		}
		if anchors.Title != "Main Title" {
			t.Errorf("expected Title 'Main Title', got %q", anchors.Title)
		}
		if anchors.DataTables != 1 {
			t.Errorf("expected 1 data table, got %d", anchors.DataTables)
		}
		if !strings.Contains(anchors.FirstPara, "first paragraph") {
			t.Errorf("expected FirstPara snippet, got %q", anchors.FirstPara)
		}
	})
}
