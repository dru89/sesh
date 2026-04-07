package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// ExternalConfig defines an external provider loaded from user config.
type ExternalConfig struct {
	Name          string   `json:"name"`
	ListCommand   []string `json:"list_command"`
	ResumeCommand string   `json:"resume_command"` // template: {{ID}}, {{DIR}}
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
	config ExternalConfig
}

func NewExternal(cfg ExternalConfig) *External {
	return &External{config: cfg}
}

func (e *External) Name() string { return e.config.Name }

func (e *External) ListSessions(ctx context.Context) ([]Session, error) {
	if len(e.config.ListCommand) == 0 {
		return nil, fmt.Errorf("no list_command configured for provider %q", e.config.Name)
	}

	cmd := exec.CommandContext(ctx, e.config.ListCommand[0], e.config.ListCommand[1:]...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("run list command for %q: %w", e.config.Name, err)
	}

	var raw []ExternalSession
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse output from %q: %w", e.config.Name, err)
	}

	var sessions []Session
	for _, r := range raw {
		searchParts := []string{r.Title, r.Slug, r.Directory, r.Text}
		sessions = append(sessions, Session{
			Agent:      e.config.Name,
			ID:         r.ID,
			Title:      r.Title,
			Slug:       r.Slug,
			Created:    parseFlexTime(r.Created),
			LastUsed:   parseFlexTime(r.LastUsed),
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
	if session.Directory != "" && !strings.Contains(e.config.ResumeCommand, "cd ") && !strings.Contains(e.config.ResumeCommand, "{{DIR}}") {
		return fmt.Sprintf("cd %s && %s", ShellQuote(session.Directory), cmd)
	}
	return cmd
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
