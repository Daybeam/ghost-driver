package browser

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/Daybeam/ghost-driver/internal/profile"
)

const (
	maxCacheEntries = 500
	cacheEntryTTL   = 7 * 24 * 60 * 60 // 7 days in seconds
)

type ActionCacheEntry struct {
	Selector     string              `json:"selector,omitempty"`
	ActionType   string              `json:"action_type"`
	Fingerprint  profile.Fingerprint `json:"fingerprint"`
	SuccessCount int                 `json:"success_count"`
	LastUsedAt   int64               `json:"last_used_at"`
}

func (e ActionCacheEntry) FingerprintMatches(fp *profile.Fingerprint) bool {
	if fp == nil || e.Fingerprint.TagName == "" {
		return false
	}
	if e.Fingerprint.TagName != fp.TagName ||
		e.Fingerprint.Text != fp.Text ||
		e.Fingerprint.ID != fp.ID ||
		e.Fingerprint.Depth != fp.Depth {
		return false
	}
	if len(e.Fingerprint.Classes) != len(fp.Classes) {
		return false
	}
	for i, c := range e.Fingerprint.Classes {
		if c != fp.Classes[i] {
			return false
		}
	}
	return true
}

type ActionCache struct {
	mu          sync.RWMutex
	path        string
	entries     map[string]ActionCacheEntry
	lastPersist time.Time
}

func NewActionCache(path string) *ActionCache {
	ac := &ActionCache{
		path:    path,
		entries: make(map[string]ActionCacheEntry),
	}
	ac.load()
	return ac
}

func (ac *ActionCache) load() {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	data, err := os.ReadFile(ac.path)
	if err != nil {
		return
	}
	if err := json.Unmarshal(data, &ac.entries); err != nil {
		fmt.Printf("[ghost-driver] Warning: failed to parse action cache: %v\n", err)
	}
}

func (ac *ActionCache) Get(url, intent string) (ActionCacheEntry, bool) {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	e, ok := ac.entries[url+"\x00"+intent]
	return e, ok
}

func (ac *ActionCache) Put(url, intent, selector, actionType string, fp profile.Fingerprint) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	key := url + "\x00" + intent
	now := time.Now().Unix()
	e := ac.entries[key]
	e.Selector = selector
	e.ActionType = actionType
	e.Fingerprint = fp
	e.SuccessCount++
	e.LastUsedAt = now
	ac.entries[key] = e
	ac.persistLocked()
}

func (ac *ActionCache) RecordSuccess(url, intent string) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	key := url + "\x00" + intent
	if e, ok := ac.entries[key]; ok {
		e.SuccessCount++
		e.LastUsedAt = time.Now().Unix()
		ac.entries[key] = e
		ac.persistLocked()
	}
}

func (ac *ActionCache) evictLocked() {
	now := time.Now().Unix()
	for k, e := range ac.entries {
		if now-e.LastUsedAt > cacheEntryTTL {
			delete(ac.entries, k)
		}
	}
	if len(ac.entries) <= maxCacheEntries {
		return
	}
	type kv struct {
		key string
		ts  int64
	}
	sorted := make([]kv, 0, len(ac.entries))
	for k, e := range ac.entries {
		sorted = append(sorted, kv{k, e.LastUsedAt})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].ts < sorted[j].ts
	})
	excess := len(ac.entries) - maxCacheEntries
	for i := 0; i < excess; i++ {
		delete(ac.entries, sorted[i].key)
	}
}

func (ac *ActionCache) persistLocked() {
	now := time.Now()
	if now.Sub(ac.lastPersist) < 2*time.Second {
		return
	}
	ac.lastPersist = now
	ac.evictLocked()
	data, err := json.MarshalIndent(ac.entries, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(ac.path, data, 0644); err != nil {
		fmt.Printf("[ghost-driver] Warning: failed to persist action cache: %v\n", err)
	}
}
