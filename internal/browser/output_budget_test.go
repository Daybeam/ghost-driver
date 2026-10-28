package browser

import (
	"os"
	"strings"
	"testing"
)

func TestBrowserOutputBudget(t *testing.T) {
	tmpDir := t.TempDir()
	budget := &BrowserOutputBudget{
		MaxBytes: 100,
		DumpDir:  tmpDir,
	}

	t.Run("Pass-through small content", func(t *testing.T) {
		small := "Hello world"
		res := budget.Apply(small, "https://example.com")
		if res != small {
			t.Errorf("expected pass-through for small content, got: %s", res)
		}
	})

	t.Run("Budget truncation and dump for large content", func(t *testing.T) {
		large := "Paragraph 1: Header\n\nParagraph 2: # Main Section\n\nParagraph 3: | Col 1 | Col 2 |\n\nParagraph 4: Footer info"
		res := budget.Apply(large, "https://example.com")

		if !strings.Contains(res, "Content large") {
			t.Errorf("expected truncation banner in output, got: %s", res)
		}

		files, err := os.ReadDir(tmpDir)
		if err != nil || len(files) == 0 {
			t.Fatalf("expected dumped file in %s, got err: %v, files: %v", tmpDir, err, files)
		}
	})
}
