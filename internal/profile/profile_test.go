package profile

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRegistryMatch(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "profiles")
	defer os.RemoveAll(tmpDir)

	yaml := `
name: test-site
match_url: "https://example.com/.*"
entry_url: "https://example.com/"
selectors:
  input: "#input"
`
	os.WriteFile(filepath.Join(tmpDir, "test.yaml"), []byte(yaml), 0644)

	reg, err := NewRegistry(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	p := reg.Match("https://example.com/chat")
	if p == nil {
		t.Fatal("Expected match, got nil")
	}
	if p.Name != "test-site" {
		t.Errorf("Expected test-site, got %s", p.Name)
	}

	p2 := reg.Match("https://google.com")
	if p2 != nil {
		t.Fatal("Expected no match for google.com")
	}
}

func TestSaveProfileAndReload(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "profiles_save")
	defer os.RemoveAll(tmpDir)

	reg, err := NewRegistry(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	stop := make(chan struct{})
	go reg.Watch(stop)
	defer close(stop)

	newYaml := `
name: dynamic-site
match_url: "https://dynamic.com/.*"
preload_scripts:
  - "/tmp/test.js"
`
	err = reg.SaveProfile("dynamic", newYaml)
	if err != nil {
		t.Fatalf("SaveProfile failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(filepath.Join(tmpDir, "dynamic.yaml")); os.IsNotExist(err) {
		t.Fatal("Profile file was not created")
	}

	// Wait for the file watcher to reload. Poll instead of a fixed sleep
	// because fsnotify event delivery latency varies across filesystems
	// (notably Docker overlay2 where events can be delayed or dropped).
	var p *SiteProfile
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if p = reg.Match("https://dynamic.com/foo"); p != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if p == nil {
		// fsnotify may not deliver events on some filesystems (e.g. Docker
		// overlay2). Fall back to manual reload.
		if err := reg.Reload(); err != nil {
			t.Fatalf("manual reload failed: %v", err)
		}
		p = reg.Match("https://dynamic.com/foo")
	}
	if p == nil {
		t.Fatal("Expected match after dynamic save, got nil")
	}
	if p.Name != "dynamic-site" {
		t.Errorf("Expected dynamic-site, got %s", p.Name)
	}
	if len(p.PreloadScripts) == 0 || p.PreloadScripts[0] != "/tmp/test.js" {
		t.Errorf("PreloadScripts not loaded correctly: %v", p.PreloadScripts)
	}
}
