package summary

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func newTestHealth(t *testing.T) *Health {
	t.Helper()
	return &Health{path: filepath.Join(t.TempDir(), "index-health.json")}
}

func TestHealthRecordRunIncrementsOnTotalFailure(t *testing.T) {
	h := newTestHealth(t)

	for i := 1; i <= 3; i++ {
		h.RecordRun(5, 0, errors.New("command failed: exit status 1"), []string{"llm", "-m", "haiku"})
		if got := h.ConsecutiveFailedRuns(); got != i {
			t.Errorf("after %d failed runs: got %d, want %d", i, got, i)
		}
	}

	if h.LastError() != "command failed: exit status 1" {
		t.Errorf("got last error %q", h.LastError())
	}
	if got := strings.Join(h.LastCommand(), " "); got != "llm -m haiku" {
		t.Errorf("got last command %q", got)
	}
}

// TestHealthRecordRunResetsOnAnySuccess covers the central rule: a single
// success proves the configured command works, so whatever else failed is a
// per-session problem rather than a broken configuration.
func TestHealthRecordRunResetsOnAnySuccess(t *testing.T) {
	h := newTestHealth(t)

	h.RecordRun(5, 0, errors.New("boom"), []string{"llm"})
	h.RecordRun(5, 0, errors.New("boom"), []string{"llm"})
	if h.ConsecutiveFailedRuns() != 2 {
		t.Fatalf("setup: got %d failed runs, want 2", h.ConsecutiveFailedRuns())
	}

	// Mostly failed, but one worked.
	h.RecordRun(5, 1, errors.New("boom"), []string{"llm"})

	if got := h.ConsecutiveFailedRuns(); got != 0 {
		t.Errorf("got %d failed runs after a partial success, want 0", got)
	}
	if h.LastError() != "" {
		t.Errorf("expected last error cleared, got %q", h.LastError())
	}
	if len(h.LastCommand()) != 0 {
		t.Errorf("expected last command cleared, got %v", h.LastCommand())
	}
}

// TestHealthRecordRunIgnoresEmptyRun guards against a run with nothing to do
// being counted as evidence. lazyIndex frequently has zero items (every session
// already summarized), and treating that as a success would paper over a real
// failure while treating it as a failure would invent one.
func TestHealthRecordRunIgnoresEmptyRun(t *testing.T) {
	h := newTestHealth(t)

	h.RecordRun(5, 0, errors.New("boom"), []string{"llm"})
	h.RecordRun(0, 0, nil, []string{"llm"})

	if got := h.ConsecutiveFailedRuns(); got != 1 {
		t.Errorf("got %d failed runs, want 1 (empty run should not count)", got)
	}
	if h.LastError() != "boom" {
		t.Errorf("empty run clobbered last error: %q", h.LastError())
	}
}

func TestHealthFailingThreshold(t *testing.T) {
	h := newTestHealth(t)

	for i := 0; i < FailureHintThreshold-1; i++ {
		h.RecordRun(1, 0, errors.New("boom"), nil)
		if h.Failing() {
			t.Fatalf("reported failing after %d runs, threshold is %d", i+1, FailureHintThreshold)
		}
	}

	h.RecordRun(1, 0, errors.New("boom"), nil)
	if !h.Failing() {
		t.Errorf("expected failing at %d runs", FailureHintThreshold)
	}
}

func TestHealthSaveLoadRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index-health.json")

	h := &Health{path: path}
	h.RecordRun(4, 0, errors.New("model 'claude-haiku-4-5' not found"), []string{"claude", "-p"})
	if err := h.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	reloaded := &Health{path: path}
	reloaded.load()

	if got := reloaded.ConsecutiveFailedRuns(); got != 1 {
		t.Errorf("got %d failed runs, want 1", got)
	}
	if got := reloaded.LastError(); got != "model 'claude-haiku-4-5' not found" {
		t.Errorf("got last error %q", got)
	}
	if got := strings.Join(reloaded.LastCommand(), " "); got != "claude -p" {
		t.Errorf("got last command %q", got)
	}
}

func TestHealthLoadCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index-health.json")
	if err := os.WriteFile(path, []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}

	h := &Health{path: path}
	h.load()

	if h.ConsecutiveFailedRuns() != 0 || h.Failing() {
		t.Error("expected corrupt health record to start fresh")
	}
}

func TestHealthReset(t *testing.T) {
	h := newTestHealth(t)
	h.RecordRun(3, 0, errors.New("boom"), []string{"llm"})
	h.RecordRun(3, 0, errors.New("boom"), []string{"llm"})

	h.Reset()

	if h.ConsecutiveFailedRuns() != 0 {
		t.Errorf("got %d failed runs after reset", h.ConsecutiveFailedRuns())
	}
	if h.LastError() != "" {
		t.Errorf("got last error %q after reset", h.LastError())
	}
}

// TestHealthLastCommandIsCopy verifies callers cannot mutate internal state
// through the returned slice.
func TestHealthLastCommandIsCopy(t *testing.T) {
	h := newTestHealth(t)
	h.RecordRun(1, 0, errors.New("boom"), []string{"llm", "-m", "haiku"})

	got := h.LastCommand()
	got[0] = "tampered"

	if h.LastCommand()[0] != "llm" {
		t.Error("LastCommand returned a slice aliasing internal state")
	}
}

// TestRunRecorderRecordsFailureImmediately is the whole point of the recorder:
// background generation is usually killed before the batch ends, so the outcome
// has to be durable after the very first result.
func TestRunRecorderRecordsFailureImmediately(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index-health.json")
	h := &Health{path: path}
	rec := NewRunRecorder(h, []string{"llm", "-m", "haiku"})

	rec.Observe(errors.New("exit status 1"))

	if got := h.ConsecutiveFailedRuns(); got != 1 {
		t.Errorf("got %d failed runs, want 1", got)
	}
	// Must already be durable — the process may die on the next instruction.
	reloaded := &Health{path: path}
	reloaded.load()
	if got := reloaded.ConsecutiveFailedRuns(); got != 1 {
		t.Errorf("outcome not persisted: got %d failed runs on disk, want 1", got)
	}
}

// TestRunRecorderCountsRunOnceDespiteManyFailures verifies a broken command
// failing across ten sessions counts as one failed run, not ten.
func TestRunRecorderCountsRunOnceDespiteManyFailures(t *testing.T) {
	h := newTestHealth(t)
	rec := NewRunRecorder(h, nil)

	for i := 0; i < 10; i++ {
		rec.Observe(errors.New("exit status 1"))
	}

	if got := h.ConsecutiveFailedRuns(); got != 1 {
		t.Errorf("got %d failed runs, want 1", got)
	}
}

// TestRunRecorderUpgradesFailedRunOnLaterSuccess covers a run that opens with a
// per-session failure but proves the command works on a later item.
func TestRunRecorderUpgradesFailedRunOnLaterSuccess(t *testing.T) {
	h := newTestHealth(t)
	h.RecordRun(1, 0, errors.New("earlier run"), nil) // prior failing run
	rec := NewRunRecorder(h, nil)

	rec.Observe(errors.New("one bad transcript"))
	if got := h.ConsecutiveFailedRuns(); got != 2 {
		t.Fatalf("got %d failed runs after opening failure, want 2", got)
	}

	rec.Observe(nil)

	if got := h.ConsecutiveFailedRuns(); got != 0 {
		t.Errorf("got %d failed runs after later success, want 0", got)
	}
}

// TestRunRecorderIgnoresFailuresAfterSuccess ensures a healthy run stays
// healthy — once the command has demonstrably worked, later per-session
// failures say nothing about the configuration.
func TestRunRecorderIgnoresFailuresAfterSuccess(t *testing.T) {
	h := newTestHealth(t)
	rec := NewRunRecorder(h, nil)

	rec.Observe(nil)
	rec.Observe(errors.New("one bad transcript"))
	rec.Observe(errors.New("another bad transcript"))

	if got := h.ConsecutiveFailedRuns(); got != 0 {
		t.Errorf("got %d failed runs, want 0", got)
	}
	if h.LastError() != "" {
		t.Errorf("healthy run recorded an error: %q", h.LastError())
	}
}

func TestRunRecorderNilHealthIsSafe(t *testing.T) {
	rec := NewRunRecorder(nil, nil)
	rec.Observe(errors.New("boom"))
	rec.Observe(nil)
}

func TestCondenseError(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"single line unchanged", "exit status 1", "exit status 1"},
		{"newlines collapsed", "command failed:\nexit status 1\n", "command failed: exit status 1"},
		{"crlf collapsed", "a\r\nb", "a b"},
		{"runs of whitespace collapsed", "a    b\t\tc", "a b c"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CondenseError(tt.in); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCondenseErrorTruncates(t *testing.T) {
	got := CondenseError(strings.Repeat("x", maxRecordedErrorLen*2))

	if len([]rune(got)) != maxRecordedErrorLen {
		t.Errorf("got %d runes, want %d", len([]rune(got)), maxRecordedErrorLen)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("expected ellipsis suffix, got %q", got)
	}
}

// TestCondenseErrorTruncatesOnRuneBoundary guards against byte-slicing a
// multi-byte rune in half, which would leave invalid UTF-8 in the record and
// render as a replacement character in the terminal.
func TestCondenseErrorTruncatesOnRuneBoundary(t *testing.T) {
	got := CondenseError(strings.Repeat("é", maxRecordedErrorLen*2))

	if !utf8.ValidString(got) {
		t.Errorf("truncation produced invalid UTF-8: %q", got)
	}
	if len([]rune(got)) != maxRecordedErrorLen {
		t.Errorf("got %d runes, want %d", len([]rune(got)), maxRecordedErrorLen)
	}
}

func TestHealthNoUsableTextThreshold(t *testing.T) {
	h := newTestHealth(t)

	h.RecordNoUsableText(NoTextHintThreshold-1, "sess-1")
	if h.NoUsableText() {
		t.Error("reported below the threshold; a couple of textless sessions is ordinary")
	}

	h.RecordNoUsableText(NoTextHintThreshold, "sess-1")
	if !h.NoUsableText() {
		t.Error("did not report at the threshold")
	}
	if h.NoTextSkips() != NoTextHintThreshold {
		t.Errorf("skips = %d, want %d", h.NoTextSkips(), NoTextHintThreshold)
	}
	if h.NoTextExample() != "sess-1" {
		t.Errorf("example = %q, want sess-1", h.NoTextExample())
	}
}

// A run that read text again means whatever was wrong is over, so the hint has
// to stop. Otherwise it outlives its cause and becomes noise.
func TestHealthRecordRunClearsNoUsableText(t *testing.T) {
	h := newTestHealth(t)
	h.RecordNoUsableText(20, "sess-1")

	h.RecordRun(3, 3, nil, []string{"llm"})

	if h.NoUsableText() {
		t.Error("still reporting unreadable text after a run that read text")
	}
	if h.NoTextExample() != "" {
		t.Errorf("stale example survived: %q", h.NoTextExample())
	}
}

// Even a run where every summary failed proves text was readable — the failure
// was downstream, in the LLM command, and that has its own hint.
func TestHealthFailedRunAlsoClearsNoUsableText(t *testing.T) {
	h := newTestHealth(t)
	h.RecordNoUsableText(20, "sess-1")

	h.RecordRun(3, 0, errors.New("boom"), []string{"llm"})

	if h.NoUsableText() {
		t.Error("a failed-but-attempted run should clear the no-text state")
	}
}

func TestHealthNoUsableTextRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index-health.json")
	h := &Health{path: path}
	h.RecordNoUsableText(12, "sess-abc")
	if err := h.Save(); err != nil {
		t.Fatal(err)
	}

	reloaded := &Health{path: path}
	reloaded.load()

	if !reloaded.NoUsableText() {
		t.Error("no-text state did not survive save/load")
	}
	if reloaded.NoTextSkips() != 12 || reloaded.NoTextExample() != "sess-abc" {
		t.Errorf("reloaded skips=%d example=%q", reloaded.NoTextSkips(), reloaded.NoTextExample())
	}
}
