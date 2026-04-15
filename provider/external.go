package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ExternalConfig defines an external provider loaded from user config.
type ExternalConfig struct {
	Name          string   `json:"name"`
	ListCommand   []string `json:"list_command"`
	ResumeCommand string   `json:"resume_command"` // template: {{ID}}, {{DIR}}
	Env           []string `json:"-"`              // merged env for list_command; nil inherits parent
}

// ExternalSession is the JSON schema external providers must emit from their list command.
type ExternalSession struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Slug      string `json:"slug,omitempty"`
	Created   string `json:"created"`   // RFC 3339 or Unix ms as string
	LastUsed  string `json:"last_used"` // RFC 3339 or Unix ms as string
	Directory string `json:"directory,omitempty"`
	Text      string `json:"text,omitempty"` // additional searchable text
}

// External shells out to a user-defined command to list sessions.
type External struct {
	config    ExternalConfig
	textCache map[string]string // session ID -> text from list response
}

func NewExternal(cfg ExternalConfig) *External {
	return &External{config: cfg, textCache: make(map[string]string)}
}

func (e *External) Name() string { return e.config.Name }

func (e *External) ListSessions(ctx context.Context) ([]Session, error) {
	if len(e.config.ListCommand) == 0 {
		return nil, fmt.Errorf("no list_command configured for provider %q", e.config.Name)
	}

	cmd := exec.CommandContext(ctx, e.config.ListCommand[0], e.config.ListCommand[1:]...)
	cmd.Env = e.config.Env // nil inherits parent env
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("run list command for %q: %w", e.config.Name, err)
	}

	var raw []ExternalSession
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse output from %q: %w", e.config.Name, err)
	}

	var sessions []Session
	for i, r := range raw {
		if r.ID == "" {
			fmt.Fprintf(os.Stderr, "sesh: warning: %s: session %d has no id, skipping\n", e.config.Name, i)
			continue
		}
		if r.Title == "" {
			fmt.Fprintf(os.Stderr, "sesh: warning: %s: session %q has no title\n", e.config.Name, r.ID)
		}
		created := parseFlexTime(r.Created)
		lastUsed := parseFlexTime(r.LastUsed)
		if created.IsZero() {
			fmt.Fprintf(os.Stderr, "sesh: warning: %s: session %q has invalid or missing created timestamp\n", e.config.Name, r.ID)
		}
		if lastUsed.IsZero() {
			fmt.Fprintf(os.Stderr, "sesh: warning: %s: session %q has invalid or missing last_used timestamp\n", e.config.Name, r.ID)
		}

		searchParts := []string{r.Title, r.Slug, r.Directory, r.Text}
		if r.Text != "" {
			e.textCache[r.ID] = r.Text
		}
		sessions = append(sessions, Session{
			Agent:      e.config.Name,
			ID:         r.ID,
			Title:      r.Title,
			Slug:       r.Slug,
			Created:    created,
			LastUsed:   lastUsed,
			Directory:  r.Directory,
			SearchText: strings.Join(searchParts, " "),
		})
	}

	return sessions, nil
}

func (e *External) ResumeCommand(session Session) string {
	cmd := e.config.ResumeCommand
	cmd = strings.ReplaceAll(cmd, "{{ID}}", session.ID)
	cmd = strings.ReplaceAll(cmd, "{{DIR}}", session.Directory)
	// Add cd prefix if the template doesn't already handle directories.
	if !strings.Contains(e.config.ResumeCommand, "cd ") &&
		!strings.Contains(e.config.ResumeCommand, "Set-Location") &&
		!strings.Contains(e.config.ResumeCommand, "{{DIR}}") {
		return CdAndRun(session.Directory, cmd)
	}
	return cmd
}

// SessionText returns the text field from the list response, if available.
// External providers supply all their data via the list command, so there's
// no secondary fetch.
func (e *External) SessionText(_ context.Context, sessionID string) string {
	return e.textCache[sessionID]
}

// parseFlexTime parses RFC 3339 or Unix milliseconds.
func parseFlexTime(s string) time.Time {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	var ms int64
	if _, err := fmt.Sscanf(s, "%d", &ms); err == nil && ms > 1e12 {
		return time.UnixMilli(ms)
	}
	return time.Time{}
}

var _ Provider = (*External)(nil)
