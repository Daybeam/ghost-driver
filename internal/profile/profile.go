package profile

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v3"
)

type NetworkConfig struct {
	StreamURLKeyword string `yaml:"stream_url_keyword"`
	StreamType       string `yaml:"stream_type"` // "sse", "json"
}

type Fingerprint struct {
	TagName    string            `yaml:"tag_name" json:"tagName"`
	ID         string            `yaml:"id" json:"id"`
	Classes    []string          `yaml:"classes" json:"classes"`
	Attributes map[string]string `yaml:"attributes" json:"attributes"`
	Text       string            `yaml:"text" json:"text"`
	Depth      int               `yaml:"depth" json:"depth"`
}

type SiteProfile struct {
	Name            string                 `yaml:"name"`
	MatchURL        string                 `yaml:"match_url"`
	EntryURL        string                 `yaml:"entry_url"`
	Selectors       map[string]string      `yaml:"selectors"`
	Hints           []string               `yaml:"hints"`
	Fingerprints    map[string]Fingerprint `yaml:"fingerprints"`
	BehavioralHints map[string]string      `yaml:"behavioral_hints"`
	Network         NetworkConfig          `yaml:"network"`
	NavStrategy     string                 `yaml:"nav_strategy"`
	PreloadScripts  []string               `yaml:"preload_scripts"`

	matchRe *regexp.Regexp // pre-compiled on load (recompiling per Match call was a perf hit)
}

type Registry struct {
	mu       sync.RWMutex
	profiles map[string]*SiteProfile
	dir      string
}

func NewRegistry(dir string) (*Registry, error) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, err
		}
	}

	r := &Registry{
		profiles: make(map[string]*SiteProfile),
		dir:      dir,
	}

	if err := r.Reload(); err != nil {
		return nil, err
	}

	return r, nil
}

func (r *Registry) Reload() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	files, err := os.ReadDir(r.dir)
	if err != nil {
		return err
	}

	newProfiles := make(map[string]*SiteProfile)
	for _, f := range files {
		ext := filepath.Ext(f.Name())
		if f.IsDir() || (ext != ".yaml" && ext != ".yml") {
			continue
		}

		path := filepath.Join(r.dir, f.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			log.Printf("Error reading profile %s: %v", path, err)
			continue
		}

		var p SiteProfile
		if err := yaml.Unmarshal(data, &p); err != nil {
			log.Printf("Error unmarshaling profile %s: %v", path, err)
			continue
		}

		if p.Name == "" {
			p.Name = f.Name()
		}
		// Pre-compile the match regex once per load (was recompiled on every
		// Match call — a measurable cost on every Navigate).
		if p.MatchURL != "" {
			if re, err := regexp.Compile(p.MatchURL); err == nil {
				p.matchRe = re
			}
		}
		newProfiles[p.Name] = &p
	}

	r.profiles = newProfiles
	log.Printf("Loaded %d site profiles from %s", len(r.profiles), r.dir)
	return nil
}

func (r *Registry) Match(url string) *SiteProfile {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, p := range r.profiles {
		if p.matchRe != nil && p.matchRe.MatchString(url) {
			cp := *p // shallow copy: captures Fingerprints map ref at snapshot time
			return &cp
		}
	}
	return nil
}

func (r *Registry) GetByName(name string) *SiteProfile {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.profiles[name]
	if !ok {
		return nil
	}
	cp := *p
	return &cp
}

func (r *Registry) SaveProfile(name string, yamlContent string) error {
	// Validate name to prevent path traversal: it is used directly to build a file path.
	if name == "" || strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return fmt.Errorf("invalid profile name %q: must not be empty or contain path separators or '..'", name)
	}
	path := filepath.Join(r.dir, name+".yaml")

	// Basic validation: try to unmarshal it
	var p SiteProfile
	if err := yaml.Unmarshal([]byte(yamlContent), &p); err != nil {
		return fmt.Errorf("invalid YAML: %w", err)
	}

	return os.WriteFile(path, []byte(yamlContent), 0644)
}

func (r *Registry) UpsertFingerprint(profileName, id string, f Fingerprint) error {
	r.mu.Lock()
	p, ok := r.profiles[profileName]
	if !ok {
		r.mu.Unlock()
		return fmt.Errorf("profile not found: %s", profileName)
	}
	newFp := make(map[string]Fingerprint, len(p.Fingerprints)+1)
	for k, v := range p.Fingerprints {
		newFp[k] = v
	}
	newFp[id] = f
	p.Fingerprints = newFp

	data, err := yaml.Marshal(p)
	r.mu.Unlock()
	if err != nil {
		return err
	}

	return r.SaveProfile(profileName, string(data))
}

func (r *Registry) Watch(ctx chan struct{}) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("Error creating watcher: %v", err)
		return
	}
	defer watcher.Close()

	if err := watcher.Add(r.dir); err != nil {
		log.Printf("Error watching directory: %v", err)
		return
	}

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Create == fsnotify.Create {
				log.Printf("Profile changed: %s, reloading...", event.Name)
				r.Reload()
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Printf("Watcher error: %v", err)
		case <-ctx:
			return
		}
	}
}
