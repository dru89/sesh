# Changelog

## [Unreleased]

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

[Unreleased]: https://github.com/dru89/sesh/compare/v1.0.0...HEAD
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
