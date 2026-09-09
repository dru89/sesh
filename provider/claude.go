package provider

import (
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

func (c *Claude) Name() string { return "claude-code" }

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
		firstReal     string // earliest real prompt (not a slash/shell command)
		firstRealTime int64
		project       string
		firstTime     int64
		lastTime      int64
		prompts       []string
	}
	grouped := make(map[string]*sessionInfo)

	scanner := newJSONLScanner(f)
	for scanner.Scan() {
		var entry historyEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil || entry.SessionID == "" {
			continue
		}

		info, exists := grouped[entry.SessionID]
		if !exists {
			info = &sessionInfo{
				project:   entry.Project,
				firstTime: entry.Timestamp,
				lastTime:  entry.Timestamp,
			}
			grouped[entry.SessionID] = info
		}
		if entry.Timestamp < info.firstTime {
			info.firstTime = entry.Timestamp
		}
		if entry.Timestamp > info.lastTime {
			info.lastTime = entry.Timestamp
		}
		// Track real prompts, skipping shell commands and slash commands.
		// The earliest one becomes the title; a session with none is either
		// junk (someone opening claude just to run /login) or was started
		// with an initial prompt argument — resolved below.
		if entry.Display != "" && !IsCommandInput(entry.Display) {
			if info.firstReal == "" || entry.Timestamp < info.firstRealTime {
				info.firstReal = entry.Display
				info.firstRealTime = entry.Timestamp
			}
			if len(info.prompts) < 5 {
				info.prompts = append(info.prompts, entry.Display)
			}
		}
	}
	warnScanErr(scanner.Err(), historyPath)

	// Load slugs from transcript files.
	slugs := c.loadSlugs()

	var sessions []Session
	for id, info := range grouped {
		rawTitle := info.firstReal
		if rawTitle == "" {
			// History shows only commands for this session — but history only
			// records interactively typed prompts, so a session started as
			// `claude "do the thing"` looks command-only here while its
			// transcript holds real work. Check the transcript before
			// dropping: a real prompt there rescues the session (and titles
			// it); none means command-only junk.
			rawTitle = c.firstTranscriptPrompt(id)
			if rawTitle == "" {
				continue
			}
		}

		searchParts := []string{rawTitle, info.project}
		searchParts = append(searchParts, info.prompts...)
		slug := slugs[id]
		if slug != "" {
			searchParts = append(searchParts, slug)
		}

		// The first prompt is often multi-line (pasted text, code blocks).
		// Flatten it to a single line so the title doesn't render across
		// multiple rows in the picker or wrap in `show`/`--json` output.
		title := FlattenWhitespace(rawTitle)
		if len(title) > 120 {
			title = title[:117] + "..."
		}

		sessions = append(sessions, Session{
			Agent:      "claude-code",
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

// firstTranscriptPrompt scans the session's transcript for the earliest real
// user prompt. Used for sessions whose history entries are all commands:
// history.jsonl only records interactively typed input, so a session started
// with an initial prompt argument has its real content only in the transcript.
func (c *Claude) firstTranscriptPrompt(sessionID string) string {
	projectsDir := filepath.Join(c.baseDir, "projects")
	dirs, err := os.ReadDir(projectsDir)
	if err != nil {
		return ""
	}
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		path := filepath.Join(projectsDir, dir.Name(), sessionID+".jsonl")
		if prompt := firstUserPrompt(path); prompt != "" {
			return prompt
		}
	}
	return ""
}

// firstUserPrompt reads a transcript JSONL and returns the first user message
// that is a real typed prompt — not a meta entry, a command input, or the
// transcript record of a slash command execution.
func firstUserPrompt(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := newJSONLScanner(f)
	for scanner.Scan() {
		var raw struct {
			IsMeta  bool `json:"isMeta"`
			Message struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &raw); err != nil {
			continue
		}
		if raw.IsMeta || raw.Message.Role != "user" {
			continue
		}
		var s string
		if err := json.Unmarshal(raw.Message.Content, &s); err != nil {
			continue
		}
		s = strings.TrimSpace(s)
		if s == "" || IsCommandInput(s) || isCommandRecord(s) {
			continue
		}
		return s
	}
	warnScanErr(scanner.Err(), path)
	return ""
}

// isCommandRecord reports whether transcript user-message content is the
// record of a slash command execution rather than a typed prompt. Claude Code
// writes these as tagged blocks in user messages.
func isCommandRecord(s string) bool {
	for _, prefix := range []string{"<command-name>", "<local-command-stdout>", "<local-command-caveat>"} {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}

// extractSlug reads the first few lines of a session JSONL to find the slug.
func (c *Claude) extractSlug(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := newJSONLScanner(f)
	for i := 0; i < 20 && scanner.Scan(); i++ {
		var msg struct {
			Slug string `json:"slug"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &msg); err == nil && msg.Slug != "" {
			return msg.Slug
		}
	}
	warnScanErr(scanner.Err(), path)
	return ""
}

func (c *Claude) ResumeCommand(session Session) string {
	var cmd string
	if c.resumeCommand != "" {
		cmd = strings.ReplaceAll(c.resumeCommand, "{{ID}}", session.ID)
	} else {
		cmd = fmt.Sprintf("claude --resume %s", Q(session.ID))
	}
	return CdAndRun(session.Directory, cmd)
}

// SessionText returns the conversation text for a session, interleaving user
// prompts and assistant responses. Reads the session transcript file and
// extracts both user and assistant message content.
func (c *Claude) SessionText(ctx context.Context, sessionID string) string {
	return transcriptTextFromProjects(c.baseDir, sessionID)
}

// transcriptTextFromProjects finds <claudeDir>/projects/*/<sessionID>.jsonl and
// extracts its conversation text. Shared by the Claude and ClaudeDesktop
// providers, which both read the same transcript store.
func transcriptTextFromProjects(claudeDir, sessionID string) string {
	// Find the transcript file by scanning project directories.
	projectsDir := filepath.Join(claudeDir, "projects")
	dirs, err := os.ReadDir(projectsDir)
	if err != nil {
		return ""
	}

	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		path := filepath.Join(projectsDir, dir.Name(), sessionID+".jsonl")
		if text := extractConversationText(path); text != "" {
			return text
		}
	}
	return ""
}

// extractConversationText reads a session JSONL and pulls both user and
// assistant message text in conversation order. Shared by the Claude,
// ClaudeDesktop, and ClaudeCowork providers, which all write/read the same
// transcript line shape: {"message": {"role": ..., "content": ...}}.
func extractConversationText(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	// Claude streams assistant messages as multiple JSONL lines with the same
	// message ID, each with progressively more content. We keep the longest
	// text for each message ID to get the final version.
	type msgEntry struct {
		role string
		text string
		seq  int // insertion order
	}
	messages := make(map[string]*msgEntry)
	var order []string // unique message keys in order
	seq := 0

	scanner := newJSONLScanner(f)
	for scanner.Scan() {
		var raw struct {
			Type    string `json:"type"`
			Message struct {
				ID      string          `json:"id"`
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"message"`
			ParentUUID string `json:"parentUuid"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &raw); err != nil {
			continue
		}

		role := raw.Message.Role
		if role != "user" && role != "assistant" {
			continue
		}

		// Extract text content.
		var text string
		if role == "user" {
			// User content is either a string or an array.
			var s string
			if err := json.Unmarshal(raw.Message.Content, &s); err == nil {
				text = s
			}
		} else {
			// Assistant content is an array of content blocks.
			var blocks []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if err := json.Unmarshal(raw.Message.Content, &blocks); err == nil {
				var textParts []string
				for _, b := range blocks {
					if b.Type == "text" && b.Text != "" {
						textParts = append(textParts, b.Text)
					}
				}
				text = strings.Join(textParts, "")
			}
		}

		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}

		// Build a unique key for dedup: message ID for assistants, parentUUID for users.
		key := raw.Message.ID
		if key == "" {
			key = raw.ParentUUID
		}
		if key == "" {
			key = fmt.Sprintf("anon-%d", seq)
		}

		if existing, ok := messages[key]; ok {
			// Keep the longer version (streaming builds up progressively).
			if len(text) > len(existing.text) {
				existing.text = text
			}
		} else {
			messages[key] = &msgEntry{role: role, text: text, seq: seq}
			order = append(order, key)
			seq++
		}
	}
	warnScanErr(scanner.Err(), path)

	// Build conversation text in order.
	var parts []string
	for _, key := range order {
		entry := messages[key]
		if entry.role == "user" {
			// Skip command inputs and slash command execution records.
			if IsCommandInput(entry.text) || isCommandRecord(entry.text) {
				continue
			}
			parts = append(parts, "User: "+entry.text)
		} else {
			parts = append(parts, "Assistant: "+entry.text)
		}
	}
	return strings.Join(parts, "\n\n")
}

var _ Provider = (*Claude)(nil)
