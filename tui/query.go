package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/dru89/sesh/provider"
	"github.com/sahilm/fuzzy"
)

// ParsedQuery holds the structured parts extracted from a search query.
// A query like "dir:sesh agent:opencode refactor" becomes:
//
//	Dir:   "sesh"
//	Agent: "opencode"
//	Text:  "refactor"
type ParsedQuery struct {
	Dir   string // value of dir: prefix (empty if absent)
	Agent string // value of agent: prefix (empty if absent)
	Text  string // remaining freeform search text
}

// ParseQuery extracts structured prefixes from a raw query string.
// Recognized prefixes: dir:<value>, agent:<value>.
// Values can be bare words or quoted with double quotes for values containing spaces.
// Last occurrence of a prefix wins if duplicated.
func ParseQuery(raw string) ParsedQuery {
	var pq ParsedQuery
	var textParts []string

	tokens := tokenize(raw)
	for _, tok := range tokens {
		if key, val, ok := splitPrefix(tok); ok {
			switch key {
			case "dir":
				pq.Dir = val
			case "agent":
				pq.Agent = val
			default:
				// Unknown prefix — treat as regular text.
				textParts = append(textParts, tok)
			}
		} else {
			textParts = append(textParts, tok)
		}
	}

	pq.Text = strings.Join(textParts, " ")
	return pq
}

// tokenize splits a query string into tokens, respecting double-quoted values.
// For example: `dir:"my project" refactor` -> ["dir:my project", "refactor"]
func tokenize(s string) []string {
	var tokens []string
	var current strings.Builder
	inQuote := false

	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch == '"':
			inQuote = !inQuote
			// Don't include the quote character itself.
		case ch == ' ' && !inQuote:
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		default:
			current.WriteByte(ch)
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

// splitPrefix checks if a token is a key:value pair.
// Returns the key, value, and true if it is, or "", "", false otherwise.
func splitPrefix(token string) (key, value string, ok bool) {
	idx := strings.IndexByte(token, ':')
	if idx <= 0 || idx == len(token)-1 {
		return "", "", false
	}
	key = strings.ToLower(token[:idx])
	value = token[idx+1:]
	return key, value, true
}

// FilterSessions applies the parsed query to a set of sessions.
// It applies prefix filters first (dir, agent), then fuzzy-matches the
// remaining text against SearchText.
func FilterSessions(sessions []provider.Session, pq ParsedQuery) []provider.Session {
	result := sessions

	// Apply dir: filter via fuzzy matching on Directory field.
	if pq.Dir != "" {
		result = fuzzyFilterField(result, pq.Dir, func(s provider.Session) string {
			return normalizePath(s.Directory)
		})
	}

	// Apply agent: filter via fuzzy matching on Agent field.
	if pq.Agent != "" {
		result = fuzzyFilterField(result, pq.Agent, func(s provider.Session) string {
			return s.Agent
		})
	}

	// Apply freeform text filter via fuzzy matching on SearchText.
	if pq.Text != "" {
		source := sessionSource(result)
		matches := fuzzy.FindFrom(pq.Text, source)
		filtered := make([]provider.Session, len(matches))
		for i, match := range matches {
			filtered[i] = result[match.Index]
		}
		result = filtered
	}

	return result
}

// fieldSource adapts a session slice for fuzzy matching against a specific field.
type fieldSource struct {
	sessions []provider.Session
	field    func(provider.Session) string
}

func (f fieldSource) String(i int) string { return f.field(f.sessions[i]) }
func (f fieldSource) Len() int            { return len(f.sessions) }

// fuzzyFilterField returns sessions whose field value fuzzy-matches the query.
func fuzzyFilterField(sessions []provider.Session, query string, field func(provider.Session) string) []provider.Session {
	source := fieldSource{sessions: sessions, field: field}
	matches := fuzzy.FindFrom(query, source)
	result := make([]provider.Session, len(matches))
	for i, match := range matches {
		result[i] = sessions[match.Index]
	}
	return result
}

// normalizePath cleans a path for comparison: expands ~ to $HOME, applies
// filepath.Clean. Does not resolve symlinks.
func normalizePath(path string) string {
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "~/") || path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, path[1:])
		}
	}
	return filepath.Clean(path)
}

// BuildPrefixQuery constructs a query string from structured prefix values.
// Used by CLI flags (--dir, --cwd, --agent) to set the initial search query.
// Any existing positional query text is appended after the prefixes.
// A trailing space is added when prefixes are present but no text follows,
// so the cursor is ready for the user to start typing additional terms.
func BuildPrefixQuery(dir, agent, text string) string {
	var parts []string
	if dir != "" {
		// Quote the value if it contains spaces.
		if strings.Contains(dir, " ") {
			parts = append(parts, fmt.Sprintf(`dir:"%s"`, dir))
		} else {
			parts = append(parts, "dir:"+dir)
		}
	}
	if agent != "" {
		if strings.Contains(agent, " ") {
			parts = append(parts, fmt.Sprintf(`agent:"%s"`, agent))
		} else {
			parts = append(parts, "agent:"+agent)
		}
	}
	if text != "" {
		parts = append(parts, text)
	}
	result := strings.Join(parts, " ")
	// Add trailing space so the cursor is positioned for typing after the prefixes.
	if len(parts) > 0 && text == "" {
		result += " "
	}
	return result
}

// ResolveDir resolves a --dir flag value to an absolute path when it looks
// like a filesystem path. Values starting with /, ~, or . are treated as paths
// and resolved to absolute form. Bare words like "sesh" are left as-is for
// fuzzy matching against directory names.
func ResolveDir(dir string) (string, error) {
	if dir == "" {
		return "", nil
	}
	// Expand ~.
	if strings.HasPrefix(dir, "~/") || dir == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			dir = filepath.Join(home, dir[1:])
		}
	}
	// Only resolve to absolute if it looks like a path (starts with /, ., or
	// was just expanded from ~). Bare words like "sesh" stay as-is for fuzzy
	// matching against directory components.
	if strings.HasPrefix(dir, "/") || strings.HasPrefix(dir, ".") {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return "", err
		}
		dir = abs
	}
	return filepath.Clean(dir), nil
}

// GitRoot returns the root directory of the git repository containing the
// current working directory. Returns an error if not inside a git repo.
func GitRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("not inside a git repository")
	}
	return strings.TrimSpace(string(out)), nil
}
