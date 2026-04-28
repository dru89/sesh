package provider

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	if s.Agent != "claude" {
		t.Errorf("agent = %q, want claude", s.Agent)
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
