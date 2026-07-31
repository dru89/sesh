# Changelog

## [Unreleased]

- Collapse repeat runs of a Claude Cowork scheduled task by default: routine runs fold into one representative (labeled with the run count), while runs you engaged with (a human turn beyond the trigger) are kept individually — so a recurring automation doesn't flood the picker or overrun the `sesh ask` filter without hiding the sessions you dug into; disable with `collapse_scheduled: false`

## [2.2.0] - 2026-07-28

- Add `sesh setup` — detects an LLM CLI on your PATH (`llm`, `claude`, or `codex`), shows the config it wants to write, and verifies the result actually works before finishing. Uses whatever you're already logged into, so there are no API keys to manage. Existing settings are never overwritten; run it with a partial config and it fills only the gaps
- Add `sesh setup --verify`, which checks a configuration you already have without writing anything. Reach for it when summaries stop appearing — it runs each configured command against a known transcript and reports which one is broken and why
- Suggest `sesh setup` from the picker when AI features aren't configured but a supported CLI is installed. Shown at most three times, and never again once you've run setup
- Generate summaries 4 at a time instead of one after another. Barely noticeable with a fast API-backed command, but an agent CLI takes 5-10 seconds per call, which was the difference between minutes and half an hour when indexing a large backlog. Tune with `index.concurrency` if your command shares a rate limit that parallel calls exhaust
- Fix the summarizer being told to read the transcript in the wrong place. The `index`, `ask`, and `recap` task prompts all said the session data was "below" them, but it is placed above — so the model would sometimes look, find only instructions, and answer that no transcript was provided. Affected every configured LLM command, not just one
- Stop generated summaries from overriding real session names. Claude Code Desktop and Cowork sessions carry a name the app generated or the user typed, and a cached summary silently displaced it — summarization skipped these sessions, but nothing stopped an already-cached summary from being shown over the title. This also means renaming a session in the desktop app now shows up in sesh on the next run
- Report persistent summary-generation failures instead of swallowing them. Background indexing runs under the TUI and previously discarded every error, so a broken LLM command (retired model ID, expired credentials, renamed binary) left the picker silently never improving — indistinguishable from having no LLM configured at all. Repeated total failures are now recorded and surfaced on a later run, along with the underlying error and the command that produced it
- Suppress the "run `sesh index`" hint while generation is failing, so the suggested next step isn't the thing that's broken
- Collapse repeated identical errors in `sesh index` output — a broken command fails the same way for every session, and printing it hundreds of times buried the one line that mattered
- Fix the summary cache silently disappearing on Windows. The update checker computed its own cache path and created `~/.cache/sesh`, which flipped where sesh looked for summaries — written to `%LOCALAPPDATA%\sesh` on one run, read from `~/.cache\sesh` on the next, so every title reverted to the raw first prompt and needed regenerating. All cache paths now resolve through one shared helper. Windows users may see one final re-index as the location settles

## [2.1.0] - 2026-07-28

- Hide command-only Claude Code sessions — opening claude just to run `/login` or `/model` no longer leaves a junk entry in the picker. Sessions whose history is all commands are checked against their transcript first, so sessions started with an initial prompt argument (`claude "..."`) are kept
- Title Claude Code sessions by their first real prompt instead of their first history entry, so a session that starts with `/model` is no longer titled "/model"
- Exclude slash-command execution records from session text sent to the summarizer

## [2.0.0] - 2026-07-28

- **Breaking:** rename the `claude` provider to `claude-code` for clarity next to the other Claude surfaces. The `claude` config key still works as a deprecated alias, but the `agent` name in output (JSON, list, stats) is now `claude-code`
- Add `claude-code-desktop` built-in provider for Claude Code sessions started in the desktop app — these never appear in `~/.claude/history.jsonl`, so they were invisible to sesh. Uses the app's own session names as display titles and resumes with `claude --resume` in the session's directory
- Add `claude-cowork` built-in provider for Claude Cowork sessions (the desktop app's local agent-mode sessions), previously invisible to sesh
- Skip LLM summarization for sessions whose agent already wrote a real title (Claude Code Desktop and Cowork app-generated names) — the app's name is used as-is
- Dedupe sessions that appear under both the `claude-code` and `claude-code-desktop` providers (a desktop session later resumed from the terminal)
- Size the agent column in the picker and `sesh stats` to the longest provider name instead of a fixed width, so long names don't break alignment
- Fix TUI picker scrolling the prompt and first sessions off the top of the screen — multi-line titles (a Claude session's raw first prompt, which can span several lines) now collapse to a single line, and every rendered line is clamped to the terminal width so nothing wraps
- Clamp the help bar and prompt line to the terminal width, anchor the help bar at the bottom when the detail pane is taller than the list, and fall back to the list-only view when the terminal is too narrow for a usable detail pane
- Show a cached summary even when it's stale instead of reverting to the raw first prompt — staleness now only governs regeneration, not display, so a session that picked up new activity after being summarized keeps a useful title until the next index run
- Fix the PowerShell shell wrapper: correct `Out-String` piping and a `Substring` offset error in `sesh init powershell` output

## [1.1.1] - 2026-04-27

- Fix `ExcerptBookends` skipping oversized message chunks — sessions with a large early assistant response (e.g. 33K chars) would produce excerpts containing only the tiny first user message, leading to poor or wrong summaries
- Truncate oversized chunks at sentence, newline, or word boundaries instead of hard substring cuts
- Unify excerpt size to 5000 chars per end across both title generation and ask answer generation (was 3000/5000)

## [1.1.0] - 2026-04-27

- Add `system_prompt` config field for role-framing LLM calls — prevents models from "responding to" session transcripts instead of summarizing them
- Add `{{TRANSCRIPT}}` template variable support in custom prompts for precise control over transcript placement
- Rewrite all default prompts with anti-response guardrails (role framing, explicit "do not engage" instructions, structured output constraints)
- Wire up `ask.prompt`, `ask.system_prompt`, `recap.prompt`, and `recap.system_prompt` config fields (previously declared in schema but unused)
- Add role framing to AI session filter prompt
- Add interactive prompt for `sesh ask` when no question is provided
- Add `resume` to help text command list
- Consolidate CLAUDE.md into AGENTS.md

## [1.0.0] - 2026-04-15

- Add `sesh resume` command for direct session resumption by ID (partial ID works)
- Add 500ms debounce to AI fallback search in TUI picker to avoid wasted LLM calls while typing
- Add page-up/page-down navigation in TUI picker
- Unify AI filter logic across `sesh ask`, `--ai-search`, and TUI fallback — all callers now share the same richer prompt with date and SearchText
- Validate external provider session fields: skip sessions with empty id, warn on missing title or unparseable timestamps
- Add OG/social card image

## [0.13.0] - 2026-04-15

- Improve `sesh ask` with smart bookend excerpting — include ~5K chars from the start and end of each conversation, splitting at message boundaries instead of hard truncation
- Regenerate stale summaries before `sesh ask` filtering so resumed sessions have current titles
- Include first few user prompts in `sesh ask` pass 1 for better session relevance filtering
- Add 30-second timeout for provider list commands to prevent hung external scripts from blocking sesh
- Add SHA256 checksum verification when downloading updates via `sesh update`
- Single-line progress indicator for `sesh index` with red error highlighting
- Replace lipgloss pseudo-version with tagged v1.1.0 (downgrade glamour to v0.9.1)
- Add app icon for repo and Raycast extension
- Add CHANGELOG.md, conventional commit guidelines, release process docs
- Update CI workflows to Go 1.25; include LICENSE in release archives

## [0.12.0] - 2026-04-15

- Add `--dir`, `--cwd`, `--repo`, `--since`, and `-n` flags to `sesh ask`
- Include session IDs, resume commands, and conversation excerpts in `sesh ask` answers
- Fix glamour rendering for all subcommands — shell wrapper now passes subcommands through directly instead of capturing stdout
- Extract shared `resolveDirFlags` helper to deduplicate flag validation

## [0.11.4] - 2026-04-13

- Add `--since` and `-n` flags to the root picker command
- Filter out subagent/child sessions from OpenCode provider
- Fix fuzzy filter sort order

## [0.11.0] - 2026-04-10

- Add `--repo` flag to filter sessions by git repository root

## [0.10.0] - 2026-04-10

- Add directory and agent search filters (`dir:` and `agent:` prefixes in the picker, `--dir` and `--cwd` flags)
- Include assistant responses in session text output
- Add agent skill file for coding agents to find and load past sessions
- Show git commit SHA in `sesh version`

## [0.9.3] - 2026-04-08

- Add glamour-rendered markdown output for `ask`, `recap`, `show`, and the detail pane
- Use deterministic hashed colors for agent badges (agents always get the same color)
- Fix shell wrapper eval'ing non-command output (e.g. `sesh version`)
- Fix summary prompt to produce short titles; add `sesh index --clear`
- Add MIT license and screenshots to README

## [0.8.0] - 2026-04-07

- Fix Raycast AI search: async execution, loading indicators, error toasts
- Fix cache warming to exclude sessions with no searchable text

## [0.7.0] - 2026-04-07

- Add `env` config for setting environment variables on LLM commands (top-level and per-command)

## [0.6.0] - 2026-04-07

- Add `sesh update` for self-updating (with Homebrew detection)
- Add `sesh version`
- Add background update check with 24-hour cache
- Add Homebrew tap via GoReleaser

## [0.5.0] - 2026-04-07

- Add `--ai-search` flag for LLM-ranked search in JSON mode
- Add Raycast AI search command

## [0.4.0] - 2026-04-07

- Add `sesh show` for session details (with partial ID matching)
- Add `sesh stats` for session statistics
- Add TUI detail pane (Tab to toggle)
- Add lazy background summary generation while picker is open

## [0.3.0] - 2026-04-07

- Add `sesh list` for non-interactive session listing
- Add `sesh init` for shell wrapper setup (bash, zsh, fish, PowerShell)
- Add Raycast extension for session browsing
- Add JSON Schema for config validation
- Add test suite and CI workflow

## [0.2.0] - 2026-04-07

- Add Windows support (PowerShell wrapper, zip archives)

## [0.1.0] - 2026-04-07

Initial release.

- Fuzzy session picker with OpenCode and Claude Code providers
- External provider protocol for custom agents
- `sesh ask` for natural language session queries (two-pass LLM)
- `sesh recap` for time-range session summaries
- `sesh index` for bulk summary generation
- LLM fallback chains across subcommands
- Shell wrapper for in-shell session resumption

[Unreleased]: https://github.com/dru89/sesh/compare/v2.2.0...HEAD
[2.2.0]: https://github.com/dru89/sesh/compare/v2.1.0...v2.2.0
[2.1.0]: https://github.com/dru89/sesh/compare/v2.0.0...v2.1.0
[2.0.0]: https://github.com/dru89/sesh/compare/v1.1.1...v2.0.0
[1.1.1]: https://github.com/dru89/sesh/compare/v1.1.0...v1.1.1
[1.1.0]: https://github.com/dru89/sesh/compare/v1.0.0...v1.1.0
[1.0.0]: https://github.com/dru89/sesh/compare/v0.13.0...v1.0.0
[0.13.0]: https://github.com/dru89/sesh/compare/v0.12.0...v0.13.0
[0.12.0]: https://github.com/dru89/sesh/compare/v0.11.4...v0.12.0
[0.11.4]: https://github.com/dru89/sesh/compare/v0.11.0...v0.11.4
[0.11.0]: https://github.com/dru89/sesh/compare/v0.10.0...v0.11.0
[0.10.0]: https://github.com/dru89/sesh/compare/v0.9.3...v0.10.0
[0.9.3]: https://github.com/dru89/sesh/compare/v0.8.0...v0.9.3
[0.8.0]: https://github.com/dru89/sesh/compare/v0.7.0...v0.8.0
[0.7.0]: https://github.com/dru89/sesh/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/dru89/sesh/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/dru89/sesh/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/dru89/sesh/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/dru89/sesh/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/dru89/sesh/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/dru89/sesh/releases/tag/v0.1.0
