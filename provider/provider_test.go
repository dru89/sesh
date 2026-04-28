package provider

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestShellQuote(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", "''"},
		{"simple", "hello", "hello"},
		{"path", "/usr/local/bin/sesh", "/usr/local/bin/sesh"},
		{"session id", "ses_abc123", "ses_abc123"},
		{"uuid", "21ed6e1a-9ebd-4418-8111-f64cdcc6cedc", "21ed6e1a-9ebd-4418-8111-f64cdcc6cedc"},
		{"tilde path", "~/Developer/project", "~/Developer/project"},
		{"space", "my project", "'my project'"},
		{"single quote", "it's", "'it'\\''s'"},
		{"ampersand", "foo&bar", "'foo&bar'"},
		{"parens", "foo(bar)", "'foo(bar)'"},
		{"semicolon", "a;b", "'a;b'"},
		{"dollar", "$HOME", "'$HOME'"},
		{"backslash path", "C:\\Users\\drew", "C:\\Users\\drew"},
		{"mixed special", "hello world's", "'hello world'\\''s'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShellQuote(tt.input)
			if got != tt.want {
				t.Errorf("ShellQuote(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestShellQuotePowerShell(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", "''"},
		{"simple", "hello", "hello"},
		{"session id", "ses_abc123", "ses_abc123"},
		{"backslash path", "C:\\Users\\drew", "C:\\Users\\drew"},
		{"space", "my project", "'my project'"},
		{"single quote", "it's", "'it''s'"},
		{"forward slash path", "/usr/local/bin", "'/usr/local/bin'"},
		{"ampersand", "foo&bar", "'foo&bar'"},
		{"dollar", "$HOME", "'$HOME'"},
		{"mixed", "hello world's", "'hello world''s'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShellQuotePowerShell(tt.input)
			if got != tt.want {
				t.Errorf("ShellQuotePowerShell(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestCdAndRun(t *testing.T) {
	tests := []struct {
		name string
		dir  string
		cmd  string
		unix string
		win  string
	}{
		{
			"no dir", "", "opencode --session abc",
			"opencode --session abc",
			"opencode --session abc",
		},
		{
			"simple dir", "/home/user/project", "opencode --session abc",
			"cd /home/user/project && opencode --session abc",
			fmt.Sprintf("Set-Location %s; opencode --session abc", ShellQuotePowerShell("/home/user/project")),
		},
		{
			"dir with spaces", "/home/user/my project", "opencode --session abc",
			"cd '/home/user/my project' && opencode --session abc",
			fmt.Sprintf("Set-Location %s; opencode --session abc", ShellQuotePowerShell("/home/user/my project")),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CdAndRun(tt.dir, tt.cmd)
			want := tt.unix
			if runtime.GOOS == "windows" {
				want = tt.win
			}
			if got != want {
				t.Errorf("CdAndRun(%q, %q) = %q, want %q", tt.dir, tt.cmd, got, want)
			}
		})
	}
}

func TestRelativeTime(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name string
		t    time.Time
		want string
	}{
		{"zero", time.Time{}, ""},
		{"just now", now.Add(-10 * time.Second), "just now"},
		{"1 minute", now.Add(-1 * time.Minute), "1m ago"},
		{"30 minutes", now.Add(-30 * time.Minute), "30m ago"},
		{"1 hour", now.Add(-1 * time.Hour), "1h ago"},
		{"5 hours", now.Add(-5 * time.Hour), "5h ago"},
		{"1 day", now.Add(-24 * time.Hour), "1d ago"},
		{"7 days", now.Add(-7 * 24 * time.Hour), "7d ago"},
		{"29 days", now.Add(-29 * 24 * time.Hour), "29d ago"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RelativeTime(tt.t)
			if got != tt.want {
				t.Errorf("RelativeTime() = %q, want %q", got, tt.want)
			}
		})
	}

	// Dates > 30 days use "Jan 2" format — just check it doesn't crash.
	old := now.Add(-60 * 24 * time.Hour)
	got := RelativeTime(old)
	if got == "" {
		t.Error("RelativeTime for 60 days ago returned empty string")
	}
}

func TestDisplayTitle(t *testing.T) {
	tests := []struct {
		name    string
		session Session
		want    string
	}{
		{
			"summary preferred",
			Session{Summary: "Built auth middleware", Title: "raw title", Slug: "eager-moon", ID: "ses_123"},
			"Built auth middleware",
		},
		{
			"title fallback",
			Session{Title: "raw title", Slug: "eager-moon", ID: "ses_123"},
			"raw title",
		},
		{
			"slug fallback",
			Session{Slug: "eager-moon", ID: "ses_123"},
			"eager-moon",
		},
		{
			"id fallback",
			Session{ID: "ses_123"},
			"ses_123",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.session.DisplayTitle()
			if got != tt.want {
				t.Errorf("DisplayTitle() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsShellSafe(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"hello", true},
		{"ses_abc123", true},
		{"/usr/local/bin", true},
		{"C:\\Users\\drew", true},
		{"hello world", false},
		{"it's", false},
		{"$HOME", false},
		{"foo;bar", false},
		{"foo&bar", false},
		{"", true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isShellSafe(tt.input)
			if got != tt.want {
				t.Errorf("isShellSafe(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestExcerptBookendsShortText(t *testing.T) {
	text := "User: hello\n\nAssistant: hi there"
	got := ExcerptBookends(text, 5000)
	if got != text {
		t.Errorf("expected full text returned, got %q", got)
	}
}

func TestExcerptBookendsExactFit(t *testing.T) {
	text := "User: a\n\nAssistant: b\n\nUser: c"
	got := ExcerptBookends(text, 100)
	if got != text {
		t.Errorf("expected full text returned, got %q", got)
	}
}

func TestExcerptBookendsSplitsAtMessageBoundary(t *testing.T) {
	messages := []string{
		"User: First message about authentication",
		"Assistant: I'll help with auth",
		"User: Second message about testing",
		"Assistant: Here are some tests",
		"User: Third message about deployment",
		"Assistant: Deploy instructions follow",
		"User: Fourth message about monitoring",
		"Assistant: Set up monitoring like this",
		"User: Fifth message about cleanup",
		"Assistant: Final cleanup steps",
	}
	text := strings.Join(messages, "\n\n")

	got := ExcerptBookends(text, 80)

	if !strings.Contains(got, "[...]") {
		t.Errorf("expected [...] separator in bookended text, got:\n%s", got)
	}

	if !strings.HasPrefix(got, "User: First message") {
		t.Errorf("expected text to start with first message, got:\n%s", got)
	}

	if !strings.HasSuffix(got, "Final cleanup steps") {
		t.Errorf("expected text to end with last message, got:\n%s", got)
	}

	parts := strings.Split(got, "\n\n[...]\n\n")
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts around [...], got %d", len(parts))
	}

	// The head should contain complete messages from the start. The last
	// chunk may be a truncated fragment from an oversized message — that's
	// expected when a message exceeds the remaining budget.
	if !strings.HasPrefix(parts[0], "User: First message") {
		t.Errorf("head should start with the first message, got:\n%s", parts[0])
	}

	// The tail should end with the last message.
	if !strings.HasSuffix(parts[1], "Final cleanup steps") {
		t.Errorf("tail should end with the last message, got:\n%s", parts[1])
	}
}

func TestExcerptBookendsNoOverlap(t *testing.T) {
	messages := []string{
		"User: Message one",
		"Assistant: Reply one",
		"User: Message two",
		"Assistant: Reply two",
		"User: Message three",
		"Assistant: Reply three",
	}
	text := strings.Join(messages, "\n\n")

	got := ExcerptBookends(text, 40)

	for _, msg := range messages {
		count := strings.Count(got, msg)
		if count > 1 {
			t.Errorf("message %q appears %d times (expected at most 1)", msg, count)
		}
	}
}

func TestExcerptBookendsSingleMessage(t *testing.T) {
	text := "User: " + strings.Repeat("a", 20000)
	got := ExcerptBookends(text, 5000)
	if len(got) > 10003 {
		t.Errorf("expected truncation, got length %d", len(got))
	}
}

func TestExcerptBookendsOversizedEarlyChunk(t *testing.T) {
	// Simulates the real-world case: short user message, then a massive
	// assistant response, then more normal-sized messages.
	messages := []string{
		"User: Fix the authentication bug",
		"Assistant: " + strings.Repeat("I looked at the code and found several issues. ", 200),
		"User: That worked, thanks",
		"Assistant: Great, let me know if you need anything else",
	}
	text := strings.Join(messages, "\n\n")

	got := ExcerptBookends(text, 200)

	// Head should contain the first user message AND part of the oversized
	// assistant response, not just the tiny user message alone.
	parts := strings.Split(got, "\n\n[...]\n\n")
	if len(parts) != 2 {
		t.Fatalf("expected [...] separator, got:\n%s", got)
	}

	if !strings.HasPrefix(parts[0], "User: Fix the authentication bug") {
		t.Errorf("head should start with first message, got:\n%s", parts[0])
	}

	// The head should be substantially larger than just the first message (32 chars).
	if len(parts[0]) < 100 {
		t.Errorf("head should include truncated content from the oversized chunk, got %d chars:\n%s", len(parts[0]), parts[0])
	}

	// Tail should end with the last message.
	if !strings.HasSuffix(parts[1], "let me know if you need anything else") {
		t.Errorf("tail should end with last message, got:\n%s", parts[1])
	}
}

func TestExcerptBookendsOversizedTailChunk(t *testing.T) {
	// Oversized chunk near the end.
	messages := []string{
		"User: Start of conversation",
		"Assistant: Short reply",
		"User: Another question",
		"Assistant: " + strings.Repeat("Here is a detailed explanation. ", 200),
		"User: Final short message",
	}
	text := strings.Join(messages, "\n\n")

	got := ExcerptBookends(text, 200)

	parts := strings.Split(got, "\n\n[...]\n\n")
	if len(parts) != 2 {
		t.Fatalf("expected [...] separator, got:\n%s", got)
	}

	// Tail should include truncated content from the oversized chunk,
	// not just the tiny final message.
	if len(parts[1]) < 100 {
		t.Errorf("tail should include truncated content from oversized chunk, got %d chars:\n%s", len(parts[1]), parts[1])
	}

	if !strings.HasSuffix(got, "Final short message") {
		t.Errorf("should end with final message, got:\n%s", got)
	}
}

func TestTruncateHead(t *testing.T) {
	t.Run("short text unchanged", func(t *testing.T) {
		got := truncateHead("hello world", 100)
		if got != "hello world" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("breaks at sentence", func(t *testing.T) {
		text := "First sentence. Second sentence. Third sentence is longer."
		got := truncateHead(text, 40)
		if !strings.HasSuffix(got, ".") {
			t.Errorf("expected sentence boundary, got %q", got)
		}
		if len(got) > 40 {
			t.Errorf("expected <= 40 chars, got %d", len(got))
		}
	})

	t.Run("breaks at newline", func(t *testing.T) {
		text := "first line with no periods\nsecond line\nthird line is extra"
		got := truncateHead(text, 35)
		if strings.Contains(got, "second") {
			t.Errorf("should break before second line, got %q", got)
		}
	})

	t.Run("breaks at word", func(t *testing.T) {
		text := strings.Repeat("word ", 20)
		got := truncateHead(text, 30)
		if strings.HasSuffix(got, "...") {
			t.Errorf("should break at word boundary, not hard truncate, got %q", got)
		}
	})

	t.Run("hard truncate no boundaries", func(t *testing.T) {
		text := strings.Repeat("x", 100)
		got := truncateHead(text, 50)
		if !strings.HasSuffix(got, "...") {
			t.Errorf("expected ... suffix for hard truncation, got %q", got)
		}
	})
}

func TestTruncateTail(t *testing.T) {
	t.Run("short text unchanged", func(t *testing.T) {
		got := truncateTail("hello world", 100)
		if got != "hello world" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("breaks at sentence", func(t *testing.T) {
		text := "First sentence is long. Second sentence. Third sentence."
		got := truncateTail(text, 40)
		// Should keep the end, breaking at a sentence boundary.
		if !strings.HasSuffix(got, "Third sentence.") {
			t.Errorf("expected to end with last sentence, got %q", got)
		}
		if len(got) > 40 {
			t.Errorf("expected <= 40 chars, got %d", len(got))
		}
	})

	t.Run("hard truncate no boundaries", func(t *testing.T) {
		text := strings.Repeat("x", 100)
		got := truncateTail(text, 50)
		if !strings.HasPrefix(got, "...") {
			t.Errorf("expected ... prefix for hard truncation, got %q", got)
		}
	})
}
