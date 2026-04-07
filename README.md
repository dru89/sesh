# sesh

A unified session browser for coding agents. Search across OpenCode, Claude Code, and any other agent with a single fuzzy picker, then resume directly into the session.

## Install

Requires Go 1.23+.

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

sesh outputs a shell command (cd + resume) that needs to run in your current shell. Add this to your `.bashrc` or `.zshrc`:

```bash
sesh() { local cmd; cmd=$(command sesh "$@") || return $?; eval "$cmd"; }
```

Pre-made files are in `shell/sesh.bash` and `shell/sesh.zsh` if you prefer to source them.

## Usage

```
sesh                    # open the picker with all sessions
sesh auth refactor      # pre-fill search with "auth refactor"
sesh --agent opencode   # only show OpenCode sessions
sesh --json             # dump all sessions as JSON (for Raycast, scripts, etc.)
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

Any coding agent can integrate with sesh through the external provider protocol. You write a script that outputs JSON, register it in config, and it appears in the picker alongside the built-ins.

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
