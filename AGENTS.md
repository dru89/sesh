# Setting up sesh

> **For humans:** Give this file to your coding agent (Claude Code, OpenCode, Cursor, etc.) and tell it which agent you want to configure as a sesh provider. It has everything the agent needs to set up the integration. You can also ask it to configure the LLM commands for summaries if you have `llm`, `claude`, or another CLI tool available.

---

## What is sesh?

sesh is a CLI tool that aggregates sessions from multiple coding agents into a single fuzzy-search picker. You type `sesh`, search across all your agents' sessions, select one, and it resumes that session in the right directory.

## Install

```bash
# Homebrew (macOS/Linux)
brew install dru89/tap/sesh

# Or with Go
go install github.com/dru89/sesh/cmd/sesh@latest
```

Or download a prebuilt binary from [GitHub Releases](https://github.com/dru89/sesh/releases).

Set up the shell wrapper by adding this to the user's shell rc file:

```bash
# bash: add to ~/.bashrc
eval "$(sesh init bash)"

# zsh: add to ~/.zshrc
eval "$(sesh init zsh)"

# fish: add to ~/.config/fish/config.fish
sesh init fish | source
```

```powershell
# PowerShell: add to $PROFILE
sesh init powershell | Out-String | Invoke-Expression
```

## Configuration file

All configuration lives in `~/.config/sesh/config.json`. Create it if it doesn't exist. The file has three sections: providers, and optionally LLM commands for AI features.

## Adding a new provider

sesh has built-in support for OpenCode and Claude Code (no configuration needed). For any other agent, you need two things:

### 1. A session list script

Write an executable script that outputs a JSON array to stdout. Name it something like `<agent>-sesh` and put it on the user's PATH.

The script must output this JSON schema:

```json
[
  {
    "id": "session-identifier",
    "title": "human-readable title or first prompt",
    "slug": "optional-short-name",
    "created": "2026-01-15T10:30:00Z",
    "last_used": "2026-01-15T11:45:00Z",
    "directory": "/absolute/path/to/working/directory",
    "text": "optional searchable text from first few user prompts"
  }
]
```

Field requirements:
- `id` (required): The session identifier used to resume it
- `title` (required): Display name, truncated to ~120 characters
- `created` (required): RFC 3339 timestamp or Unix milliseconds as a string
- `last_used` (required): RFC 3339 timestamp or Unix milliseconds as a string
- `slug` (optional): Short human-readable name
- `directory` (optional): Absolute path to the working directory
- `text` (optional): Extra searchable text (concatenated user prompts work well)

Rules for the script:
- Exit 0 on success, non-zero on failure
- Output `[]` if no sessions exist
- Only JSON goes to stdout; warnings and errors go to stderr
- Exclude subagent/child sessions — only return top-level sessions a user would resume directly. Many agents spawn background sessions for subtasks (e.g., explore or code-review subagents). These shouldn't appear in the picker.

### 2. A config entry

Add the provider to `~/.config/sesh/config.json`:

```json
{
  "providers": {
    "<agent-name>": {
      "list_command": ["<agent>-sesh"],
      "resume_command": "<agent> --resume {{ID}}"
    }
  }
}
```

`{{ID}}` is replaced with the session ID. `{{DIR}}` is replaced with the session's working directory if you need it in the command.

If the user has a wrapper script around a built-in agent (e.g., `ca opencode` instead of `opencode`), override just the resume command:

```json
{
  "providers": {
    "opencode": {
      "resume_command": "ca opencode -s {{ID}}"
    }
  }
}
```

## Configuring LLM commands

sesh uses LLMs for three optional features: title generation (`sesh index`), natural language queries (`sesh ask`), and recaps (`sesh recap`). Each can use a different model.

The LLM command receives input on stdin and must write its response to stdout. Any CLI tool works: `llm`, `claude -p`, a script that calls a local model, etc.

### Minimal (one model for everything)

```json
{
  "index": {
    "command": ["llm", "-m", "haiku"]
  }
}
```

### Split fast and heavy models

```json
{
  "index": {
    "command": ["llm", "-m", "haiku"]
  },
  "ask": {
    "command": ["llm", "-m", "sonnet"]
  },
  "recap": {
    "command": ["llm", "-m", "sonnet"]
  }
}
```

Each subcommand falls back to the others if its own command isn't set, so configuring just `index` is enough for everything to work. The fallback order prefers models of similar weight: `ask` and `recap` try each other before falling back to `index`.

### Custom prompts

Each section accepts optional `system_prompt` and `prompt` fields. The `system_prompt` provides role framing (tells the model what it is), while `prompt` is the task instruction (tells the model what to do). Both override their respective defaults:

```json
{
  "index": {
    "command": ["llm", "-m", "haiku"],
    "system_prompt": "You are a session indexer for coding transcripts.",
    "prompt": "Describe this coding session in one sentence, under 15 words. Output only the description."
  }
}
```

The prompt structure piped to the LLM on stdin is: `[system_prompt] --- [transcript] --- [prompt]`. If `prompt` contains `{{TRANSCRIPT}}`, the transcript is inserted at that position instead of between separators.

## Full config example

```json
{
  "index": {
    "command": ["llm", "-m", "haiku"]
  },
  "ask": {
    "command": ["llm", "-m", "sonnet"],
    "filter_command": ["llm", "-m", "haiku"]
  },
  "recap": {
    "command": ["llm", "-m", "sonnet"]
  },
  "providers": {
    "opencode": {
      "resume_command": "ca opencode -s {{ID}}"
    },
    "claude": {
      "resume_command": "ca -r {{ID}}"
    },
    "omp": {
      "list_command": ["omp-sesh"],
      "resume_command": "omp --resume {{ID}}"
    }
  }
}
```

## After setup

```bash
sesh                          # open the fuzzy picker (tab for detail pane)
sesh --since 1d --repo        # picker filtered to today's sessions in this repo
sesh list                     # non-interactive session list
sesh list --since 3d -n 5     # last 5 sessions from the past 3 days
sesh show <session-id>        # session details (partial ID works)
sesh stats                    # session statistics
sesh index                    # generate titles for all sessions (run once)
sesh ask "what did I do?"     # natural language query
sesh recap --days 7           # weekly recap
sesh --json                   # JSON output for scripts/Raycast
sesh --json --repo --since 1d # JSON output filtered by repo and time
```

---

## Development

The sections below are for agents and contributors working on the sesh codebase itself.

### Development guidelines

When making changes to sesh, always:

1. **Update tests.** New functions should have test coverage. Run `go test ./...` before committing.
2. **Update README.md** if user-facing behavior changes (new commands, flags, config options).
3. **Update AGENTS.md** if internal architecture changes (new packages, data flows, design decisions) or if the setup process or config schema changes.
4. **Update schema.json** if config fields are added or modified.
5. **Run the CI check locally** (`go test ./... && go build ./cmd/sesh/`) before pushing.

### Commit conventions

Use [Conventional Commits](https://www.conventionalcommits.org/) for all commit messages:

```
feat: add --repo flag to sesh ask
fix: prevent hang when external provider script blocks
refactor: extract resolveDirFlags helper
docs: update README with new Go version requirement
test: add checksum verification tests
chore: update CI to Go 1.25
```

Common types: `feat`, `fix`, `refactor`, `docs`, `test`, `chore`, `perf`, `ci`. Include a scope in parentheses when it clarifies the change: `feat(ask):`, `fix(update):`, `ci(release):`.

Breaking changes get a `!` suffix: `feat!: change config format`.

### Release process

Releases are triggered by pushing a version tag. The process:

1. **Update CHANGELOG.md** — Move items from `[Unreleased]` to a new version section. Add the date. Update the comparison links at the bottom of the file.
2. **Commit** — `git commit -am "chore: prepare release v0.13.0"`
3. **Tag and push** — `git tag v0.13.0 && git push origin main --tags`
4. GoReleaser builds cross-platform binaries, publishes to GitHub Releases, and updates the Homebrew cask.

When updating the changelog at release time, review `git log` since the last tag and write concise, user-facing descriptions. Group related changes. The auto-generated GoReleaser changelog covers commit-level detail on the GitHub release page; the CHANGELOG.md is the curated version.

### Project structure

```
sesh/
├── cmd/sesh/main.go         # CLI entry point, all subcommands, config, provider wiring
├── cmd/sesh/main_test.go    # Tests: config resolution, findSession, computeStats, parseDateish, init, aiFilterSessions
├── provider/
│   ├── provider.go           # Session type, Provider interface, helpers (Q, CdAndRun, RelativeTime)
│   ├── provider_test.go      # Tests: ShellQuote, ShellQuotePowerShell, CdAndRun, RelativeTime, DisplayTitle
│   ├── session_test.go       # Tests: OpenCode SQLite, Claude JSONL, External provider
│   ├── opencode.go           # OpenCode adapter — reads SQLite at ~/.local/share/opencode/opencode.db
│   ├── claude.go             # Claude Code adapter — reads ~/.claude/history.jsonl + project transcripts
│   └── external.go           # External provider — shells out to user-defined command, parses JSON
├── summary/
│   ├── cache.go              # JSON file cache at ~/.cache/sesh/summaries.json
│   ├── cache_test.go         # Tests: Get/Put, staleness, NeedsSummary, Save/load
│   ├── generate.go           # LLM command execution, summary generation, RunLLM shared function
│   └── generate_test.go      # Tests: RunLLM success/failure/timeout, Generate, GenerateBatch
├── update/
│   ├── update.go             # GitHub release checker, binary downloader, self-updater, version cache
│   └── update_test.go        # Tests: IsNewer, compareSemver, AssetName, FindAsset, cache roundtrip/expiry
├── tui/
│   └── tui.go                # Bubbletea fzf-style picker with AI fallback search + detail pane
├── raycast/                   # Raycast extension (TypeScript)
│   ├── src/search-sessions.tsx  # Main fuzzy search command with AI fallback on empty view
│   ├── src/ai-search-sessions.tsx # Dedicated AI search command with debounced LLM queries
│   ├── src/components.tsx       # Shared list item rendering, actions, detail markdown
│   ├── src/sesh.ts              # loadSessions, aiSearchSessions, relativeTime helpers
│   ├── src/terminal.ts          # Terminal launch logic (Terminal.app, iTerm2, Ghostty, Warp, custom)
│   └── src/types.ts
├── shell/
│   ├── sesh.bash             # Bash wrapper function
│   ├── sesh.zsh              # Zsh wrapper function
│   ├── sesh.ps1              # PowerShell wrapper function
│   └── (generated by sesh init for fish)
├── schema.json               # JSON Schema for config validation
├── .goreleaser.yaml           # Cross-platform release builds + Homebrew tap
├── .github/workflows/
│   ├── ci.yaml               # Test on push/PR
│   └── release.yaml          # Build + release on tag (requires CI pass)
├── go.mod
└── go.sum
```

### Architecture

#### Provider interface

Every session source implements `provider.Provider`:

```go
type Provider interface {
    Name() string
    ListSessions(ctx context.Context) ([]Session, error)
    ResumeCommand(session Session) string
}
```

Built-in providers (OpenCode, Claude) read agent data directly. External providers shell out to an executable that returns JSON. All providers return the same `Session` struct.

#### Resume flow

The binary outputs a shell command string to stdout (`cd /path && agent --resume ID`). A shell wrapper function evals it so the `cd` takes effect in the user's current shell. The TUI renders to stderr to keep stdout clean for capture.

#### Config

`~/.config/sesh/config.json` (optional). Three categories of config:

**Providers** (`providers`): Listed under built-in names (`opencode`, `claude`) to override resume commands or disable. Any other name is an external provider requiring `list_command`.

**LLM commands** (`index`, `ask`, `recap`): Each subcommand has its own `command`, `system_prompt`, `prompt`, and `env` fields. `ask` also has `filter_command` for the classification pass. Each subcommand falls back through the others via a priority chain so you only need to configure one. The `system_prompt` field provides role-framing context (preventing the model from engaging with transcript content), while `prompt` is the task instruction. Both have sensible defaults with anti-response guardrails. Custom prompts can use `{{TRANSCRIPT}}` to control where session data is inserted.

**Environment** (`env`): Top-level `env` map applies to all LLM commands. Per-command `env` overrides specific keys. Merge order: process env < top-level env < per-command env. Built by `buildEnv()` which starts from `os.Environ()` and overlays config values. Critical for Raycast/non-shell environments where AWS credentials aren't in the process environment.

Fallback chains (flat, no recursion):
- `index`: index -> recap -> ask -> ask.filter_command
- `ask` (prose): ask -> recap -> index
- `ask` (filter): ask.filter_command -> index -> ask -> recap
- `recap`: recap -> ask -> index

The `config` struct in main.go has methods `indexCommand()`, `askCommand()`, `askFilterCommand()`, `recapCommand()` that walk these chains via `resolveCommand()`. Prompt resolution methods (`indexPrompt()`, `indexSystemPrompt()`, `recapPrompt()`, `recapSystemPrompt()`, `askPrompt()`, `askSystemPrompt()`) return the config value or empty string. `summaryConfig()` builds a `summary.Config` from the resolved index command/prompt/system_prompt for the generator. Recap and ask prompts are assembled in their respective `run` functions using `summary.BuildPrompt()`, which handles the system/transcript/task layering and `{{TRANSCRIPT}}` expansion.

### Data sources

#### OpenCode

SQLite database at `~/.local/share/opencode/opencode.db`. Key tables:
- `session`: id, title, slug, directory, time_created, time_updated, time_archived
- `message`: id, session_id, data (JSON with role)
- `part`: id, message_id, session_id, data (JSON with type and text)

Timestamps are Unix milliseconds. Archived sessions (time_archived IS NOT NULL) are excluded. The adapter also queries the first 3 text parts from user messages to enrich the fuzzy search corpus.

Resume: `opencode --session <id>` (binary at `~/.opencode/bin/opencode`)

#### Claude Code

`~/.claude/history.jsonl` — one JSON line per user prompt, grouped by sessionId. Fields: display, timestamp (Unix ms), project (working directory), sessionId (UUID).

Session transcripts live in `~/.claude/projects/<encoded-path>/<sessionId>.jsonl`. The path encoding replaces `/` with `-`. The `slug` field appears on messages after the first exchange.

Resume: `claude --resume <id>` (binary at `~/.local/bin/claude`)

#### External providers

Any executable that outputs `[{"id", "title", "created", "last_used", ...}]` to stdout. Timestamps accept RFC 3339 or Unix milliseconds as strings. See the provider setup section above for the full schema.

### Key design decisions

- **TUI renders to stderr.** The binary's stdout is reserved for the shell command output. Uses `tea.WithOutput(os.Stderr)` and `tea.WithAltScreen()`.
- **Fuzzy search via sahilm/fuzzy.** Each session has a `SearchText` field (not exported to JSON) concatenating title, slug, directory, first user prompts, and cached summary.
- **Pure Go SQLite.** Uses `modernc.org/sqlite` to avoid CGO. Opens the database read-only with WAL mode.
- **Shell quoting.** `provider.ShellQuote()` handles paths with spaces and special characters (single-quote wrapping with escaped internal quotes).
- **Provider options pattern.** Built-in providers accept functional options (e.g., `WithOpenCodeResumeCommand()`) so config overrides are injected at construction time without the provider needing to know about the config system.
- **Summary generation is pluggable.** No built-in LLM client. The user configures any command that reads stdin and writes a summary to stdout (e.g., `llm`, `claude -p`, a local model script). This avoids credential management complexity in sesh itself.
- **Summaries replace display titles.** `Session.DisplayTitle()` prefers `Summary` > `Title` > `Slug` > `ID`. This means sessions with ugly auto-generated titles (common in external providers) get clean display names once summarized.
- **Providers collect sessions concurrently.** `collectSessions()` launches goroutines per provider and merges results. External provider failures log a warning and don't block other providers.

### Summary system

#### Architecture

- `summary/cache.go` — JSON-file-backed cache at `~/.cache/sesh/summaries.json`. Keyed by session ID. Staleness check: `last_used` has changed AND summary is >1 hour old (prevents re-summarizing active sessions on every run).
- `summary/generate.go` — Shells out to user-configured command. Session text (user prompts) goes on stdin, summary comes out on stdout. 30-second per-summary timeout. Supports batch generation with progress callback. All LLM prompts are assembled by `BuildPrompt()`, which layers a system prompt (role framing), transcript, and task prompt, with support for `{{TRANSCRIPT}}` template expansion in custom prompts.
- `cmd/sesh/main.go` — Wires it together. `sesh index` for bulk generation. Normal `sesh` runs lazy background generation (up to 10 sessions) in a goroutine during the TUI picker.

#### Provider.SessionText()

Each provider implements `SessionText(ctx, sessionID) string` to supply raw user prompt text for summary generation:
- **OpenCode:** Queries first 10 user text parts from the SQLite database.
- **Claude Code:** Reads the session transcript JSONL and extracts user message content strings.
- **External:** Returns the `text` field from the list command response (cached in memory from the initial list call).

### Build and test

```bash
go build ./cmd/sesh/                    # build
go build -o ~/go/bin/sesh ./cmd/sesh/   # build and install
go test ./... -v                        # run all tests
sesh --json                             # verify both providers return data
sesh list -n 10                         # non-interactive list
sesh show <partial-id>                  # session detail
sesh stats                              # cross-agent statistics
sesh index                              # test title generation (needs index.command configured)
sesh recap --days 7                     # test recap (needs recap or index command)
sesh ask "what did I work on?"          # test ask (needs ask or index command)
```

### TUI detail pane

Press Tab in the picker to toggle a split view: the list narrows to ~40% and a detail pane shows session metadata + first messages on the right. The detail pane uses `SessionTextFunc` (passed via `PickOptions`) to fetch session text from the appropriate provider. In list-only mode, the directory is shown inline below the selected item. In detail mode, it moves to the pane.

### Show subcommand

`sesh show <id>` accepts a full or partial session ID. Uses `findSession()` which checks exact match first, then unique prefix. If multiple sessions match a prefix, it lists the ambiguous candidates and exits. Prints metadata (agent, ID, slug, title, summary, directory, timestamps, resume command) and the first ~1000 chars of user messages via `SessionText()`. `sesh show --json <id>` outputs the full session as JSON including the session text (used by the Raycast detail view).

### Stats subcommand

`sesh stats` uses `computeStats()` to count sessions by agent, time bucket (today/week/month), directory, and summary coverage. Shows top 5 directories and 5 most recent sessions.

### AI fallback search

When fuzzy search returns zero results in the TUI (with 3+ characters typed), the picker fires an async LLM call via `summary.RunLLM()`. Uses the resolved `askFilterCommand()` — prefers `ask.filter_command`, falls back through `index`, `ask`, `recap`.

The fallback is wired through `tui.FallbackSearchFunc`, a callback passed via `tui.PickOptions`. It runs in a bubbletea `tea.Cmd` goroutine. Results arrive as a `fallbackResultMsg`. The TUI shows "Searching with AI..." while waiting. If the call fails, the picker stays on the empty state.

`buildFallbackSearch()` in main.go takes a `[]string` command and constructs the closure.

### Ask subcommand (two-pass)

`sesh ask` uses two LLM calls:

1. **Pass 1 (filter):** Sends the numbered session list + question to `askFilterCommand()`. Asks the LLM to return relevant session numbers. Classification task — fast model.
2. **Pass 2 (generate):** Sends only the filtered sessions + question to `askCommand()`. Asks for a prose answer. Generation task — heavy model.

This split keeps the heavy model's context small (5-20 sessions) regardless of total session count.

### Recap subcommand

`sesh recap` collects sessions in a time range, builds a formatted list with their summaries/titles, and sends it to `recapCommand()` with a recap prompt. Output goes to stdout as prose.

Time parsing (`parseDateish`) supports: ISO dates (`2026-04-01`), relative days (`3d`), day names (`monday`), and keywords (`today`, `yesterday`, `last week`). Default window is 7 days.

Uses `summary.RunLLM()` with a 60-second timeout (longer than the 30-second per-summary timeout since the recap prompt is larger).

### Cache warming

The main picker shows a hint to stderr when >20 sessions lack summaries and an LLM command is configured: `sesh: N sessions without summaries. Run 'sesh index' to generate them.` This only appears once the user has set up an LLM command but hasn't run the initial index yet.

### Raycast extension

Lives in `raycast/`. TypeScript extension calling `sesh --json`. Features:
- Session list with agent-colored icons, fuzzy search over title/agent/directory/summary
- Detail pane (Cmd+D) showing session metadata in markdown
- AI Search Sessions command — dedicated LLM-powered search with 600ms debounce
- Empty view AI fallback — when fuzzy returns nothing, Enter triggers AI search
- Cmd+Shift+A action to AI-search from any session in the list
- Terminal launch preference: Terminal.app, iTerm2, Ghostty, Warp, or custom command
- Shared components in `components.tsx` (SessionActions, sessionListItemProps, displayTitle)
- Actions: resume, copy command/ID/dir, open in Finder/VS Code

### --ai-search flag

`sesh --json --ai-search "query"` runs the LLM filter pass and outputs ranked results as JSON. Uses `aiFilterSessions()`, the same shared function used by the TUI fallback, `buildFallbackSearch()`, and the `ask` subcommand's pass 1. This is the integration point for the Raycast AI search commands.

### Self-updater

The `update/` package handles version checking and binary replacement.

`sesh version` prints the compiled-in version (set via `-ldflags "-X main.version=..."` by GoReleaser).

`sesh update` checks GitHub releases, downloads the right archive for the current OS/arch, extracts the binary, and atomically replaces the running executable. Detects Homebrew installs (binary path contains `/Cellar/` or `/homebrew/`) and redirects to `brew upgrade sesh`.

Background version check runs on the main picker (non-blocking goroutine) and `sesh list` (cache-only, no network). Checks at most once per 24 hours via `~/.cache/sesh/version-check.json`. The update hint shows the correct command based on install method (`sesh update` vs `brew upgrade sesh`).

### Distribution

- **Homebrew:** `brew tap dru89/tap && brew install sesh`. GoReleaser auto-publishes the formula to `dru89/homebrew-tap` on each tagged release.
- **GitHub Releases:** Prebuilt binaries for macOS/Linux/Windows (amd64 + arm64). `sesh update` downloads from here.
- **Go install:** `go install github.com/dru89/sesh/cmd/sesh@latest`

### Dependencies

| Package | Purpose |
|---|---|
| `github.com/charmbracelet/bubbletea` | TUI framework |
| `github.com/charmbracelet/lipgloss` | Terminal styling |
| `github.com/sahilm/fuzzy` | Fuzzy string matching |
| `modernc.org/sqlite` | Pure Go SQLite driver |
