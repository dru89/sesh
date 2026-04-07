package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dru89/sesh/provider"
	"github.com/dru89/sesh/summary"
	"github.com/dru89/sesh/tui"
)

// config is the user configuration loaded from ~/.config/sesh/config.json.
type config struct {
	Providers map[string]providerConfig `json:"providers"`
	Index     commandConfig             `json:"index"`
	Ask       askConfig                 `json:"ask"`
	Recap     commandConfig             `json:"recap"`
}

// commandConfig holds a command + prompt pair for a subcommand.
type commandConfig struct {
	Command []string `json:"command,omitempty"`
	Prompt  string   `json:"prompt,omitempty"`
}

// askConfig extends commandConfig with a separate filter command.
type askConfig struct {
	Command       []string `json:"command,omitempty"`
	Prompt        string   `json:"prompt,omitempty"`
	FilterCommand []string `json:"filter_command,omitempty"`
}

// resolveCommand walks a fallback chain and returns the first non-empty command.
func resolveCommand(candidates ...[]string) []string {
	for _, cmd := range candidates {
		if len(cmd) > 0 {
			return cmd
		}
	}
	return nil
}

// indexCommand returns the resolved command for title generation (sesh index).
// Fallback: index -> recap -> ask -> ask.filter_command
func (c config) indexCommand() []string {
	return resolveCommand(c.Index.Command, c.Recap.Command, c.Ask.Command, c.Ask.FilterCommand)
}

// askCommand returns the resolved command for prose generation (sesh ask pass 2).
// Fallback: ask -> recap -> index
func (c config) askCommand() []string {
	return resolveCommand(c.Ask.Command, c.Recap.Command, c.Index.Command)
}

// askFilterCommand returns the resolved command for session filtering (sesh ask pass 1, AI fallback).
// Fallback: ask.filter_command -> index -> ask -> recap
func (c config) askFilterCommand() []string {
	return resolveCommand(c.Ask.FilterCommand, c.Index.Command, c.Ask.Command, c.Recap.Command)
}

// recapCommand returns the resolved command for recap prose generation.
// Fallback: recap -> ask -> index
func (c config) recapCommand() []string {
	return resolveCommand(c.Recap.Command, c.Ask.Command, c.Index.Command)
}

// indexPrompt returns the prompt for title generation.
func (c config) indexPrompt() string {
	if c.Index.Prompt != "" {
		return c.Index.Prompt
	}
	return ""
}

// hasAnyCommand returns true if any LLM command is configured.
func (c config) hasAnyCommand() bool {
	return len(c.indexCommand()) > 0
}

// summaryConfig builds a summary.Config from the resolved index command/prompt.
func (c config) summaryConfig() summary.Config {
	return summary.Config{
		Command: c.indexCommand(),
		Prompt:  c.indexPrompt(),
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
		}
	}

	jsonMode := flag.Bool("json", false, "Output session list as JSON and exit")
	agentFilter := flag.String("agent", "", "Filter by agent name")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: sesh [options] [query]\n\n")
		fmt.Fprintf(os.Stderr, "A unified session browser for coding agents.\n\n")
		fmt.Fprintf(os.Stderr, "Commands:\n")
		fmt.Fprintf(os.Stderr, "  init     Output shell wrapper for your shell\n")
		fmt.Fprintf(os.Stderr, "  list     List sessions (non-interactive)\n")
		fmt.Fprintf(os.Stderr, "  index    Generate summaries for all sessions\n")
		fmt.Fprintf(os.Stderr, "  recap    Summarize what you worked on over a time period\n")
		fmt.Fprintf(os.Stderr, "  ask      Ask a natural language question about your sessions\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nConfig: ~/.config/sesh/config.json\n")
		fmt.Fprintf(os.Stderr, "\nShell wrapper (add to your shell rc):\n")
		fmt.Fprintf(os.Stderr, "  sesh() { local cmd; cmd=$(command sesh \"$@\") || return $?; eval \"$cmd\"; }\n")
	}
	flag.Parse()
	query := strings.Join(flag.Args(), " ")

	cfg := loadConfig()
	providers := buildProviders(cfg)
	cache := summary.NewCache()

	// Collect sessions from all providers.
	ctx := context.Background()
	all := collectSessions(ctx, providers, *agentFilter)

	// Apply cached summaries to sessions.
	applySummaries(all, cache)

	// Sort by last used, newest first.
	sort.Slice(all, func(i, j int) bool {
		return all[i].LastUsed.After(all[j].LastUsed)
	})

	// JSON mode: dump and exit.
	if *jsonMode {
		providerMap := providersByName(providers)
		var out []jsonSession
		for _, s := range all {
			var cmd string
			if p, ok := providerMap[s.Agent]; ok {
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

	// Kick off lazy background summary generation for unsummarized sessions.
	if cfg.hasAnyCommand() {
		go lazyIndex(ctx, cfg.summaryConfig(), cache, all, providers)
	}

	// Run the TUI picker.
	pickOpts := tui.PickOptions{InitialQuery: query}
	if filterCmd := cfg.askFilterCommand(); len(filterCmd) > 0 {
		pickOpts.FallbackSearch = buildFallbackSearch(filterCmd)
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
	providerMap := providersByName(providers)
	if p, ok := providerMap[result.Session.Agent]; ok {
		fmt.Println(p.ResumeCommand(result.Session))
	} else {
		fmt.Fprintf(os.Stderr, "sesh: unknown provider %q\n", result.Session.Agent)
		os.Exit(1)
	}
}

// runIndex handles the `sesh index` subcommand.
func runIndex(args []string) {
	fs := flag.NewFlagSet("index", flag.ExitOnError)
	agentFilter := fs.String("agent", "", "Only index sessions for a specific agent")
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

	fmt.Fprintf(os.Stderr, "Generating summaries for %d/%d sessions...\n", len(need), len(all))

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
			// Use title + search text as fallback.
			text = s.Title
		}
		items = append(items, summary.BatchItem{
			ID:       s.ID,
			LastUsed: s.LastUsed,
			Text:     text,
		})
	}

	gen := summary.NewGenerator(cfg.summaryConfig())
	succeeded := gen.GenerateBatch(ctx, items, cache, func(i, total int, id string, err error) {
		if err != nil {
			fmt.Fprintf(os.Stderr, "  [%d/%d] %s: error: %v\n", i, total, id, err)
		} else {
			fmt.Fprintf(os.Stderr, "  [%d/%d] %s: done\n", i, total, id)
		}
	})

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
			text = s.Title
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
	recapCmd := cfg.recapCommand()
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
			"Group related work together. Focus on what was accomplished, not the tools used. "+
			"Use plain text, no markdown formatting.\n\n%s",
		start.Format("Mon Jan 2"), end.Format("Mon Jan 2"), recapInput.String())

	result, err := summary.RunLLM(ctx, recapCmd, prompt, 60*time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sesh: recap failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(result)
}

// runAsk handles the `sesh ask` subcommand.
func runAsk(args []string) {
	fs := flag.NewFlagSet("ask", flag.ExitOnError)
	agentFilter := fs.String("agent", "", "Only include sessions for a specific agent")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: sesh ask [options] <question>\n\n")
		fmt.Fprintf(os.Stderr, "Ask a natural language question about your coding sessions.\n")
		fmt.Fprintf(os.Stderr, "Requires summary.command to be configured.\n\n")
		fmt.Fprintf(os.Stderr, "Examples:\n")
		fmt.Fprintf(os.Stderr, "  sesh ask \"What did I work on with auth last week?\"\n")
		fmt.Fprintf(os.Stderr, "  sesh ask \"Show me everything related to the API gateway\"\n")
		fmt.Fprintf(os.Stderr, "  sesh ask --agent claude \"What refactoring have I done recently?\"\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		fs.PrintDefaults()
	}
	fs.Parse(args)

	question := strings.Join(fs.Args(), " ")
	if question == "" {
		fs.Usage()
		os.Exit(1)
	}

	cfg := loadConfig()
	askCmd := cfg.askCommand()
	filterCmd := cfg.askFilterCommand()
	if len(askCmd) == 0 && len(filterCmd) == 0 {
		fmt.Fprintf(os.Stderr, "sesh: no LLM command configured for ask\n")
		os.Exit(1)
	}
	// If only one is available, use it for both.
	if len(filterCmd) == 0 {
		filterCmd = askCmd
	}
	if len(askCmd) == 0 {
		askCmd = filterCmd
	}

	providers := buildProviders(cfg)
	cache := summary.NewCache()
	ctx := context.Background()

	all := collectSessions(ctx, providers, *agentFilter)
	applySummaries(all, cache)

	sort.Slice(all, func(i, j int) bool {
		return all[i].LastUsed.After(all[j].LastUsed)
	})

	if len(all) == 0 {
		fmt.Fprintf(os.Stderr, "No sessions found.\n")
		return
	}

	// --- Pass 1: Filter — ask the LLM which sessions are relevant. ---

	var numberedList strings.Builder
	for i, s := range all {
		title := s.DisplayTitle()
		if len(title) > 100 {
			title = title[:97] + "..."
		}
		when := s.LastUsed.Format("Mon Jan 2 3:04pm")
		numberedList.WriteString(fmt.Sprintf("%d. [%s] %s | %s | %s\n",
			i+1, s.Agent, title, s.Directory, when))
	}

	filterPrompt := fmt.Sprintf(
		"I'm looking through my coding agent sessions to answer this question:\n%s\n\n"+
			"Below is a numbered list of sessions. Each has the agent name, "+
			"session summary/title, working directory, and date.\n\n"+
			"%s\n"+
			"Return ONLY the numbers of sessions relevant to my question, "+
			"one per line, most relevant first. Return at most 20. "+
			"If none are relevant, return nothing.",
		question, numberedList.String())

	fmt.Fprintf(os.Stderr, "Searching %d sessions...\n", len(all))

	filterResult, err := summary.RunLLM(ctx, filterCmd, filterPrompt, 30*time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sesh: ask failed during filtering: %v\n", err)
		os.Exit(1)
	}

	// Parse the numbered results from pass 1.
	var relevant []provider.Session
	for _, line := range strings.Split(filterResult, "\n") {
		line = strings.TrimSpace(line)
		var num int
		if _, err := fmt.Sscanf(line, "%d", &num); err == nil {
			idx := num - 1
			if idx >= 0 && idx < len(all) {
				relevant = append(relevant, all[idx])
			}
		}
		if len(relevant) >= 20 {
			break
		}
	}

	if len(relevant) == 0 {
		fmt.Fprintf(os.Stderr, "No relevant sessions found for that question.\n")
		return
	}

	fmt.Fprintf(os.Stderr, "Found %d relevant sessions. Generating answer...\n", len(relevant))

	// --- Pass 2: Generate — answer the question using only the relevant sessions. ---

	var detailList strings.Builder
	for _, s := range relevant {
		title := s.DisplayTitle()
		when := s.LastUsed.Format("Mon Jan 2 3:04pm")
		detailList.WriteString(fmt.Sprintf("- [%s] %s | %s | %s\n",
			s.Agent, title, s.Directory, when))
	}

	answerPrompt := fmt.Sprintf(
		"Here are my coding agent sessions relevant to my question. "+
			"Each line has the agent name, session summary/title, working directory, and date.\n\n"+
			"%s\n"+
			"My question: %s\n\n"+
			"Answer my question based on these sessions. Be specific about what was worked on. "+
			"Use plain text, no markdown formatting.",
		detailList.String(), question)

	result, err := summary.RunLLM(ctx, askCmd, answerPrompt, 60*time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sesh: ask failed during answer generation: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(result)
}

// runList handles the `sesh list` subcommand.
func runList(args []string) {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	agentFilter := fs.String("agent", "", "Filter by agent name")
	since := fs.String("since", "", "Only show sessions since date (e.g. 'monday', '2026-04-01', '3d')")
	limit := fs.Int("n", 0, "Maximum number of sessions to show")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: sesh list [options]\n\n")
		fmt.Fprintf(os.Stderr, "List sessions in a non-interactive table format.\n\n")
		fmt.Fprintf(os.Stderr, "Examples:\n")
		fmt.Fprintf(os.Stderr, "  sesh list                    # all sessions\n")
		fmt.Fprintf(os.Stderr, "  sesh list --agent opencode   # only OpenCode\n")
		fmt.Fprintf(os.Stderr, "  sesh list --since monday     # since Monday\n")
		fmt.Fprintf(os.Stderr, "  sesh list -n 20              # last 20 sessions\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		fs.PrintDefaults()
	}
	fs.Parse(args)

	cfg := loadConfig()
	providers := buildProviders(cfg)
	cache := summary.NewCache()
	ctx := context.Background()

	all := collectSessions(ctx, providers, *agentFilter)
	applySummaries(all, cache)

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

	if len(all) == 0 {
		fmt.Fprintf(os.Stderr, "No sessions found.\n")
		return
	}

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
	switch name {
	case "opencode":
		return "\033[34m" + name + "\033[0m" // blue
	case "claude":
		return "\033[35m" + name + "\033[0m" // magenta
	default:
		return "\033[33m" + name + "\033[0m" // yellow
	}
}

// isTerminal checks if stdout is a terminal.
func isTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
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
		fmt.Println(initBash)
	case "zsh":
		fmt.Println(initZsh)
	case "fish":
		fmt.Println(initFish)
	case "powershell", "pwsh":
		fmt.Println(initPowerShell)
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
  local cmd
  cmd=$(command sesh "$@") || return $?
  eval "$cmd"
}`

const initZsh = `sesh() {
  local cmd
  cmd=$(command sesh "$@") || return $?
  eval "$cmd"
}`

const initFish = `function sesh
    set -l cmd (command sesh $argv)
    or return $status
    eval $cmd
end`

const initPowerShell = `function sesh {
    $output = & sesh.exe @args
    if ($LASTEXITCODE -eq 0 -and $output) {
        Invoke-Expression $output
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
			sessions, err := p.ListSessions(ctx)
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
			sessions[i].Summary = s
			// Also add summary to search text for fuzzy matching.
			sessions[i].SearchText += " " + s
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
	if oc, ok := cfg.Providers["opencode"]; ok {
		if oc.isEnabled() {
			var opts []provider.OpenCodeOption
			if cmd := oc.resumeCommandStr(); cmd != "" {
				opts = append(opts, provider.WithOpenCodeResumeCommand(cmd))
			}
			providers = append(providers, provider.NewOpenCode(opts...))
		}
	} else {
		providers = append(providers, provider.NewOpenCode())
	}

	// Built-in: Claude Code.
	if cc, ok := cfg.Providers["claude"]; ok {
		if cc.isEnabled() {
			var opts []provider.ClaudeOption
			if cmd := cc.resumeCommandStr(); cmd != "" {
				opts = append(opts, provider.WithClaudeResumeCommand(cmd))
			}
			providers = append(providers, provider.NewClaude(opts...))
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

// buildFallbackSearch returns a function that uses the configured LLM to
// semantically search sessions when fuzzy search returns no results.
func buildFallbackSearch(command []string) tui.FallbackSearchFunc {
	return func(ctx context.Context, query string, sessions []provider.Session) []provider.Session {
		// Build a numbered list of sessions with their summaries/titles.
		var input strings.Builder
		input.WriteString(fmt.Sprintf(
			"I'm searching my coding agent sessions for: %q\n\n"+
				"Below is a numbered list of sessions. Return ONLY the numbers of "+
				"sessions that are relevant to my search, one per line, most relevant first. "+
				"Return at most 10 numbers. If none are relevant, return nothing.\n\n", query))

		for i, s := range sessions {
			title := s.DisplayTitle()
			if len(title) > 100 {
				title = title[:97] + "..."
			}
			input.WriteString(fmt.Sprintf("%d. [%s] %s | %s\n", i+1, s.Agent, title, s.Directory))
		}

		result, err := summary.RunLLM(ctx, command, input.String(), 30*time.Second)
		if err != nil {
			return nil
		}

		// Parse the numbered results.
		var matched []provider.Session
		for _, line := range strings.Split(result, "\n") {
			line = strings.TrimSpace(line)
			// Extract number from various formats: "1", "1.", "1 -", etc.
			var num int
			if _, err := fmt.Sscanf(line, "%d", &num); err == nil {
				idx := num - 1
				if idx >= 0 && idx < len(sessions) {
					matched = append(matched, sessions[idx])
				}
			}
			if len(matched) >= 10 {
				break
			}
		}

		return matched
	}
}
