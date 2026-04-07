# CLAUDE.md

## What this project is

`sesh` is a CLI tool that aggregates coding agent sessions (OpenCode, Claude Code, and external agents) into a unified fuzzy-search picker. Select a session and it resumes the agent in the right directory.

## Project structure

```
sesh/
├── cmd/sesh/main.go         # CLI entry point, flag parsing, config loading, provider wiring
├── provider/
│   ├── provider.go           # Session type, Provider interface, helpers (RelativeTime, ShellQuote)
│   ├── opencode.go           # OpenCode adapter — reads SQLite at ~/.local/share/opencode/opencode.db
│   ├── claude.go             # Claude Code adapter — reads ~/.claude/history.jsonl + project transcripts
│   └── external.go           # External provider — shells out to user-defined command, parses JSON
├── tui/
│   └── tui.go                # Bubbletea fzf-style picker, renders to stderr, result to stdout
├── shell/
│   ├── sesh.bash             # Bash wrapper function
│   └── sesh.zsh              # Zsh wrapper function
├── go.mod
└── go.sum
```

## Architecture

### Provider interface

Every session source implements `provider.Provider`:

```go
type Provider interface {
    Name() string
    ListSessions(ctx context.Context) ([]Session, error)
    ResumeCommand(session Session) string
}
```

Built-in providers (OpenCode, Claude) read agent data directly. External providers shell out to an executable that returns JSON. All providers return the same `Session` struct.

### Resume flow

The binary outputs a shell command string to stdout (`cd /path && agent --resume ID`). A shell wrapper function evals it so the `cd` takes effect in the user's current shell. The TUI renders to stderr to keep stdout clean for capture.

### Config

`~/.config/sesh/config.json` (optional). Providers listed in config under built-in names (`opencode`, `claude`) override resume commands or disable the provider. Any other name is treated as an external provider and requires `list_command`.

The `providerConfig` struct in `cmd/sesh/main.go` handles both cases:
- `resume_command`: template string with `{{ID}}` and `{{DIR}}` placeholders
- `enabled`: boolean, defaults to true
- `list_command`: array of strings (external providers only)

## Data sources

### OpenCode

SQLite database at `~/.local/share/opencode/opencode.db`. Key tables:
- `session`: id, title, slug, directory, time_created, time_updated, time_archived
- `message`: id, session_id, data (JSON with role)
- `part`: id, message_id, session_id, data (JSON with type and text)

Timestamps are Unix milliseconds. Archived sessions (time_archived IS NOT NULL) are excluded. The adapter also queries the first 3 text parts from user messages to enrich the fuzzy search corpus.

Resume: `opencode --session <id>` (binary at `~/.opencode/bin/opencode`)

### Claude Code

`~/.claude/history.jsonl` — one JSON line per user prompt, grouped by sessionId. Fields: display, timestamp (Unix ms), project (working directory), sessionId (UUID).

Session transcripts live in `~/.claude/projects/<encoded-path>/<sessionId>.jsonl`. The path encoding replaces `/` with `-`. The `slug` field appears on messages after the first exchange.

Resume: `claude --resume <id>` (binary at `~/.local/bin/claude`)

### External providers

Any executable that outputs `[{"id", "title", "created", "last_used", ...}]` to stdout. Timestamps accept RFC 3339 or Unix milliseconds as strings. See README.md for the full schema.

## Key design decisions

- **TUI renders to stderr.** The binary's stdout is reserved for the shell command output. Uses `tea.WithOutput(os.Stderr)` and `tea.WithAltScreen()`.
- **Fuzzy search via sahilm/fuzzy.** Each session has a `SearchText` field (not exported to JSON) concatenating title, slug, directory, and first user prompts.
- **Pure Go SQLite.** Uses `modernc.org/sqlite` to avoid CGO. Opens the database read-only with WAL mode.
- **Shell quoting.** `provider.ShellQuote()` handles paths with spaces and special characters (single-quote wrapping with escaped internal quotes).
- **Provider options pattern.** Built-in providers accept functional options (e.g., `WithOpenCodeResumeCommand()`) so config overrides are injected at construction time without the provider needing to know about the config system.

## Build and test

```bash
go build ./cmd/sesh/                    # build
go build -o ~/go/bin/sesh ./cmd/sesh/   # build and install
sesh --json                             # verify both providers return data
sesh --json --agent opencode            # test single-agent filter
```

## Planned features (not yet implemented)

These were part of the original design but deferred from v1:

1. **Haiku-generated session summaries.** Use a fast model (Haiku) to produce a one-line summary of each session's content, cached alongside the session data. The summary becomes searchable text, improving discovery for sessions whose titles are auto-generated or unhelpful (e.g., "[Pasted text #1 +195 lines]").

2. **AI fallback search.** When fuzzy search returns no results, fall back to a small/fast model to re-rank sessions by semantic relevance to the query. The interface should be designed so this is a pluggable search strategy, not hardcoded.

3. **Raycast extension.** The `--json` output provides the data contract. A Raycast script extension would call `sesh --json`, present results in the Raycast UI, and on selection open a terminal window running the resume command.

4. **Session ID in TUI.** The picker currently shows agent, title, and time. The original spec also called for displaying a truncated session ID.

## Dependencies

| Package | Purpose |
|---|---|
| `github.com/charmbracelet/bubbletea` | TUI framework |
| `github.com/charmbracelet/lipgloss` | Terminal styling |
| `github.com/sahilm/fuzzy` | Fuzzy string matching |
| `modernc.org/sqlite` | Pure Go SQLite driver |
