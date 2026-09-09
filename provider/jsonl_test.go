package provider

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// oversizedLine is longer than the 1 MB scanner cap these readers used to
// carry, so any reader that regresses to a fixed 1 MB buffer stops dead at the
// line built from it and drops everything after. Sized just past the largest
// line measured in a real ~/.claude/projects store (1484 KB).
const oversizedLine = 1536 * 1024

func hugeText() string { return strings.Repeat("x", oversizedLine) }

// A transcript whose long line sits in the middle: the assertions are about
// what comes *after* it, which is what an undersized buffer silently loses.
func writeTranscript(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExtractConversationTextReadsPastOversizedLine(t *testing.T) {
	path := writeTranscript(t,
		`{"type":"user","message":{"role":"user","content":"first prompt"},"uuid":"u1"}`,
		fmt.Sprintf(`{"type":"assistant","message":{"id":"a1","role":"assistant","content":[{"type":"text","text":%q}]}}`, hugeText()),
		`{"type":"user","message":{"role":"user","content":"last prompt"},"uuid":"u2"}`,
	)

	text := extractConversationText(path)
	if !strings.Contains(text, "first prompt") {
		t.Error("lost the message before the oversized line")
	}
	if !strings.Contains(text, "last prompt") {
		t.Error("lost the message after the oversized line — scan buffer too small")
	}
}

func TestFirstUserPromptReadsPastOversizedLine(t *testing.T) {
	path := writeTranscript(t,
		fmt.Sprintf(`{"type":"assistant","message":{"id":"a1","role":"assistant","content":[{"type":"text","text":%q}]}}`, hugeText()),
		`{"type":"user","message":{"role":"user","content":"rescue this session"},"uuid":"u1"}`,
	)

	if got := firstUserPrompt(path); got != "rescue this session" {
		t.Errorf("firstUserPrompt = %q, want %q", got, "rescue this session")
	}
}

func TestExtractSlugReadsPastOversizedLine(t *testing.T) {
	path := writeTranscript(t,
		fmt.Sprintf(`{"type":"user","message":{"role":"user","content":%q},"uuid":"u1"}`, hugeText()),
		`{"type":"assistant","slug":"hazy-moon","message":{"id":"a1","role":"assistant","content":[{"type":"text","text":"ok"}]}}`,
	)

	c := &Claude{}
	if got := c.extractSlug(path); got != "hazy-moon" {
		t.Errorf("extractSlug = %q, want %q", got, "hazy-moon")
	}
}

func TestClaudeListSessionsReadsPastOversizedHistoryLine(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UnixMilli()

	// A pasted file or a long stack trace typed at the prompt lands in
	// history.jsonl as one line, taking every session logged after it with it.
	history := strings.Join([]string{
		fmt.Sprintf(`{"display":%q,"timestamp":%d,"project":"/home/user/a","sessionId":"sess-huge"}`, hugeText(), now-60000),
		fmt.Sprintf(`{"display":"Refactor the API","timestamp":%d,"project":"/home/user/b","sessionId":"sess-after"}`, now),
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "history.jsonl"), []byte(history), 0644); err != nil {
		t.Fatal(err)
	}

	sessions, err := (&Claude{baseDir: dir}).ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d — history scan stopped at the oversized line", len(sessions))
	}
	if sessions[0].ID != "sess-after" {
		t.Errorf("newest session = %q, want sess-after", sessions[0].ID)
	}
}
