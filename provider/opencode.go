package provider

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// OpenCode reads sessions from the OpenCode SQLite database.
type OpenCode struct {
	dbPath        string
	resumeCommand string // override for resume command template
}

// OpenCodeOption configures the OpenCode provider.
type OpenCodeOption func(*OpenCode)

// WithOpenCodeResumeCommand overrides the default resume command template.
// Use {{ID}} as a placeholder for the session ID.
func WithOpenCodeResumeCommand(cmd string) OpenCodeOption {
	return func(o *OpenCode) {
		o.resumeCommand = cmd
	}
}

func NewOpenCode(opts ...OpenCodeOption) *OpenCode {
	home, _ := os.UserHomeDir()
	dbPath := filepath.Join(home, ".local", "share", "opencode", "opencode.db")
	if env := os.Getenv("OPENCODE_DB"); env != "" {
		dbPath = env
	}
	o := &OpenCode{dbPath: dbPath}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

func (o *OpenCode) Name() string { return "opencode" }

func (o *OpenCode) ListSessions(ctx context.Context) ([]Session, error) {
	if _, err := os.Stat(o.dbPath); os.IsNotExist(err) {
		return nil, nil
	}

	dsn := fmt.Sprintf("file:%s?mode=ro&_pragma=journal_mode(WAL)&_pragma=busy_timeout(3000)", o.dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open opencode db: %w", err)
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, `
		SELECT s.id, s.title, s.slug, s.directory, s.time_created, s.time_updated
		FROM session s
		WHERE s.time_archived IS NULL
		ORDER BY s.time_updated DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query sessions: %w", err)
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var (
			id, title, slug, directory string
			created, updated           int64
		)
		if err := rows.Scan(&id, &title, &slug, &directory, &created, &updated); err != nil {
			continue
		}
		sessions = append(sessions, Session{
			Agent:      "opencode",
			ID:         id,
			Title:      title,
			Slug:       slug,
			Created:    time.UnixMilli(created),
			LastUsed:   time.UnixMilli(updated),
			Directory:  directory,
			SearchText: strings.Join([]string{title, slug, directory}, " "),
		})
	}

	// Enrich search text with first user prompts
	o.enrichSearchText(ctx, db, sessions)

	return sessions, nil
}

func (o *OpenCode) enrichSearchText(ctx context.Context, db *sql.DB, sessions []Session) {
	stmt, err := db.PrepareContext(ctx, `
		SELECT json_extract(p.data, '$.text')
		FROM part p
		JOIN message m ON p.message_id = m.id
		WHERE m.session_id = ?
		  AND json_extract(m.data, '$.role') = 'user'
		  AND json_extract(p.data, '$.type') = 'text'
		ORDER BY m.time_created ASC, p.time_created ASC
		LIMIT 3
	`)
	if err != nil {
		return
	}
	defer stmt.Close()

	for i := range sessions {
		rows, err := stmt.QueryContext(ctx, sessions[i].ID)
		if err != nil {
			continue
		}
		var parts []string
		for rows.Next() {
			var text sql.NullString
			if err := rows.Scan(&text); err == nil && text.Valid && text.String != "" {
				parts = append(parts, text.String)
			}
		}
		rows.Close()
		if len(parts) > 0 {
			sessions[i].SearchText += " " + strings.Join(parts, " ")
		}
	}
}

func (o *OpenCode) ResumeCommand(session Session) string {
	var cmd string
	if o.resumeCommand != "" {
		cmd = strings.ReplaceAll(o.resumeCommand, "{{ID}}", session.ID)
	} else {
		cmd = fmt.Sprintf("opencode --session %s", ShellQuote(session.ID))
	}
	if session.Directory != "" {
		return fmt.Sprintf("cd %s && %s", ShellQuote(session.Directory), cmd)
	}
	return cmd
}

// Ensure OpenCode implements Provider at compile time.
var _ Provider = (*OpenCode)(nil)
