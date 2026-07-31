<p align="center">
  <img src="icon.png" alt="sesh" width="128">
</p>

# sesh

A unified session browser for coding agents. Search across OpenCode, Claude Code (CLI and desktop app), Claude Cowork, and any other agent with a single fuzzy picker, then resume directly into the session.

![sesh picker showing sessions from multiple coding agents](screenshots/picker.png)

## Install

### Homebrew (macOS/Linux)

```bash
brew install --cask dru89/tap/sesh
```

### Other methods

Download a prebuilt binary from [GitHub Releases](https://github.com/dru89/sesh/releases), or install with Go (1.25+):

```bash
go install github.com/dru89/sesh/cmd/sesh@latest
```

To update a non-Homebrew install, run `sesh update`.

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
echo 'sesh init powershell | Out-String | Invoke-Expression' >> $PROFILE
```

Or source the pre-made wrapper files in `shell/` directly. Run `sesh init --help` to see all options.

## Usage

```

sesh                    # open the picker with all sessions
sesh auth refactor      # pre-fill search with "auth refactor"
sesh --agent opencode   # only show OpenCode sessions
sesh --json             # dump all sessions as JSON (for Raycast, scripts, etc.)
sesh list               # non-interactive session list
sesh list --since monday -n 20
sesh show ses_abc       # show details for a session (partial ID works)
sesh resume ses_abc     # resume a session directly (partial ID works)
sesh stats              # session statistics across all agents
sesh setup              # detect an LLM CLI and configure AI features
sesh setup --verify     # check that the configured commands still work
sesh index              # generate summaries for all sessions
sesh index --agent omp  # generate summaries for one agent only
sesh recap --days 7     # summarize what you worked on this week
sesh ask "What did I work on around login with claude code?"
```

<details>
<summary><code>sesh list</code></summary>

![sesh list showing a non-interactive table of sessions](screenshots/list.png)
</details>

<details>
<summary><code>sesh show</code></summary>

![sesh show displaying session details with glamour-rendered markdown](screenshots/show.png)
</details>

<details>
<summary><code>sesh stats</code></summary>

![sesh stats showing session statistics across agents](screenshots/stats.png)
</details>

In the picker: type to filter, arrow keys to navigate, enter to select, tab to toggle detail pane, esc to cancel.

![sesh detail pane showing session metadata and glamour-rendered markdown](screenshots/detail.png)

## Built-in providers

**OpenCode** reads `~/.local/share/opencode/opencode.db` (SQLite). Pulls session title, slug, working directory, and first user prompts for search.

**Claude Code** reads `~/.claude/history.jsonl` and scans `~/.claude/projects/` for session slugs. Pulls the first prompt text, working directory, and timestamps.

Both providers work automatically if the agent is installed. If the data files don't exist, the provider returns nothing and sesh continues with the others.

## Configuration

Optional. Create `~/.config/sesh/config.json` to override resume commands or add external providers. Add the `$schema` field for autocomplete and validation in your editor:

```json
{
  "$schema": "https://raw.githubusercontent.com/dru89/sesh/main/schema.json"
}
```

### Override resume commands

If you use a wrapper script (like `ca`) instead of calling the agent binary directly:

```json
{
  "providers": {
    "opencode": {
      "resume_command": "ca opencode -s {{ID}}"
    },
    "claude-code": {
      "resume_command": "ca -r {{ID}}"
    }
  }
}
```

`{{ID}}` is replaced with the session ID. The default commands are `opencode --session {{ID}}` and `claude --resume {{ID}}`.

### Claude Code in the desktop app

The Claude desktop app can run Claude Code sessions from its Claude Code tab. These are real Claude Code sessions against your project directories, but they never show up in the CLI's history file, so sesh surfaces them through a separate `claude-code-desktop` provider. It reads the app's session metadata — including the session names the app generates, which sesh uses as-is instead of generating its own summaries — and resumes with a plain `claude --resume` in the right directory, so you can pull a desktop session into your terminal.

The three Claude providers are:

| Provider | What it covers |
|---|---|
| `claude-code` | Claude Code sessions started in a terminal |
| `claude-code-desktop` | Claude Code sessions started in the desktop app |
| `claude-cowork` | Cowork (the desktop app's local agent mode) |

### Claude Cowork sessions

sesh also surfaces Claude Cowork sessions (the desktop app's local agent-mode feature) as a `claude-cowork` provider, separate from Claude Code CLI sessions. These sessions live inside the desktop app and can't be resumed from a terminal, so the default "resume" command just brings the app to the foreground (`open -a Claude` on macOS). Override it if you have a better entry point:

```json
{
  "providers": {
    "claude-cowork": {
      "resume_command": "my-claude-launcher {{ID}}"
    }
  }
}
```

By default, repeat runs of the same scheduled task (a recurring automation) are collapsed: the routine runs fold into one entry labeled with the run count, while any run you actually engaged with — where you typed something beyond the automated trigger — is kept on its own. So a nightly task doesn't flood the picker with hundreds of near-identical sessions, but the ones where you dug in stay findable. Set `collapse_scheduled` to `false` to list every run instead:

```json
{
  "providers": {
    "claude-cowork": {
      "collapse_scheduled": false
    }
  }
}
```

### Disable a built-in provider

```json
{
  "providers": {
    "claude-code": {
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

### Automatic setup

```
sesh setup
```

Looks for a supported CLI on your PATH, shows you the config it wants to write, and checks that it actually works before finishing. It uses whatever you're already logged into — no API keys to manage.

```
Found claude on your PATH.
  (uses your existing Claude Code login; ~5-10s per summary)

Creating ~/.config/sesh/config.json with:

  {
    "ask":   { "command": ["claude", "-p", "--model", "sonnet", ...] },
    "index": { "command": ["claude", "-p", "--model", "haiku", ...] }
  }

Write this config? [y/N] y
Wrote ~/.config/sesh/config.json

Checking index, ask.filter_command: claude -p --model haiku ...
  ok
Checking ask, recap: claude -p --model sonnet ...
  ok

All 2 command(s) working.
```

It searches for `llm`, then `claude`, then `codex`, preferring `llm` because it's a single API call rather than a whole agent harness. Existing settings are never overwritten — run it with a partial config and it fills only the gaps.

Run it again any time to check a configuration you already have:

```
sesh setup --verify   # check the configured commands; never writes
sesh setup --yes      # skip the confirmation prompt
```

`--verify` is the thing to reach for when summaries stop appearing. It runs each configured command against a known test transcript and reports which one is broken and why — which catches an expired login or a retired model far faster than reading config.

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
    "system_prompt": "You are a session indexer. Output only a short label.",
    "prompt": "Label this session in under 15 words."
  },
  "ask": {
    "command": ["llm", "-m", "sonnet"],
    "system_prompt": "You are a session search assistant. Answer only from the provided data.",
    "prompt": "custom prompt for answer generation",
    "filter_command": ["llm", "-m", "haiku"]
  },
  "recap": {
    "command": ["llm", "-m", "sonnet"],
    "system_prompt": "You are a work recap assistant. Summarize only the session data provided.",
    "prompt": "custom recap prompt"
  },
  "providers": { ... }
}
```

### Prompt structure

Each LLM call assembles input from three parts: a **system prompt** (role framing), the **transcript/data**, and a **task prompt** (what to produce). The structure piped to stdin looks like:

```
[system_prompt]
---
[transcript / session data]
---
[prompt]
```

- `system_prompt` tells the model what role to adopt. The defaults prevent the model from trying to "help with" or "respond to" the session content — a common failure mode when LLMs see conversation transcripts.
- `prompt` is the task-specific instruction (e.g., "label this session" or "write a recap"). Note it comes **after** the data, so a custom prompt referring to the transcript's position should say "above" — models told to read a transcript "below" find the instructions there instead and will sometimes reply that no transcript was provided.
- If `prompt` contains `{{TRANSCRIPT}}`, the transcript is inserted at that location instead of between the separators. This gives full control over prompt layout, and is how to place the transcript after the instructions if you want that ordering.

Both fields are optional. When omitted, sesh uses built-in defaults with anti-response guardrails.

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

### Concurrency

Summaries are generated 4 at a time. This matters most for agent CLIs: `llm` answers in well under a second, while `claude -p` boots a whole harness per call and takes 5-10 seconds, which is the difference between a few minutes and half an hour on a large backlog.

Lower it if your command shares a rate limit that parallel calls exhaust:

```json
{
  "index": {
    "command": ["claude", "-p", "--model", "haiku"],
    "concurrency": 2
  }
}
```

It only applies to `index`, the one batch path.

### Cache

Summaries are cached at `~/.cache/sesh/summaries.json`. A cached summary is considered stale when the session's `last_used` timestamp changes and the summary is more than an hour old. This avoids re-summarizing active sessions on every run while keeping finished sessions up to date.

If summary generation fails (expired credentials, command not found, timeout), sesh continues with the raw title. Nothing crashes.

A single failure is usually transient, so it passes quietly. Repeated total failure is not — it means the configured command is broken and waiting won't fix it — so sesh reports it the next time you open the picker, along with the underlying error:

```
sesh: summary generation has failed 3 times running.
sesh:   last error: command failed: exec: "claude": executable file not found in $PATH
sesh:   command: claude -p --model haiku
sesh:   run 'sesh setup --verify' to re-check your configuration.
```

The "run `sesh index`" hint is suppressed while this is showing, and the whole thing clears itself as soon as generation succeeds again.

## Recap

Generate a prose summary of what you worked on across all agents over a time period:

```
sesh recap --days 7           # last 7 days
sesh recap --since monday     # since Monday
sesh recap --since 2026-04-01 --until 2026-04-07
sesh recap --agent opencode   # only OpenCode sessions
```

Output goes to stdout as plain text. Use `--raw` to get the raw markdown without terminal formatting.

![sesh recap showing a glamour-rendered weekly summary](screenshots/recap.png)

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

Uses a two-pass approach: first filters sessions to the relevant subset (fast model), then generates a prose answer from just those sessions (heavy model). Output goes to stdout. Use `--raw` to get the raw markdown without terminal formatting.

![sesh ask answering a natural language question about authentication work](screenshots/ask.png)

## Agent skill

sesh ships with a [skill](https://skills.sh/) that teaches coding agents (Claude Code, OpenCode, Cursor, etc.) how to search and load past sessions on your behalf. Once installed, your agent can find previous sessions by topic, pull in conversation context, and answer questions like "what did we decide about auth last week?"

```bash
npx skills add dru89/sesh -g
```

The installer will prompt you to choose which agents to configure. You can also target specific agents with `--agent claude-code opencode` or drop the `-g` flag to install at the project level instead.

## Raycast Extension

A Raycast extension is included in the `raycast/` directory. It provides the same session browsing experience from Raycast's launcher, with configurable terminal support (Terminal.app, iTerm2, Ghostty, Warp, or custom). See [raycast/README.md](raycast/README.md) for setup instructions.
