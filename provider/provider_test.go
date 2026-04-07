package provider

import (
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
		want string
	}{
		{"no dir", "", "opencode --session abc", "opencode --session abc"},
		{"simple dir", "/home/user/project", "opencode --session abc", "cd /home/user/project && opencode --session abc"},
		{"dir with spaces", "/home/user/my project", "opencode --session abc", "cd '/home/user/my project' && opencode --session abc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CdAndRun(tt.dir, tt.cmd)
			if got != tt.want {
				t.Errorf("CdAndRun(%q, %q) = %q, want %q", tt.dir, tt.cmd, got, tt.want)
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
