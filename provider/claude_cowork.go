package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// ClaudeCowork reads Claude Cowork sessions — the local agent-mode
// sessions run by the Claude desktop app. They are stored separately from
// Claude Code CLI sessions (handled by the Claude provider): Cowork keeps
// its own metadata and transcript files under the desktop app's Electron
// userData directory. This covers Cowork sessions only — the desktop app's
// Chat tab conversations live in separate web storage (the claude.ai
// IndexedDB) and are not surfaced here.
type ClaudeCowork struct {
	baseDir           string
	resumeCommand     string // override for resume command template
	collapseScheduled bool   // fold repeat runs of a scheduled task into one
}

// ClaudeCoworkOption configures the ClaudeCowork provider.
type ClaudeCoworkOption func(*ClaudeCowork)

// WithClaudeCoworkResumeCommand overrides the default resume command template.
// Use {{ID}} as a placeholder for the session ID.
func WithClaudeCoworkResumeCommand(cmd string) ClaudeCoworkOption {
	return func(c *ClaudeCowork) {
		c.resumeCommand = cmd
	}
}

// WithClaudeCoworkCollapseScheduled controls whether repeat runs of the same
// scheduled task are folded into a single representative session (the most
// recent run, annotated with the run count). Enabled by default. Pass false to
// surface every run separately.
func WithClaudeCoworkCollapseScheduled(collapse bool) ClaudeCoworkOption {
	return func(c *ClaudeCowork) {
		c.collapseScheduled = collapse
	}
}

// NewClaudeCowork creates a ClaudeCowork provider rooted at the platform's
// Electron userData directory for the Claude desktop app:
//   - macOS:   ~/Library/Application Support/Claude
//   - Windows: %AppData%\Claude
//   - Linux:   ~/.config/Claude
func NewClaudeCowork(opts ...ClaudeCoworkOption) *ClaudeCowork {
	c := &ClaudeCowork{collapseScheduled: true}
	if cfgDir, err := os.UserConfigDir(); err == nil {
		c.baseDir = filepath.Join(cfgDir, "Claude")
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *ClaudeCowork) Name() string { return "claude-cowork" }

// claudeCoworkMetadata is the shape of a local_<uuid>.json session metadata file.
type claudeCoworkMetadata struct {
	SessionID           string   `json:"sessionId"`
	CLISessionID        string   `json:"cliSessionId"`
	Title               string   `json:"title"`
	UserSelectedFolders []string `json:"userSelectedFolders"`
	CreatedAt           int64    `json:"createdAt"`
	LastActivityAt      int64    `json:"lastActivityAt"`
	LastFocusedAt       int64    `json:"lastFocusedAt"`
	InitialMessage      string   `json:"initialMessage"`
	ScheduledTaskID     string   `json:"scheduledTaskId"`
	SessionType         string   `json:"sessionType"`
	Model               string   `json:"model"`
	IsArchived          bool     `json:"isArchived"`
}

// ListSessions walks <baseDir>/local-agent-mode-sessions/<uuid>/<uuid>/local_<uuid>.json
// metadata files and builds a Session for each. It does not read
// <baseDir>/claude-code-sessions — those are the app's own VM/terminal
// Claude Code sessions, already surfaced by the built-in Claude provider.
func (c *ClaudeCowork) ListSessions(ctx context.Context) ([]Session, error) {
	if c.baseDir == "" {
		return nil, nil
	}
	root := filepath.Join(c.baseDir, "local-agent-mode-sessions")
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return nil, nil
	}

	matches, err := filepath.Glob(filepath.Join(root, "*", "*", "local_*.json"))
	if err != nil {
		return nil, fmt.Errorf("glob session metadata: %w", err)
	}

	// Each metadata file is large (embedded system prompt, MCP config, etc.),
	// so parse them concurrently rather than one at a time.
	type parsed struct {
		session Session
		schedID string
		cliID   string
		path    string
		ok      bool
	}
	results := make([]parsed, len(matches))
	var pwg sync.WaitGroup
	psem := make(chan struct{}, runtime.NumCPU())
	for i, path := range matches {
		pwg.Add(1)
		psem <- struct{}{}
		go func(i int, path string) {
			defer pwg.Done()
			defer func() { <-psem }()
			s, sched, cli, ok := c.parseMetadata(path)
			results[i] = parsed{s, sched, cli, path, ok}
		}(i, path)
	}
	pwg.Wait()

	var sessions []Session
	var schedIDs []string
	var cliIDs []string
	var paths []string
	for _, r := range results {
		if !r.ok {
			continue
		}
		sessions = append(sessions, r.session)
		schedIDs = append(schedIDs, r.schedID)
		cliIDs = append(cliIDs, r.cliID)
		paths = append(paths, r.path)
	}

	if c.collapseScheduled {
		// Group by scheduled task; keep any run the user actually engaged with
		// (a human turn beyond the trigger), collapse the pure-automation rest.
		keep := c.classifyRuns(sessions, schedIDs, paths, cliIDs)
		sessions = collapseGroups(sessions,
			func(i int) string { return schedIDs[i] },
			func(i int) bool { return keep[i] },
		)
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].LastUsed.After(sessions[j].LastUsed)
	})

	return sessions, nil
}

// classifyRuns returns, per session, whether it should be kept out of its
// collapse group: a scheduled run the user actually engaged with (a human turn
// beyond the trigger) is kept; a pure-automation run is not. Counting reads
// each transcript, so the reads run concurrently.
//
// KNOWN LIMITATION: this recomputes turn counts on every invocation, reading
// every scheduled run's transcript each time. It's sub-second at today's scale
// but the cost grows with history. The fix is deliberately NOT a bespoke cache
// here — the turn count should be computed inside sesh's existing summary/index
// pass (which already reads each transcript) and stored alongside the summary,
// so collapse reuses the standard cache instead of re-reading. Tracked as
// follow-up; this ships the correct behavior first, the caching second.
func (c *ClaudeCowork) classifyRuns(sessions []Session, schedIDs, paths, cliIDs []string) []bool {
	keep := make([]bool, len(sessions))
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.NumCPU())
	for i := range sessions {
		if schedIDs[i] == "" {
			continue // ungrouped: keep flag is unused
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			keep[i] = humanTurns(paths[i], cliIDs[i]) >= 1
		}(i)
	}
	wg.Wait()
	return keep
}

// collapseGroups folds sessions sharing a group key into a single
// representative, keeping any session the keep predicate marks as distinct.
// Sessions whose group key is "" are never grouped. This is provider-agnostic:
// the caller supplies what "same group" and "worth keeping" mean. The
// claude-cowork provider groups by scheduled-task ID and keeps runs with a
// human turn, so a recurring automation collapses to one entry while the runs
// you engaged with stay individually listed. Output order is unspecified; the
// caller sorts afterwards.
func collapseGroups(sessions []Session, groupKey func(i int) string, keep func(i int) bool) []Session {
	groups := make(map[string][]int)
	var out []Session
	for i := range sessions {
		key := groupKey(i)
		if key == "" || keep(i) {
			out = append(out, sessions[i]) // ungrouped, or distinct: pass through
			continue
		}
		groups[key] = append(groups[key], i)
	}

	// Iterate group keys in sorted order so the result is deterministic before
	// the caller's sort (which only orders by LastUsed and leaves ties as-is).
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out = append(out, groupRepresentative(sessions, groups[k]))
	}
	return out
}

// groupRepresentative builds the single session that stands in for a collapsed
// group: the most recent member (so its ID resumes the latest run and its text
// is the freshest), with Created spanning back to the earliest member and the
// title annotated with the collapsed count.
func groupRepresentative(sessions []Session, idxs []int) Session {
	rep := sessions[idxs[0]]
	earliest := rep.Created
	for _, i := range idxs {
		s := sessions[i]
		if s.LastUsed.After(rep.LastUsed) {
			rep = s
		}
		if !s.Created.IsZero() && (earliest.IsZero() || s.Created.Before(earliest)) {
			earliest = s.Created
		}
	}
	rep.Created = earliest
	if n := len(idxs); n > 1 {
		rep.Title = fmt.Sprintf("%s (%d runs)", rep.Title, n)
	}
	return rep
}

// humanTurns counts the user prompts in a session's transcript that aren't the
// scheduled-task trigger or a system-injected message — i.e. turns where a
// person actually typed something. Returns 0 when the transcript is missing or
// unreadable, so an un-inspectable run is treated as routine. metaPath locates
// the sibling sandbox; cliID (already parsed by parseMetadata) names the
// transcript, so this avoids re-reading the large metadata file.
func humanTurns(metaPath, cliID string) int {
	sandbox := strings.TrimSuffix(metaPath, ".json")
	projects := filepath.Join(sandbox, ".claude", "projects")

	// cliID (from the already-parsed metadata) names the authoritative
	// transcript; fall back to any nested transcript if it's missing.
	var tx string
	if cliID != "" {
		if g, _ := filepath.Glob(filepath.Join(projects, "*", cliID+".jsonl")); len(g) > 0 {
			tx = g[0]
		}
	}
	if tx == "" {
		if g, _ := filepath.Glob(filepath.Join(projects, "*", "*.jsonl")); len(g) > 0 {
			tx = g[0]
		}
	}
	if tx == "" {
		return 0
	}
	return countHumanTurns(tx)
}

// countHumanTurns scans a Claude Code transcript (JSONL) and counts user
// messages with real text content, excluding tool results and the automated
// prompts (scheduled-task trigger, system reminders, local-command wrappers).
func countHumanTurns(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()

	n := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 32*1024*1024) // transcripts have long lines
	for scanner.Scan() {
		line := scanner.Bytes()
		if !bytes.Contains(line, []byte(`"user"`)) {
			continue
		}
		var e struct {
			Type    string `json:"type"`
			Role    string `json:"role"`
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &e) != nil {
			continue
		}
		if e.Type != "user" && e.Role != "user" {
			continue
		}
		text := strings.TrimSpace(userText(e.Message.Content))
		if text == "" {
			continue // tool_result or empty content
		}
		switch {
		case strings.HasPrefix(text, "<scheduled-task"),
			strings.HasPrefix(text, "<system-reminder"),
			strings.HasPrefix(text, "<local-command"),
			strings.HasPrefix(text, "<command-"),
			strings.HasPrefix(text, "Caveat:"):
			continue
		}
		n++
	}
	return n
}

// userText extracts the text of a transcript user message, whose content is
// either a bare string or a list of blocks (only "text" blocks are user input;
// "tool_result" blocks are tool output, not a human turn).
func userText(content json.RawMessage) string {
	if len(content) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(content, &s) == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(content, &blocks) == nil {
		for _, b := range blocks {
			if b.Type == "text" {
				return b.Text
			}
		}
	}
	return ""
}

// parseMetadata reads and validates a single metadata file, returning the
// session, its scheduled-task ID (empty if not a scheduled run), and ok=false
// for malformed or incomplete records.
func (c *ClaudeCowork) parseMetadata(path string) (Session, string, string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Session{}, "", "", false
	}

	// The desktop app writes this directory live, so a file caught mid-write
	// parses as invalid; skip it silently, as the other providers do for
	// malformed records.
	var meta claudeCoworkMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return Session{}, "", "", false
	}

	if meta.SessionID == "" {
		return Session{}, "", "", false
	}

	// Archived sessions are hidden in the app; exclude them like the OpenCode
	// provider excludes its archived sessions.
	if meta.IsArchived {
		return Session{}, "", "", false
	}

	title := meta.Title
	if title == "" {
		title = firstLine(meta.InitialMessage, 120)
	}
	if title == "" {
		title = meta.SessionID
	}

	// Directory is the attached project folder. Folderless sessions get no
	// directory: the metadata's cwd points inside the app's own sandbox, not a
	// real project path, so surfacing it would be misleading.
	directory := ""
	if len(meta.UserSelectedFolders) > 0 {
		directory = meta.UserSelectedFolders[0]
	}

	var created time.Time
	if meta.CreatedAt != 0 {
		created = time.UnixMilli(meta.CreatedAt)
	}

	var lastUsed time.Time
	switch {
	case meta.LastActivityAt != 0:
		lastUsed = time.UnixMilli(meta.LastActivityAt)
	case meta.LastFocusedAt != 0:
		lastUsed = time.UnixMilli(meta.LastFocusedAt)
	default:
		lastUsed = created
	}

	searchParts := []string{
		meta.Title,
		directory,
		meta.InitialMessage,
		meta.ScheduledTaskID,
		meta.SessionType,
		meta.Model,
	}

	return Session{
		Agent:      "claude-cowork",
		ID:         meta.SessionID,
		Title:      title,
		Created:    created,
		LastUsed:   lastUsed,
		Directory:  directory,
		SearchText: strings.Join(searchParts, " "),
		// The app's own titles are good display names; titles derived from
		// the initial message still benefit from LLM summarization.
		CuratedTitle: meta.Title != "",
	}, meta.ScheduledTaskID, meta.CLISessionID, true
}

// firstLine returns the first non-empty line of s, truncated to max
// characters (adding an ellipsis if truncated).
func firstLine(s string, max int) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if r := []rune(line); len(r) > max {
			return string(r[:max-3]) + "..."
		}
		return line
	}
	return ""
}

func (c *ClaudeCowork) ResumeCommand(session Session) string {
	if c.resumeCommand != "" {
		return strings.ReplaceAll(c.resumeCommand, "{{ID}}", session.ID)
	}
	// Cowork sessions are owned by the desktop app — there's no CLI resume
	// path into a running Cowork session. Best effort: bring the app to the
	// foreground so the user can find the session themselves.
	switch runtime.GOOS {
	case "darwin":
		return "open -a Claude"
	case "windows":
		return fmt.Sprintf("start %s", Q("claude://"))
	default:
		return fmt.Sprintf("xdg-open %s", Q("claude://"))
	}
}

// metadataPath locates the local_<uuid>.json metadata file for a session ID
// by globbing under the two-level session tree, mirroring ListSessions.
func (c *ClaudeCowork) metadataPath(sessionID string) (string, bool) {
	if c.baseDir == "" {
		return "", false
	}
	root := filepath.Join(c.baseDir, "local-agent-mode-sessions")
	matches, err := filepath.Glob(filepath.Join(root, "*", "*", sessionID+".json"))
	if err != nil || len(matches) == 0 {
		return "", false
	}
	return matches[0], true
}

// SessionText returns the conversation text for a session. It prefers the
// transcript named after the session's cliSessionId (the authoritative one,
// since a sandbox can hold several transcripts), then any other nested Claude
// Code transcript, then audit.jsonl — all the same JSONL shape, parsed by the
// shared extractConversationText helper.
func (c *ClaudeCowork) SessionText(ctx context.Context, sessionID string) string {
	metaPath, ok := c.metadataPath(sessionID)
	if !ok {
		return ""
	}
	sandbox := strings.TrimSuffix(metaPath, ".json")
	projects := filepath.Join(sandbox, ".claude", "projects")

	// Preferred: the transcript whose filename matches cliSessionId.
	if data, err := os.ReadFile(metaPath); err == nil {
		var meta claudeCoworkMetadata
		if json.Unmarshal(data, &meta) == nil && meta.CLISessionID != "" {
			preferred, _ := filepath.Glob(filepath.Join(projects, "*", meta.CLISessionID+".jsonl"))
			for _, path := range preferred {
				if text := extractConversationText(path); text != "" {
					return text
				}
			}
		}
	}

	// Fallback: any nested transcript, then audit.jsonl.
	matches, _ := filepath.Glob(filepath.Join(projects, "*", "*.jsonl"))
	for _, path := range matches {
		if text := extractConversationText(path); text != "" {
			return text
		}
	}
	return extractConversationText(filepath.Join(sandbox, "audit.jsonl"))
}

var _ Provider = (*ClaudeCowork)(nil)
