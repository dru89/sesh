package provider

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Session represents a coding agent session with normalized metadata.
type Session struct {
	Agent      string    `json:"agent"`
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Slug       string    `json:"slug,omitempty"`
	Created    time.Time `json:"created"`
	LastUsed   time.Time `json:"last_used"`
	Directory  string    `json:"directory,omitempty"`
	SearchText string    `json:"-"`
}

// Provider discovers sessions for a specific coding agent.
type Provider interface {
	// Name returns the display name of the coding agent (e.g. "opencode", "claude").
	Name() string

	// ListSessions returns all available sessions.
	ListSessions(ctx context.Context) ([]Session, error)

	// ResumeCommand returns the shell command to resume a session.
	// The returned string is eval'd by the shell wrapper, so cd + exec patterns work.
	ResumeCommand(session Session) string
}

// DisplayTitle returns the best available display title for a session.
func (s Session) DisplayTitle() string {
	if s.Title != "" {
		return s.Title
	}
	if s.Slug != "" {
		return s.Slug
	}
	return s.ID
}

// RelativeTime formats a time as a human-readable relative string.
func RelativeTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		return fmt.Sprintf("%dm ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		return fmt.Sprintf("%dh ago", h)
	case d < 30*24*time.Hour:
		days := int(d.Hours() / 24)
		return fmt.Sprintf("%dd ago", days)
	default:
		return t.Format("Jan 2")
	}
}

// ShellQuote quotes a string for safe use in shell commands.
func ShellQuote(s string) string {
	if s == "" {
		return "''"
	}
	safe := true
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '-' || c == '_' ||
			c == '.' || c == '/' || c == ':' || c == '~') {
			safe = false
			break
		}
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
