package provider

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestOpenCodeListSessions(t *testing.T) {
	dbPath := createTestOpenCodeDB(t)
	oc := &OpenCode{dbPath: dbPath}

	sessions, err := oc.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}

	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}

	// Sessions should be ordered by time_updated DESC.
	if sessions[0].ID != "ses_newer" {
		t.Errorf("first session should be ses_newer, got %s", sessions[0].ID)
	}
	if sessions[1].ID != "ses_older" {
		t.Errorf("second session should be ses_older, got %s", sessions[1].ID)
	}

	// Check fields.
	s := sessions[0]
	if s.Agent != "opencode" {
		t.Errorf("agent = %q, want opencode", s.Agent)
	}
	if s.Title != "Fix auth middleware" {
		t.Errorf("title = %q, want %q", s.Title, "Fix auth middleware")
	}
	if s.Slug != "eager-cactus" {
		t.Errorf("slug = %q, want %q", s.Slug, "eager-cactus")
	}
	if s.Directory != "/home/user/project" {
		t.Errorf("directory = %q, want %q", s.Directory, "/home/user/project")
	}
}

func TestOpenCodeExcludesArchived(t *testing.T) {
	dbPath := createTestOpenCodeDB(t)

	// Archive one session.
	db, _ := sql.Open("sqlite", dbPath)
	db.Exec("UPDATE session SET time_archived = 1000 WHERE id = 'ses_older'")
	db.Close()

	oc := &OpenCode{dbPath: dbPath}
	sessions, err := oc.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("expected 1 session (archived excluded), got %d", len(sessions))
	}
}

func TestOpenCodeExcludesSubagentSessions(t *testing.T) {
	dbPath := createTestOpenCodeDB(t)
	oc := &OpenCode{dbPath: dbPath}

	sessions, err := oc.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}

	// Should only include top-level sessions (ses_newer, ses_older), not ses_subagent.
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions (subagent excluded), got %d", len(sessions))
	}
	for _, s := range sessions {
		if s.ID == "ses_subagent" {
			t.Error("subagent session should not be included in results")
		}
	}
}

func TestOpenCodeMissingDB(t *testing.T) {
	oc := &OpenCode{dbPath: "/nonexistent/path/opencode.db"}
	sessions, err := oc.ListSessions(context.Background())
	if err != nil {
		t.Errorf("expected nil error for missing DB, got %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(sessions))
	}
}

func TestOpenCodeSessionText(t *testing.T) {
	dbPath := createTestOpenCodeDB(t)
	oc := &OpenCode{dbPath: dbPath}

	text := oc.SessionText(context.Background(), "ses_newer")
	if text == "" {
		t.Error("expected non-empty session text")
	}
	want := "User: Help me fix the auth middleware\n\nAssistant: I'll take a look at the auth middleware code."
	if text != want {
		t.Errorf("got %q, want %q", text, want)
	}
}

func TestOpenCodeResumeCommand(t *testing.T) {
	oc := &OpenCode{}
	s := Session{ID: "ses_abc", Directory: "/home/user/project"}
	got := oc.ResumeCommand(s)
	want := CdAndRun("/home/user/project", "opencode --session ses_abc")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestOpenCodeResumeCommandOverride(t *testing.T) {
	oc := &OpenCode{resumeCommand: "ca opencode -s {{ID}}"}
	s := Session{ID: "ses_abc", Directory: "/home/user/project"}
	got := oc.ResumeCommand(s)
	want := CdAndRun("/home/user/project", "ca opencode -s ses_abc")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestOpenCodeResumeCommandNoDir(t *testing.T) {
	oc := &OpenCode{}
	s := Session{ID: "ses_abc"}
	got := oc.ResumeCommand(s)
	want := "opencode --session ses_abc"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// createTestOpenCodeDB creates a minimal SQLite database with the OpenCode schema.
func createTestOpenCodeDB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "opencode.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Create tables.
	for _, ddl := range []string{
		`CREATE TABLE session (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL DEFAULT 'global',
			parent_id TEXT,
			slug TEXT NOT NULL DEFAULT '',
			directory TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL DEFAULT '',
			version TEXT NOT NULL DEFAULT '1.0.0',
			time_created INTEGER NOT NULL,
			time_updated INTEGER NOT NULL,
			time_archived INTEGER
		)`,
		`CREATE TABLE message (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			time_created INTEGER NOT NULL,
			time_updated INTEGER NOT NULL,
			data TEXT NOT NULL
		)`,
		`CREATE TABLE part (
			id TEXT PRIMARY KEY,
			message_id TEXT NOT NULL,
			session_id TEXT NOT NULL,
			time_created INTEGER NOT NULL,
			time_updated INTEGER NOT NULL,
			data TEXT NOT NULL
		)`,
	} {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatal(err)
		}
	}

	now := time.Now().UnixMilli()
	older := now - 3600000 // 1 hour ago

	// Insert sessions.
	db.Exec(`INSERT INTO session (id, title, slug, directory, time_created, time_updated) VALUES (?, ?, ?, ?, ?, ?)`,
		"ses_newer", "Fix auth middleware", "eager-cactus", "/home/user/project", now-1000, now)
	db.Exec(`INSERT INTO session (id, title, slug, directory, time_created, time_updated) VALUES (?, ?, ?, ?, ?, ?)`,
		"ses_older", "Refactor tests", "bold-tiger", "/home/user/tests", older-1000, older)

	// Insert a subagent session (has parent_id set).
	db.Exec(`INSERT INTO session (id, parent_id, title, slug, directory, time_created, time_updated) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"ses_subagent", "ses_newer", "Explore codebase (@explore subagent)", "silent-rocket", "/home/user/project", now-800, now-700)

	// Insert a user message + text part for ses_newer.
	msgData, _ := json.Marshal(map[string]string{"role": "user"})
	db.Exec(`INSERT INTO message (id, session_id, time_created, time_updated, data) VALUES (?, ?, ?, ?, ?)`,
		"msg_1", "ses_newer", now-500, now-500, string(msgData))

	partData, _ := json.Marshal(map[string]string{"type": "text", "text": "Help me fix the auth middleware"})
	db.Exec(`INSERT INTO part (id, message_id, session_id, time_created, time_updated, data) VALUES (?, ?, ?, ?, ?, ?)`,
		"prt_1", "msg_1", "ses_newer", now-500, now-500, string(partData))

	// Insert an assistant message + text part for ses_newer.
	assistMsgData, _ := json.Marshal(map[string]string{"role": "assistant"})
	db.Exec(`INSERT INTO message (id, session_id, time_created, time_updated, data) VALUES (?, ?, ?, ?, ?)`,
		"msg_2", "ses_newer", now-400, now-400, string(assistMsgData))

	assistPartData, _ := json.Marshal(map[string]string{"type": "text", "text": "I'll take a look at the auth middleware code."})
	db.Exec(`INSERT INTO part (id, message_id, session_id, time_created, time_updated, data) VALUES (?, ?, ?, ?, ?, ?)`,
		"prt_2", "msg_2", "ses_newer", now-400, now-400, string(assistPartData))

	return dbPath
}

// --- Claude tests ---

func TestClaudeListSessions(t *testing.T) {
	baseDir := createTestClaudeData(t)
	c := &Claude{baseDir: baseDir}

	sessions, err := c.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}

	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}

	// Should be sorted by last used DESC.
	if sessions[0].ID != "sess-2" {
		t.Errorf("first session should be sess-2 (newest), got %s", sessions[0].ID)
	}

	s := sessions[0]
	if s.Agent != "claude-code" {
		t.Errorf("agent = %q, want claude-code", s.Agent)
	}
	if s.Directory != "/home/user/project-b" {
		t.Errorf("directory = %q, want %q", s.Directory, "/home/user/project-b")
	}
}

func TestClaudeMissingHistoryFile(t *testing.T) {
	c := &Claude{baseDir: "/nonexistent/path"}
	sessions, err := c.ListSessions(context.Background())
	if err != nil {
		t.Errorf("expected nil error for missing dir, got %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(sessions))
	}
}

func TestClaudeSessionText(t *testing.T) {
	baseDir := createTestClaudeData(t)
	c := &Claude{baseDir: baseDir}

	text := c.SessionText(context.Background(), "sess-1")
	if text == "" {
		t.Error("expected non-empty session text")
	}
	want := "User: Help me with auth\n\nAssistant: Sure"
	if text != want {
		t.Errorf("got %q, want %q", text, want)
	}
}

func TestClaudeSlugExtraction(t *testing.T) {
	baseDir := createTestClaudeData(t)
	c := &Claude{baseDir: baseDir}

	sessions, _ := c.ListSessions(context.Background())
	// Find sess-1, which has a slug in its transcript.
	for _, s := range sessions {
		if s.ID == "sess-1" {
			if s.Slug != "hazy-moon" {
				t.Errorf("slug = %q, want %q", s.Slug, "hazy-moon")
			}
			return
		}
	}
	t.Error("sess-1 not found")
}

func TestClaudeSkipsCommandOnlySessions(t *testing.T) {
	baseDir := createTestClaudeData(t)

	// Add a session whose only history entries are slash/shell commands and
	// that has no transcript — the /login-style junk session.
	now := time.Now().UnixMilli()
	junk := fmt.Sprintf(`{"display":"/login","timestamp":%d,"project":"/home/user/project-a","sessionId":"sess-junk"}
{"display":"/model","timestamp":%d,"project":"/home/user/project-a","sessionId":"sess-junk"}
{"display":"!ls","timestamp":%d,"project":"/home/user/project-a","sessionId":"sess-junk"}
`, now, now+1000, now+2000)
	appendFile(t, filepath.Join(baseDir, "history.jsonl"), junk)

	c := &Claude{baseDir: baseDir}
	sessions, err := c.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	for _, s := range sessions {
		if s.ID == "sess-junk" {
			t.Error("command-only session with no transcript should be excluded")
		}
	}
	if len(sessions) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(sessions))
	}
}

func TestClaudeCommandFirstSessionTitledByRealPrompt(t *testing.T) {
	baseDir := createTestClaudeData(t)

	// A session that starts with /model but has a real prompt after it should
	// be kept, titled by the real prompt rather than the command.
	now := time.Now().UnixMilli()
	mixed := fmt.Sprintf(`{"display":"/model","timestamp":%d,"project":"/home/user/project-c","sessionId":"sess-mixed"}
{"display":"Explain the cache layer","timestamp":%d,"project":"/home/user/project-c","sessionId":"sess-mixed"}
`, now, now+1000)
	appendFile(t, filepath.Join(baseDir, "history.jsonl"), mixed)

	c := &Claude{baseDir: baseDir}
	sessions, err := c.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	for _, s := range sessions {
		if s.ID == "sess-mixed" {
			if s.Title != "Explain the cache layer" {
				t.Errorf("title = %q, want the first real prompt, not the leading command", s.Title)
			}
			return
		}
	}
	t.Error("sess-mixed not found — a session with real prompts must not be dropped")
}

func TestClaudeCommandOnlyHistoryRescuedByTranscript(t *testing.T) {
	baseDir := createTestClaudeData(t)

	// A session started as `claude "initial prompt"` has only commands in
	// history (the argument prompt is never logged there), but its transcript
	// holds the real conversation. It must be kept and titled from the
	// transcript's first real user message.
	now := time.Now().UnixMilli()
	entry := fmt.Sprintf(`{"display":"/model","timestamp":%d,"project":"/home/user/project-a","sessionId":"sess-arg"}
`, now)
	appendFile(t, filepath.Join(baseDir, "history.jsonl"), entry)

	transcript := `{"type":"user","isMeta":true,"message":{"role":"user","content":"<local-command-caveat>Caveat: local commands</local-command-caveat>"},"uuid":"m1"}
{"type":"user","message":{"role":"user","content":"<command-name>/model</command-name>"},"uuid":"c1"}
{"type":"user","message":{"role":"user","content":"<local-command-stdout>Set model</local-command-stdout>"},"uuid":"c2"}
{"type":"user","message":{"role":"user","content":"Ship the release notes for v3"},"uuid":"u1"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"On it."}]},"uuid":"a1"}
`
	if err := os.WriteFile(filepath.Join(baseDir, "projects", "-home-user-project-a", "sess-arg.jsonl"), []byte(transcript), 0644); err != nil {
		t.Fatal(err)
	}

	c := &Claude{baseDir: baseDir}
	sessions, err := c.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	for _, s := range sessions {
		if s.ID == "sess-arg" {
			if s.Title != "Ship the release notes for v3" {
				t.Errorf("title = %q, want the transcript's first real prompt", s.Title)
			}
			return
		}
	}
	t.Error("sess-arg not found — a command-only history session with a real transcript must be kept")
}

func TestExtractConversationTextSkipsCommandRecords(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.jsonl")
	transcript := `{"type":"user","message":{"role":"user","content":"<command-name>/model</command-name>"},"uuid":"c1"}
{"type":"user","message":{"role":"user","content":"real question"},"uuid":"u1"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"real answer"}]},"uuid":"a1"}
`
	if err := os.WriteFile(path, []byte(transcript), 0644); err != nil {
		t.Fatal(err)
	}
	text := extractConversationText(path)
	if strings.Contains(text, "<command-name>") {
		t.Errorf("session text should not contain command records: %q", text)
	}
	if !strings.Contains(text, "real question") || !strings.Contains(text, "real answer") {
		t.Errorf("session text missing real conversation: %q", text)
	}
}

// appendFile appends content to an existing file.
func appendFile(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
}

func TestClaudeResumeCommand(t *testing.T) {
	c := &Claude{}
	s := Session{ID: "abc-123", Directory: "/home/user/project"}
	got := c.ResumeCommand(s)
	want := CdAndRun("/home/user/project", "claude --resume abc-123")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// createTestClaudeData creates a minimal Claude Code data directory with
// history.jsonl and a project transcript file.
func createTestClaudeData(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	now := time.Now().UnixMilli()
	older := now - 3600000

	// history.jsonl — two sessions with multiple entries each.
	historyLines := []string{
		fmt.Sprintf(`{"display":"Help me with auth","timestamp":%d,"project":"/home/user/project-a","sessionId":"sess-1"}`, older),
		fmt.Sprintf(`{"display":"Now fix the tests","timestamp":%d,"project":"/home/user/project-a","sessionId":"sess-1"}`, older+60000),
		fmt.Sprintf(`{"display":"Refactor the API","timestamp":%d,"project":"/home/user/project-b","sessionId":"sess-2"}`, now-60000),
		fmt.Sprintf(`{"display":"Add error handling","timestamp":%d,"project":"/home/user/project-b","sessionId":"sess-2"}`, now),
	}
	historyContent := ""
	for _, line := range historyLines {
		historyContent += line + "\n"
	}
	os.WriteFile(filepath.Join(dir, "history.jsonl"), []byte(historyContent), 0644)

	// Project transcript for sess-1 (with slug).
	projectDir := filepath.Join(dir, "projects", "-home-user-project-a")
	os.MkdirAll(projectDir, 0755)

	transcript := fmt.Sprintf(`{"type":"user","message":{"role":"user","content":"Help me with auth"},"uuid":"u1","timestamp":"%s"}
{"type":"assistant","slug":"hazy-moon","message":{"role":"assistant","content":[{"type":"text","text":"Sure"}]},"uuid":"a1","timestamp":"%s"}
`, time.UnixMilli(older).Format(time.RFC3339), time.UnixMilli(older+1000).Format(time.RFC3339))
	os.WriteFile(filepath.Join(projectDir, "sess-1.jsonl"), []byte(transcript), 0644)

	return dir
}

// --- Claude Desktop (desktop app Claude Code) tests ---

func TestClaudeDesktopListSessions(t *testing.T) {
	baseDir, claudeDir, cliSessionID := createTestClaudeDesktopData(t)
	c := &ClaudeDesktop{baseDir: baseDir, claudeDir: claudeDir}

	sessions, err := c.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}

	s := sessions[0]
	if s.Agent != "claude-code-desktop" {
		t.Errorf("agent = %q, want claude-code-desktop", s.Agent)
	}
	if s.ID != cliSessionID {
		t.Errorf("id = %q, want the cliSessionId %q (the resumable Claude Code UUID)", s.ID, cliSessionID)
	}
	if s.Title != "Fix login redirect loop" {
		t.Errorf("title = %q, want %q", s.Title, "Fix login redirect loop")
	}
	if !s.CuratedTitle {
		t.Error("expected CuratedTitle=true for an app-generated title")
	}
	if s.Directory != "/home/user/webapp" {
		t.Errorf("directory = %q, want %q", s.Directory, "/home/user/webapp")
	}
	if s.Created.IsZero() {
		t.Error("expected non-zero Created time")
	}
	if s.LastUsed.IsZero() {
		t.Error("expected non-zero LastUsed time")
	}
	if !strings.Contains(s.SearchText, s.Title) {
		t.Errorf("SearchText = %q, want it to contain the title %q", s.SearchText, s.Title)
	}
	if !strings.Contains(s.SearchText, s.Directory) {
		t.Errorf("SearchText = %q, want it to contain the directory %q", s.SearchText, s.Directory)
	}
}

func TestClaudeDesktopIDFallsBackToSessionID(t *testing.T) {
	dir := t.TempDir()
	sessionsRoot := filepath.Join(dir, "claude-code-sessions", "uuid-a", "uuid-b")
	if err := os.MkdirAll(sessionsRoot, 0755); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	metadata := map[string]any{
		"sessionId":      "local_no-cli-id",
		"createdAt":      now,
		"lastActivityAt": now,
	}
	metaBytes, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionsRoot, "local_no-cli-id.json"), metaBytes, 0644); err != nil {
		t.Fatal(err)
	}

	sessions, err := (&ClaudeDesktop{baseDir: dir}).ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].ID != "local_no-cli-id" {
		t.Errorf("id = %q, want fallback to sessionId %q", sessions[0].ID, "local_no-cli-id")
	}
	if sessions[0].CuratedTitle {
		t.Error("expected CuratedTitle=false when the metadata has no title")
	}
}

func TestClaudeDesktopExcludesArchived(t *testing.T) {
	dir := t.TempDir()
	sessionsRoot := filepath.Join(dir, "claude-code-sessions", "uuid-a", "uuid-b")
	if err := os.MkdirAll(sessionsRoot, 0755); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	metadata := map[string]any{
		"sessionId":      "local_archived",
		"cliSessionId":   "11111111-2222-3333-4444-555555555555",
		"title":          "Archived session",
		"createdAt":      now,
		"lastActivityAt": now,
		"isArchived":     true,
	}
	metaBytes, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionsRoot, "local_archived.json"), metaBytes, 0644); err != nil {
		t.Fatal(err)
	}

	sessions, err := (&ClaudeDesktop{baseDir: dir}).ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected archived session excluded, got %d", len(sessions))
	}
}

func TestClaudeDesktopSkipsMalformedMetadata(t *testing.T) {
	baseDir, claudeDir, _ := createTestClaudeDesktopData(t)

	badDir := filepath.Join(baseDir, "claude-code-sessions", "uuid-a", "uuid-c")
	if err := os.MkdirAll(badDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(badDir, "local_bad-session.json"), []byte("{not valid json"), 0644); err != nil {
		t.Fatal(err)
	}

	c := &ClaudeDesktop{baseDir: baseDir, claudeDir: claudeDir}
	sessions, err := c.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session (malformed one skipped), got %d", len(sessions))
	}
}

func TestClaudeDesktopMissingSessionsDir(t *testing.T) {
	c := &ClaudeDesktop{baseDir: "/nonexistent/path"}
	sessions, err := c.ListSessions(context.Background())
	if err != nil {
		t.Errorf("expected nil error for missing dir, got %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(sessions))
	}
}

func TestClaudeDesktopSessionText(t *testing.T) {
	baseDir, claudeDir, cliSessionID := createTestClaudeDesktopData(t)
	c := &ClaudeDesktop{baseDir: baseDir, claudeDir: claudeDir}

	text := c.SessionText(context.Background(), cliSessionID)
	if text == "" {
		t.Fatal("expected non-empty session text")
	}
	want := "User: The login page loops back to itself\n\nAssistant: Green Anvil Desktop Test"
	if text != want {
		t.Errorf("got %q, want %q", text, want)
	}
}

func TestClaudeDesktopResumeCommand(t *testing.T) {
	c := &ClaudeDesktop{}
	s := Session{ID: "abc-123", Directory: "/home/user/webapp"}
	got := c.ResumeCommand(s)
	want := CdAndRun("/home/user/webapp", "claude --resume abc-123")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestClaudeDesktopResumeCommandOverride(t *testing.T) {
	c := &ClaudeDesktop{resumeCommand: "ca -r {{ID}}"}
	s := Session{ID: "abc-123", Directory: "/home/user/webapp"}
	got := c.ResumeCommand(s)
	want := CdAndRun("/home/user/webapp", "ca -r abc-123")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// createTestClaudeDesktopData creates the two stores a desktop Claude Code
// session spans: metadata under <userData>/claude-code-sessions/<uuid>/<uuid>/
// local_<id>.json, and the transcript in the shared <claudeDir>/projects
// store named by cliSessionId. Mirrors the real on-disk shape.
func createTestClaudeDesktopData(t *testing.T) (baseDir, claudeDir, cliSessionID string) {
	t.Helper()
	baseDir = t.TempDir()
	claudeDir = t.TempDir()

	sessionID := "local_b7e2f114-88a1-4c26-9b3d-5f0e2a9c7d61"
	cliSessionID = "4d2f8c3a-1b5e-4f7a-9c8d-2e6b0a4f9d13"

	now := time.Now().UnixMilli()
	created := now - 60000

	sessionsRoot := filepath.Join(baseDir, "claude-code-sessions", "uuid-a", "uuid-b")
	if err := os.MkdirAll(sessionsRoot, 0755); err != nil {
		t.Fatal(err)
	}

	metadata := map[string]any{
		"sessionId":      sessionID,
		"cliSessionId":   cliSessionID,
		"title":          "Fix login redirect loop",
		"titleSource":    "auto",
		"cwd":            "/home/user/webapp",
		"originCwd":      "/home/user/webapp",
		"createdAt":      created,
		"lastActivityAt": now,
		"model":          "claude-opus-4-8",
		"isArchived":     false,
	}
	metaBytes, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionsRoot, sessionID+".json"), metaBytes, 0644); err != nil {
		t.Fatal(err)
	}

	// Transcript in the shared ~/.claude/projects store, named by cliSessionId.
	projectDir := filepath.Join(claudeDir, "projects", "-home-user-webapp")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatal(err)
	}
	transcript := `{"type":"user","message":{"role":"user","content":"The login page loops back to itself"},"uuid":"u1"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Green Anvil Desktop Test"}]},"uuid":"a1"}
`
	if err := os.WriteFile(filepath.Join(projectDir, cliSessionID+".jsonl"), []byte(transcript), 0644); err != nil {
		t.Fatal(err)
	}

	return baseDir, claudeDir, cliSessionID
}

// --- Claude Cowork tests ---

func TestClaudeCoworkListSessions(t *testing.T) {
	baseDir, sessionID := createTestClaudeCoworkData(t, true)
	c := &ClaudeCowork{baseDir: baseDir}

	sessions, err := c.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}

	s := sessions[0]
	if s.Agent != "claude-cowork" {
		t.Errorf("agent = %q, want claude-cowork", s.Agent)
	}
	if s.ID != sessionID {
		t.Errorf("id = %q, want %q", s.ID, sessionID)
	}
	if s.Title != "Purple elephant planning session" {
		t.Errorf("title = %q, want %q", s.Title, "Purple elephant planning session")
	}
	if s.Directory != "/home/user/project" {
		t.Errorf("directory = %q, want %q", s.Directory, "/home/user/project")
	}
	if s.Created.IsZero() {
		t.Error("expected non-zero Created time")
	}
	if s.LastUsed.IsZero() {
		t.Error("expected non-zero LastUsed time")
	}
	if !strings.Contains(s.SearchText, "scheduled") {
		t.Errorf("SearchText = %q, want it to contain sessionType %q", s.SearchText, "scheduled")
	}
	if !strings.Contains(s.SearchText, s.Title) {
		t.Errorf("SearchText = %q, want it to contain the title %q so the picker can match it", s.SearchText, s.Title)
	}
	if !strings.Contains(s.SearchText, s.Directory) {
		t.Errorf("SearchText = %q, want it to contain the directory %q", s.SearchText, s.Directory)
	}
	if !s.CuratedTitle {
		t.Error("expected CuratedTitle=true for an app-generated title")
	}
}

func TestClaudeCoworkInitialMessageTitleIsNotCurated(t *testing.T) {
	dir := t.TempDir()
	sessionsRoot := filepath.Join(dir, "local-agent-mode-sessions", "uuid-a", "uuid-b")
	if err := os.MkdirAll(sessionsRoot, 0755); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	metadata := map[string]any{
		"sessionId":      "local_untitled",
		"initialMessage": "please look into the flaky tests",
		"createdAt":      now,
		"lastActivityAt": now,
	}
	metaBytes, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionsRoot, "local_untitled.json"), metaBytes, 0644); err != nil {
		t.Fatal(err)
	}

	sessions, err := (&ClaudeCowork{baseDir: dir}).ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].CuratedTitle {
		t.Error("a title derived from the initial message should not be curated — it still benefits from summarization")
	}
}

func TestClaudeCoworkFolderlessSessionHasNoDirectory(t *testing.T) {
	dir := t.TempDir()
	sessionsRoot := filepath.Join(dir, "local-agent-mode-sessions", "uuid-a", "uuid-b")
	if err := os.MkdirAll(sessionsRoot, 0755); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	metadata := map[string]any{
		"sessionId":           "local_folderless",
		"title":               "Folderless task",
		"userSelectedFolders": []string{},
		"cwd":                 "/some/internal/sandbox/path/outputs",
		"createdAt":           now,
		"lastActivityAt":      now,
	}
	metaBytes, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionsRoot, "local_folderless.json"), metaBytes, 0644); err != nil {
		t.Fatal(err)
	}

	c := &ClaudeCowork{baseDir: dir}
	sessions, err := c.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].Directory != "" {
		t.Errorf("directory = %q, want empty (cwd sandbox path must not be used)", sessions[0].Directory)
	}
}

func TestClaudeCoworkSessionText(t *testing.T) {
	baseDir, sessionID := createTestClaudeCoworkData(t, true)
	c := &ClaudeCowork{baseDir: baseDir}

	text := c.SessionText(context.Background(), sessionID)
	if text == "" {
		t.Fatal("expected non-empty session text")
	}
	if !strings.Contains(text, "Purple Elephant Sesh Test") {
		t.Errorf("session text = %q, want it to contain the transcript marker %q", text, "Purple Elephant Sesh Test")
	}
	if strings.Contains(text, "Audit Fallback Marker") {
		t.Errorf("session text should come from the transcript, not audit.jsonl: %q", text)
	}
}

func TestClaudeCoworkSessionTextFallsBackToAudit(t *testing.T) {
	// No nested .claude/projects transcript this time — only audit.jsonl.
	baseDir, sessionID := createTestClaudeCoworkData(t, false)
	c := &ClaudeCowork{baseDir: baseDir}

	text := c.SessionText(context.Background(), sessionID)
	if text == "" {
		t.Fatal("expected non-empty session text from audit.jsonl fallback")
	}
	if !strings.Contains(text, "Audit Fallback Marker") {
		t.Errorf("session text = %q, want it to contain the audit marker %q", text, "Audit Fallback Marker")
	}
}

func TestClaudeCoworkSessionTextPrefersCliSessionIdTranscript(t *testing.T) {
	baseDir, sessionID := createTestClaudeCoworkData(t, true)
	// Add a decoy transcript whose name sorts before the cliSessionId file,
	// so a glob-first implementation would return it instead.
	projectDir := filepath.Join(baseDir, "local-agent-mode-sessions", "uuid-a", "uuid-b", sessionID, ".claude", "projects", "-escaped-path")
	decoy := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"WRONG DECOY TRANSCRIPT"}]},"uuid":"d1"}` + "\n"
	if err := os.WriteFile(filepath.Join(projectDir, "00000000-decoy.jsonl"), []byte(decoy), 0644); err != nil {
		t.Fatal(err)
	}

	text := (&ClaudeCowork{baseDir: baseDir}).SessionText(context.Background(), sessionID)
	if strings.Contains(text, "WRONG DECOY TRANSCRIPT") {
		t.Errorf("SessionText returned the decoy transcript, not the cliSessionId one: %q", text)
	}
	if !strings.Contains(text, "Purple Elephant Sesh Test") {
		t.Errorf("SessionText should return the cliSessionId transcript, got %q", text)
	}
}

func TestClaudeCoworkExcludesArchived(t *testing.T) {
	dir := t.TempDir()
	sessionsRoot := filepath.Join(dir, "local-agent-mode-sessions", "uuid-a", "uuid-b")
	if err := os.MkdirAll(sessionsRoot, 0755); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	metadata := map[string]any{
		"sessionId":      "local_archived",
		"title":          "Archived session",
		"createdAt":      now,
		"lastActivityAt": now,
		"isArchived":     true,
	}
	metaBytes, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionsRoot, "local_archived.json"), metaBytes, 0644); err != nil {
		t.Fatal(err)
	}

	sessions, err := (&ClaudeCowork{baseDir: dir}).ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected archived session excluded, got %d", len(sessions))
	}
}

func TestClaudeCoworkSkipsMalformedMetadata(t *testing.T) {
	baseDir, _ := createTestClaudeCoworkData(t, true)

	// Drop a malformed metadata file alongside the good one.
	badDir := filepath.Join(baseDir, "local-agent-mode-sessions", "uuid-a", "uuid-c")
	if err := os.MkdirAll(badDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(badDir, "local_bad-session.json"), []byte("{not valid json"), 0644); err != nil {
		t.Fatal(err)
	}

	c := &ClaudeCowork{baseDir: baseDir}
	sessions, err := c.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session (malformed one skipped), got %d", len(sessions))
	}
}

func TestClaudeCoworkMissingSessionsDir(t *testing.T) {
	c := &ClaudeCowork{baseDir: "/nonexistent/path"}
	sessions, err := c.ListSessions(context.Background())
	if err != nil {
		t.Errorf("expected nil error for missing dir, got %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(sessions))
	}
}

func TestClaudeCoworkResumeCommand(t *testing.T) {
	c := &ClaudeCowork{}
	s := Session{ID: "local_abc-123", Directory: "/home/user/project"}
	got := c.ResumeCommand(s)
	if got == "" {
		t.Error("expected a non-empty best-effort resume command")
	}
}

func TestClaudeCoworkResumeCommandOverride(t *testing.T) {
	c := &ClaudeCowork{resumeCommand: "open-claude-cowork --session {{ID}}"}
	s := Session{ID: "local_abc-123", Directory: "/home/user/project"}
	got := c.ResumeCommand(s)
	want := "open-claude-cowork --session local_abc-123"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// writeCoworkScheduledMeta drops a minimal metadata file for one scheduled run
// into an existing two-level session dir. schedID may be empty for a one-off.
func writeCoworkScheduledMeta(t *testing.T, sessionsRoot, sessionID, schedID string, ms int64) {
	t.Helper()
	metadata := map[string]any{
		"sessionId":       sessionID,
		"title":           "Nightly triage",
		"createdAt":       ms,
		"lastActivityAt":  ms,
		"scheduledTaskId": schedID,
		"isArchived":      false,
	}
	metaBytes, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionsRoot, sessionID+".json"), metaBytes, 0644); err != nil {
		t.Fatal(err)
	}
}

// setupCoworkScheduledRuns writes three runs of one scheduled task plus one
// unscheduled session, and returns the base dir. run3 is the most recent.
func setupCoworkScheduledRuns(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	sessionsRoot := filepath.Join(dir, "local-agent-mode-sessions", "uuid-a", "uuid-b")
	if err := os.MkdirAll(sessionsRoot, 0755); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	writeCoworkScheduledMeta(t, sessionsRoot, "local_run1", "triage-task", now-3000)
	writeCoworkScheduledMeta(t, sessionsRoot, "local_run2", "triage-task", now-2000)
	writeCoworkScheduledMeta(t, sessionsRoot, "local_run3", "triage-task", now-1000)
	writeCoworkScheduledMeta(t, sessionsRoot, "local_oneoff", "", now)
	return dir
}

func TestClaudeCoworkCollapsesScheduledRuns(t *testing.T) {
	dir := setupCoworkScheduledRuns(t)

	sessions, err := (&ClaudeCowork{baseDir: dir, collapseScheduled: true}).ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	// Three runs collapse to one representative; the one-off stays separate.
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions after collapse, got %d", len(sessions))
	}

	var rep *Session
	for i := range sessions {
		if strings.Contains(sessions[i].Title, "runs") {
			rep = &sessions[i]
		}
	}
	if rep == nil {
		t.Fatalf("no collapsed representative found in %+v", sessions)
	}
	if rep.Title != "Nightly triage (3 runs)" {
		t.Errorf("representative title = %q, want %q", rep.Title, "Nightly triage (3 runs)")
	}
	if rep.ID != "local_run3" {
		t.Errorf("representative should carry the most recent run's ID, got %q", rep.ID)
	}
}

func TestClaudeCoworkCollapseDisabled(t *testing.T) {
	dir := setupCoworkScheduledRuns(t)

	sessions, err := (&ClaudeCowork{baseDir: dir, collapseScheduled: false}).ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if len(sessions) != 4 {
		t.Fatalf("expected 4 sessions with collapse disabled, got %d", len(sessions))
	}
}

// writeCoworkTranscript writes a transcript for a session's sandbox with the
// given user-message contents (one JSONL line each).
func writeCoworkTranscript(t *testing.T, sessionsRoot, sessionID string, userContents []string) {
	t.Helper()
	proj := filepath.Join(sessionsRoot, sessionID, ".claude", "projects", "proj")
	if err := os.MkdirAll(proj, 0755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, c := range userContents {
		line, err := json.Marshal(map[string]any{
			"type":    "user",
			"message": map[string]any{"content": c},
		})
		if err != nil {
			t.Fatal(err)
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(filepath.Join(proj, "transcript.jsonl"), []byte(b.String()), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestClaudeCoworkKeepsInteractiveRuns(t *testing.T) {
	dir := t.TempDir()
	sessionsRoot := filepath.Join(dir, "local-agent-mode-sessions", "uuid-a", "uuid-b")
	if err := os.MkdirAll(sessionsRoot, 0755); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	writeCoworkScheduledMeta(t, sessionsRoot, "local_r1", "t", now-3000)
	writeCoworkScheduledMeta(t, sessionsRoot, "local_r2", "t", now-2000)
	writeCoworkScheduledMeta(t, sessionsRoot, "local_r3", "t", now-1000)
	// r2 has a human follow-up beyond the trigger, so it must be kept separate.
	writeCoworkTranscript(t, sessionsRoot, "local_r2", []string{
		`<scheduled-task name="triage">go`,
		"Actually, archive the Chad message and walk me through the calendar",
	})

	sessions, err := (&ClaudeCowork{baseDir: dir, collapseScheduled: true}).ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	// r1 and r3 (pure automation) collapse to one rep; r2 (interactive) stays.
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions (rep for r1/r3 + kept r2), got %d: %+v", len(sessions), sessions)
	}
	var kept, rep *Session
	for i := range sessions {
		if sessions[i].ID == "local_r2" {
			kept = &sessions[i]
		}
		if strings.Contains(sessions[i].Title, "runs") {
			rep = &sessions[i]
		}
	}
	if kept == nil {
		t.Fatal("interactive run local_r2 was collapsed away")
	}
	if strings.Contains(kept.Title, "runs") {
		t.Errorf("kept run should not be annotated as collapsed: %q", kept.Title)
	}
	if rep == nil || rep.Title != "Nightly triage (2 runs)" {
		t.Errorf("expected representative %q, got %+v", "Nightly triage (2 runs)", rep)
	}
}

// createTestClaudeCoworkData creates a minimal Claude desktop app session
// store: local-agent-mode-sessions/<uuidA>/<uuidB>/local_<id>.json plus a
// sibling sandbox dir local_<id>/ containing audit.jsonl and, when
// withTranscript is true, a nested .claude/projects/<escaped>/<cli>.jsonl
// transcript. Mirrors the real on-disk shape used by the desktop app.
func createTestClaudeCoworkData(t *testing.T, withTranscript bool) (baseDir, sessionID string) {
	t.Helper()
	dir := t.TempDir()

	sessionID = "local_a9f7b603-20ee-44a9-9042-f8cf7afd9195"
	cliSessionID := "93a9c12b-2f64-436a-b592-f6dea7ae66c2"

	now := time.Now().UnixMilli()
	created := now - 60000

	sessionsRoot := filepath.Join(dir, "local-agent-mode-sessions", "uuid-a", "uuid-b")
	if err := os.MkdirAll(sessionsRoot, 0755); err != nil {
		t.Fatal(err)
	}

	metadata := map[string]any{
		"sessionId":           sessionID,
		"cliSessionId":        cliSessionID,
		"title":               "Purple elephant planning session",
		"userSelectedFolders": []string{"/home/user/project"},
		"cwd":                 "/some/internal/sandbox/path",
		"createdAt":           created,
		"lastActivityAt":      now,
		"initialMessage":      "This is a test for Sesh.",
		"scheduledTaskId":     "snippets-bot",
		"sessionType":         "scheduled",
		"model":               "claude-sonnet-5",
		"isArchived":          false,
	}
	metaBytes, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionsRoot, sessionID+".json"), metaBytes, 0644); err != nil {
		t.Fatal(err)
	}

	sandboxDir := filepath.Join(sessionsRoot, sessionID)

	if withTranscript {
		projectDir := filepath.Join(sandboxDir, ".claude", "projects", "-escaped-path")
		if err := os.MkdirAll(projectDir, 0755); err != nil {
			t.Fatal(err)
		}
		transcript := `{"type":"user","message":{"role":"user","content":"This is a test for Sesh."},"uuid":"u1"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Purple Elephant Sesh Test"}]},"uuid":"a1"}
`
		if err := os.WriteFile(filepath.Join(projectDir, cliSessionID+".jsonl"), []byte(transcript), 0644); err != nil {
			t.Fatal(err)
		}
	} else {
		if err := os.MkdirAll(sandboxDir, 0755); err != nil {
			t.Fatal(err)
		}
	}

	// audit.jsonl always present, used as the fallback source.
	audit := `{"type":"user","session_id":"abc","message":{"role":"user","content":"This is a test for Sesh."},"uuid":"u1"}
{"type":"assistant","session_id":"abc","message":{"role":"assistant","content":[{"type":"text","text":"Audit Fallback Marker"}]},"uuid":"a1"}
`
	if err := os.WriteFile(filepath.Join(sandboxDir, "audit.jsonl"), []byte(audit), 0644); err != nil {
		t.Fatal(err)
	}

	return dir, sessionID
}

// --- External provider tests ---

func TestExternalResumeCommand(t *testing.T) {
	e := &External{
		config:    ExternalConfig{ResumeCommand: "myagent --resume {{ID}}"},
		textCache: make(map[string]string),
	}
	s := Session{ID: "abc", Directory: "/home/user/proj"}
	got := e.ResumeCommand(s)
	want := CdAndRun("/home/user/proj", "myagent --resume abc")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestExternalResumeCommandWithDirTemplate(t *testing.T) {
	e := &External{
		config:    ExternalConfig{ResumeCommand: "myagent --dir={{DIR}} --resume {{ID}}"},
		textCache: make(map[string]string),
	}
	s := Session{ID: "abc", Directory: "/home/user/proj"}
	got := e.ResumeCommand(s)
	// Should NOT add cd prefix since template contains {{DIR}}.
	want := "myagent --dir=/home/user/proj --resume abc"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestExternalSessionText(t *testing.T) {
	e := &External{
		config:    ExternalConfig{},
		textCache: map[string]string{"ses_1": "cached text"},
	}
	got := e.SessionText(context.Background(), "ses_1")
	if got != "cached text" {
		t.Errorf("got %q, want %q", got, "cached text")
	}

	got = e.SessionText(context.Background(), "nonexistent")
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}
