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

sesh has built-in support for OpenCode, Claude Code (both the CLI and the desktop app's Claude Code sessions), and Claude Cowork (no configuration needed). For any other agent, you need two things:

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
- Exclude sessions with no real user input — someone opening the agent just to run a command like `/login` or `/model` didn't create resumable work. The built-in Claude Code provider skips sessions whose only content is slash/shell commands; external providers should apply the same standard.

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
    "claude-code": {
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
6. **Open a PR instead of pushing directly to main.** CI runs tests on both Ubuntu and Windows and enforces `gofmt` formatting — all three checks must pass before merging. Branch protection is enabled on main.
7. **Use `internal/testhelper.WriteMockScript`** for any test that shells out to a script. It returns the right command array for the current OS — a `.sh` file on Unix, a PowerShell invocation on Windows. Never write a shell script inline in a test without it.

To enable the pre-commit formatting hook locally:

```bash
git config core.hooksPath .githooks
```

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

Releases use a PR-based flow. Creating a `release/vX.Y.Z` branch and opening a PR against main is the only manual step — merging triggers everything else automatically.

1. **Update CHANGELOG.md** — Move items from `[Unreleased]` to a new version section. Add the date. Update the comparison links at the bottom of the file.
2. **Commit, open a release PR, and enable auto-merge** — Branch name must match `release/vX.Y.Z` exactly.
   ```bash
   git checkout -b release/v1.2.0
   git commit -am "chore: prepare release v1.2.0"
   git push origin release/v1.2.0
   gh pr create --title "chore: release v1.2.0" --base main
   gh pr merge --auto --squash
   ```
   Auto-merge queues the PR to merge as soon as all required CI checks pass — no manual merge needed.
3. **Wait for CI.** Once checks pass, GitHub merges the PR automatically. The `release-pr.yaml` workflow fires: it extracts the version from the branch name, runs tests, creates and pushes the `vX.Y.Z` tag, then runs GoReleaser directly.
4. GoReleaser builds cross-platform binaries, publishes to GitHub Releases, and updates the Homebrew cask.

**Why not push the tag first?** GitHub Actions blocks workflows triggered by `GITHUB_TOKEN` from firing other workflows (loop prevention). The release-pr workflow runs GoReleaser itself rather than pushing a tag and waiting for `release.yaml` to fire. The existing `release.yaml` remains as an escape hatch for manually tagged releases.

**Updating CHANGELOG.md is the agent's job, not the automation's.** Before opening the release PR, review `git log` since the last tag and write concise, user-facing descriptions. Group related changes. The auto-generated GoReleaser changelog covers commit-level detail on the GitHub release page; CHANGELOG.md is the curated, human-readable version.

### Project structure

```
sesh/
├── cmd/sesh/main.go         # CLI entry point, all subcommands, config, provider wiring
├── cmd/sesh/main_test.go    # Tests: config resolution, findSession, computeStats, parseDateish, init, aiFilterSessions
├── cmd/sesh/setup.go        # sesh setup: CLI detection, config merge-write, canary verification, first-run hint
├── cmd/sesh/setup_test.go   # Tests: detection order, gap-filling, unknown-key preservation, canary, hint cap
├── provider/
│   ├── provider.go           # Session type, Provider interface, helpers (Q, CdAndRun, RelativeTime)
│   ├── provider_test.go      # Tests: ShellQuote, ShellQuotePowerShell, CdAndRun, RelativeTime, DisplayTitle
│   ├── session_test.go       # Tests: OpenCode SQLite, Claude JSONL, External provider
│   ├── opencode.go           # OpenCode adapter — reads SQLite at ~/.local/share/opencode/opencode.db
│   ├── claude.go             # Claude Code CLI adapter — reads ~/.claude/history.jsonl + project transcripts
│   ├── claude_desktop.go        # Claude Code Desktop adapter — reads desktop app session metadata + shared ~/.claude transcripts
│   ├── claude_cowork.go         # Claude Cowork adapter — reads Cowork session metadata + transcripts (desktop app userData dir)
│   └── external.go           # External provider — shells out to user-defined command, parses JSON
├── internal/paths/
│   ├── paths.go              # Shared cache-directory resolution — every caller must agree
│   └── paths_test.go         # Tests: XDG precedence, stability across directory creation
├── summary/
│   ├── cache.go              # JSON file cache at ~/.cache/sesh/summaries.json; CacheDir() delegates to internal/paths
│   ├── cache_test.go         # Tests: Get/Put, staleness, NeedsSummary, Save/load
│   ├── health.go             # Generation health record at ~/.cache/sesh/index-health.json
│   ├── health_test.go        # Tests: RecordRun increment/reset, threshold, roundtrip, CondenseError
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

Built-in providers (OpenCode, Claude, ClaudeCowork) read agent data directly. External providers shell out to an executable that returns JSON. All providers return the same `Session` struct.

#### Resume flow

The binary outputs a shell command string to stdout (`cd /path && agent --resume ID`). A shell wrapper function evals it so the `cd` takes effect in the user's current shell. The TUI renders to stderr to keep stdout clean for capture.

#### Config

`~/.config/sesh/config.json` (optional). Three categories of config:

**Providers** (`providers`): Listed under built-in names (`opencode`, `claude-code`, `claude-code-desktop`, `claude-cowork`; `claude` is a deprecated alias for `claude-code`) to override resume commands or disable. Any other name is an external provider requiring `list_command`.

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

#### Claude Code (CLI)

`~/.claude/history.jsonl` — one JSON line per user prompt, grouped by sessionId. Fields: display, timestamp (Unix ms), project (working directory), sessionId (UUID). Only prompts typed in a terminal land here — sessions started from the desktop app never do (see the `claude-code-desktop` provider below).

Session transcripts live in `~/.claude/projects/<encoded-path>/<sessionId>.jsonl`. The path encoding replaces `/` with `-`. The `slug` field appears on messages after the first exchange.

Command-only sessions are skipped: slash commands (`/login`, `/model`) and shell escapes (`!ls`) are logged to history like prompts, so a session whose entries are all commands is junk — unless it was started with an initial prompt argument (`claude "..."`), which never lands in history. `firstTranscriptPrompt()` checks the transcript for a real user message before dropping such a session, and that message becomes the title. The title is always the earliest *real* prompt (shared predicate: `provider.IsCommandInput()`); transcript command-execution records (`<command-name>`, `<local-command-stdout>`, `isMeta` entries) are excluded from titles and session text by `isCommandRecord()`.

Resume: `claude --resume <id>` (binary at `~/.local/bin/claude`)

#### Claude Code Desktop (`claude-code-desktop`)

Claude Code sessions started from the desktop app's Claude Code tab. The app runs the regular Claude Code engine against real project directories, so transcripts land in the standard `~/.claude/projects/<encoded-path>/<cliSessionId>.jsonl` store — but the sessions never appear in `~/.claude/history.jsonl`, so the `claude-code` provider can't see them.

Metadata lives at `<base>/claude-code-sessions/<uuid>/<uuid>/local_<uuid>.json` under the same Electron `userData` dir the Cowork provider uses. Same metadata family as Cowork but with real project paths: fields include `sessionId`, `cliSessionId`, `title` (app-generated — e.g. "gdocs-sync empty results bug"), `cwd`/`originCwd` (the attached project directory, not a sandbox), `createdAt`/`lastActivityAt`/`lastFocusedAt` in Unix ms, `isArchived`, `scheduledTaskId`, `model`. Archived sessions are excluded. There is no sibling sandbox dir — the transcript is in the shared `~/.claude` store.

The session ID exposed to sesh is `cliSessionId` (the Claude Code UUID that names the transcript and that `claude --resume` accepts), falling back to the `local_` sessionId if absent. App-generated titles are marked `CuratedTitle`, which exempts them from LLM summarization.

`SessionText`: `transcriptTextFromProjects()` scans `~/.claude/projects/*/<id>.jsonl` — the same helper the `claude-code` provider uses.

Resume: `cd <cwd> && claude --resume <cliSessionId>` — desktop sessions are ordinary Claude Code sessions in the shared store, so terminal resume works (unlike Cowork).

#### Claude Cowork (local agent-mode sessions)

Cowork is the Claude desktop app's local agent-mode feature, stored under the app's Electron `userData` dir (macOS `~/Library/Application Support/Claude`, Windows `%AppData%\Claude`, Linux `~/.config/Claude` — no official Linux app yet, so that path is untested). Separate from `~/.claude` (Claude Code CLI); the app's Chat tab lives in claude.ai web storage (IndexedDB) and is out of scope.

One metadata file per session at `<base>/local-agent-mode-sessions/<uuid>/<uuid>/local_<uuid>.json` (fields include `sessionId`, `cliSessionId`, `title`, `userSelectedFolders`, `createdAt`/`lastActivityAt` in Unix ms, `isArchived`, `scheduledTaskId`/`sessionType`), with a sibling `local_<uuid>/` sandbox dir holding the transcript. Archived sessions are excluded (as in OpenCode). Repeat runs of one scheduled task (same `scheduledTaskId`) are collapsed unless `collapse_scheduled` is `false`: routine runs fold into one representative (most recent, annotated with the run count), but any run with a human turn beyond the trigger is kept individually — detected by counting non-trigger user turns in the transcript. (Those counts are recomputed each list, reading transcripts concurrently; the intended follow-up is to compute them in the summary/index pass rather than re-reading.) `Directory` is `userSelectedFolders[0]`, else empty — `cwd` points inside the app sandbox, not a project path.

`SessionText` (lazy) prefers the transcript named `<cliSessionId>.jsonl` under `local_<uuid>/.claude/projects/*/`, else any nested transcript, else `local_<uuid>/audit.jsonl` — all the same JSONL shape, parsed by the shared `extractConversationText`.

Not read: `<base>/claude-code-sessions/` — those are the desktop app's Claude Code sessions, surfaced by the `claude-code-desktop` provider.

Resume: not possible from a terminal (the app owns the session); best effort foregrounds the app (`open -a Claude` on macOS, `xdg-open`/`start` elsewhere).

#### External providers

Any executable that outputs `[{"id", "title", "created", "last_used", ...}]` to stdout. Timestamps accept RFC 3339 or Unix milliseconds as strings. See the provider setup section above for the full schema.

### Key design decisions

- **TUI renders to stderr.** The binary's stdout is reserved for the shell command output. Uses `tea.WithOutput(os.Stderr)` and `tea.WithAltScreen()`.
- **Fuzzy search via sahilm/fuzzy.** Each session has a `SearchText` field (not exported to JSON) concatenating title, slug, directory, first user prompts, and cached summary.
- **One cache directory, resolved in one place.** `internal/paths.Cache()` is the only implementation; `summary.CacheDir()` and `update.cachePath()` both delegate. This is not just tidiness — the Windows branch probes the filesystem (`%LOCALAPPDATA%\sesh` when `~/.cache` is absent), so two independent implementations could disagree, and one of them calling `MkdirAll` changed the answer the other got on the next run. That is exactly what happened: the update checker created `~/.cache/sesh`, which flipped `summary.CacheDir()` from `%LOCALAPPDATA%` to `~/.cache` and silently orphaned every cached summary. Any new cache file goes through this helper.
- **Pure Go SQLite.** Uses `modernc.org/sqlite` to avoid CGO. Opens the database read-only with WAL mode.
- **Shell quoting.** `provider.ShellQuote()` handles paths with spaces and special characters (single-quote wrapping with escaped internal quotes).
- **Provider options pattern.** Built-in providers accept functional options (e.g., `WithOpenCodeResumeCommand()`) so config overrides are injected at construction time without the provider needing to know about the config system.
- **Summary generation is pluggable.** No built-in LLM client. The user configures any command that reads stdin and writes a summary to stdout (e.g., `llm`, `claude -p`, a local model script). This avoids credential management complexity in sesh itself.
- **Summaries replace display titles, except real names.** `Session.DisplayTitle()` prefers curated `Title` > `Summary` > `Title` > `Slug` > `ID`. Sessions with ugly auto-generated titles (common in external providers) get clean display names once summarized, but a session that already has a real name keeps it.
- **Curated titles win on both generation and display.** `Session.CuratedTitle` marks a title as a real name rather than a raw first prompt — the desktop app generates real session names for both Claude Code Desktop and Cowork sessions, and the user can rename them.
  - *Generation:* `sesh index`, lazy indexing, the ask-time refresh, and the cache-warming hint all skip curated sessions.
  - *Display:* `DisplayTitle()` prefers the curated title over any summary, and `applySummaries()` skips curated sessions entirely so a cached summary is never attached (which would also pollute `SearchText`).

  Both halves are needed. Generation guards only prevent *new* summaries; a cache entry can predate the flag, or be generated for the same session ID under a different provider — a desktop session resumed in the terminal is summarized as `claude-code`, where `CuratedTitle` is false, and the summary is keyed by an ID both entries share. Without the display guard it lands on the surviving desktop entry and permanently masks the app's name.
- **Renames need no invalidation.** Cowork and Claude Code Desktop titles are re-read from the app's metadata JSON on every run, so renaming a session in the app propagates on the next invocation. Nothing is cached and nothing needs to detect the change. Note this could *not* have been solved by re-summarizing: `NeedsSummary` gates regeneration on `last_used` changing, and a rename doesn't touch it.
- **Providers collect sessions concurrently.** `collectSessions()` launches goroutines per provider and merges results. External provider failures log a warning and don't block other providers. `dedupeSessions()` then collapses entries sharing a session ID — a desktop session resumed from the terminal can appear in both the `claude-code` and `claude-code-desktop` providers, and the desktop entry (curated title) wins while keeping the latest `LastUsed` and both search corpora.
- **Provider naming.** Three Claude surfaces map to three providers: `claude-code` (Claude Code CLI), `claude-code-desktop` (Claude Code in the desktop app), `claude-cowork` (the desktop app's Cowork/local agent mode). The pre-rename `claude` config key is still accepted as a deprecated alias for `claude-code`.

### Summary system

#### Architecture

- `summary/cache.go` — JSON-file-backed cache at `~/.cache/sesh/summaries.json`. Keyed by session ID. Display and regeneration are decoupled: `Get()` returns a cached summary whenever one exists (a stale summary is a better display title than the raw, often multi-line first prompt), while `NeedsSummary()` flags entries for regeneration when `last_used` has changed AND the summary is >1 hour old (prevents re-summarizing active sessions on every run).
- `summary/generate.go` — Shells out to user-configured command. Session text (user prompts) goes on stdin, summary comes out on stdout. 30-second per-summary timeout. `GenerateBatch` runs a bounded worker pool (`DefaultBatchConcurrency` = 4, overridable via `index.concurrency`); the progress callback is invoked under a lock, so callers that mutate shared state from it — `runIndex`'s error-count map, `RunRecorder`'s flags — stay correct without their own synchronization. Its first argument is a completion count, not a position: results land out of order, so there is no meaningful index. All LLM prompts are assembled by `BuildPrompt()`, which layers a system prompt (role framing), transcript, and task prompt, with support for `{{TRANSCRIPT}}` template expansion in custom prompts. `GenerateBatch` makes one independent LLM call per session — there is no multi-session prompt here, so summaries cannot bleed into each other (the multi-session prompt lives in `aiFilterSessions`, where cross-session reasoning is the point).
- `summary/health.go` — JSON record at `~/.cache/sesh/index-health.json` tracking whether generation actually works. `RecordRun(attempted, succeeded, firstErr, command)` increments a counter when every attempted summary in a run failed and resets it on any success; `Failing()` reports once the counter reaches `FailureHintThreshold` (3). Empty runs are ignored so "nothing to summarize" is not read as evidence either way.
- `cmd/sesh/main.go` — Wires it together. `sesh index` for bulk generation. Normal `sesh` runs lazy background generation (up to 10 sessions) in a goroutine during the TUI picker.

#### Provider.SessionText()

Each provider implements `SessionText(ctx, sessionID) string` to supply raw user prompt text for summary generation:
- **OpenCode:** Queries first 10 user text parts from the SQLite database.
- **Claude Code:** Reads the session transcript JSONL and extracts user message content strings.
- **Claude Code Desktop:** Same shared `~/.claude/projects` transcript store as the CLI, via the shared `transcriptTextFromProjects()` helper.
- **Claude Cowork:** Reads the nested Claude Code-format transcript, falling back to `audit.jsonl`, both parsed by the same `extractConversationText` helper as the Claude Code provider.
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

### Setup subcommand

`sesh setup` detects an available LLM CLI, writes a config, and verifies it works. `cmd/sesh/setup.go`.

**Detection** (`candidates`, `detectCandidate`) probes PATH via an indirected `lookPath` — a package var so tests control what appears installed without depending on PATHEXT/executable-bit differences across platforms. Order is `llm`, `claude`, `codex`: `llm` first because it is one API call rather than a whole agent harness. PATH presence is the signal, not session mix — OpenCode is model-agnostic, so a session list dominated by it says nothing about which credential exists.

Commands use **model aliases** (`haiku`, `sonnet`) rather than pinned IDs. An alias floats in version but holds in cost tier, which is the dimension that matters when any model can write a fifteen-word title. A pinned ID is stable today and on a deprecation clock.

The `claude` candidate carries **`--setting-sources ""`**, and it is load-bearing. Without it `claude -p` loads project settings and CLAUDE.md from the working directory and summarizes *those* instead of stdin — measured at 1/5 success from inside a project versus 3/3 with it, and sesh is essentially always run from a project directory. The failure is silent: exit 0, fluent English, wrong subject. `--no-session-persistence` keeps summarization from writing its own session transcripts; `--strict-mcp-config` skips MCP startup a one-shot summary has no use for.

**Verification** (`verifyCommand`, `summarizesCanary`) runs the command against a canary transcript through `summary.Generator`, so it exercises the real prompt assembly, excerpting, and 30-second timeout. `exec.LookPath` proves a binary exists, not that it works — an expired login, a retired model, and the context leak above all produce non-empty output. The check matches **several** anchors (`quokka`, `cache.go`, `call site`) rather than one token, because summarizers legitimately paraphrase: one model returned "Renaming a helper function in cache.go and updating its call sites", dropping the proper noun and failing a single-token check on a working config.

**Writing** (`writeConfigKeys`) merges top-level keys through `map[string]json.RawMessage` rather than round-tripping the typed `config` struct. Neither `config` nor `providerConfig` has a catch-all, so unmarshal-then-marshal silently drops unknown fields — a key from a newer sesh, or one the user hand-wrote. Backs up to `config.json.bak`, writes atomically via tmp+rename, and refuses to touch an unparseable config. Key order is normalized (encoding/json sorts map keys); content is preserved, layout is not.

Setup only ever **adds absent keys** and never modifies an existing one, so the list of keys being added is the complete diff — which is why it prints that list instead of a real diff. Two keys cover all four resolution chains: `index` serves title generation and the ask filter pass, `ask` serves prose generation and is inherited by recap.

`loadConfigWithPath()` reports which of the candidate paths the config came from, so setup edits the file the user actually has rather than assuming `~/.config/sesh/config.json`.

### Cache warming and failure reporting

The main picker writes at most one stderr hint. When **no** LLM command is configured, `maybeShowSetupHint` suggests `sesh setup` — but only if something was detected to configure and the user has enough sessions (`setupHintMinSessions`, matching the cache-warming threshold) for titles to matter. It shows at most `setupHintLimit` (3) times, tracked in `~/.cache/sesh/setup-hint.json`, then stops for good; running `sesh setup` at all suppresses it permanently, including when the user declines the write. There is deliberately no time-based re-prompt — a decayed nag is still a nag for a one-time onboarding nudge. The hint is picker-only: `sesh list`, `--json`, and `--ai-search` back other tools where it would just be chatter.

When an LLM command **is** configured, one of two other hints fires. They are mutually exclusive, and which one depends on `summary.Health`:

- **Generation is healthy** and >20 sessions lack summaries: `sesh: N sessions without summaries. Run 'sesh index' to generate them.` The user has set up an LLM command but hasn't run the initial index yet.
- **Generation is failing** (`Health.Failing()`, i.e. 3+ consecutive runs where every summary failed): the failure hint, carrying the condensed error and the command that produced it.

The failure hint takes precedence — pointing someone at `sesh index` when indexing is what's broken sends them in a circle.

Reporting is deferred by necessity. `lazyIndex` runs in a goroutine underneath the bubbletea alt screen and has nowhere to print, so it records the run outcome to `Health` and a **later** sesh run surfaces it. This mirrors how `checkVersionBackground` defers update notices through its own cache. The distinction that makes this worth doing: a single failed run is usually transient (rate limit, timeout on one long transcript) and resolves on its own, while repeated total failure means a broken command that never will — and before this existed, the latter was indistinguishable from having no LLM configured.

`sesh index` records to the same `Health` record, so a successful index clears a stale failure hint. It also collapses repeated identical errors, printing each distinct error once as it first appears and rolling up counts at the end.

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
