package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dru89/sesh/provider"
	"github.com/dru89/sesh/tui"
)

// config is the user configuration loaded from ~/.config/sesh/config.json.
type config struct {
	Providers map[string]providerConfig `json:"providers"`
}

type providerConfig struct {
	// ResumeCommand overrides the default resume command for a built-in provider.
	// Use {{ID}} as a placeholder for the session ID.
	ResumeCommand string `json:"resume_command,omitempty"`

	// Enabled controls whether this provider is active (default: true).
	// Set to false to disable a built-in provider.
	Enabled *bool `json:"enabled,omitempty"`

	// ListCommand is the command to run to list sessions (external providers only).
	ListCommand []string `json:"list_command,omitempty"`
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
	jsonMode := flag.Bool("json", false, "Output session list as JSON and exit")
	agentFilter := flag.String("agent", "", "Filter by agent name")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: sesh [options] [query]\n\n")
		fmt.Fprintf(os.Stderr, "A unified session browser for coding agents.\n\n")
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

	// Collect sessions from all providers.
	ctx := context.Background()
	var all []provider.Session
	for _, p := range providers {
		if *agentFilter != "" && p.Name() != *agentFilter {
			continue
		}
		sessions, err := p.ListSessions(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "sesh: warning: %s: %v\n", p.Name(), err)
			continue
		}
		all = append(all, sessions...)
	}

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

	// Run the TUI picker.
	result, err := tui.Pick(all, query)
	if err != nil {
		os.Exit(130) // same as ctrl-c convention
	}

	// Find the provider and output the resume command.
	providerMap := providersByName(providers)
	if p, ok := providerMap[result.Session.Agent]; ok {
		fmt.Println(p.ResumeCommand(result.Session))
	} else {
		fmt.Fprintf(os.Stderr, "sesh: unknown provider %q\n", result.Session.Agent)
		os.Exit(1)
	}
}

func loadConfig() config {
	home, _ := os.UserHomeDir()
	paths := []string{
		filepath.Join(home, ".config", "sesh", "config.json"),
	}
	// Also check XDG_CONFIG_HOME if set.
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		paths = append([]string{filepath.Join(xdg, "sesh", "config.json")}, paths...)
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
			if oc.ResumeCommand != "" {
				opts = append(opts, provider.WithOpenCodeResumeCommand(oc.ResumeCommand))
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
			if cc.ResumeCommand != "" {
				opts = append(opts, provider.WithClaudeResumeCommand(cc.ResumeCommand))
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
			ResumeCommand: pc.ResumeCommand,
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
