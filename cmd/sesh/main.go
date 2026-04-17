package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/dru89/sesh/agent"
	"github.com/dru89/sesh/provider"
	"github.com/dru89/sesh/summary"
	"github.com/dru89/sesh/tui"
	"github.com/dru89/sesh/update"
	"golang.org/x/term"
)

// version and commit are set at build time via ldflags.
var version = "dev"
var commit = ""

// versionString returns the display version, including the commit SHA
// if available. For release builds, GoReleaser injects both via ldflags.
// For local builds, Go embeds VCS info automatically via runtime/debug.
func versionString() string {
	c := commit
	dirty := false
	if c == "" {
		if info, ok := debug.ReadBuildInfo(); ok {
			for _, s := range info.Settings {
				switch s.Key {
				case "vcs.revision":
					if len(s.Value) >= 7 {
						c = s.Value[:7]
					} else {
						c = s.Value
					}
				case "vcs.modified":
					dirty = s.Value == "true"
				}
			}
		}
	}
	if c != "" {
		if dirty {
			return version + " (" + c + "-dirty)"
		}
		return version + " (" + c + ")"
	}
	return version
}

// config is the user configuration loaded from ~/.config/sesh/config.json.
type config struct {
	Schema    string                    `json:"$schema,omitempty"` // JSON Schema ref, ignored by sesh
	Env       map[string]string         `json:"env,omitempty"`     // top-level env applied to all commands
	Providers map[string]providerConfig `json:"providers"`
	Index     commandConfig             `json:"index"`
	Ask       askConfig                 `json:"ask"`
	Recap     commandConfig             `json:"recap"`
}

// commandConfig holds a command + prompt pair for a subcommand.
type commandConfig struct {
	Command []string          `json:"command,omitempty"`
	Prompt  string            `json:"prompt,omitempty"`
	Env     map[string]string `json:"env,omitempty"` // per-command env overrides top-level
}

// askConfig extends commandConfig with a separate filter command.
type askConfig struct {
	Command       []string          `json:"command,omitempty"`
	Prompt        string            `json:"prompt,omitempty"`
	Env           map[string]string `json:"env,omitempty"` // env for ask.command
	FilterCommand []string          `json:"filter_command,omitempty"`
	FilterEnv     map[string]string `json:"filter_env,omitempty"` // env for ask.filter_command
}

// commandWithEnv pairs a command with its per-slot environment overrides.
type commandWithEnv struct {
	command []string
	env     map[string]string
}

// resolveCommand walks a fallback chain and returns the first non-empty command
// along with its associated env.
func resolveCommand(candidates ...commandWithEnv) ([]string, map[string]string) {
	for _, c := range candidates {
		if len(c.command) > 0 {
			return c.command, c.env
		}
	}
	return nil, nil
}

// indexCommand returns the resolved command for title generation (sesh index).
// Fallback: index -> recap -> ask -> ask.filter_command
func (c config) indexCommand() ([]string, map[string]string) {
	return resolveCommand(
		commandWithEnv{c.Index.Command, c.Index.Env},
		commandWithEnv{c.Recap.Command, c.Recap.Env},
		commandWithEnv{c.Ask.Command, c.Ask.Env},
		commandWithEnv{c.Ask.FilterCommand, c.Ask.FilterEnv},
	)
}

// askCommand returns the resolved command for prose generation (sesh ask pass 2).
// Fallback: ask -> recap -> index
func (c config) askCommand() ([]string, map[string]string) {
	return resolveCommand(
		commandWithEnv{c.Ask.Command, c.Ask.Env},
		commandWithEnv{c.Recap.Command, c.Recap.Env},
		commandWithEnv{c.Index.Command, c.Index.Env},
	)
}

// askFilterCommand returns the resolved command for session filtering (sesh ask pass 1, AI fallback).
// Fallback: ask.filter_command -> index -> ask -> recap
func (c config) askFilterCommand() ([]string, map[string]string) {
	return resolveCommand(
		commandWithEnv{c.Ask.FilterCommand, c.Ask.FilterEnv},
		commandWithEnv{c.Index.Command, c.Index.Env},
		commandWithEnv{c.Ask.Command, c.Ask.Env},
		commandWithEnv{c.Recap.Command, c.Recap.Env},
	)
}

// recapCommand returns the resolved command for recap prose generation.
// Fallback: recap -> ask -> index
func (c config) recapCommand() ([]string, map[string]string) {
	return resolveCommand(
		commandWithEnv{c.Recap.Command, c.Recap.Env},
		commandWithEnv{c.Ask.Command, c.Ask.Env},
		commandWithEnv{c.Index.Command, c.Index.Env},
	)
}

// indexPrompt returns the prompt for title generation.
func (c config) indexPrompt() string {
	if c.Index.Prompt != "" {
		return c.Index.Prompt
	}
	return ""
}

// buildEnv merges the top-level env with per-command env overrides and returns
// a slice suitable for exec.Cmd.Env. Returns nil if no env overrides are
// configured, which causes exec.Cmd to inherit the parent process environment.
// Merge order: process env < top-level env < per-command env.
func (c config) buildEnv(commandEnv map[string]string) []string {
	if len(c.Env) == 0 && len(commandEnv) == 0 {
		return nil
	}

	// Merge top-level and command-level (command wins).
	merged := make(map[string]string, len(c.Env)+len(commandEnv))
	for k, v := range c.Env {
		merged[k] = v
	}
	for k, v := range commandEnv {
		merged[k] = v
	}

	// Start from the parent process env, remove keys we're overriding,
	// then append the merged overrides.
	base := os.Environ()
	n := 0
	for _, e := range base {
		k, _, _ := strings.Cut(e, "=")
		if _, override := merged[k]; !override {
			base[n] = e
			n++
		}
	}
	base = base[:n]
	for k, v := range merged {
		base = append(base, k+"="+v)
	}
	return base
}

// hasAnyCommand returns true if any LLM command is configured.
func (c config) hasAnyCommand() bool {
	cmd, _ := c.indexCommand()
	return len(cmd) > 0
}

// summaryConfig builds a summary.Config from the resolved index command/prompt.
func (c config) summaryConfig() summary.Config {
	cmd, env := c.indexCommand()
	return summary.Config{
		Command: cmd,
		Prompt:  c.indexPrompt(),
		Env:     c.buildEnv(env),
	}
}

type providerConfig struct {
	// ResumeCommand overrides the default resume command for a built-in provider.
	// Accepts a string ("opencode -s {{ID}}") or an array (["opencode", "-s", "{{ID}}"]).
	// Use {{ID}} as a placeholder for the session ID.
	ResumeCommand json.RawMessage `json:"resume_command,omitempty"`

	// Enabled controls whether this provider is active (default: true).
	// Set to false to disable a built-in provider.
	Enabled *bool `json:"enabled,omitempty"`

	// ListCommand is the command to run to list sessions (external providers only).
	// Array of executable + arguments.
	ListCommand []string `json:"list_command,omitempty"`

	// Env sets environment variables for list_command execution.
	// Overrides top-level env for this provider.
	Env map[string]string `json:"env,omitempty"`
}

// resumeCommandStr parses resume_command from either string or array form.
// String form is a raw shell expression — the user handles quoting.
// Array form is structured — sesh shell-quotes each element individually,
// preserving argument boundaries. Template markers ({{ID}}, {{DIR}}) in
// array elements are left unquoted since they expand to safe values.
func (pc providerConfig) resumeCommandStr() string {
	if len(pc.ResumeCommand) == 0 {
		return ""
	}
	// Try string first — raw shell expression, pass through as-is.
	var s string
	if err := json.Unmarshal(pc.ResumeCommand, &s); err == nil {
		return s
	}
	// Try array — shell-quote each element to preserve argument boundaries.
	var arr []string
	if err := json.Unmarshal(pc.ResumeCommand, &arr); err == nil && len(arr) > 0 {
		parts := make([]string, len(arr))
		for i, a := range arr {
			if strings.Contains(a, "{{ID}}") || strings.Contains(a, "{{DIR}}") {
				// Template markers expand to safe values (alphanumeric IDs,
				// paths that get their own cd prefix). Quote the static
				// portions around the marker.
				parts[i] = a
			} else {
				parts[i] = provider.Q(a)
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}

func (pc providerConfig) isEnabled() bool {
	if pc.Enabled == nil {
		return true
	}
	return *pc.Enabled
}

// jsonSession extends provider.Session with the resume command for JSON output.
type jsonSession struct {
	provider.Session
	ResumeCommand string `json:"resume_command"`
}

func main() {
	// Check for subcommands before flag parsing.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "index":
			runIndex(os.Args[2:])
			return
		case "recap":
			runRecap(os.Args[2:])
			return
		case "ask":
			runAsk(os.Args[2:])
			return
		case "init":
			runInit(os.Args[2:])
			return
		case "list":
			runList(os.Args[2:])
			return
		case "show":
			runShow(os.Args[2:])
			return
		case "resume":
			runResume(os.Args[2:])
			return
		case "stats":
			runStats(os.Args[2:])
			return
		case "version":
			fmt.Printf("sesh %s %s/%s\n", versionString(), runtime.GOOS, runtime.GOARCH)
			return
		case "update":
			runUpdate()
			return
		}
	}

	jsonMode := flag.Bool("json", false, "Output session list as JSON and exit")
	aiSearch := flag.String("ai-search", "", "AI-ranked search query (use with --json)")
	agentFilter := flag.String("agent", "", "Filter by agent name (or use agent: in search)")
	dirFilter := flag.String("dir", "", "Filter by directory path (or use dir: in search)")
	cwdFlag := flag.Bool("cwd", false, "Filter to current working directory")
	repoFlag := flag.Bool("repo", false, "Filter to the git repository root")
	since := flag.String("since", "", "Only show sessions since date (e.g. 'monday', '2026-04-01', '3d')")
	limit := flag.Int("n", 0, "Maximum number of sessions to show")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: sesh [options] [query]\n\n")
		fmt.Fprintf(os.Stderr, "A unified session browser for coding agents.\n\n")
		fmt.Fprintf(os.Stderr, "Commands:\n")
		fmt.Fprintf(os.Stderr, "  init     Output shell wrapper for your shell\n")
		fmt.Fprintf(os.Stderr, "  list     List sessions (non-interactive)\n")
		fmt.Fprintf(os.Stderr, "  show     Show details for a session\n")
		fmt.Fprintf(os.Stderr, "  resume   Resume a session by ID\n")
		fmt.Fprintf(os.Stderr, "  stats    Show session statistics\n")
		fmt.Fprintf(os.Stderr, "  index    Generate summaries for all sessions\n")
		fmt.Fprintf(os.Stderr, "  recap    Summarize what you worked on over a time period\n")
		fmt.Fprintf(os.Stderr, "  ask      Ask a natural language question about your sessions\n")
		fmt.Fprintf(os.Stderr, "  update   Update sesh to the latest version\n")
		fmt.Fprintf(os.Stderr, "  version  Print version information\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nSearch prefixes:\n")
		fmt.Fprintf(os.Stderr, "  dir:<path>    Filter by directory (fuzzy match)\n")
		fmt.Fprintf(os.Stderr, "  agent:<name>  Filter by agent name (fuzzy match)\n")
		fmt.Fprintf(os.Stderr, "\nConfig: ~/.config/sesh/config.json\n")
		fmt.Fprintf(os.Stderr, "\nShell wrapper (add to your shell rc):\n")
		fmt.Fprintf(os.Stderr, "  eval \"$(sesh init bash)\"   # or zsh, fish, powershell\n")
	}
	flag.Parse()

	// Resolve directory flags.
	*dirFilter = resolveDir(*dirFilter, *cwdFlag, *repoFlag)

	// Build the initial query from flags + positional args.
	positionalQuery := strings.Join(flag.Args(), " ")
	query := tui.BuildPrefixQuery(*dirFilter, *agentFilter, positionalQuery)

	cfg := loadConfig()
	providers := buildProviders(cfg)
	cache := summary.NewCache()

	// Collect sessions from all providers (filtering is handled by the query system).
	ctx := context.Background()
	all := collectSessions(ctx, providers, "")

	// Apply cached summaries to sessions.
	applySummaries(all, cache)

	// Sort by last used, newest first.
	sort.Slice(all, func(i, j int) bool {
		return all[i].LastUsed.After(all[j].LastUsed)
	})

	// Apply time filter.
	if *since != "" {
		cutoff := parseDateish(*since, time.Now())
		filtered := all[:0]
		for _, s := range all {
			if s.LastUsed.After(cutoff) || s.LastUsed.Equal(cutoff) {
				filtered = append(filtered, s)
			}
		}
		all = filtered
	}

	// Apply limit.
	if *limit > 0 && *limit < len(all) {
		all = all[:*limit]
	}

	// JSON mode: dump and exit.
	if *jsonMode {
		sessions := all

		// Apply structured query filters (dir:, agent:, freeform text).
		if query != "" {
			pq := tui.ParseQuery(query)
			sessions = tui.FilterSessions(sessions, pq)
		}

		// AI search: filter through LLM if query provided.
		if *aiSearch != "" {
			filterCmd, filterCmdEnv := cfg.askFilterCommand()
			if len(filterCmd) == 0 {
				fmt.Fprintf(os.Stderr, "sesh: --ai-search requires an LLM command to be configured\n")
				os.Exit(1)
			}
			sessions = aiFilterSessions(ctx, filterCmd, cfg.buildEnv(filterCmdEnv), *aiSearch, sessions, 10)
		}

		pMap := providersByName(providers)
		out := make([]jsonSession, 0) // always encode as [] not null
		for _, s := range sessions {
			var cmd string
			if p, ok := pMap[s.Agent]; ok {
				cmd = p.ResumeCommand(s)
			}
			out = append(out, jsonSession{Session: s, ResumeCommand: cmd})
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			fmt.Fprintf(os.Stderr, "sesh: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if len(all) == 0 {
		fmt.Fprintf(os.Stderr, "sesh: no sessions found\n")
		os.Exit(1)
	}

	// Cache warming hint: if many sessions lack summaries and an LLM is configured.
	if cfg.hasAnyCommand() {
		unsummarized := 0
		for _, s := range all {
			if s.Summary == "" && s.SearchText != "" {
				unsummarized++
			}
		}
		if unsummarized > 20 {
			fmt.Fprintf(os.Stderr, "sesh: %d sessions without summaries. Run 'sesh index' to generate them.\n", unsummarized)
		}
	}

	// Background version check (non-blocking, cached for 24h).
	go checkVersionBackground()

	// Kick off lazy background summary generation for unsummarized sessions.
	if cfg.hasAnyCommand() {
		go lazyIndex(ctx, cfg.summaryConfig(), cache, all, providers)
	}

	// Run the TUI picker.
	providerMap := providersByName(providers)
	pickOpts := tui.PickOptions{
		InitialQuery: query,
		SessionText: func(agent, sessionID string) string {
			if p, ok := providerMap[agent]; ok {
				return p.SessionText(ctx, sessionID)
			}
			return ""
		},
	}
	if filterCmd, filterCmdEnv := cfg.askFilterCommand(); len(filterCmd) > 0 {
		pickOpts.FallbackSearch = buildFallbackSearch(filterCmd, cfg.buildEnv(filterCmdEnv))
	}

	result, err := tui.Pick(all, pickOpts)
	if err != nil {
		// Save any summaries generated in the background before exiting.
		cache.Save()
		os.Exit(130)
	}

	// Save any summaries generated in the background.
	cache.Save()

	// Find the provider and output the resume command.
	// When invoked through the shell wrapper (SESH_WRAPPER=1), prefix
	// with __sesh_eval: so the wrapper knows to eval rather than print.
	// When run without the wrapper, print the raw command so the user
	// can copy-paste it.
	if p, ok := providerMap[result.Session.Agent]; ok {
		cmd := p.ResumeCommand(result.Session)
		if os.Getenv("SESH_WRAPPER") != "" {
			fmt.Println("__sesh_eval:" + cmd)
		} else {
			fmt.Println(cmd)
		}
	} else {
		fmt.Fprintf(os.Stderr, "sesh: unknown provider %q\n", result.Session.Agent)
		os.Exit(1)
	}
}

// runIndex handles the `sesh index` subcommand.
func runIndex(args []string) {
	fs := flag.NewFlagSet("index", flag.ExitOnError)
	agentFilter := fs.String("agent", "", "Only index sessions for a specific agent")
	clearFlag := fs.Bool("clear", false, "Clear all cached summaries before indexing")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: sesh index [options]\n\n")
		fmt.Fprintf(os.Stderr, "Generate summaries for all sessions that don't have one.\n")
		fmt.Fprintf(os.Stderr, "Requires summary.command to be configured.\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		fs.PrintDefaults()
	}
	fs.Parse(args)

	cfg := loadConfig()
	if !cfg.hasAnyCommand() {
		fmt.Fprintf(os.Stderr, "sesh: no LLM command configured\n")
		fmt.Fprintf(os.Stderr, "sesh: add an \"index\" section to ~/.config/sesh/config.json:\n")
		fmt.Fprintf(os.Stderr, "  {\n    \"index\": {\n      \"command\": [\"llm\", \"-m\", \"haiku\"]\n    }\n  }\n")
		os.Exit(1)
	}

	providers := buildProviders(cfg)
	cache := summary.NewCache()

	if *clearFlag {
		cache.Clear()
		if err := cache.Save(); err != nil {
			fmt.Fprintf(os.Stderr, "sesh: warning: failed to save cleared cache: %v\n", err)
		}
		fmt.Fprintf(os.Stderr, "Cleared summary cache.\n")
	}

	ctx := context.Background()

	all := collectSessions(ctx, providers, *agentFilter)
	providerMap := providersByName(providers)

	// Find sessions that need summaries.
	var refs []summary.SessionRef
	for _, s := range all {
		refs = append(refs, summary.SessionRef{ID: s.ID, LastUsed: s.LastUsed})
	}
	need := cache.NeedsSummary(refs)

	if len(need) == 0 {
		fmt.Fprintf(os.Stderr, "All %d sessions already have summaries.\n", len(all))
		return
	}

	// Build batch items by fetching session text from providers.
	needMap := make(map[string]bool, len(need))
	for _, n := range need {
		needMap[n.ID] = true
	}

	var items []summary.BatchItem
	for _, s := range all {
		if !needMap[s.ID] {
			continue
		}
		p, ok := providerMap[s.Agent]
		if !ok {
			continue
		}
		text := p.SessionText(ctx, s.ID)
		if text == "" {
			// No session text available — skip rather than summarizing the
			// bare title, which produces confused LLM output like
			// "I don't have access to this session."
			continue
		}
		items = append(items, summary.BatchItem{
			ID:       s.ID,
			LastUsed: s.LastUsed,
			Text:     text,
		})
	}

	skipped := len(need) - len(items)
	if skipped > 0 {
		fmt.Fprintf(os.Stderr, "Generating summaries for %d/%d sessions (%d skipped — no session text)...\n", len(items), len(all), skipped)
	} else {
		fmt.Fprintf(os.Stderr, "Generating summaries for %d/%d sessions...\n", len(items), len(all))
	}

	gen := summary.NewGenerator(cfg.summaryConfig())
	succeeded := gen.GenerateBatch(ctx, items, cache, func(i, total int, id string, err error) {
		if err != nil {
			// Clear the progress line, print error, then continue progress below.
			fmt.Fprintf(os.Stderr, "\r\033[K\033[31m  [%d/%d] %s: %v\033[0m\n", i, total, id, err)
		}
		fmt.Fprintf(os.Stderr, "\r\033[K  [%d/%d] Generating summaries...", i, total)
	})
	// Clear the progress line.
	fmt.Fprintf(os.Stderr, "\r\033[K")

	if err := cache.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "sesh: warning: failed to save cache: %v\n", err)
	}

	fmt.Fprintf(os.Stderr, "Generated %d summaries (%d failed).\n", succeeded, len(items)-succeeded)
}

// lazyIndex generates summaries for unsummarized sessions in the background.
// It runs during the TUI picker and saves results to the cache.
// Errors are silently ignored — the user will see summaries next time.
func lazyIndex(ctx context.Context, cfg summary.Config, cache *summary.Cache, sessions []provider.Session, providers []provider.Provider) {
	providerMap := providersByName(providers)

	var refs []summary.SessionRef
	for _, s := range sessions {
		refs = append(refs, summary.SessionRef{ID: s.ID, LastUsed: s.LastUsed})
	}
	need := cache.NeedsSummary(refs)
	if len(need) == 0 {
		return
	}

	// Limit background generation to avoid hogging resources.
	limit := 10
	if len(need) > limit {
		need = need[:limit]
	}

	needMap := make(map[string]bool, len(need))
	for _, n := range need {
		needMap[n.ID] = true
	}

	var items []summary.BatchItem
	for _, s := range sessions {
		if !needMap[s.ID] {
			continue
		}
		p, ok := providerMap[s.Agent]
		if !ok {
			continue
		}
		text := p.SessionText(ctx, s.ID)
		if text == "" {
			// No session text available — skip rather than summarizing the
			// bare title, which produces confused LLM output.
			continue
		}
		items = append(items, summary.BatchItem{
			ID:       s.ID,
			LastUsed: s.LastUsed,
			Text:     text,
		})
	}

	gen := summary.NewGenerator(cfg)
	gen.GenerateBatch(ctx, items, cache, nil)
}

// runRecap handles the `sesh recap` subcommand.
func runRecap(args []string) {
	fs := flag.NewFlagSet("recap", flag.ExitOnError)
	since := fs.String("since", "", "Start date (e.g. 'monday', '2026-04-01', '3d')")
	until := fs.String("until", "", "End date (default: now)")
	days := fs.Int("days", 0, "Number of days to look back (shorthand for --since)")
	agentFilter := fs.String("agent", "", "Only include sessions for a specific agent")
	raw := fs.Bool("raw", false, "Output raw markdown without formatting")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: sesh recap [options]\n\n")
		fmt.Fprintf(os.Stderr, "Summarize what you worked on across all agent sessions.\n")
		fmt.Fprintf(os.Stderr, "Requires summary.command to be configured.\n\n")
		fmt.Fprintf(os.Stderr, "Examples:\n")
		fmt.Fprintf(os.Stderr, "  sesh recap --days 7        # last 7 days\n")
		fmt.Fprintf(os.Stderr, "  sesh recap --since monday  # since Monday\n")
		fmt.Fprintf(os.Stderr, "  sesh recap --since 2026-04-01 --until 2026-04-07\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		fs.PrintDefaults()
	}
	fs.Parse(args)

	cfg := loadConfig()
	recapCmd, recapCmdEnv := cfg.recapCommand()
	if len(recapCmd) == 0 {
		fmt.Fprintf(os.Stderr, "sesh: no LLM command configured for recap\n")
		os.Exit(1)
	}

	// Parse time range.
	start, end := parseTimeRange(*since, *until, *days)

	providers := buildProviders(cfg)
	cache := summary.NewCache()
	ctx := context.Background()

	all := collectSessions(ctx, providers, *agentFilter)
	applySummaries(all, cache)

	// Filter sessions to the time range.
	var inRange []provider.Session
	for _, s := range all {
		if (s.LastUsed.After(start) || s.LastUsed.Equal(start)) &&
			(s.LastUsed.Before(end) || s.LastUsed.Equal(end)) {
			inRange = append(inRange, s)
		}
	}

	sort.Slice(inRange, func(i, j int) bool {
		return inRange[i].LastUsed.Before(inRange[j].LastUsed)
	})

	if len(inRange) == 0 {
		fmt.Fprintf(os.Stderr, "No sessions found in the specified time range.\n")
		return
	}

	fmt.Fprintf(os.Stderr, "Found %d sessions from %s to %s. Generating recap...\n",
		len(inRange), start.Format("Jan 2"), end.Format("Jan 2"))

	// Build the recap input: a list of sessions with their summaries/titles,
	// agents, directories, and timestamps.
	var recapInput strings.Builder
	for _, s := range inRange {
		title := s.DisplayTitle()
		dir := s.Directory
		agent := s.Agent
		when := s.LastUsed.Format("Mon Jan 2 3:04pm")
		recapInput.WriteString(fmt.Sprintf("- [%s] %s | %s | %s\n", agent, title, dir, when))
	}

	prompt := fmt.Sprintf(
		"Here are my coding agent sessions from %s to %s. "+
			"Each line has the agent name, a session summary or title, the working directory, and the date.\n\n"+
			"Write a concise recap of what I worked on during this period. "+
			"Group related work together. Focus on what was accomplished, not the tools used.\n\n%s",
		start.Format("Mon Jan 2"), end.Format("Mon Jan 2"), recapInput.String())

	result, err := summary.RunLLM(ctx, recapCmd, cfg.buildEnv(recapCmdEnv), prompt, 60*time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sesh: recap failed: %v\n", err)
		os.Exit(1)
	}

	if !*raw && isTerminal() {
		rendered, gErr := glamour.Render(result, "dark")
		if gErr == nil {
			fmt.Print(strings.TrimRight(rendered, "\n"))
			fmt.Println()
			return
		}
	}
	fmt.Println(result)
}

// runAsk handles the `sesh ask` subcommand.
func runAsk(args []string) {
	fs := flag.NewFlagSet("ask", flag.ExitOnError)
	agentFilter := fs.String("agent", "", "Only include sessions for a specific agent")
	dirFilter := fs.String("dir", "", "Filter by directory path (fuzzy match)")
	cwdFlag := fs.Bool("cwd", false, "Filter to current working directory")
	repoFlag := fs.Bool("repo", false, "Filter to the git repository root")
	since := fs.String("since", "", "Only include sessions since date (e.g. 'monday', '2026-04-01', '3d')")
	limit := fs.Int("n", 0, "Maximum number of sessions to consider")
	raw := fs.Bool("raw", false, "Output raw markdown without formatting")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: sesh ask [options] <question>\n\n")
		fmt.Fprintf(os.Stderr, "Ask a natural language question about your coding sessions.\n")
		fmt.Fprintf(os.Stderr, "Requires summary.command to be configured.\n\n")
		fmt.Fprintf(os.Stderr, "Examples:\n")
		fmt.Fprintf(os.Stderr, "  sesh ask \"What did I work on with auth last week?\"\n")
		fmt.Fprintf(os.Stderr, "  sesh ask \"Show me everything related to the API gateway\"\n")
		fmt.Fprintf(os.Stderr, "  sesh ask --agent claude \"What refactoring have I done recently?\"\n")
		fmt.Fprintf(os.Stderr, "  sesh ask --repo \"What changes have I made in this project?\"\n")
		fmt.Fprintf(os.Stderr, "  sesh ask --since 3d \"What did I work on recently?\"\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		fs.PrintDefaults()
	}
	fs.Parse(args)

	// Resolve directory flags.
	*dirFilter = resolveDir(*dirFilter, *cwdFlag, *repoFlag)

	question := strings.Join(fs.Args(), " ")
	if question == "" {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			fs.Usage()
			os.Exit(1)
		}
		var err error
		question, err = tui.PromptInput("Ask:")
		if err != nil {
			fmt.Fprintf(os.Stderr, "sesh: %v\n", err)
			os.Exit(1)
		}
		if question == "" {
			os.Exit(0)
		}
	}

	cfg := loadConfig()
	askCmd, askCmdEnv := cfg.askCommand()
	filterCmd, filterCmdEnv := cfg.askFilterCommand()
	if len(askCmd) == 0 && len(filterCmd) == 0 {
		fmt.Fprintf(os.Stderr, "sesh: no LLM command configured for ask\n")
		os.Exit(1)
	}
	// If only one is available, use it for both.
	if len(filterCmd) == 0 {
		filterCmd = askCmd
		filterCmdEnv = askCmdEnv
	}
	if len(askCmd) == 0 {
		askCmd = filterCmd
		askCmdEnv = filterCmdEnv
	}
	filterEnv := cfg.buildEnv(filterCmdEnv)
	askEnv := cfg.buildEnv(askCmdEnv)

	providers := buildProviders(cfg)
	cache := summary.NewCache()
	ctx := context.Background()

	all := collectSessions(ctx, providers, *agentFilter)
	applySummaries(all, cache)

	sort.Slice(all, func(i, j int) bool {
		return all[i].LastUsed.After(all[j].LastUsed)
	})

	// Apply structured filters (agent, dir) via the query system.
	if *dirFilter != "" {
		query := tui.BuildPrefixQuery(*dirFilter, "", "")
		pq := tui.ParseQuery(query)
		all = tui.FilterSessions(all, pq)
	}

	// Apply time filter.
	if *since != "" {
		cutoff := parseDateish(*since, time.Now())
		filtered := all[:0]
		for _, s := range all {
			if s.LastUsed.After(cutoff) || s.LastUsed.Equal(cutoff) {
				filtered = append(filtered, s)
			}
		}
		all = filtered
	}

	// Apply limit.
	if *limit > 0 && *limit < len(all) {
		all = all[:*limit]
	}

	if len(all) == 0 {
		fmt.Fprintf(os.Stderr, "No sessions found.\n")
		return
	}

	// Regenerate stale summaries before filtering so pass 1 has current titles.
	sumCfg := cfg.summaryConfig()
	if sumCfg.IsEnabled() {
		providerMap := providersByName(providers)
		var refs []summary.SessionRef
		for _, s := range all {
			refs = append(refs, summary.SessionRef{ID: s.ID, LastUsed: s.LastUsed})
		}
		stale := cache.NeedsSummary(refs)
		if len(stale) > 0 {
			gen := summary.NewGenerator(sumCfg)
			var items []summary.BatchItem
			for _, ref := range stale {
				// Find the session to get its agent for text lookup.
				for _, s := range all {
					if s.ID == ref.ID {
						if p, ok := providerMap[s.Agent]; ok {
							text := p.SessionText(ctx, s.ID)
							if text != "" {
								items = append(items, summary.BatchItem{
									ID:       s.ID,
									LastUsed: s.LastUsed,
									Text:     text,
								})
							}
						}
						break
					}
				}
			}
			if len(items) > 0 {
				fmt.Fprintf(os.Stderr, "Updating %d stale summaries...", len(items))
				gen.GenerateBatch(ctx, items, cache, func(i, total int, id string, err error) {
					if err != nil {
						fmt.Fprintf(os.Stderr, "\r\033[K\033[31m  [%d/%d] %s: %v\033[0m\n", i, total, id, err)
					}
					fmt.Fprintf(os.Stderr, "\r\033[K  [%d/%d] Updating summaries...", i, total)
				})
				fmt.Fprintf(os.Stderr, "\r\033[K")
				cache.Save()
				// Re-apply summaries so DisplayTitle() reflects the new ones.
				applySummaries(all, cache)
			}
		}
	}

	// --- Pass 1: Filter — ask the LLM which sessions are relevant. ---

	fmt.Fprintf(os.Stderr, "Searching %d sessions...\n", len(all))

	relevant := aiFilterSessions(ctx, filterCmd, filterEnv, question, all, 20)

	if len(relevant) == 0 {
		fmt.Fprintf(os.Stderr, "No relevant sessions found for that question.\n")
		return
	}

	fmt.Fprintf(os.Stderr, "Found %d relevant sessions. Generating answer...\n", len(relevant))

	// --- Pass 2: Generate — answer the question using only the relevant sessions. ---

	providerMap2 := providersByName(providers)

	var detailList strings.Builder
	for _, s := range relevant {
		title := s.DisplayTitle()
		when := s.LastUsed.Format("Mon Jan 2 3:04pm")
		relWhen := provider.RelativeTime(s.LastUsed)
		var sessionText string
		if p, ok := providerMap2[s.Agent]; ok {
			sessionText = provider.ExcerptBookends(p.SessionText(ctx, s.ID), 5000)
		}
		detailList.WriteString(fmt.Sprintf("- [%s] %s | %s | %s (%s) | id: %s | resume: `sesh resume %s`\n",
			s.Agent, title, s.Directory, when, relWhen, s.ID, s.ID))
		if sessionText != "" {
			detailList.WriteString(fmt.Sprintf("  Conversation excerpt:\n  %s\n",
				strings.ReplaceAll(sessionText, "\n", "\n  ")))
		}
	}

	answerPrompt := fmt.Sprintf(
		"Here are my coding agent sessions relevant to my question. "+
			"Each session has the agent name, session summary/title, working directory, "+
			"date (with relative time), session ID, resume command, and an excerpt from the conversation.\n\n"+
			"%s\n"+
			"My question: %s\n\n"+
			"Answer my question based on these sessions. Be specific about what was worked on.\n\n"+
			"When referencing a session, use exactly this format:\n\n"+
			"### Session title here\n"+
			"[agent] · 3 days ago (Wed Apr 15 4:30pm)\n\n"+
			"```\nsesh resume ses_abc123\n```\n\n"+
			"Description of what was worked on...\n\n"+
			"Use a horizontal rule (---) between sessions if you reference more than one.",
		detailList.String(), question)

	result, err := summary.RunLLM(ctx, askCmd, askEnv, answerPrompt, 60*time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sesh: ask failed during answer generation: %v\n", err)
		os.Exit(1)
	}

	if !*raw && isTerminal() {
		rendered, gErr := glamour.Render(result, "dark")
		if gErr == nil {
			fmt.Print(strings.TrimRight(rendered, "\n"))
			fmt.Println()
			return
		}
	}
	fmt.Println(result)
}

// runList handles the `sesh list` subcommand.
func runList(args []string) {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	agentFilter := fs.String("agent", "", "Filter by agent name (fuzzy match)")
	dirFilter := fs.String("dir", "", "Filter by directory path (fuzzy match)")
	cwdFlag := fs.Bool("cwd", false, "Filter to current working directory")
	repoFlag := fs.Bool("repo", false, "Filter to the git repository root")
	since := fs.String("since", "", "Only show sessions since date (e.g. 'monday', '2026-04-01', '3d')")
	limit := fs.Int("n", 0, "Maximum number of sessions to show")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: sesh list [options]\n\n")
		fmt.Fprintf(os.Stderr, "List sessions in a non-interactive table format.\n\n")
		fmt.Fprintf(os.Stderr, "Examples:\n")
		fmt.Fprintf(os.Stderr, "  sesh list                    # all sessions\n")
		fmt.Fprintf(os.Stderr, "  sesh list --agent opencode   # only OpenCode\n")
		fmt.Fprintf(os.Stderr, "  sesh list --repo             # sessions for this git repo\n")
		fmt.Fprintf(os.Stderr, "  sesh list --cwd              # sessions for current directory\n")
		fmt.Fprintf(os.Stderr, "  sesh list --dir ~/projects   # sessions matching a directory\n")
		fmt.Fprintf(os.Stderr, "  sesh list --since monday     # since Monday\n")
		fmt.Fprintf(os.Stderr, "  sesh list -n 20              # last 20 sessions\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		fs.PrintDefaults()
	}
	fs.Parse(args)

	// Resolve directory flags.
	*dirFilter = resolveDir(*dirFilter, *cwdFlag, *repoFlag)

	cfg := loadConfig()
	providers := buildProviders(cfg)
	cache := summary.NewCache()
	ctx := context.Background()

	all := collectSessions(ctx, providers, "")
	applySummaries(all, cache)

	sort.Slice(all, func(i, j int) bool {
		return all[i].LastUsed.After(all[j].LastUsed)
	})

	// Apply structured filters (agent, dir) via the query system.
	if *agentFilter != "" || *dirFilter != "" {
		query := tui.BuildPrefixQuery(*dirFilter, *agentFilter, "")
		pq := tui.ParseQuery(query)
		all = tui.FilterSessions(all, pq)
	}

	// Apply time filter.
	if *since != "" {
		cutoff := parseDateish(*since, time.Now())
		filtered := all[:0]
		for _, s := range all {
			if s.LastUsed.After(cutoff) || s.LastUsed.Equal(cutoff) {
				filtered = append(filtered, s)
			}
		}
		all = filtered
	}

	// Apply limit.
	if *limit > 0 && *limit < len(all) {
		all = all[:*limit]
	}

	if len(all) == 0 {
		fmt.Fprintf(os.Stderr, "No sessions found.\n")
		return
	}

	// Version check (synchronous but cached — only hits network once per day).
	checkVersionSync()

	// Detect if stdout is a terminal for color output.
	isTTY := isTerminal()

	// Find the longest agent name for padding.
	maxAgent := 0
	for _, s := range all {
		if len(s.Agent) > maxAgent {
			maxAgent = len(s.Agent)
		}
	}
	if maxAgent < 6 {
		maxAgent = 6
	}

	for _, s := range all {
		title := s.DisplayTitle()
		if len(title) > 60 {
			title = title[:59] + "…"
		}

		when := provider.RelativeTime(s.LastUsed)
		sid := s.ID
		if len(sid) > 12 {
			sid = sid[:12] + "…"
		}

		if isTTY {
			agentStr := colorAgent(s.Agent)
			// Pad after the color codes based on raw agent name length.
			pad := maxAgent - len(s.Agent) + 2
			fmt.Printf("%s%s%-60s  %-8s  %s\n",
				agentStr, strings.Repeat(" ", pad), title, when, sid)
		} else {
			fmt.Printf("%-*s  %-60s  %-8s  %s\n",
				maxAgent, s.Agent, title, when, sid)
		}
	}
}

// colorAgent returns an ANSI-colored agent name for terminal output.
func colorAgent(name string) string {
	// ANSI color codes: 31=red, 32=green, 33=yellow, 34=blue, 35=magenta, 36=cyan.
	return fmt.Sprintf("\033[%dm%s\033[0m", 30+agent.ANSIColor(name), name)
}

// isTerminal checks if stdout is a terminal.
func isTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// runShow handles the `sesh show` subcommand.
func runShow(args []string) {
	fs := flag.NewFlagSet("show", flag.ExitOnError)
	jsonFlag := fs.Bool("json", false, "Output as JSON (includes session text)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: sesh show [--json] <session-id>\n\n")
		fmt.Fprintf(os.Stderr, "Show details for a session. Accepts a full or partial session ID.\n")
	}
	fs.Parse(args)

	query := fs.Arg(0)
	if query == "" {
		fs.Usage()
		os.Exit(1)
	}

	cfg := loadConfig()
	providers := buildProviders(cfg)
	cache := summary.NewCache()
	ctx := context.Background()

	all := collectSessions(ctx, providers, "")
	applySummaries(all, cache)

	// Find session by exact or prefix match.
	match, ambiguous := findSession(all, query)
	if match == nil && len(ambiguous) > 1 {
		fmt.Fprintf(os.Stderr, "Ambiguous session ID %q — matches %d sessions:\n", query, len(ambiguous))
		for _, c := range ambiguous {
			fmt.Fprintf(os.Stderr, "  %s  [%s]  %s\n", c.ID, c.Agent, c.DisplayTitle())
		}
		os.Exit(1)
	}

	if match == nil {
		fmt.Fprintf(os.Stderr, "No session found matching %q\n", query)
		os.Exit(1)
	}

	s := match
	providerMap := providersByName(providers)

	// JSON output mode.
	if *jsonFlag {
		type jsonShowSession struct {
			jsonSession
			Text string `json:"text,omitempty"`
		}
		out := jsonShowSession{
			jsonSession: jsonSession{Session: *s},
		}
		if p, ok := providerMap[s.Agent]; ok {
			out.ResumeCommand = p.ResumeCommand(*s)
			out.Text = p.SessionText(ctx, s.ID)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetEscapeHTML(false)
		enc.Encode(out)
		return
	}

	// Print metadata.
	isTTY := isTerminal()
	printField := func(label, value string) {
		if isTTY {
			fmt.Printf("\033[1m%-12s\033[0m %s\n", label+":", value)
		} else {
			fmt.Printf("%-12s %s\n", label+":", value)
		}
	}

	printField("Agent", s.Agent)
	printField("Session ID", s.ID)
	if s.Slug != "" {
		printField("Slug", s.Slug)
	}
	printField("Title", s.Title)
	if s.Summary != "" {
		printField("Summary", s.Summary)
	}
	if s.Directory != "" {
		printField("Directory", s.Directory)
	}
	printField("Created", s.Created.Format("Mon Jan 2, 2006 3:04pm"))
	printField("Last Used", s.LastUsed.Format("Mon Jan 2, 2006 3:04pm")+" ("+provider.RelativeTime(s.LastUsed)+")")

	if p, ok := providerMap[s.Agent]; ok {
		printField("Resume", p.ResumeCommand(*s))
	}

	// Print first messages if available.
	if p, ok := providerMap[s.Agent]; ok {
		text := p.SessionText(ctx, s.ID)
		if text != "" {
			fmt.Println()
			if isTTY {
				fmt.Println("\033[1mFirst messages:\033[0m")
			} else {
				fmt.Println("First messages:")
			}
			// Truncate to ~1000 chars for display.
			if len(text) > 1000 {
				text = text[:997] + "..."
			}
			if isTTY {
				rendered, err := glamour.Render(text, "dark")
				if err == nil {
					fmt.Print(strings.TrimRight(rendered, "\n"))
				} else {
					fmt.Println(text)
				}
			} else {
				fmt.Println(text)
			}
		}
	}
}

// runResume handles the `sesh resume` subcommand.
func runResume(args []string) {
	fs := flag.NewFlagSet("resume", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: sesh resume <session-id>\n\n")
		fmt.Fprintf(os.Stderr, "Resume a session by ID (partial ID works).\n\n")
		fmt.Fprintf(os.Stderr, "Examples:\n")
		fmt.Fprintf(os.Stderr, "  sesh resume ses_abc123\n")
		fmt.Fprintf(os.Stderr, "  sesh resume abc\n")
	}
	fs.Parse(args)

	if fs.NArg() == 0 {
		fs.Usage()
		os.Exit(1)
	}
	query := fs.Arg(0)

	cfg := loadConfig()
	providers := buildProviders(cfg)
	cache := summary.NewCache()
	ctx := context.Background()

	all := collectSessions(ctx, providers, "")
	applySummaries(all, cache)

	s, candidates := findSession(all, query)
	if s == nil && candidates == nil {
		fmt.Fprintf(os.Stderr, "sesh: no session matching %q\n", query)
		os.Exit(1)
	}
	if s == nil {
		fmt.Fprintf(os.Stderr, "sesh: ambiguous session ID %q — matches %d sessions:\n", query, len(candidates))
		for _, c := range candidates {
			fmt.Fprintf(os.Stderr, "  %s  %s  %s\n", c.ID, c.Agent, c.DisplayTitle())
		}
		os.Exit(1)
	}

	providerMap := providersByName(providers)
	p, ok := providerMap[s.Agent]
	if !ok {
		fmt.Fprintf(os.Stderr, "sesh: unknown provider %q\n", s.Agent)
		os.Exit(1)
	}

	cmd := p.ResumeCommand(*s)
	if os.Getenv("SESH_WRAPPER") != "" {
		fmt.Println("__sesh_eval:" + cmd)
	} else {
		fmt.Println(cmd)
	}
}

// runStats handles the `sesh stats` subcommand.
func runStats(args []string) {
	fs := flag.NewFlagSet("stats", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: sesh stats\n\n")
		fmt.Fprintf(os.Stderr, "Show session statistics across all agents.\n")
	}
	fs.Parse(args)

	cfg := loadConfig()
	providers := buildProviders(cfg)
	cache := summary.NewCache()
	ctx := context.Background()

	all := collectSessions(ctx, providers, "")
	applySummaries(all, cache)

	if len(all) == 0 {
		fmt.Println("No sessions found.")
		return
	}

	sort.Slice(all, func(i, j int) bool {
		return all[i].LastUsed.After(all[j].LastUsed)
	})

	stats := computeStats(all)

	// Top directories sorted by count.
	type dirEntry struct {
		dir   string
		count int
	}
	var topDirs []dirEntry
	for d, c := range stats.DirCounts {
		topDirs = append(topDirs, dirEntry{d, c})
	}
	sort.Slice(topDirs, func(i, j int) bool {
		return topDirs[i].count > topDirs[j].count
	})

	isTTY := isTerminal()
	heading := func(title string) {
		if isTTY {
			fmt.Printf("\n\033[1m%s\033[0m\n", title)
		} else {
			fmt.Printf("\n%s\n", title)
		}
	}

	fmt.Printf("Total sessions: %d\n", stats.Total)
	fmt.Printf("Summarized:     %d/%d", stats.Summarized, stats.Total)
	if stats.Total > 0 {
		fmt.Printf(" (%d%%)", stats.Summarized*100/stats.Total)
	}
	fmt.Println()

	heading("By agent")
	type agentEntry struct {
		name  string
		count int
	}
	var agents []agentEntry
	for a, c := range stats.AgentCounts {
		agents = append(agents, agentEntry{a, c})
	}
	sort.Slice(agents, func(i, j int) bool {
		return agents[i].count > agents[j].count
	})
	for _, a := range agents {
		if isTTY {
			// ANSI codes add 9 bytes of invisible chars; pad extra to compensate.
			fmt.Printf("  %-21s %d\n", colorAgent(a.name), a.count)
		} else {
			fmt.Printf("  %-12s %d\n", a.name, a.count)
		}
	}

	heading("By time")
	fmt.Printf("  Today:      %d\n", stats.Today)
	fmt.Printf("  This week:  %d\n", stats.ThisWeek)
	fmt.Printf("  This month: %d\n", stats.ThisMonth)
	fmt.Printf("  Oldest:     %s\n", stats.Oldest.Format("Jan 2, 2006"))

	heading("Top directories")
	limit := 5
	if len(topDirs) < limit {
		limit = len(topDirs)
	}
	for _, d := range topDirs[:limit] {
		dir := d.dir
		if home, err := os.UserHomeDir(); err == nil {
			dir = strings.Replace(dir, home, "~", 1)
		}
		fmt.Printf("  %-50s %d sessions\n", dir, d.count)
	}

	heading("Recent")
	recentLimit := 5
	if len(all) < recentLimit {
		recentLimit = len(all)
	}
	for _, s := range all[:recentLimit] {
		agent := s.Agent
		if isTTY {
			agent = colorAgent(agent)
			fmt.Printf("  %-19s %-50s %s\n", agent, truncate(s.DisplayTitle(), 50), provider.RelativeTime(s.LastUsed))
		} else {
			fmt.Printf("  %-10s %-50s %s\n", agent, truncate(s.DisplayTitle(), 50), provider.RelativeTime(s.LastUsed))
		}
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

// runInit handles the `sesh init` subcommand.
func runInit(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: sesh init <shell>\n\n")
		fmt.Fprintf(os.Stderr, "Output the shell wrapper function for sesh.\n\n")
		fmt.Fprintf(os.Stderr, "Supported shells: bash, zsh, fish, powershell\n\n")
		fmt.Fprintf(os.Stderr, "Add to your shell's rc file:\n")
		fmt.Fprintf(os.Stderr, "  bash:       eval \"$(sesh init bash)\"\n")
		fmt.Fprintf(os.Stderr, "  zsh:        eval \"$(sesh init zsh)\"\n")
		fmt.Fprintf(os.Stderr, "  fish:       sesh init fish | source\n")
		fmt.Fprintf(os.Stderr, "  powershell: sesh init powershell | Invoke-Expression\n\n")
		fmt.Fprintf(os.Stderr, "Or append to your rc file directly:\n")
		fmt.Fprintf(os.Stderr, "  sesh init bash >> ~/.bashrc\n")
		fmt.Fprintf(os.Stderr, "  sesh init zsh >> ~/.zshrc\n")
		fmt.Fprintf(os.Stderr, "  sesh init fish >> ~/.config/fish/config.fish\n")
		fmt.Fprintf(os.Stderr, "  sesh init powershell >> $PROFILE\n")
	}
	fs.Parse(args)

	shell := fs.Arg(0)
	if shell == "" {
		// Try to detect from $SHELL.
		shell = detectShell()
	}
	if shell == "" {
		fs.Usage()
		os.Exit(1)
	}

	switch strings.ToLower(shell) {
	case "bash":
		os.Stdout.WriteString(initBash + "\n")
	case "zsh":
		os.Stdout.WriteString(initZsh + "\n")
	case "fish":
		os.Stdout.WriteString(initFish + "\n")
	case "powershell", "pwsh":
		os.Stdout.WriteString(initPowerShell + "\n")
	default:
		fmt.Fprintf(os.Stderr, "sesh: unsupported shell %q\n", shell)
		fmt.Fprintf(os.Stderr, "sesh: supported shells: bash, zsh, fish, powershell\n")
		os.Exit(1)
	}
}

func detectShell() string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		return ""
	}
	base := filepath.Base(shell)
	switch base {
	case "bash", "zsh", "fish":
		return base
	default:
		return ""
	}
}

const initBash = `sesh() {
  # Subcommands never emit __sesh_eval:, so run them directly to preserve
  # TTY (glamour rendering, colors, etc.).
  case "${1-}" in
    index|recap|ask|init|list|show|stats|version|update)
      command sesh "$@"
      return $?
      ;;
  esac

  # Root picker: capture output so we can eval resume commands.
  local out
  out=$(SESH_WRAPPER=1 command sesh "$@") || return $?
  if [[ "$out" == __sesh_eval:* ]]; then
    eval "${out#__sesh_eval:}"
  elif [[ -n "$out" ]]; then
    printf '%s\n' "$out"
  fi
}`

const initZsh = `sesh() {
  # Subcommands never emit __sesh_eval:, so run them directly to preserve
  # TTY (glamour rendering, colors, etc.).
  case "${1-}" in
    index|recap|ask|init|list|show|stats|version|update)
      command sesh "$@"
      return $?
      ;;
  esac

  # Root picker: capture output so we can eval resume commands.
  local out
  out=$(SESH_WRAPPER=1 command sesh "$@") || return $?
  if [[ "$out" == __sesh_eval:* ]]; then
    eval "${out#__sesh_eval:}"
  elif [[ -n "$out" ]]; then
    printf '%s\n' "$out"
  fi
}`

const initFish = `function sesh
    # Subcommands never emit __sesh_eval:, so run them directly to preserve
    # TTY (glamour rendering, colors, etc.).
    if set -q argv[1]
        switch $argv[1]
            case index recap ask init list show stats version update
                command sesh $argv
                return $status
        end
    end

    # Root picker: capture output so we can eval resume commands.
    set -l out (SESH_WRAPPER=1 command sesh $argv)
    or return $status
    if string match -q '__sesh_eval:*' -- $out
        eval (string replace -r '^__sesh_eval:' '' -- $out)
    else if test -n "$out"
        echo $out
    end
end`

const initPowerShell = `function sesh {
    # Subcommands never emit __sesh_eval:, so run them directly to preserve
    # TTY (glamour rendering, colors, etc.).
    $passthrough = @('index','recap','ask','init','list','show','stats','version','update')
    if ($args.Count -gt 0 -and $passthrough -contains $args[0]) {
        & sesh.exe @args
        return
    }

    # Root picker: capture output so we can eval resume commands.
    $env:SESH_WRAPPER = '1'
    $output = & sesh.exe @args
    Remove-Item Env:\SESH_WRAPPER
    if ($LASTEXITCODE -ne 0) { return }
    if ($output -and $output.StartsWith('__sesh_eval:')) {
        Invoke-Expression $output.Substring(13)
    } elseif ($output) {
        Write-Output $output
    }
}`

// parseTimeRange interprets the --since, --until, and --days flags.
func parseTimeRange(since, until string, days int) (time.Time, time.Time) {
	now := time.Now()
	end := now

	if until != "" {
		if t, err := time.ParseInLocation("2006-01-02", until, time.Local); err == nil {
			end = t.Add(24*time.Hour - time.Second) // end of day
		}
	}

	var start time.Time
	if days > 0 {
		start = now.AddDate(0, 0, -days)
	} else if since != "" {
		start = parseDateish(since, now)
	} else {
		// Default: last 7 days.
		start = now.AddDate(0, 0, -7)
	}

	return start, end
}

// resolveDirFlags validates that --dir, --cwd, and --repo are mutually
// exclusive, then resolves --cwd and --repo to a directory path. Returns
// the resolved absolute directory path, or "" if no directory filter was
// specified.
func resolveDirFlags(dir string, cwd, repo bool) (string, error) {
	flags := 0
	if dir != "" {
		flags++
	}
	if cwd {
		flags++
	}
	if repo {
		flags++
	}
	if flags > 1 {
		return "", fmt.Errorf("--dir, --cwd, and --repo are mutually exclusive")
	}

	if cwd {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("failed to get working directory: %w", err)
		}
		dir = wd
	}

	if repo {
		root, err := tui.GitRoot()
		if err != nil {
			return "", err
		}
		dir = root
	}

	if dir != "" {
		resolved, err := tui.ResolveDir(dir)
		if err != nil {
			return "", fmt.Errorf("failed to resolve directory %q: %w", dir, err)
		}
		dir = resolved
	}

	return dir, nil
}

// resolveDir calls resolveDirFlags and exits on error.
func resolveDir(dir string, cwd, repo bool) string {
	result, err := resolveDirFlags(dir, cwd, repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sesh: %v\n", err)
		os.Exit(1)
	}
	return result
}

// parseDateish parses flexible date inputs.
func parseDateish(s string, now time.Time) time.Time {
	s = strings.ToLower(strings.TrimSpace(s))

	// Try exact date.
	if t, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
		return t
	}

	// Relative days: "3d", "7d".
	if strings.HasSuffix(s, "d") {
		var d int
		if _, err := fmt.Sscanf(s, "%dd", &d); err == nil {
			return now.AddDate(0, 0, -d)
		}
	}

	// Day names: "monday", "tuesday", etc.
	dayNames := map[string]time.Weekday{
		"sunday": time.Sunday, "monday": time.Monday, "tuesday": time.Tuesday,
		"wednesday": time.Wednesday, "thursday": time.Thursday,
		"friday": time.Friday, "saturday": time.Saturday,
	}
	if target, ok := dayNames[s]; ok {
		current := now.Weekday()
		diff := int(current - target)
		if diff <= 0 {
			diff += 7
		}
		return time.Date(now.Year(), now.Month(), now.Day()-diff, 0, 0, 0, 0, time.Local)
	}

	// Keywords.
	switch s {
	case "today":
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	case "yesterday":
		return time.Date(now.Year(), now.Month(), now.Day()-1, 0, 0, 0, 0, time.Local)
	case "last week":
		return now.AddDate(0, 0, -7)
	}

	// Fallback: 7 days.
	return now.AddDate(0, 0, -7)
}

// collectSessions gathers sessions from all providers, with warnings on failure.
// Each provider gets a 30-second timeout to prevent hung external scripts from
// blocking sesh indefinitely.
func collectSessions(ctx context.Context, providers []provider.Provider, agentFilter string) []provider.Session {
	var (
		mu  sync.Mutex
		all []provider.Session
		wg  sync.WaitGroup
	)
	for _, p := range providers {
		if agentFilter != "" && p.Name() != agentFilter {
			continue
		}
		wg.Add(1)
		go func(p provider.Provider) {
			defer wg.Done()
			pctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			sessions, err := p.ListSessions(pctx)
			if err != nil {
				fmt.Fprintf(os.Stderr, "sesh: warning: %s: %v\n", p.Name(), err)
				return
			}
			mu.Lock()
			all = append(all, sessions...)
			mu.Unlock()
		}(p)
	}
	wg.Wait()
	return all
}

// applySummaries enriches sessions with cached summaries.
func applySummaries(sessions []provider.Session, cache *summary.Cache) {
	for i := range sessions {
		if s, ok := cache.Get(sessions[i].ID, sessions[i].LastUsed); ok {
			sessions[i].Summary = summary.StripMarkdown(s)
			// Also add summary to search text for fuzzy matching.
			sessions[i].SearchText += " " + sessions[i].Summary
		}
	}
}

func loadConfig() config {
	home, _ := os.UserHomeDir()
	paths := []string{
		filepath.Join(home, ".config", "sesh", "config.json"),
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		paths = append([]string{filepath.Join(xdg, "sesh", "config.json")}, paths...)
	}
	// On Windows, also check %APPDATA%\sesh\config.json.
	if runtime.GOOS == "windows" {
		if appdata := os.Getenv("APPDATA"); appdata != "" {
			paths = append(paths, filepath.Join(appdata, "sesh", "config.json"))
		}
	}

	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var cfg config
		if err := json.Unmarshal(data, &cfg); err != nil {
			fmt.Fprintf(os.Stderr, "sesh: warning: invalid config %s: %v\n", p, err)
			continue
		}
		return cfg
	}

	return config{}
}

func buildProviders(cfg config) []provider.Provider {
	var providers []provider.Provider

	// Built-in: OpenCode.
	// If list_command is set, use it as an external provider instead.
	if oc, ok := cfg.Providers["opencode"]; ok {
		if oc.isEnabled() {
			if len(oc.ListCommand) > 0 {
				providers = append(providers, provider.NewExternal(provider.ExternalConfig{
					Name:          "opencode",
					ListCommand:   oc.ListCommand,
					ResumeCommand: oc.resumeCommandStr(),
					Env:           cfg.buildEnv(oc.Env),
				}))
			} else {
				var opts []provider.OpenCodeOption
				if cmd := oc.resumeCommandStr(); cmd != "" {
					opts = append(opts, provider.WithOpenCodeResumeCommand(cmd))
				}
				providers = append(providers, provider.NewOpenCode(opts...))
			}
		}
	} else {
		providers = append(providers, provider.NewOpenCode())
	}

	// Built-in: Claude Code.
	// If list_command is set, use it as an external provider instead.
	if cc, ok := cfg.Providers["claude"]; ok {
		if cc.isEnabled() {
			if len(cc.ListCommand) > 0 {
				providers = append(providers, provider.NewExternal(provider.ExternalConfig{
					Name:          "claude",
					ListCommand:   cc.ListCommand,
					ResumeCommand: cc.resumeCommandStr(),
					Env:           cfg.buildEnv(cc.Env),
				}))
			} else {
				var opts []provider.ClaudeOption
				if cmd := cc.resumeCommandStr(); cmd != "" {
					opts = append(opts, provider.WithClaudeResumeCommand(cmd))
				}
				providers = append(providers, provider.NewClaude(opts...))
			}
		}
	} else {
		providers = append(providers, provider.NewClaude())
	}

	// External providers: anything in config that isn't a built-in name.
	builtins := map[string]bool{"opencode": true, "claude": true}
	for name, pc := range cfg.Providers {
		if builtins[name] || !pc.isEnabled() {
			continue
		}
		if len(pc.ListCommand) == 0 {
			fmt.Fprintf(os.Stderr, "sesh: warning: external provider %q has no list_command\n", name)
			continue
		}
		providers = append(providers, provider.NewExternal(provider.ExternalConfig{
			Name:          name,
			ListCommand:   pc.ListCommand,
			ResumeCommand: pc.resumeCommandStr(),
			Env:           cfg.buildEnv(pc.Env),
		}))
	}

	return providers
}

func providersByName(providers []provider.Provider) map[string]provider.Provider {
	m := make(map[string]provider.Provider, len(providers))
	for _, p := range providers {
		m[p.Name()] = p
	}
	return m
}

// findSession looks up a session by exact ID or unique prefix match.
// Returns the matched session and nil ambiguous list on success.
// Returns nil session and the ambiguous candidates if multiple match.
// Returns nil, nil if nothing matches.
func findSession(sessions []provider.Session, query string) (*provider.Session, []provider.Session) {
	// Exact match first.
	for i := range sessions {
		if sessions[i].ID == query {
			return &sessions[i], nil
		}
	}
	// Prefix match.
	var candidates []provider.Session
	for i := range sessions {
		if len(sessions[i].ID) >= len(query) && sessions[i].ID[:len(query)] == query {
			candidates = append(candidates, sessions[i])
		}
	}
	if len(candidates) == 1 {
		return &candidates[0], nil
	}
	if len(candidates) > 1 {
		return nil, candidates
	}
	return nil, nil
}

// sessionStats holds computed statistics about a set of sessions.
type sessionStats struct {
	Total       int
	Summarized  int
	AgentCounts map[string]int
	Today       int
	ThisWeek    int
	ThisMonth   int
	Oldest      time.Time
	DirCounts   map[string]int
}

// computeStats calculates statistics for a set of sessions.
func computeStats(sessions []provider.Session) sessionStats {
	now := time.Now()
	stats := sessionStats{
		Total:       len(sessions),
		AgentCounts: make(map[string]int),
		DirCounts:   make(map[string]int),
	}

	for _, s := range sessions {
		stats.AgentCounts[s.Agent]++
		if s.Summary != "" {
			stats.Summarized++
		}
		if s.Directory != "" {
			stats.DirCounts[s.Directory]++
		}
		d := now.Sub(s.LastUsed)
		if d < 24*time.Hour {
			stats.Today++
		}
		if d < 7*24*time.Hour {
			stats.ThisWeek++
		}
		if d < 30*24*time.Hour {
			stats.ThisMonth++
		}
		if stats.Oldest.IsZero() || s.LastUsed.Before(stats.Oldest) {
			stats.Oldest = s.LastUsed
		}
	}

	return stats
}

// buildFallbackSearch returns a function that uses the configured LLM to
// semantically search sessions when fuzzy search returns no results.
func buildFallbackSearch(command []string, env []string) tui.FallbackSearchFunc {
	return func(ctx context.Context, query string, sessions []provider.Session) []provider.Session {
		return aiFilterSessions(ctx, command, env, query, sessions, 10)
	}
}

// aiFilterSessions sends a query + numbered session list to the LLM and returns
// the sessions it identifies as relevant. Used by --ai-search, TUI fallback,
// and the ask subcommand's pass 1. maxResults caps the number of returned sessions.
func aiFilterSessions(ctx context.Context, command []string, env []string, query string, sessions []provider.Session, maxResults int) []provider.Session {
	var input strings.Builder
	input.WriteString(fmt.Sprintf(
		"I'm searching my coding agent sessions for: %q\n\n"+
			"Below is a numbered list of sessions. Each has the agent name, "+
			"session summary/title, working directory, date, and additional context "+
			"from the first few prompts.\n\n", query))

	for i, s := range sessions {
		title := s.DisplayTitle()
		if len(title) > 100 {
			title = title[:97] + "..."
		}
		when := s.LastUsed.Format("Mon Jan 2 3:04pm")
		search := s.SearchText
		if len(search) > 200 {
			search = search[:197] + "..."
		}
		input.WriteString(fmt.Sprintf("%d. [%s] %s | %s | %s | context: %s\n",
			i+1, s.Agent, title, s.Directory, when, search))
	}

	input.WriteString(fmt.Sprintf(
		"\nReturn ONLY the numbers of sessions that are relevant to my search, "+
			"one per line, most relevant first. "+
			"Return at most %d numbers. If none are relevant, return nothing.\n", maxResults))

	result, err := summary.RunLLM(ctx, command, env, input.String(), 30*time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sesh: ai search failed: %v\n", err)
		return nil
	}

	// Debug: log what the LLM returned for troubleshooting.
	if os.Getenv("SESH_DEBUG") != "" {
		preview := result
		if len(preview) > 200 {
			preview = preview[:200]
		}
		fmt.Fprintf(os.Stderr, "sesh: debug: llm returned %d bytes: %q\n", len(result), preview)
	}

	var matched []provider.Session
	for _, line := range strings.Split(result, "\n") {
		line = strings.TrimSpace(line)
		var num int
		if _, err := fmt.Sscanf(line, "%d", &num); err == nil {
			idx := num - 1
			if idx >= 0 && idx < len(sessions) {
				matched = append(matched, sessions[idx])
			}
		}
		if len(matched) >= maxResults {
			break
		}
	}

	return matched
}

// runUpdate checks for and installs the latest version.
func runUpdate() {
	// Detect Homebrew installation.
	if isHomebrew() {
		fmt.Fprintf(os.Stderr, "sesh was installed via Homebrew. Use 'brew upgrade sesh' instead.\n")
		os.Exit(0)
	}

	fmt.Fprintf(os.Stderr, "Current version: %s\n", version)
	fmt.Fprintf(os.Stderr, "Checking for updates...\n")

	release, err := update.CheckLatest()
	if err != nil {
		fmt.Fprintf(os.Stderr, "sesh: failed to check for updates: %v\n", err)
		os.Exit(1)
	}

	latest := strings.TrimPrefix(release.TagName, "v")
	update.SaveCache(latest)

	if !update.IsNewer(version, latest) {
		fmt.Fprintf(os.Stderr, "Already up to date (v%s).\n", version)
		return
	}

	fmt.Fprintf(os.Stderr, "New version available: v%s -> v%s\n", version, latest)

	url, err := update.FindAsset(release)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sesh: %v\n", err)
		fmt.Fprintf(os.Stderr, "Download manually: %s\n", release.HTMLURL)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Downloading v%s...\n", latest)
	if err := update.DownloadAndReplace(release, url); err != nil {
		fmt.Fprintf(os.Stderr, "sesh: update failed: %v\n", err)
		fmt.Fprintf(os.Stderr, "Download manually: %s\n", release.HTMLURL)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Updated to v%s.\n", latest)
}

// updateHint returns the appropriate update command for how sesh was installed.
func updateHint() string {
	if isHomebrew() {
		return "brew upgrade sesh"
	}
	return "sesh update"
}

// isHomebrew checks if the running binary was installed via Homebrew.
func isHomebrew() bool {
	execPath, err := os.Executable()
	if err != nil {
		return false
	}
	return strings.Contains(execPath, "/Cellar/") || strings.Contains(execPath, "/homebrew/")
}

// checkVersionBackground checks for updates in a goroutine and prints
// a hint to stderr. Non-blocking — the TUI launches immediately.
func checkVersionBackground() {
	// Check cache first.
	if vc := update.CheckCached(); vc != nil {
		if update.IsNewer(version, vc.Latest) {
			fmt.Fprintf(os.Stderr, "sesh: update available (v%s -> v%s). Run '%s' to install.\n", version, vc.Latest, updateHint())
		}
		return
	}

	// Cache miss — check GitHub (this blocks the goroutine, not the main thread).
	release, err := update.CheckLatest()
	if err != nil {
		return // silently ignore network errors
	}
	latest := strings.TrimPrefix(release.TagName, "v")
	update.SaveCache(latest)
	if update.IsNewer(version, latest) {
		fmt.Fprintf(os.Stderr, "sesh: update available (v%s -> v%s). Run '%s' to install.\n", version, latest, updateHint())
	}
}

// checkVersionSync checks for updates synchronously using the cache.
// Only prints if a cached check indicates an update is available.
// Does NOT hit the network — call this from non-interactive commands.
func checkVersionSync() {
	if vc := update.CheckCached(); vc != nil {
		if update.IsNewer(version, vc.Latest) {
			fmt.Fprintf(os.Stderr, "sesh: update available (v%s -> v%s). Run '%s' to install.\n", version, vc.Latest, updateHint())
		}
	}
}
