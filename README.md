# sesh

A unified session browser for coding agents. Search across OpenCode, Claude Code, and any other agent with a single fuzzy picker, then resume directly into the session.

## Install

Download a prebuilt binary from [GitHub Releases](https://github.com/dru89/sesh/releases) and put it on your PATH.

Or install with Go (1.23+):

```bash
go install github.com/dru89/sesh/cmd/sesh@latest
```

Or build from source:

```bash
git clone https://github.com/dru89/sesh.git
cd sesh
go build -o ~/go/bin/sesh ./cmd/sesh/
```

### Shell wrapper

sesh outputs a shell command (cd + resume) that needs to run in your current shell. The easiest way to set this up:

```bash
# bash
echo 'eval "$(sesh init bash)"' >> ~/.bashrc

# zsh
echo 'eval "$(sesh init zsh)"' >> ~/.zshrc

# fish
echo 'sesh init fish | source' >> ~/.config/fish/config.fish
```

```powershell
# PowerShell
echo 'sesh init powershell | Invoke-Expression' >> $PROFILE
```

Or source the pre-made wrapper files in `shell/` directly. Run `sesh init --help` to see all options.

## Usage

```

sesh                    # open the picker with all sessions
sesh auth refactor      # pre-fill search with "auth refactor"
sesh --agent opencode   # only show OpenCode sessions
sesh --json             # dump all sessions as JSON (for Raycast, scripts, etc.)
sesh index              # generate summaries for all sessions
sesh index --agent omp  # generate summaries for one agent only
sesh recap --days 7     # summarize what you worked on this week
sesh ask "What did I work on around login with claude code?"
```

In the picker: type to filter, arrow keys to navigate, enter to select, esc to cancel.

## Built-in providers

**OpenCode** reads `~/.local/share/opencode/opencode.db` (SQLite). Pulls session title, slug, working directory, and first user prompts for search.

**Claude Code** reads `~/.claude/history.jsonl` and scans `~/.claude/projects/` for session slugs. Pulls the first prompt text, working directory, and timestamps.

Both providers work automatically if the agent is installed. If the data files don't exist, the provider returns nothing and sesh continues with the others.

## Configuration

Optional. Create `~/.config/sesh/config.json` to override resume commands or add external providers.

### Override resume commands

If you use a wrapper script (like `ca`) instead of calling the agent binary directly:

```json
{
  "providers": {
    "opencode": {
      "resume_command": "ca opencode -s {{ID}}"
    },
    "claude": {
      "resume_command": "ca -r {{ID}}"
    }
  }
}
```

`{{ID}}` is replaced with the session ID. The default commands are `opencode --session {{ID}}` and `claude --resume {{ID}}`.

### Disable a built-in provider

```json
{
  "providers": {
    "claude": {
      "enabled": false
    }
  }
}
```

### Add an external provider

Any coding agent can integrate with sesh through the external provider protocol. You write a script that outputs JSON, register it in config, and it appears in the picker alongside the built-ins. If you're setting this up from inside a coding agent, see [AGENTS.md](AGENTS.md) for instructions you can give it directly.

```json
{
  "providers": {
    "omp": {
      "list_command": ["omp-sesh"],
      "resume_command": "omp --resume {{ID}}"
    }
  }
}
```

`list_command` is an executable (plus arguments) that outputs a JSON array to stdout. `resume_command` is a template with `{{ID}}` and optional `{{DIR}}` placeholders.

## External provider protocol

The list command must output a JSON array of session objects:

```json
[
  {
    "id": "session-id",
    "title": "human-readable title or first prompt",
    "slug": "optional-short-name",
    "created": "2026-01-15T10:30:00Z",
    "last_used": "2026-01-15T11:45:00Z",
    "directory": "/absolute/path/to/working/directory",
    "text": "optional extra searchable text"
  }
]
```

| Field | Required | Notes |
|---|---|---|
| `id` | yes | Whatever the agent uses to identify a session for resuming |
| `title` | yes | Display name: session title, first prompt (truncated), or slug |
| `slug` | no | Short human-readable name |
| `created` | yes | RFC 3339 or Unix milliseconds as string |
| `last_used` | yes | RFC 3339 or Unix milliseconds as string |
| `directory` | no | Working directory where the session was started |
| `text` | no | Additional searchable text (first few prompts work well) |

Rules:
- Exit 0 on success, non-zero on failure
- If no sessions exist, output `[]`
- Only JSON goes to stdout. Warnings and errors go to stderr.

## JSON output

`sesh --json` returns an array of all sessions with an added `resume_command` field:

```json
[
  {
    "agent": "opencode",
    "id": "ses_abc123",
    "title": "Fix auth middleware",
    "slug": "eager-cactus",
    "created": "2026-04-07T09:43:39Z",
    "last_used": "2026-04-07T09:47:37Z",
    "directory": "/Users/you/project",
    "resume_command": "cd /Users/you/project && opencode --session ses_abc123"
  }
]
```

This is the integration point for Raycast extensions or other tools that want to present session data in their own UI.

## LLM configuration

sesh uses LLMs for title generation, session search, recaps, and natural language queries. Each subcommand can use a different model — fast/cheap for high-volume tasks, heavier for prose generation.

### Minimal setup

Configure one command and everything uses it:

```json
{
  "index": {
    "command": ["llm", "-m", "haiku"]
  }
}
```

The command receives input on stdin and writes output to stdout. Any executable works: `llm`, `claude -p`, a script that calls a local model, etc.

### Split fast and heavy models

Use a fast model for title generation and filtering, a heavier model for prose:

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

### Full configuration

```json
{
  "index": {
    "command": ["llm", "-m", "haiku"],
    "prompt": "Summarize this coding session in one sentence, under 20 words."
  },
  "ask": {
    "command": ["llm", "-m", "sonnet"],
    "prompt": "custom prompt for answer generation",
    "filter_command": ["llm", "-m", "haiku"]
  },
  "recap": {
    "command": ["llm", "-m", "sonnet"],
    "prompt": "custom recap prompt"
  },
  "providers": { ... }
}
```

### Fallback chains

Each subcommand falls back through other configured commands so you only need to set up the ones you care about:

| Task | Tries in order |
|---|---|
| `index` (title generation) | `index` -> `recap` -> `ask` -> `ask.filter_command` |
| `ask` (prose answer) | `ask` -> `recap` -> `index` |
| `ask` (session filtering) | `ask.filter_command` -> `index` -> `ask` -> `recap` |
| `recap` (prose summary) | `recap` -> `ask` -> `index` |

The pattern: heavy tasks prefer other heavy commands, light tasks prefer other light commands.

## Summaries

sesh can generate one-line summaries for each session. Summaries replace ugly or auto-generated titles in the picker and are included in the fuzzy search corpus.

### Generating summaries

**Bulk (recommended for first run):**

```
sesh index
```

Shows a progress line per session. Run this once to backfill, then sesh keeps up incrementally.

**Lazy background generation:** During normal `sesh` usage, up to 10 unsummarized sessions are processed in the background while the picker is open. Summaries won't appear in the current invocation but will be there next time.

### Cache

Summaries are cached at `~/.cache/sesh/summaries.json`. A cached summary is considered stale when the session's `last_used` timestamp changes and the summary is more than an hour old. This avoids re-summarizing active sessions on every run while keeping finished sessions up to date.

If summary generation fails (expired credentials, command not found, timeout), sesh logs a warning and continues with the raw title. Nothing crashes.

## Recap

Generate a prose summary of what you worked on across all agents over a time period:

```
sesh recap --days 7           # last 7 days
sesh recap --since monday     # since Monday
sesh recap --since 2026-04-01 --until 2026-04-07
sesh recap --agent opencode   # only OpenCode sessions
```

Output goes to stdout as plain text.

## AI fallback search

When the fuzzy picker returns no results and an LLM command is configured, sesh automatically triggers an AI-powered search. The LLM receives your query along with all session titles/summaries and returns the most relevant matches.

The fallback activates after 3+ characters with no fuzzy matches. A "Searching with AI..." indicator appears while the LLM processes. Results are marked with "(AI)" in the match count. If the LLM call fails, the picker stays on the empty state.

## Ask

Ask a natural language question about your sessions:

```
sesh ask "What did I work on around login with claude code since last Monday?"
sesh ask "Show me everything related to the API gateway"
sesh ask --agent opencode "What refactoring have I done recently?"
```

Uses a two-pass approach: first filters sessions to the relevant subset (fast model), then generates a prose answer from just those sessions (heavy model). Output goes to stdout.
