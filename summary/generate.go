package summary

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

const defaultPrompt = "Summarize what was worked on in this coding session. One sentence, under 20 words. Use plain text only — no markdown, no backticks, no bold markers. Output only the summary, nothing else."

// Config holds the summary generation settings from the user's config file.
type Config struct {
	// Command is the executable (plus args) that generates a summary.
	// Session text is passed on stdin. Summary is read from stdout.
	// If empty, summary generation is disabled.
	Command []string `json:"command"`

	// Prompt is prepended to the session text sent to the command.
	// If empty, a default prompt is used.
	Prompt string `json:"prompt,omitempty"`

	// Env is the merged environment for the command. If nil, the command
	// inherits the parent process environment.
	Env []string `json:"-"`
}

// IsEnabled returns true if summary generation is configured.
func (c Config) IsEnabled() bool {
	return len(c.Command) > 0
}

// Generator produces summaries by shelling out to a user-configured command.
type Generator struct {
	config Config
}

// NewGenerator creates a summary generator from config.
func NewGenerator(cfg Config) *Generator {
	return &Generator{config: cfg}
}

// Generate produces a summary for the given session text.
func (g *Generator) Generate(ctx context.Context, sessionText string) (string, error) {
	if !g.config.IsEnabled() {
		return "", fmt.Errorf("summary generation not configured")
	}

	prompt := g.config.Prompt
	if prompt == "" {
		prompt = defaultPrompt
	}

	input := prompt + "\n\n" + sessionText
	result, err := RunLLM(ctx, g.config.Command, g.config.Env, input, 30*time.Second)
	if err != nil {
		return "", err
	}

	// Truncate very long summaries.
	if len(result) > 200 {
		result = result[:197] + "..."
	}

	// Strip markdown artifacts that LLMs sometimes include despite instructions.
	result = StripMarkdown(result)

	return result, nil
}

// GenerateBatch generates summaries for multiple sessions, calling the
// progress callback after each one. Returns the number of successful
// summaries generated.
func (g *Generator) GenerateBatch(ctx context.Context, items []BatchItem, cache *Cache, progress func(i, total int, id string, err error)) int {
	succeeded := 0
	for i, item := range items {
		if ctx.Err() != nil {
			break
		}
		summary, err := g.Generate(ctx, item.Text)
		if err == nil {
			cache.Put(item.ID, summary, item.LastUsed)
			succeeded++
		}
		if progress != nil {
			progress(i+1, len(items), item.ID, err)
		}
	}
	return succeeded
}

// BatchItem is a session to be summarized.
type BatchItem struct {
	ID       string
	LastUsed time.Time
	Text     string // concatenated user prompts
}

// RunLLM sends input to the configured LLM command and returns the output.
// This is the shared execution function used by summary generation, recap,
// and AI fallback search. The command receives input on stdin and should
// write its response to stdout. If env is non-nil, it is used as the
// command's environment; otherwise the parent process environment is inherited.
func RunLLM(ctx context.Context, command []string, env []string, input string, timeout time.Duration) (string, error) {
	if len(command) == 0 {
		return "", fmt.Errorf("no LLM command configured")
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Stdin = strings.NewReader(input)
	cmd.Env = env // nil inherits parent env

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg != "" {
			return "", fmt.Errorf("command failed: %w: %s", err, errMsg)
		}
		return "", fmt.Errorf("command failed: %w", err)
	}

	result := strings.TrimSpace(stdout.String())
	if result == "" {
		return "", fmt.Errorf("command returned empty output")
	}

	return result, nil
}

// stripMarkdown removes common markdown formatting artifacts from text.
// LLMs sometimes emit markdown despite explicit instructions not to.
var (
	reBold       = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reItalic     = regexp.MustCompile(`(?:^|[^*])\*([^*]+?)\*(?:[^*]|$)`)
	reInlineCode = regexp.MustCompile("`([^`]+)`")
	reHeading    = regexp.MustCompile(`(?m)^#{1,6}\s+`)
	reHRule      = regexp.MustCompile(`(?m)^[-*_]{3,}\s*$`)
)

// StripMarkdown removes common markdown formatting artifacts from text.
// Collapses multi-line output to a single line since summaries are displayed
// as single-row entries in the TUI.
func StripMarkdown(s string) string {
	s = reBold.ReplaceAllString(s, "$1")
	s = reItalic.ReplaceAllString(s, "$1")
	s = reInlineCode.ReplaceAllString(s, "$1")
	s = reHeading.ReplaceAllString(s, "")
	s = reHRule.ReplaceAllString(s, "")

	// Collapse newlines into spaces — summaries must be single-line for
	// the TUI list and other display contexts.
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")

	// Clean up runs of whitespace left by stripping.
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}

	// Strip leading list markers (-, *, •).
	s = strings.TrimLeft(s, "-*• ")
	s = strings.TrimSpace(s)
	return s
}
