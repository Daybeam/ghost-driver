package browser

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDetectAppType_SignatureScan uses synthetic binaries to verify the
// signature-scanning logic without depending on a real .exe being present
// at a hardcoded path. Runs on all platforms.
func TestDetectAppType_SignatureScan(t *testing.T) {
	tests := []struct {
		name    string
		content []byte
		want    string
	}{
		{"webview2 signature", []byte("padding\x00\x00WebView2\x00more bytes"), "webview2"},
		{"webview2 env var signature", []byte("foo\x00WEBVIEW2_ADDITIONAL\x00bar"), "webview2"},
		{"electron chrome_elf signature", []byte("data\x00chrome_elf.dll\x00end"), "electron"},
		{"electron libffmpeg signature", []byte("data\x00libffmpeg.dll\x00end"), "electron"},
		{"unknown binary", []byte("just a regular binary with no signatures"), "chrome"},
		{"empty file", []byte{}, "chrome"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmp := t.TempDir()
			exe := filepath.Join(tmp, "test_app.exe")
			if err := os.WriteFile(exe, tt.content, 0644); err != nil {
				t.Fatal(err)
			}
			got := detectAppType(exe)
			if got != tt.want {
				t.Errorf("detectAppType() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestDetectAppType_FirefoxPath checks the path-name fast path: any path
// containing "firefox" (case-insensitive) short-circuits to "firefox" before
// attempting to open the file.
func TestDetectAppType_FirefoxPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/usr/bin/firefox", "firefox"},
		{`C:\Program Files\Mozilla Firefox\firefox.exe`, "firefox"},
		{"/snap/firefox/current/firefox", "firefox"},
		{"/usr/bin/Firefox", "firefox"}, // case-insensitive
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := detectAppType(tt.path)
			if got != tt.want {
				t.Errorf("detectAppType(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

// TestDetectAppType_NonexistentPath verifies the fallback when the file
// cannot be opened (e.g. wrong path, permission denied, or the binary
// doesn't exist on this machine).
func TestDetectAppType_NonexistentPath(t *testing.T) {
	got := detectAppType("/nonexistent/path/to/binary.exe")
	if got != "chrome" {
		t.Errorf("detectAppType(nonexistent) = %q, want %q", got, "chrome")
	}
}

// TestDetectAppType_ZenPath checks that Zen Browser (a Firefox fork) is
// detected as "firefox" so the BiDi adapter is used.
func TestDetectAppType_ZenPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/usr/bin/zen-browser", "firefox"},
		{`C:\Program Files\Zen Browser\zen.exe`, "firefox"},
		{"/Applications/Zen Browser.app/Contents/MacOS/zen", "firefox"},
		{"/usr/bin/Zen", "firefox"}, // case-insensitive
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := detectAppType(tt.path)
			if got != tt.want {
				t.Errorf("detectAppType(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}
