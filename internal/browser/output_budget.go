package browser

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

type BrowserOutputBudget struct {
	MaxBytes int    // Default 8192 (8KB)
	DumpDir  string // Directory for dumping full content on truncation
}

func DefaultBrowserOutputBudget() *BrowserOutputBudget {
	return &BrowserOutputBudget{
		MaxBytes: 8192,
		DumpDir:  filepath.Join("memory", "dumps"),
	}
}

func (b *BrowserOutputBudget) Apply(content string, sourceURL string) string {
	if b == nil {
		return content
	}
	maxBytes := b.MaxBytes
	if maxBytes <= 0 {
		maxBytes = 8192
	}
	if len(content) <= maxBytes {
		return content // Pass-through
	}

	dumpPath := b.dumpToDisk(content, sourceURL)

	// Split by paragraphs (\n\n)
	paragraphs := strings.Split(content, "\n\n")
	const maxRunesPerParagraph = 800

	if len(paragraphs) <= 3 {
		// Pathology 1: Few paragraphs but large byte size (e.g. single huge line/paragraph)
		var truncated []string
		for _, p := range paragraphs {
			truncated = append(truncated, truncateRunes(p, maxRunesPerParagraph))
		}
		return strings.Join(truncated, "\n\n") +
			fmt.Sprintf("\n\n[⚠️ Content exceeded budget (%d KB). Each paragraph truncated to %d runes. Full content saved to: %s]",
				len(content)/1024, maxRunesPerParagraph, dumpPath)
	}

	// Pathology 2: Many paragraphs. Drop header and footer (nav/crumbs/copyright), keep middle.
	mid := paragraphs[1 : len(paragraphs)-1]
	sampled := sampleByStructure(mid, maxBytes)

	return fmt.Sprintf("[📄 Content large: %d paragraphs, %d KB. Full content saved to: %s]\n\n%s",
		len(paragraphs), len(content)/1024, dumpPath, sampled)
}

func (b *BrowserOutputBudget) dumpToDisk(content string, sourceURL string) string {
	dir := b.DumpDir
	if dir == "" {
		dir = filepath.Join("memory", "dumps")
	}
	_ = os.MkdirAll(dir, 0755)

	filename := fmt.Sprintf("content_%d.md", time.Now().UnixNano())
	fullPath := filepath.Join(dir, filename)

	header := fmt.Sprintf("<!-- Source: %s | Time: %s -->\n\n", sourceURL, time.Now().Format(time.RFC3339))
	_ = os.WriteFile(fullPath, []byte(header+content), 0644)

	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		return fullPath
	}
	return absPath
}

func truncateRunes(s string, maxRunes int) string {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxRunes]) + "…[truncated]"
}

func sampleByStructure(paragraphs []string, maxBytes int) string {
	var headings []string
	var tables []string
	var lists []string
	var normals []string

	for _, p := range paragraphs {
		trimmed := strings.TrimSpace(p)
		if strings.HasPrefix(trimmed, "#") {
			headings = append(headings, p)
		} else if strings.HasPrefix(trimmed, "|") {
			tables = append(tables, p)
		} else if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") || (len(trimmed) > 2 && trimmed[1] == '.' && trimmed[0] >= '0' && trimmed[0] <= '9') {
			lists = append(lists, p)
		} else {
			normals = append(normals, p)
		}
	}

	var selected []string
	currLen := 0

	addP := func(p string) bool {
		if currLen+len(p) > maxBytes {
			return false
		}
		selected = append(selected, p)
		currLen += len(p)
		return true
	}

	// Priority: Headings -> Tables -> Lists -> Normals
	for _, p := range headings {
		_ = addP(p)
	}
	for _, p := range tables {
		if !addP(p) {
			break
		}
	}
	for _, p := range lists {
		if !addP(p) {
			break
		}
	}
	for _, p := range normals {
		if !addP(p) {
			break
		}
	}

	if len(selected) == 0 && len(paragraphs) > 0 {
		return truncateRunes(paragraphs[0], 800)
	}

	return strings.Join(selected, "\n\n")
}
