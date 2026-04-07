# Setting up sesh

> **For humans:** Give this file to your coding agent (Claude Code, OpenCode, Cursor, etc.) and tell it which agent you want to configure as a sesh provider. It has everything the agent needs to set up the integration. You can also ask it to configure the LLM commands for summaries if you have `llm`, `claude`, or another CLI tool available.

---

## What is sesh?

sesh is a CLI tool that aggregates sessions from multiple coding agents into a single fuzzy-search picker. You type `sesh`, search across all your agents' sessions, select one, and it resumes that session in the right directory.

## Install

```bash
# Homebrew (macOS/Linux)
brew tap dru89/tap && brew install sesh

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
sesh init powershell | Invoke-Expression
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

Each section accepts an optional `prompt` field to override the default:

```json
{
  "index": {
    "command": ["llm", "-m", "haiku"],
    "prompt": "Describe this coding session in one sentence, under 15 words. Output only the description."
  }
}
```

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
sesh list                     # non-interactive session list
sesh show <session-id>        # session details (partial ID works)
sesh stats                    # session statistics
sesh index                    # generate titles for all sessions (run once)
sesh ask "what did I do?"     # natural language query
sesh recap --days 7           # weekly recap
sesh --json                   # JSON output for scripts/Raycast
```
