package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dru89/sesh/agent"
	"github.com/dru89/sesh/internal/testhelper"
	"github.com/dru89/sesh/provider"
	"github.com/dru89/sesh/summary"
	"github.com/dru89/sesh/tui"
)

func TestResolveCommand(t *testing.T) {
	haiku := []string{"llm", "-m", "haiku"}
	sonnet := []string{"llm", "-m", "sonnet"}
	haikuEnv := map[string]string{"MODEL": "haiku"}
	sonnetEnv := map[string]string{"MODEL": "sonnet"}

	tests := []struct {
		name    string
		pairs   []commandWithEnv
		wantCmd []string
		wantEnv map[string]string
	}{
		{"first set", []commandWithEnv{{haiku, haikuEnv}, {sonnet, sonnetEnv}}, haiku, haikuEnv},
		{"skip empty", []commandWithEnv{{nil, nil}, {sonnet, sonnetEnv}}, sonnet, sonnetEnv},
		{"all empty", []commandWithEnv{{nil, nil}, {nil, nil}}, nil, nil},
		{"single", []commandWithEnv{{haiku, haikuEnv}}, haiku, haikuEnv},
		{"empty slice", []commandWithEnv{{[]string{}, nil}, {sonnet, sonnetEnv}}, sonnet, sonnetEnv},
		{"env follows command", []commandWithEnv{{nil, haikuEnv}, {sonnet, sonnetEnv}}, sonnet, sonnetEnv},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCmd, gotEnv := resolveCommand(tt.pairs...)
			assertCmd(t, "command", gotCmd, tt.wantCmd)
			assertEnvMap(t, "env", gotEnv, tt.wantEnv)
		})
	}
}

func TestFallbackChains(t *testing.T) {
	haiku := []string{"llm", "-m", "haiku"}
	sonnet := []string{"llm", "-m", "sonnet"}
	fast := []string{"llm", "-m", "flash"}

	t.Run("single command configures everything", func(t *testing.T) {
		cfg := config{
			Index: commandConfig{Command: haiku},
		}
		cmd, _ := cfg.indexCommand()
		assertCmd(t, "indexCommand", cmd, haiku)
		cmd, _ = cfg.askCommand()
		assertCmd(t, "askCommand", cmd, haiku)
		cmd, _ = cfg.askFilterCommand()
		assertCmd(t, "askFilterCommand", cmd, haiku)
		cmd, _ = cfg.recapCommand()
		assertCmd(t, "recapCommand", cmd, haiku)
	})

	t.Run("split fast and heavy", func(t *testing.T) {
		cfg := config{
			Index: commandConfig{Command: haiku},
			Ask:   askConfig{Command: sonnet},
			Recap: commandConfig{Command: sonnet},
		}
		cmd, _ := cfg.indexCommand()
		assertCmd(t, "indexCommand", cmd, haiku)
		cmd, _ = cfg.askCommand()
		assertCmd(t, "askCommand", cmd, sonnet)
		cmd, _ = cfg.askFilterCommand()
		assertCmd(t, "askFilterCommand", cmd, haiku) // falls back to index
		cmd, _ = cfg.recapCommand()
		assertCmd(t, "recapCommand", cmd, sonnet)
	})

	t.Run("full config", func(t *testing.T) {
		cfg := config{
			Index: commandConfig{Command: haiku},
			Ask:   askConfig{Command: sonnet, FilterCommand: fast},
			Recap: commandConfig{Command: sonnet},
		}
		cmd, _ := cfg.indexCommand()
		assertCmd(t, "indexCommand", cmd, haiku)
		cmd, _ = cfg.askCommand()
		assertCmd(t, "askCommand", cmd, sonnet)
		cmd, _ = cfg.askFilterCommand()
		assertCmd(t, "askFilterCommand", cmd, fast)
		cmd, _ = cfg.recapCommand()
		assertCmd(t, "recapCommand", cmd, sonnet)
	})

	t.Run("only recap configured", func(t *testing.T) {
		cfg := config{
			Recap: commandConfig{Command: sonnet},
		}
		cmd, _ := cfg.indexCommand()
		assertCmd(t, "indexCommand", cmd, sonnet) // index -> recap
		cmd, _ = cfg.askCommand()
		assertCmd(t, "askCommand", cmd, sonnet) // ask -> recap
		cmd, _ = cfg.recapCommand()
		assertCmd(t, "recapCommand", cmd, sonnet)
	})

	t.Run("only ask configured", func(t *testing.T) {
		cfg := config{
			Ask: askConfig{Command: sonnet},
		}
		cmd, _ := cfg.indexCommand()
		assertCmd(t, "indexCommand", cmd, sonnet) // index -> recap(nil) -> ask
		cmd, _ = cfg.askCommand()
		assertCmd(t, "askCommand", cmd, sonnet)
		cmd, _ = cfg.askFilterCommand()
		assertCmd(t, "askFilterCommand", cmd, sonnet) // filter -> index(nil) -> ask
		cmd, _ = cfg.recapCommand()
		assertCmd(t, "recapCommand", cmd, sonnet) // recap -> ask
	})

	t.Run("nothing configured", func(t *testing.T) {
		cfg := config{}
		cmd, _ := cfg.indexCommand()
		if cmd != nil {
			t.Errorf("expected nil, got %v", cmd)
		}
		if cfg.hasAnyCommand() {
			t.Error("hasAnyCommand should be false")
		}
	})

	t.Run("env follows command through fallback", func(t *testing.T) {
		recapEnv := map[string]string{"AWS_PROFILE": "recap"}
		cfg := config{
			Recap: commandConfig{Command: sonnet, Env: recapEnv},
		}
		// indexCommand falls back to recap — env should come from recap.
		cmd, env := cfg.indexCommand()
		assertCmd(t, "indexCommand", cmd, sonnet)
		assertEnvMap(t, "indexEnv", env, recapEnv)
	})
}

func TestResumeCommandStr(t *testing.T) {
	t.Run("string form", func(t *testing.T) {
		pc := providerConfig{
			ResumeCommand: json.RawMessage(`"opencode --session {{ID}}"`),
		}
		got := pc.resumeCommandStr()
		if got != "opencode --session {{ID}}" {
			t.Errorf("got %q, want %q", got, "opencode --session {{ID}}")
		}
	})

	t.Run("array form", func(t *testing.T) {
		pc := providerConfig{
			ResumeCommand: json.RawMessage(`["ca", "opencode", "-s", "{{ID}}"]`),
		}
		got := pc.resumeCommandStr()
		// "ca" and "opencode" and "-s" are shell-safe, {{ID}} is a template marker
		want := "ca opencode -s {{ID}}"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("array with spaces in element", func(t *testing.T) {
		pc := providerConfig{
			ResumeCommand: json.RawMessage(`["/usr/local/my tools/agent", "--session", "{{ID}}"]`),
		}
		got := pc.resumeCommandStr()
		want := "'/usr/local/my tools/agent' --session {{ID}}"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("empty", func(t *testing.T) {
		pc := providerConfig{}
		got := pc.resumeCommandStr()
		if got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("null json", func(t *testing.T) {
		pc := providerConfig{
			ResumeCommand: json.RawMessage(`null`),
		}
		got := pc.resumeCommandStr()
		if got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
}

func TestParseDateish(t *testing.T) {
	// Use a fixed "now" — Tuesday April 7, 2026 12:00 local time.
	now := time.Date(2026, 4, 7, 12, 0, 0, 0, time.Local)

	tests := []struct {
		name  string
		input string
		want  time.Time
	}{
		{"iso date", "2026-04-01", time.Date(2026, 4, 1, 0, 0, 0, 0, time.Local)},
		{"relative days", "3d", now.AddDate(0, 0, -3)},
		{"today", "today", time.Date(2026, 4, 7, 0, 0, 0, 0, time.Local)},
		{"yesterday", "yesterday", time.Date(2026, 4, 6, 0, 0, 0, 0, time.Local)},
		{"last week", "last week", now.AddDate(0, 0, -7)},
		// Tuesday -> Monday = 1 day back
		{"monday", "monday", time.Date(2026, 4, 6, 0, 0, 0, 0, time.Local)},
		// Tuesday -> Friday = 4 days back
		{"friday", "friday", time.Date(2026, 4, 3, 0, 0, 0, 0, time.Local)},
		// Tuesday -> Wednesday = 6 days back
		{"wednesday", "wednesday", time.Date(2026, 4, 1, 0, 0, 0, 0, time.Local)},
		// Tuesday -> Tuesday = 7 days back (not 0)
		{"tuesday same day", "tuesday", time.Date(2026, 3, 31, 0, 0, 0, 0, time.Local)},
		{"case insensitive", "Monday", time.Date(2026, 4, 6, 0, 0, 0, 0, time.Local)},
		// Fallback: unknown string returns 7 days ago.
		{"unknown", "gibberish", now.AddDate(0, 0, -7)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseDateish(tt.input, now)
			if !got.Equal(tt.want) {
				t.Errorf("parseDateish(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseTimeRange(t *testing.T) {
	t.Run("days flag", func(t *testing.T) {
		start, end := parseTimeRange("", "", 7)
		if time.Since(start) > 8*24*time.Hour || time.Since(start) < 6*24*time.Hour {
			t.Errorf("start should be ~7 days ago, got %v", start)
		}
		if time.Since(end) > time.Minute {
			t.Errorf("end should be ~now, got %v", end)
		}
	})

	t.Run("default is 7 days", func(t *testing.T) {
		start, _ := parseTimeRange("", "", 0)
		if time.Since(start) > 8*24*time.Hour || time.Since(start) < 6*24*time.Hour {
			t.Errorf("default start should be ~7 days ago, got %v", start)
		}
	})

	t.Run("until date", func(t *testing.T) {
		_, end := parseTimeRange("", "2026-04-07", 7)
		// end should be end of 2026-04-07
		if end.Year() != 2026 || end.Month() != 4 || end.Day() != 7 {
			t.Errorf("end should be 2026-04-07, got %v", end)
		}
	})
}

func TestConfigHasAnyCommand(t *testing.T) {
	t.Run("index set", func(t *testing.T) {
		cfg := config{Index: commandConfig{Command: []string{"llm"}}}
		if !cfg.hasAnyCommand() {
			t.Error("expected true")
		}
	})

	t.Run("ask set", func(t *testing.T) {
		cfg := config{Ask: askConfig{Command: []string{"llm"}}}
		if !cfg.hasAnyCommand() {
			t.Error("expected true")
		}
	})

	t.Run("nothing set", func(t *testing.T) {
		cfg := config{}
		if cfg.hasAnyCommand() {
			t.Error("expected false")
		}
	})
}

func TestSummaryConfig(t *testing.T) {
	cfg := config{
		Index: commandConfig{
			Command: []string{"llm", "-m", "haiku"},
			Prompt:  "custom prompt",
		},
	}
	sc := cfg.summaryConfig()
	if len(sc.Command) != 3 || sc.Command[2] != "haiku" {
		t.Errorf("summaryConfig command = %v, want haiku", sc.Command)
	}
	if sc.Prompt != "custom prompt" {
		t.Errorf("summaryConfig prompt = %q, want %q", sc.Prompt, "custom prompt")
	}
	if sc.Env != nil {
		t.Errorf("summaryConfig env = %v, want nil (no env configured)", sc.Env)
	}

	t.Run("with env", func(t *testing.T) {
		cfg := config{
			Env:   map[string]string{"AWS_PROFILE": "default"},
			Index: commandConfig{Command: []string{"llm"}, Env: map[string]string{"FOO": "bar"}},
		}
		sc := cfg.summaryConfig()
		if sc.Env == nil {
			t.Fatal("expected non-nil env")
		}
		envMap := envSliceToMap(sc.Env)
		if envMap["AWS_PROFILE"] != "default" {
			t.Errorf("expected AWS_PROFILE=default, got %q", envMap["AWS_PROFILE"])
		}
		if envMap["FOO"] != "bar" {
			t.Errorf("expected FOO=bar, got %q", envMap["FOO"])
		}
	})

	t.Run("with system_prompt", func(t *testing.T) {
		cfg := config{
			Index: commandConfig{
				Command:      []string{"llm"},
				SystemPrompt: "You are a custom indexer.",
				Prompt:       "Label it.",
			},
		}
		sc := cfg.summaryConfig()
		if sc.SystemPrompt != "You are a custom indexer." {
			t.Errorf("summaryConfig system_prompt = %q, want %q", sc.SystemPrompt, "You are a custom indexer.")
		}
		if sc.Prompt != "Label it." {
			t.Errorf("summaryConfig prompt = %q, want %q", sc.Prompt, "Label it.")
		}
	})
}

func TestPromptResolution(t *testing.T) {
	cfg := config{
		Index: commandConfig{
			SystemPrompt: "index system",
			Prompt:       "index prompt",
		},
		Recap: commandConfig{
			SystemPrompt: "recap system",
			Prompt:       "recap prompt",
		},
		Ask: askConfig{
			SystemPrompt: "ask system",
			Prompt:       "ask prompt",
		},
	}

	if cfg.indexPrompt() != "index prompt" {
		t.Errorf("indexPrompt = %q", cfg.indexPrompt())
	}
	if cfg.indexSystemPrompt() != "index system" {
		t.Errorf("indexSystemPrompt = %q", cfg.indexSystemPrompt())
	}
	if cfg.recapPrompt() != "recap prompt" {
		t.Errorf("recapPrompt = %q", cfg.recapPrompt())
	}
	if cfg.recapSystemPrompt() != "recap system" {
		t.Errorf("recapSystemPrompt = %q", cfg.recapSystemPrompt())
	}
	if cfg.askPrompt() != "ask prompt" {
		t.Errorf("askPrompt = %q", cfg.askPrompt())
	}
	if cfg.askSystemPrompt() != "ask system" {
		t.Errorf("askSystemPrompt = %q", cfg.askSystemPrompt())
	}

	t.Run("empty returns empty", func(t *testing.T) {
		empty := config{}
		if empty.indexPrompt() != "" {
			t.Error("expected empty indexPrompt")
		}
		if empty.indexSystemPrompt() != "" {
			t.Error("expected empty indexSystemPrompt")
		}
		if empty.recapPrompt() != "" {
			t.Error("expected empty recapPrompt")
		}
		if empty.askPrompt() != "" {
			t.Error("expected empty askPrompt")
		}
	})
}

// assertCmd checks that two string slices match.
func assertCmd(t *testing.T, name string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s = %v, want %v", name, got, want)
		return
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("%s[%d] = %q, want %q", name, i, got[i], want[i])
		}
	}
}

// --- findSession tests ---

func testSessions() []provider.Session {
	now := time.Now()
	return []provider.Session{
		{Agent: "opencode", ID: "ses_abc123", Title: "Auth middleware", LastUsed: now, Directory: "/home/drew/projects/sesh", SearchText: "Auth middleware sesh"},
		{Agent: "opencode", ID: "ses_abc456", Title: "Fix tests", LastUsed: now.Add(-time.Hour), Directory: "/home/drew/projects/sesh", SearchText: "Fix tests sesh"},
		{Agent: "claude", ID: "uuid-1234-5678", Title: "Refactor API", LastUsed: now.Add(-2 * time.Hour), Directory: "/home/drew/projects/api-gateway", SearchText: "Refactor API api-gateway"},
		{Agent: "opencode", ID: "ses_def789", Title: "Build pipeline", LastUsed: now.Add(-24 * time.Hour), Directory: "/home/drew/work/infra", SearchText: "Build pipeline infra"},
	}
}

// --- query integration tests ---

func TestQueryFilterIntegration(t *testing.T) {
	sessions := testSessions()

	t.Run("agent flag filters by agent name", func(t *testing.T) {
		query := tui.BuildPrefixQuery("", "opencode", "")
		pq := tui.ParseQuery(query)
		result := tui.FilterSessions(sessions, pq)
		for _, s := range result {
			if s.Agent != "opencode" {
				t.Errorf("expected opencode, got %s", s.Agent)
			}
		}
		if len(result) != 3 {
			t.Errorf("expected 3 opencode sessions, got %d", len(result))
		}
	})

	t.Run("dir flag filters by directory", func(t *testing.T) {
		query := tui.BuildPrefixQuery("/home/drew/projects/sesh", "", "")
		pq := tui.ParseQuery(query)
		result := tui.FilterSessions(sessions, pq)
		if len(result) < 2 {
			t.Fatalf("expected at least 2 results for sesh dir, got %d", len(result))
		}
		// Sessions 1 and 2 have this exact directory.
		ids := make(map[string]bool)
		for _, s := range result {
			ids[s.ID] = true
		}
		if !ids["ses_abc123"] || !ids["ses_abc456"] {
			t.Errorf("expected ses_abc123 and ses_abc456 in results, got %v", ids)
		}
	})

	t.Run("dir and agent combined", func(t *testing.T) {
		query := tui.BuildPrefixQuery("/home/drew/projects/sesh", "opencode", "")
		pq := tui.ParseQuery(query)
		result := tui.FilterSessions(sessions, pq)
		if len(result) != 2 {
			t.Fatalf("expected 2 results, got %d", len(result))
		}
	})

	t.Run("dir and agent and text", func(t *testing.T) {
		query := tui.BuildPrefixQuery("/home/drew/projects/sesh", "opencode", "Auth")
		pq := tui.ParseQuery(query)
		result := tui.FilterSessions(sessions, pq)
		if len(result) != 1 {
			t.Fatalf("expected 1 result, got %d", len(result))
		}
		if result[0].ID != "ses_abc123" {
			t.Errorf("expected ses_abc123, got %s", result[0].ID)
		}
	})

	t.Run("fuzzy agent match via flag", func(t *testing.T) {
		query := tui.BuildPrefixQuery("", "clude", "")
		pq := tui.ParseQuery(query)
		result := tui.FilterSessions(sessions, pq)
		// "clude" should fuzzy-match "claude".
		if len(result) != 1 {
			t.Fatalf("expected 1 claude result, got %d", len(result))
		}
		if result[0].Agent != "claude" {
			t.Errorf("expected claude, got %s", result[0].Agent)
		}
	})

	t.Run("empty flags returns all", func(t *testing.T) {
		query := tui.BuildPrefixQuery("", "", "")
		if query != "" {
			t.Fatalf("expected empty query, got %q", query)
		}
		// Empty query means no filtering.
		pq := tui.ParseQuery(query)
		result := tui.FilterSessions(sessions, pq)
		if len(result) != len(sessions) {
			t.Errorf("expected %d sessions, got %d", len(sessions), len(result))
		}
	})
}

func TestFindSessionExactMatch(t *testing.T) {
	sessions := testSessions()
	match, ambiguous := findSession(sessions, "ses_abc123")
	if match == nil {
		t.Fatal("expected match")
	}
	if match.ID != "ses_abc123" {
		t.Errorf("got %q, want ses_abc123", match.ID)
	}
	if ambiguous != nil {
		t.Error("expected no ambiguous candidates")
	}
}

func TestFindSessionPrefixUnique(t *testing.T) {
	sessions := testSessions()
	match, ambiguous := findSession(sessions, "ses_def")
	if match == nil {
		t.Fatal("expected match")
	}
	if match.ID != "ses_def789" {
		t.Errorf("got %q, want ses_def789", match.ID)
	}
	if ambiguous != nil {
		t.Error("expected no ambiguous candidates")
	}
}

func TestFindSessionPrefixAmbiguous(t *testing.T) {
	sessions := testSessions()
	match, ambiguous := findSession(sessions, "ses_abc")
	if match != nil {
		t.Error("expected nil match for ambiguous prefix")
	}
	if len(ambiguous) != 2 {
		t.Errorf("expected 2 ambiguous candidates, got %d", len(ambiguous))
	}
}

func TestFindSessionNotFound(t *testing.T) {
	sessions := testSessions()
	match, ambiguous := findSession(sessions, "nonexistent")
	if match != nil {
		t.Error("expected nil match")
	}
	if ambiguous != nil {
		t.Error("expected nil ambiguous")
	}
}

func TestFindSessionExactOverPrefix(t *testing.T) {
	// If an ID is an exact match, it should win even if it's also a prefix of another.
	sessions := []provider.Session{
		{ID: "ses_abc"},
		{ID: "ses_abc123"},
	}
	match, _ := findSession(sessions, "ses_abc")
	if match == nil {
		t.Fatal("expected match")
	}
	if match.ID != "ses_abc" {
		t.Errorf("got %q, want ses_abc (exact should win)", match.ID)
	}
}

// --- computeStats tests ---

func TestComputeStats(t *testing.T) {
	now := time.Now()
	sessions := []provider.Session{
		{Agent: "opencode", Summary: "summary", Directory: "/project-a", LastUsed: now},
		{Agent: "opencode", Summary: "", Directory: "/project-a", LastUsed: now.Add(-2 * time.Hour)},
		{Agent: "claude", Summary: "summary", Directory: "/project-b", LastUsed: now.Add(-3 * 24 * time.Hour)},
		{Agent: "claude", Summary: "summary", Directory: "/project-b", LastUsed: now.Add(-40 * 24 * time.Hour)},
	}

	stats := computeStats(sessions)

	if stats.Total != 4 {
		t.Errorf("Total = %d, want 4", stats.Total)
	}
	if stats.Summarized != 3 {
		t.Errorf("Summarized = %d, want 3", stats.Summarized)
	}
	if stats.AgentCounts["opencode"] != 2 {
		t.Errorf("opencode count = %d, want 2", stats.AgentCounts["opencode"])
	}
	if stats.AgentCounts["claude"] != 2 {
		t.Errorf("claude count = %d, want 2", stats.AgentCounts["claude"])
	}
	if stats.Today != 2 {
		t.Errorf("Today = %d, want 2", stats.Today)
	}
	if stats.ThisWeek != 3 {
		t.Errorf("ThisWeek = %d, want 3", stats.ThisWeek)
	}
	if stats.ThisMonth != 3 {
		t.Errorf("ThisMonth = %d, want 3", stats.ThisMonth)
	}
	if stats.DirCounts["/project-a"] != 2 {
		t.Errorf("project-a count = %d, want 2", stats.DirCounts["/project-a"])
	}
	if stats.DirCounts["/project-b"] != 2 {
		t.Errorf("project-b count = %d, want 2", stats.DirCounts["/project-b"])
	}
}

func TestComputeStatsEmpty(t *testing.T) {
	stats := computeStats(nil)
	if stats.Total != 0 {
		t.Errorf("Total = %d, want 0", stats.Total)
	}
}

// --- truncate tests ---

func TestTruncate(t *testing.T) {
	tests := []struct {
		name  string
		input string
		max   int
		want  string
	}{
		{"short", "hello", 10, "hello"},
		{"exact", "hello", 5, "hello"},
		{"truncated", "hello world", 8, "hello w…"},
		{"one char", "ab", 1, "…"},
		{"empty", "", 5, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.input, tt.max)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.want)
			}
		})
	}
}

// --- detectShell tests ---

func TestDetectShell(t *testing.T) {
	tests := []struct {
		shell string
		want  string
	}{
		{"/bin/bash", "bash"},
		{"/bin/zsh", "zsh"},
		{"/usr/bin/fish", "fish"},
		{"/bin/sh", ""},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.shell, func(t *testing.T) {
			orig := os.Getenv("SHELL")
			os.Setenv("SHELL", tt.shell)
			defer os.Setenv("SHELL", orig)

			got := detectShell()
			if got != tt.want {
				t.Errorf("detectShell() with SHELL=%q = %q, want %q", tt.shell, got, tt.want)
			}
		})
	}
}

// --- init output tests ---

func TestInitOutputs(t *testing.T) {
	// Verify the init strings contain the essential function definition.
	if !strings.Contains(initBash, "sesh()") {
		t.Error("initBash missing function definition")
	}
	if !strings.Contains(initBash, "eval") {
		t.Error("initBash missing eval")
	}
	if !strings.Contains(initBash, "command sesh") {
		t.Error("initBash missing 'command sesh'")
	}

	if !strings.Contains(initZsh, "sesh()") {
		t.Error("initZsh missing function definition")
	}

	if !strings.Contains(initFish, "function sesh") {
		t.Error("initFish missing function definition")
	}
	if !strings.Contains(initFish, "eval") {
		t.Error("initFish missing eval")
	}

	if !strings.Contains(initPowerShell, "function sesh") {
		t.Error("initPowerShell missing function definition")
	}
	if !strings.Contains(initPowerShell, "Invoke-Expression") {
		t.Error("initPowerShell missing Invoke-Expression")
	}
	if !strings.Contains(initPowerShell, "sesh.exe") {
		t.Error("initPowerShell missing sesh.exe")
	}

	// Verify subcommand passthrough: subcommands should run directly
	// without stdout capture (so they preserve TTY for glamour, colors, etc.).
	subcommands := []string{"index", "recap", "ask", "init", "list", "show", "stats", "version", "update"}
	for _, sub := range subcommands {
		if !strings.Contains(initBash, sub) {
			t.Errorf("initBash missing subcommand passthrough for %q", sub)
		}
		if !strings.Contains(initZsh, sub) {
			t.Errorf("initZsh missing subcommand passthrough for %q", sub)
		}
		if !strings.Contains(initFish, sub) {
			t.Errorf("initFish missing subcommand passthrough for %q", sub)
		}
		if !strings.Contains(initPowerShell, sub) {
			t.Errorf("initPowerShell missing subcommand passthrough for %q", sub)
		}
	}
}

// --- buildProviders tests ---

func TestBuildProvidersDefault(t *testing.T) {
	// No config: should get built-in opencode and claude.
	cfg := config{}
	providers := buildProviders(cfg)

	names := providerNames(providers)
	if !contains(names, "opencode") {
		t.Error("expected opencode provider")
	}
	if !contains(names, "claude") {
		t.Error("expected claude provider")
	}

	// Verify they're the built-in types, not external.
	for _, p := range providers {
		switch p.Name() {
		case "opencode":
			if _, ok := p.(*provider.OpenCode); !ok {
				t.Errorf("opencode should be *provider.OpenCode, got %T", p)
			}
		case "claude":
			if _, ok := p.(*provider.Claude); !ok {
				t.Errorf("claude should be *provider.Claude, got %T", p)
			}
		}
	}
}

func TestBuildProvidersListCommandOverridesBuiltin(t *testing.T) {
	// If opencode has a list_command, it should be treated as external.
	cfg := config{
		Providers: map[string]providerConfig{
			"opencode": {
				ListCommand:   []string{"cat", "sessions.json"},
				ResumeCommand: json.RawMessage(`"opencode -s {{ID}}"`),
			},
		},
	}
	providers := buildProviders(cfg)

	for _, p := range providers {
		if p.Name() == "opencode" {
			if _, ok := p.(*provider.External); !ok {
				t.Errorf("opencode with list_command should be *provider.External, got %T", p)
			}
			return
		}
	}
	t.Error("expected opencode provider")
}

func TestBuildProvidersListCommandOverridesClaude(t *testing.T) {
	// Same for claude.
	cfg := config{
		Providers: map[string]providerConfig{
			"claude": {
				ListCommand:   []string{"cat", "claude.json"},
				ResumeCommand: json.RawMessage(`"claude --resume {{ID}}"`),
			},
		},
	}
	providers := buildProviders(cfg)

	for _, p := range providers {
		if p.Name() == "claude" {
			if _, ok := p.(*provider.External); !ok {
				t.Errorf("claude with list_command should be *provider.External, got %T", p)
			}
			return
		}
	}
	t.Error("expected claude provider")
}

func TestBuildProvidersBuiltinWithResumeOnly(t *testing.T) {
	// If opencode has only a resume_command (no list_command), it stays built-in.
	cfg := config{
		Providers: map[string]providerConfig{
			"opencode": {
				ResumeCommand: json.RawMessage(`"ca opencode -s {{ID}}"`),
			},
		},
	}
	providers := buildProviders(cfg)

	for _, p := range providers {
		if p.Name() == "opencode" {
			if _, ok := p.(*provider.OpenCode); !ok {
				t.Errorf("opencode without list_command should be *provider.OpenCode, got %T", p)
			}
			return
		}
	}
	t.Error("expected opencode provider")
}

func TestBuildProvidersDisabled(t *testing.T) {
	disabled := false
	cfg := config{
		Providers: map[string]providerConfig{
			"opencode": {Enabled: &disabled},
		},
	}
	providers := buildProviders(cfg)

	for _, p := range providers {
		if p.Name() == "opencode" {
			t.Error("opencode should be disabled")
		}
	}
}

func TestBuildProvidersExternal(t *testing.T) {
	cfg := config{
		Providers: map[string]providerConfig{
			"omp": {
				ListCommand:   []string{"omp-sesh"},
				ResumeCommand: json.RawMessage(`"omp --resume {{ID}}"`),
			},
		},
	}
	providers := buildProviders(cfg)

	names := providerNames(providers)
	if !contains(names, "omp") {
		t.Error("expected omp provider")
	}

	for _, p := range providers {
		if p.Name() == "omp" {
			if _, ok := p.(*provider.External); !ok {
				t.Errorf("omp should be *provider.External, got %T", p)
			}
		}
	}
}

func providerNames(providers []provider.Provider) []string {
	var names []string
	for _, p := range providers {
		names = append(names, p.Name())
	}
	return names
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// --- colorAgent tests ---

func TestColorAgent(t *testing.T) {
	// Just verify it returns non-empty strings and doesn't panic.
	for _, name := range []string{"opencode", "claude", "omp", "unknown"} {
		got := colorAgent(name)
		if got == "" {
			t.Errorf("colorAgent(%q) returned empty", name)
		}
		if !strings.Contains(got, name) {
			t.Errorf("colorAgent(%q) = %q, should contain agent name", name, got)
		}
	}
}

func TestColorAgentUsesSharedHash(t *testing.T) {
	// Verify that colorAgent produces the ANSI code matching agent.ANSIColor.
	for _, name := range []string{"opencode", "claude", "pi-mono", "omp"} {
		got := colorAgent(name)
		wantCode := fmt.Sprintf("\033[%dm", 30+agent.ANSIColor(name))
		if !strings.HasPrefix(got, wantCode) {
			t.Errorf("colorAgent(%q) starts with wrong ANSI code: got %q, want prefix %q", name, got, wantCode)
		}
	}
}

// --- aiFilterSessions tests ---

func TestAiFilterSessions(t *testing.T) {
	sessions := testSessions()

	// Mock LLM that returns "1\n3\n" (first and third sessions).
	cmd := testhelper.WriteMockScript(t,
		"#!/bin/sh\necho '1\n3'",
		"Write-Output 1\nWrite-Output 3",
	)
	result := aiFilterSessions(context.Background(), cmd, nil, "auth", sessions, 10)
	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}
	if result[0].ID != "ses_abc123" {
		t.Errorf("first result should be ses_abc123, got %s", result[0].ID)
	}
	if result[1].ID != "uuid-1234-5678" {
		t.Errorf("second result should be uuid-1234-5678, got %s", result[1].ID)
	}
}

func TestAiFilterSessionsFormats(t *testing.T) {
	sessions := testSessions()

	// LLM returns numbers in various formats.
	cmd := testhelper.WriteMockScript(t,
		"#!/bin/sh\necho '2.\n4 - build pipeline'",
		"Write-Output '2.'\nWrite-Output '4 - build pipeline'",
	)
	result := aiFilterSessions(context.Background(), cmd, nil, "tests", sessions, 10)
	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}
	if result[0].ID != "ses_abc456" {
		t.Errorf("first result should be ses_abc456, got %s", result[0].ID)
	}
	if result[1].ID != "ses_def789" {
		t.Errorf("second result should be ses_def789, got %s", result[1].ID)
	}
}

func TestAiFilterSessionsNoResults(t *testing.T) {
	sessions := testSessions()

	// LLM returns empty output.
	cmd := testhelper.WriteMockScript(t,
		"#!/bin/sh\necho ''",
		"Write-Output ''",
	)
	result := aiFilterSessions(context.Background(), cmd, nil, "nothing", sessions, 10)
	if len(result) != 0 {
		t.Errorf("expected 0 results, got %d", len(result))
	}
}

func TestAiFilterSessionsCommandFailure(t *testing.T) {
	sessions := testSessions()

	cmd := testhelper.WriteMockScript(t,
		"#!/bin/sh\nexit 1",
		"exit 1",
	)
	result := aiFilterSessions(context.Background(), cmd, nil, "query", sessions, 10)
	if result != nil {
		t.Errorf("expected nil on failure, got %d results", len(result))
	}
}

func TestAiFilterSessionsMaxResults(t *testing.T) {
	// Build 20 sessions.
	var sessions []provider.Session
	for i := 0; i < 20; i++ {
		sessions = append(sessions, provider.Session{
			ID:    fmt.Sprintf("ses_%d", i),
			Title: fmt.Sprintf("Session %d", i),
		})
	}

	// LLM returns all 20.
	cmd := testhelper.WriteMockScript(t,
		"#!/bin/sh\nseq 1 20",
		"1..20 | ForEach-Object { Write-Output $_ }",
	)
	result := aiFilterSessions(context.Background(), cmd, nil, "all", sessions, 10)
	if len(result) != 10 {
		t.Errorf("expected max 10 results, got %d", len(result))
	}
}

// --- buildEnv tests ---

func TestBuildEnvNil(t *testing.T) {
	cfg := config{}
	env := cfg.buildEnv(nil)
	if env != nil {
		t.Errorf("expected nil when no env configured, got %d entries", len(env))
	}
}

func TestBuildEnvTopLevelOnly(t *testing.T) {
	cfg := config{
		Env: map[string]string{"AWS_PROFILE": "my-profile"},
	}
	env := cfg.buildEnv(nil)
	if env == nil {
		t.Fatal("expected non-nil env")
	}
	m := envSliceToMap(env)
	if m["AWS_PROFILE"] != "my-profile" {
		t.Errorf("AWS_PROFILE = %q, want %q", m["AWS_PROFILE"], "my-profile")
	}
	// Should still have PATH from the parent process.
	if m["PATH"] == "" {
		t.Error("expected PATH to be inherited from parent process")
	}
}

func TestBuildEnvCommandOverridesTopLevel(t *testing.T) {
	cfg := config{
		Env: map[string]string{"AWS_PROFILE": "default", "AWS_DEFAULT_REGION": "us-east-1"},
	}
	cmdEnv := map[string]string{"AWS_DEFAULT_REGION": "us-west-2"}
	env := cfg.buildEnv(cmdEnv)
	m := envSliceToMap(env)
	if m["AWS_PROFILE"] != "default" {
		t.Errorf("AWS_PROFILE = %q, want %q", m["AWS_PROFILE"], "default")
	}
	if m["AWS_DEFAULT_REGION"] != "us-west-2" {
		t.Errorf("AWS_DEFAULT_REGION = %q, want %q (command should override top-level)", m["AWS_DEFAULT_REGION"], "us-west-2")
	}
}

func TestBuildEnvCommandOnlyNoTopLevel(t *testing.T) {
	cfg := config{}
	cmdEnv := map[string]string{"FOO": "bar"}
	env := cfg.buildEnv(cmdEnv)
	m := envSliceToMap(env)
	if m["FOO"] != "bar" {
		t.Errorf("FOO = %q, want %q", m["FOO"], "bar")
	}
}

func TestBuildEnvOverridesProcessEnv(t *testing.T) {
	// Set a process env var and verify buildEnv overrides it.
	t.Setenv("SESH_TEST_VAR", "original")

	cfg := config{
		Env: map[string]string{"SESH_TEST_VAR": "overridden"},
	}
	env := cfg.buildEnv(nil)
	m := envSliceToMap(env)
	if m["SESH_TEST_VAR"] != "overridden" {
		t.Errorf("SESH_TEST_VAR = %q, want %q", m["SESH_TEST_VAR"], "overridden")
	}

	// Verify no duplicate entries for the same key.
	count := 0
	for _, e := range env {
		if strings.HasPrefix(e, "SESH_TEST_VAR=") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 SESH_TEST_VAR entry, got %d", count)
	}
}

func TestBuildEnvPassedToRunLLM(t *testing.T) {
	cmd := testhelper.WriteMockScript(t,
		"#!/bin/sh\necho $TEST_ENV_VAR",
		"Write-Output $env:TEST_ENV_VAR",
	)
	cfg := config{
		Env: map[string]string{"TEST_ENV_VAR": "from_config"},
	}
	env := cfg.buildEnv(nil)
	result, err := summary.RunLLM(context.Background(), cmd, env, "input", 5*time.Second)
	if err != nil {
		t.Fatalf("RunLLM failed: %v", err)
	}
	if result != "from_config" {
		t.Errorf("got %q, want %q", result, "from_config")
	}
}

// --- helpers ---

// envSliceToMap converts a []string env slice to a map for easy lookup.
func envSliceToMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, e := range env {
		if k, v, ok := strings.Cut(e, "="); ok {
			m[k] = v
		}
	}
	return m
}

// assertEnvMap checks that two env maps match.
func assertEnvMap(t *testing.T, name string, got, want map[string]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s = %v, want %v", name, got, want)
		return
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s[%q] = %q, want %q", name, k, got[k], v)
		}
	}
}

// --- resolveDirFlags tests ---

func TestResolveDirFlagsNone(t *testing.T) {
	got, err := resolveDirFlags("", false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestResolveDirFlagsDirOnly(t *testing.T) {
	dir := t.TempDir()
	got, err := resolveDirFlags(dir, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should resolve to an absolute, cleaned path.
	if !filepath.IsAbs(got) {
		t.Errorf("got %q, want absolute path", got)
	}
}

func TestResolveDirFlagsCwdOnly(t *testing.T) {
	got, err := resolveDirFlags("", true, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cwd, _ := os.Getwd()
	if got != cwd {
		t.Errorf("got %q, want cwd %q", got, cwd)
	}
}

func TestResolveDirFlagsRepoOnly(t *testing.T) {
	// This test is running inside the sesh git repo, so --repo should work.
	got, err := resolveDirFlags("", false, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("got %q, want absolute path", got)
	}
	// The resolved path should contain "sesh" somewhere (it's the repo root).
	if !strings.Contains(got, "sesh") {
		t.Errorf("got %q, expected it to contain 'sesh'", got)
	}
}

func TestResolveDirFlagsMutuallyExclusive(t *testing.T) {
	tests := []struct {
		name string
		dir  string
		cwd  bool
		repo bool
	}{
		{"dir+cwd", "/tmp", true, false},
		{"dir+repo", "/tmp", false, true},
		{"cwd+repo", "", true, true},
		{"all three", "/tmp", true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolveDirFlags(tt.dir, tt.cwd, tt.repo)
			if err == nil {
				t.Error("expected error for mutually exclusive flags, got nil")
			}
			if !strings.Contains(err.Error(), "mutually exclusive") {
				t.Errorf("error %q should mention 'mutually exclusive'", err.Error())
			}
		})
	}
}
