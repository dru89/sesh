---
name: sesh
description: Find and load past coding agent sessions using the sesh CLI. Use this when the user asks about previous sessions, wants to find a session where they worked on something, or wants to recall what was decided or discussed in a past session. Also use when the user says "in this project" or "in this repo" to search sessions scoped to the current working directory.
---

## What I do

I help you find and load context from past coding agent sessions (OpenCode, Claude Code, and any configured external agents) using the `sesh` CLI tool.

## Available commands

All commands run via Bash. sesh is already installed on the user's PATH.

### Finding sessions

**List sessions (non-interactive):**
```bash
sesh list [options]
```
Options:
- `--cwd` -- filter to sessions in the current working directory
- `--repo` -- filter to sessions in the current git repository (resolves to repo root)
- `--dir <path>` -- filter by directory (absolute path for exact match, bare word for fuzzy)
- `--agent <name>` -- filter by agent name (fuzzy match)
- `--since <date>` -- only sessions since a date (e.g. `monday`, `2026-04-01`, `3d`)
- `-n <count>` -- limit number of results

**JSON output for programmatic use:**
```bash
sesh --json [options]
```
Same filtering options as `sesh list`. Returns an array of session objects with `id`, `agent`, `title`, `summary`, `directory`, `created`, `last_used`, and `resume_command`.

**Search with AI:**
```bash
sesh ask "what did I work on with auth last week?"
```
Two-pass LLM search: filters sessions by relevance, then generates a prose answer. Requires an LLM command to be configured.

### Loading session context

**Show session details + conversation text:**
```bash
sesh show --json <session-id>
```
Returns full session metadata plus a `text` field containing the conversation (user prompts and assistant responses, prefixed with `User:` and `Assistant:`). Accepts partial session IDs.

**Show session details (human-readable):**
```bash
sesh show <session-id>
```

### Other useful commands

```bash
sesh stats          # session statistics, top directories, recent sessions
sesh recap --days 7 # LLM-generated summary of recent work
```

## When to use me

- User asks "which session did we..." or "find the session where..."
- User asks "what did we decide about..." or "what was the plan for..."
- User wants to recall past work in a specific directory or project
- User says "in this project", "in this repo", "in this directory", or "here" when referring to past sessions
- User asks "what have I been working on?" (use `sesh list --since` or `sesh recap`)
- User references a past conversation and wants to pull in context

## CWD and repo context

**`--repo` vs `--cwd` vs `--dir`:**
- `--repo` resolves to the git repository root, regardless of which subdirectory the user is in. Use this when the user says "in this project", "in this repo", "here", or "for this codebase". This is the most common filter for project-scoped queries.
- `--cwd` uses the literal current working directory. Use this only when the user specifically means "in this exact directory" (rare).
- `--dir <path>` filters by a specific path or fuzzy term. Use when the user names a specific project or path.

When the user says any of the following, use `--repo`:

- "in this project" / "in this repo" / "in this directory" / "in this folder"
- "here" (when referring to past sessions)
- "for this codebase"
- "the session where we did X" (without specifying a directory — assume current project)

When the user says "in <project name>" or references a specific path, use `--dir <path>` instead.

If the user's question is clearly about work across all projects (e.g. "what have I been working on this week?"), don't add any directory filter — search everything.

## Workflows

### Finding a session by topic

When the user asks to find a session about a specific topic:

1. Start with `sesh --json --repo` if the user is asking about work in the current project, or `sesh --json` for all sessions
2. Parse the JSON and scan summaries/titles for relevance
3. If the initial list is too broad, narrow with `--dir`, `--agent`, or `--since`
4. Present the top candidates to the user with session ID, title/summary, agent, directory, and relative time
5. If the user picks one, load it with `sesh show --json <id>`

### Loading conversation context

When the user wants to recall what was discussed or decided:

1. Find the session (using the workflow above, or if the user provides an ID)
2. Run `sesh show --json <session-id>` and read the `text` field
3. The text contains the full conversation with `User:` and `Assistant:` prefixes
4. Answer the user's question based on the conversation content
5. If the conversation is very long, summarize relevant sections rather than dumping everything

### Quick directory overview

When the user wants to know what sessions exist for a project:

```bash
sesh list --repo              # current git repository
sesh list --cwd               # current directory (exact)
sesh list --dir ~/projects    # fuzzy match on directory
sesh list --repo --since 3d   # recent sessions in this repo
```

### Searching across all sessions

When the topic could span multiple sessions or directories:

```bash
sesh ask "what did I work on related to auth?"
```

Or for structured search without LLM:
```bash
sesh --json | # parse and filter by title/summary yourself
```

## Guidelines

- Prefer `--repo` when the user is asking about work in the current project, or when they say "here", "this repo", "this project", etc.
- Use `--cwd` only when the user specifically means the exact current directory
- Use `--json` output for programmatic filtering; use plain `sesh list` or `sesh show` for human-readable output shown directly to the user
- Session IDs can be partial — `sesh show ses_abc` works if it's unambiguous
- The `text` field from `sesh show --json` can be large for long sessions. Read it, extract what's relevant, and summarize rather than pasting the entire thing
- When presenting session candidates, include enough context for the user to identify the right one: title/summary, directory, agent, and when it was last used
- If `sesh ask` is available (LLM configured), prefer it for open-ended questions across many sessions — it's designed for exactly that use case
