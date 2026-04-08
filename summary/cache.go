package summary

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// Entry is a cached summary for a single session.
type Entry struct {
	Summary   string    `json:"summary"`
	LastUsed  time.Time `json:"last_used"`
	Generated time.Time `json:"generated"`
}

// Cache manages the on-disk summary cache at ~/.cache/sesh/summaries.json.
// It is safe for concurrent reads but writes should be serialized by the caller.
type Cache struct {
	path    string
	mu      sync.RWMutex
	entries map[string]Entry // keyed by session ID
}

// NewCache loads or creates the summary cache.
func NewCache() *Cache {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".cache", "sesh")
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		dir = filepath.Join(xdg, "sesh")
	}
	// On Windows, prefer %LOCALAPPDATA%\sesh if ~/.cache doesn't exist.
	if runtime.GOOS == "windows" {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
				dir = filepath.Join(localAppData, "sesh")
			}
		}
	}
	c := &Cache{
		path:    filepath.Join(dir, "summaries.json"),
		entries: make(map[string]Entry),
	}
	c.load()
	return c
}

// Get returns the cached summary for a session, if it exists and is still fresh.
// A summary is considered stale if the session's last_used time has changed
// since the summary was generated.
func (c *Cache) Get(sessionID string, lastUsed time.Time) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[sessionID]
	if !ok {
		return "", false
	}
	// Stale if the session has been used since we generated the summary,
	// with a 1-hour cooldown to avoid re-summarizing active sessions constantly.
	if lastUsed.After(e.LastUsed) && time.Since(e.Generated) > time.Hour {
		return "", false
	}
	return e.Summary, true
}

// Put stores a summary in the cache. Call Save() afterward to persist to disk.
func (c *Cache) Put(sessionID string, summary string, lastUsed time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[sessionID] = Entry{
		Summary:   summary,
		LastUsed:  lastUsed,
		Generated: time.Now(),
	}
}

// NeedsSummary returns session IDs from the given list that don't have a fresh
// cached summary. Returns them in the order provided.
func (c *Cache) NeedsSummary(sessions []SessionRef) []SessionRef {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var need []SessionRef
	for _, s := range sessions {
		e, ok := c.entries[s.ID]
		if !ok {
			need = append(need, s)
			continue
		}
		if s.LastUsed.After(e.LastUsed) && time.Since(e.Generated) > time.Hour {
			need = append(need, s)
		}
	}
	return need
}

// Len returns the number of cached summaries.
func (c *Cache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// Clear removes all cached summaries.
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]Entry)
}

// Save persists the cache to disk.
func (c *Cache) Save() error {
	c.mu.RLock()
	data, err := json.MarshalIndent(c.entries, "", "  ")
	c.mu.RUnlock()
	if err != nil {
		return fmt.Errorf("marshal summary cache: %w", err)
	}

	dir := filepath.Dir(c.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}

	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("write summary cache: %w", err)
	}
	if err := os.Rename(tmp, c.path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename summary cache: %w", err)
	}
	return nil
}

func (c *Cache) load() {
	data, err := os.ReadFile(c.path)
	if err != nil {
		return // no cache yet, that's fine
	}
	var entries map[string]Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		fmt.Fprintf(os.Stderr, "sesh: warning: corrupt summary cache, starting fresh\n")
		return
	}
	c.entries = entries
}

// SessionRef is a minimal reference to a session for cache lookups.
type SessionRef struct {
	ID       string
	LastUsed time.Time
}
