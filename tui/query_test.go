package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dru89/sesh/provider"
)

func TestParseQuery(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  ParsedQuery
	}{
		{"empty", "", ParsedQuery{}},
		{"plain text", "refactor auth", ParsedQuery{Text: "refactor auth"}},
		{"dir only", "dir:sesh", ParsedQuery{Dir: "sesh"}},
		{"agent only", "agent:opencode", ParsedQuery{Agent: "opencode"}},
		{"dir and text", "dir:sesh refactor", ParsedQuery{Dir: "sesh", Text: "refactor"}},
		{"agent and text", "agent:claude fix tests", ParsedQuery{Agent: "claude", Text: "fix tests"}},
		{"dir and agent", "dir:sesh agent:opencode", ParsedQuery{Dir: "sesh", Agent: "opencode"}},
		{"all three", "dir:sesh agent:opencode refactor", ParsedQuery{Dir: "sesh", Agent: "opencode", Text: "refactor"}},
		{"prefix in middle", "refactor dir:sesh auth", ParsedQuery{Dir: "sesh", Text: "refactor auth"}},
		{"last dir wins", "dir:foo dir:bar", ParsedQuery{Dir: "bar"}},
		{"unknown prefix is text", "foo:bar baz", ParsedQuery{Text: "foo:bar baz"}},
		{"dir with path", "dir:/Users/drew/sesh", ParsedQuery{Dir: "/Users/drew/sesh"}},
		{"dir with tilde", "dir:~/Developer", ParsedQuery{Dir: "~/Developer"}},
		{"quoted dir", `dir:"my project" refactor`, ParsedQuery{Dir: "my project", Text: "refactor"}},
		{"colon at end is text", "dir:", ParsedQuery{Text: "dir:"}},
		{"colon at start is text", ":value", ParsedQuery{Text: ":value"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseQuery(tt.input)
			if got != tt.want {
				t.Errorf("ParseQuery(%q) = %+v, want %+v", tt.input, got, tt.want)
			}
		})
	}
}

func TestTokenize(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"empty", "", nil},
		{"single word", "hello", []string{"hello"}},
		{"two words", "hello world", []string{"hello", "world"}},
		{"extra spaces", "  hello  world  ", []string{"hello", "world"}},
		{"quoted", `dir:"my project"`, []string{"dir:my project"}},
		{"quoted with extra", `dir:"my project" foo`, []string{"dir:my project", "foo"}},
		{"unclosed quote", `dir:"my project`, []string{"dir:my project"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tokenize(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("tokenize(%q) = %v (len %d), want %v (len %d)", tt.input, got, len(got), tt.want, len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("tokenize(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestSplitPrefix(t *testing.T) {
	tests := []struct {
		token   string
		wantKey string
		wantVal string
		wantOk  bool
	}{
		{"dir:sesh", "dir", "sesh", true},
		{"agent:opencode", "agent", "opencode", true},
		{"dir:/path/to/thing", "dir", "/path/to/thing", true},
		{"Dir:Sesh", "dir", "Sesh", true},
		{"noprefix", "", "", false},
		{":value", "", "", false},
		{"dir:", "", "", false},
		{"a:b", "a", "b", true},
	}

	for _, tt := range tests {
		t.Run(tt.token, func(t *testing.T) {
			key, val, ok := splitPrefix(tt.token)
			if key != tt.wantKey || val != tt.wantVal || ok != tt.wantOk {
				t.Errorf("splitPrefix(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tt.token, key, val, ok, tt.wantKey, tt.wantVal, tt.wantOk)
			}
		})
	}
}

func TestFilterSessions(t *testing.T) {
	sessions := []provider.Session{
		{Agent: "opencode", ID: "1", Directory: "/home/drew/projects/sesh", SearchText: "sesh CLI tool refactor"},
		{Agent: "opencode", ID: "2", Directory: "/home/drew/projects/todo-app", SearchText: "todo app task manager"},
		{Agent: "claude", ID: "3", Directory: "/home/drew/projects/sesh", SearchText: "sesh add feature"},
		{Agent: "claude", ID: "4", Directory: "/home/drew/work/myproject", SearchText: "myproject auth middleware"},
	}

	t.Run("dir filter", func(t *testing.T) {
		pq := ParsedQuery{Dir: "/home/drew/projects/sesh"}
		result := FilterSessions(sessions, pq)
		// Exact path should match sessions 1 and 3.
		if len(result) < 2 {
			t.Fatalf("expected at least 2 results, got %d", len(result))
		}
		ids := sessionIDs(result)
		if !containsStr(ids, "1") || !containsStr(ids, "3") {
			t.Errorf("expected sessions 1 and 3 in results, got %v", ids)
		}
	})

	t.Run("agent filter", func(t *testing.T) {
		pq := ParsedQuery{Agent: "claude"}
		result := FilterSessions(sessions, pq)
		// Should match sessions 3 and 4.
		if len(result) != 2 {
			t.Fatalf("expected 2 results, got %d", len(result))
		}
		for _, s := range result {
			if s.Agent != "claude" {
				t.Errorf("expected claude, got %s", s.Agent)
			}
		}
	})

	t.Run("dir and agent", func(t *testing.T) {
		pq := ParsedQuery{Dir: "/home/drew/projects/sesh", Agent: "opencode"}
		result := FilterSessions(sessions, pq)
		// Should narrow to session 1 (sesh + opencode).
		if len(result) != 1 {
			t.Fatalf("expected 1 result, got %d", len(result))
		}
		if result[0].ID != "1" {
			t.Errorf("expected session 1, got %s", result[0].ID)
		}
	})

	t.Run("text filter", func(t *testing.T) {
		pq := ParsedQuery{Text: "auth"}
		result := FilterSessions(sessions, pq)
		// Should match session 4 (has "auth" in SearchText).
		if len(result) != 1 {
			t.Fatalf("expected 1 result, got %d", len(result))
		}
		if result[0].ID != "4" {
			t.Errorf("expected session 4, got %s", result[0].ID)
		}
	})

	t.Run("all filters", func(t *testing.T) {
		pq := ParsedQuery{Dir: "/home/drew/projects/sesh", Agent: "opencode", Text: "refactor"}
		result := FilterSessions(sessions, pq)
		if len(result) != 1 {
			t.Fatalf("expected 1 result, got %d", len(result))
		}
		if result[0].ID != "1" {
			t.Errorf("expected session 1, got %s", result[0].ID)
		}
	})

	t.Run("empty query returns all", func(t *testing.T) {
		pq := ParsedQuery{}
		result := FilterSessions(sessions, pq)
		if len(result) != len(sessions) {
			t.Errorf("expected %d results, got %d", len(sessions), len(result))
		}
	})

	t.Run("no match returns empty", func(t *testing.T) {
		pq := ParsedQuery{Dir: "zzzznonexistent"}
		result := FilterSessions(sessions, pq)
		if len(result) != 0 {
			t.Errorf("expected 0 results, got %d", len(result))
		}
	})

	t.Run("fuzzy agent match", func(t *testing.T) {
		pq := ParsedQuery{Agent: "clude"}
		result := FilterSessions(sessions, pq)
		// "clude" should fuzzy-match "claude".
		if len(result) != 2 {
			t.Fatalf("expected 2 results for fuzzy 'clude', got %d", len(result))
		}
		for _, s := range result {
			if s.Agent != "claude" {
				t.Errorf("expected claude, got %s", s.Agent)
			}
		}
	})
}

func TestNormalizePath(t *testing.T) {
	home, _ := os.UserHomeDir()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"absolute", "/Users/drew/project", "/Users/drew/project"},
		{"trailing slash", "/Users/drew/project/", "/Users/drew/project"},
		{"double slash", "/Users/drew//project", "/Users/drew/project"},
		{"tilde", "~/project", filepath.Join(home, "project")},
		{"bare tilde", "~", home},
		{"dot", ".", "."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizePath(tt.input)
			if got != tt.want {
				t.Errorf("normalizePath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func sessionIDs(sessions []provider.Session) []string {
	ids := make([]string, len(sessions))
	for i, s := range sessions {
		ids[i] = s.ID
	}
	return ids
}

func containsStr(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

func TestBuildPrefixQuery(t *testing.T) {
	tests := []struct {
		name  string
		dir   string
		agent string
		text  string
		want  string
	}{
		{"empty", "", "", "", ""},
		{"dir only", "/path/to/project", "", "", "dir:/path/to/project"},
		{"agent only", "", "opencode", "", "agent:opencode"},
		{"text only", "", "", "refactor", "refactor"},
		{"dir and text", "/path/to/project", "", "refactor", "dir:/path/to/project refactor"},
		{"all three", "/path/to/project", "opencode", "refactor", "dir:/path/to/project agent:opencode refactor"},
		{"dir with spaces", "/path/to/my project", "", "", `dir:"/path/to/my project"`},
		{"agent with spaces", "", "my agent", "", `agent:"my agent"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildPrefixQuery(tt.dir, tt.agent, tt.text)
			if got != tt.want {
				t.Errorf("BuildPrefixQuery(%q, %q, %q) = %q, want %q",
					tt.dir, tt.agent, tt.text, got, tt.want)
			}
		})
	}
}

func TestBuildPrefixQueryRoundTrip(t *testing.T) {
	// Verify that BuildPrefixQuery output parses back correctly.
	tests := []struct {
		name  string
		dir   string
		agent string
		text  string
	}{
		{"dir only", "/path/to/project", "", ""},
		{"agent only", "", "opencode", ""},
		{"all three", "/path/to/project", "opencode", "refactor"},
		{"dir with spaces", "/path/to/my project", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query := BuildPrefixQuery(tt.dir, tt.agent, tt.text)
			pq := ParseQuery(query)
			if pq.Dir != tt.dir {
				t.Errorf("round-trip Dir: got %q, want %q", pq.Dir, tt.dir)
			}
			if pq.Agent != tt.agent {
				t.Errorf("round-trip Agent: got %q, want %q", pq.Agent, tt.agent)
			}
			if pq.Text != tt.text {
				t.Errorf("round-trip Text: got %q, want %q", pq.Text, tt.text)
			}
		})
	}
}

func TestResolveDir(t *testing.T) {
	home, _ := os.UserHomeDir()

	t.Run("empty", func(t *testing.T) {
		got, err := ResolveDir("")
		if err != nil {
			t.Fatal(err)
		}
		if got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})

	t.Run("absolute path", func(t *testing.T) {
		got, err := ResolveDir("/usr/local/bin")
		if err != nil {
			t.Fatal(err)
		}
		if got != "/usr/local/bin" {
			t.Errorf("got %q, want /usr/local/bin", got)
		}
	})

	t.Run("tilde expansion", func(t *testing.T) {
		got, err := ResolveDir("~/projects")
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(home, "projects")
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("relative path", func(t *testing.T) {
		got, err := ResolveDir(".")
		if err != nil {
			t.Fatal(err)
		}
		// Should be an absolute path.
		if !filepath.IsAbs(got) {
			t.Errorf("expected absolute path, got %q", got)
		}
	})

	t.Run("trailing slash cleaned", func(t *testing.T) {
		got, err := ResolveDir("/usr/local/bin/")
		if err != nil {
			t.Fatal(err)
		}
		if got != "/usr/local/bin" {
			t.Errorf("got %q, want /usr/local/bin", got)
		}
	})
}
