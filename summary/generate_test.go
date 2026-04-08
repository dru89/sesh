package summary

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRunLLMSuccess(t *testing.T) {
	// Create a mock script that echoes its stdin back.
	script := writeMockScript(t, "#!/bin/sh\ncat")

	result, err := RunLLM(context.Background(), []string{script}, nil, "hello world", 5*time.Second)
	if err != nil {
		t.Fatalf("RunLLM failed: %v", err)
	}
	if result != "hello world" {
		t.Errorf("got %q, want %q", result, "hello world")
	}
}

func TestRunLLMTruncatesWhitespace(t *testing.T) {
	script := writeMockScript(t, "#!/bin/sh\necho '  trimmed  '")

	result, err := RunLLM(context.Background(), []string{script}, nil, "", 5*time.Second)
	if err != nil {
		t.Fatalf("RunLLM failed: %v", err)
	}
	if result != "trimmed" {
		t.Errorf("got %q, want %q", result, "trimmed")
	}
}

func TestRunLLMEmptyOutput(t *testing.T) {
	script := writeMockScript(t, "#!/bin/sh\ntrue")

	_, err := RunLLM(context.Background(), []string{script}, nil, "input", 5*time.Second)
	if err == nil {
		t.Error("expected error for empty output")
	}
}

func TestRunLLMCommandFailure(t *testing.T) {
	script := writeMockScript(t, "#!/bin/sh\nexit 1")

	_, err := RunLLM(context.Background(), []string{script}, nil, "input", 5*time.Second)
	if err == nil {
		t.Error("expected error for failed command")
	}
}

func TestRunLLMCommandNotFound(t *testing.T) {
	_, err := RunLLM(context.Background(), []string{"/nonexistent/binary"}, nil, "input", 5*time.Second)
	if err == nil {
		t.Error("expected error for missing command")
	}
}

func TestRunLLMTimeout(t *testing.T) {
	script := writeMockScript(t, "#!/bin/sh\nsleep 10")

	_, err := RunLLM(context.Background(), []string{script}, nil, "input", 100*time.Millisecond)
	if err == nil {
		t.Error("expected error for timeout")
	}
}

func TestRunLLMNoCommand(t *testing.T) {
	_, err := RunLLM(context.Background(), nil, nil, "input", 5*time.Second)
	if err == nil {
		t.Error("expected error for nil command")
	}
}

func TestRunLLMStderrInError(t *testing.T) {
	script := writeMockScript(t, "#!/bin/sh\necho 'bad model' >&2\nexit 1")

	_, err := RunLLM(context.Background(), []string{script}, nil, "input", 5*time.Second)
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); got == "" {
		t.Error("error message should include stderr")
	}
}

func TestRunLLMWithEnv(t *testing.T) {
	script := writeMockScript(t, "#!/bin/sh\necho $TEST_LLM_ENV")

	env := append(os.Environ(), "TEST_LLM_ENV=hello_from_env")
	result, err := RunLLM(context.Background(), []string{script}, env, "input", 5*time.Second)
	if err != nil {
		t.Fatalf("RunLLM failed: %v", err)
	}
	if result != "hello_from_env" {
		t.Errorf("got %q, want %q", result, "hello_from_env")
	}
}

func TestGenerateSuccess(t *testing.T) {
	// Mock that returns a fixed summary regardless of input.
	script := writeMockScript(t, "#!/bin/sh\necho 'Built JWT auth middleware'")

	gen := NewGenerator(Config{
		Command: []string{script},
	})

	result, err := gen.Generate(context.Background(), "session text here")
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if result != "Built JWT auth middleware" {
		t.Errorf("got %q, want %q", result, "Built JWT auth middleware")
	}
}

func TestGenerateTruncatesLongOutput(t *testing.T) {
	// Generate a 300-char output.
	long := ""
	for i := 0; i < 300; i++ {
		long += "x"
	}
	script := writeMockScript(t, "#!/bin/sh\nprintf '"+long+"'")

	gen := NewGenerator(Config{Command: []string{script}})
	result, err := gen.Generate(context.Background(), "input")
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if len(result) != 200 {
		t.Errorf("expected truncated to 200, got %d", len(result))
	}
}

func TestGenerateNotConfigured(t *testing.T) {
	gen := NewGenerator(Config{})
	_, err := gen.Generate(context.Background(), "input")
	if err == nil {
		t.Error("expected error when not configured")
	}
}

func TestGenerateBatch(t *testing.T) {
	script := writeMockScript(t, "#!/bin/sh\necho 'summary'")

	gen := NewGenerator(Config{Command: []string{script}})
	cache := newTestCache(t)

	items := []BatchItem{
		{ID: "ses_1", LastUsed: time.Now(), Text: "text 1"},
		{ID: "ses_2", LastUsed: time.Now(), Text: "text 2"},
	}

	var progress []int
	succeeded := gen.GenerateBatch(context.Background(), items, cache, func(i, total int, id string, err error) {
		progress = append(progress, i)
	})

	if succeeded != 2 {
		t.Errorf("expected 2 succeeded, got %d", succeeded)
	}
	if len(progress) != 2 {
		t.Errorf("expected 2 progress calls, got %d", len(progress))
	}
	if cache.Len() != 2 {
		t.Errorf("expected 2 cached entries, got %d", cache.Len())
	}
}

func TestGenerateBatchPartialFailure(t *testing.T) {
	// Fail on second call by checking if stdin contains "fail".
	script := writeMockScript(t, "#!/bin/sh\ninput=$(cat)\ncase \"$input\" in *fail*) exit 1;; esac\necho 'ok'")

	gen := NewGenerator(Config{Command: []string{script}})
	cache := newTestCache(t)

	items := []BatchItem{
		{ID: "ses_1", LastUsed: time.Now(), Text: "good"},
		{ID: "ses_2", LastUsed: time.Now(), Text: "fail"},
		{ID: "ses_3", LastUsed: time.Now(), Text: "also good"},
	}

	succeeded := gen.GenerateBatch(context.Background(), items, cache, nil)
	if succeeded != 2 {
		t.Errorf("expected 2 succeeded, got %d", succeeded)
	}
	if cache.Len() != 2 {
		t.Errorf("expected 2 cached, got %d", cache.Len())
	}
}

// writeMockScript creates an executable shell script in a temp dir.
func writeMockScript(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "mock.sh")
	if err := os.WriteFile(path, []byte(content), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestStripMarkdown(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain text", "hello world", "hello world"},
		{"bold", "this is **bold** text", "this is bold text"},
		{"inline code", "use `sesh` to search", "use sesh to search"},
		{"heading", "## Session Summary\nthe content", "Session Summary the content"},
		{"hrule", "---\ncontent after", "content after"},
		{"multiline collapses", "line one\nline two\nline three", "line one line two line three"},
		{"crlf collapses", "line one\r\nline two", "line one line two"},
		{"double spaces cleaned", "too  many   spaces", "too many spaces"},
		{"leading list marker dash", "- Built auth middleware", "Built auth middleware"},
		{"leading list marker bullet", "• Fixed login bug", "Fixed login bug"},
		{"leading list marker star", "* Refactored API", "Refactored API"},
		{"combined", "## Summary\n**Built** `auth` middleware\n- for the API", "Summary Built auth middleware - for the API"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripMarkdown(tt.input)
			if got != tt.want {
				t.Errorf("StripMarkdown(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
