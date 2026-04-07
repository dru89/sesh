package provider

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Claude reads sessions from Claude Code's history.jsonl and project transcript files.
type Claude struct {
	baseDir       string
	resumeCommand string // override for resume command template
}

// ClaudeOption configures the Claude provider.
type ClaudeOption func(*Claude)

// WithClaudeResumeCommand overrides the default resume command template.
// Use {{ID}} as a placeholder for the session ID.
func WithClaudeResumeCommand(cmd string) ClaudeOption {
	return func(c *Claude) {
		c.resumeCommand = cmd
	}
}

func NewClaude(opts ...ClaudeOption) *Claude {
	home, _ := os.UserHomeDir()
	c := &Claude{baseDir: filepath.Join(home, ".claude")}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Claude) Name() string { return "claude" }

// historyEntry represents a single line from history.jsonl.
type historyEntry struct {
	Display   string `json:"display"`
	Timestamp int64  `json:"timestamp"`
	Project   string `json:"project"`
	SessionID string `json:"sessionId"`
}

func (c *Claude) ListSessions(ctx context.Context) ([]Session, error) {
	historyPath := filepath.Join(c.baseDir, "history.jsonl")
	if _, err := os.Stat(historyPath); os.IsNotExist(err) {
		return nil, nil
	}

	f, err := os.Open(historyPath)
	if err != nil {
		return nil, fmt.Errorf("open history.jsonl: %w", err)
	}
	defer f.Close()

	// Group entries by session ID.
	type sessionInfo struct {
		firstDisplay string
		project      string
		firstTime    int64
		lastTime     int64
		prompts      []string
	}
	grouped := make(map[string]*sessionInfo)

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		var entry historyEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil || entry.SessionID == "" {
			continue
		}

		info, exists := grouped[entry.SessionID]
		if !exists {
			info = &sessionInfo{
				firstDisplay: entry.Display,
				project:      entry.Project,
				firstTime:    entry.Timestamp,
				lastTime:     entry.Timestamp,
			}
			grouped[entry.SessionID] = info
		}
		if entry.Timestamp < info.firstTime {
			info.firstTime = entry.Timestamp
			info.firstDisplay = entry.Display
		}
		if entry.Timestamp > info.lastTime {
			info.lastTime = entry.Timestamp
		}
		// Collect user prompts for search, skip shell commands and slash commands.
		if entry.Display != "" &&
			!strings.HasPrefix(entry.Display, "!") &&
			!strings.HasPrefix(entry.Display, "/") {
			if len(info.prompts) < 5 {
				info.prompts = append(info.prompts, entry.Display)
			}
		}
	}

	// Load slugs from transcript files.
	slugs := c.loadSlugs()

	var sessions []Session
	for id, info := range grouped {
		searchParts := []string{info.firstDisplay, info.project}
		searchParts = append(searchParts, info.prompts...)
		slug := slugs[id]
		if slug != "" {
			searchParts = append(searchParts, slug)
		}

		title := info.firstDisplay
		if len(title) > 120 {
			title = title[:117] + "..."
		}

		sessions = append(sessions, Session{
			Agent:      "claude",
			ID:         id,
			Title:      title,
			Slug:       slug,
			Created:    time.UnixMilli(info.firstTime),
			LastUsed:   time.UnixMilli(info.lastTime),
			Directory:  info.project,
			SearchText: strings.Join(searchParts, " "),
		})
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].LastUsed.After(sessions[j].LastUsed)
	})

	return sessions, nil
}

// loadSlugs scans project transcript files for the slug field.
// Claude Code sets the slug on messages after the first exchange.
func (c *Claude) loadSlugs() map[string]string {
	slugs := make(map[string]string)
	projectsDir := filepath.Join(c.baseDir, "projects")
	dirs, err := os.ReadDir(projectsDir)
	if err != nil {
		return slugs
	}

	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(projectsDir, dir.Name()))
		if err != nil {
			continue
		}
		for _, f := range files {
			if !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			sessionID := strings.TrimSuffix(f.Name(), ".jsonl")
			slug := c.extractSlug(filepath.Join(projectsDir, dir.Name(), f.Name()))
			if slug != "" {
				slugs[sessionID] = slug
			}
		}
	}
	return slugs
}

// extractSlug reads the first few lines of a session JSONL to find the slug.
func (c *Claude) extractSlug(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)
	for i := 0; i < 20 && scanner.Scan(); i++ {
		var msg struct {
			Slug string `json:"slug"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &msg); err == nil && msg.Slug != "" {
			return msg.Slug
		}
	}
	return ""
}

func (c *Claude) ResumeCommand(session Session) string {
	var cmd string
	if c.resumeCommand != "" {
		cmd = strings.ReplaceAll(c.resumeCommand, "{{ID}}", session.ID)
	} else {
		cmd = fmt.Sprintf("claude --resume %s", ShellQuote(session.ID))
	}
	if session.Directory != "" {
		return fmt.Sprintf("cd %s && %s", ShellQuote(session.Directory), cmd)
	}
	return cmd
}

var _ Provider = (*Claude)(nil)
