package summary

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// FailureHintThreshold is how many consecutive all-failed generation runs must
// accumulate before sesh tells the user something is wrong. A single failed run
// is usually transient — a rate limit, a network blip, a timeout on one long
// transcript — and nagging about those would be noise. Repeated total failure
// is not transient: it means the configured command is broken (a retired model
// ID, an expired OAuth session, a renamed binary) and no amount of waiting will
// fix it.
const FailureHintThreshold = 3

// maxRecordedErrorLen caps the stored error string. Command stderr can be
// arbitrarily long, and this value is shown on a single terminal line.
const maxRecordedErrorLen = 240

// Health records whether summary generation is actually working.
//
// Background generation (lazyIndex) runs in a goroutine underneath the TUI's
// alt screen, so it cannot report anything to the user at the moment it fails.
// Instead it persists the outcome here and the *next* sesh run surfaces it —
// the same deferred-reporting approach the update checker uses for version
// checks.
type Health struct {
	path  string
	mu    sync.Mutex
	state healthState
}

type healthState struct {
	// ConsecutiveFailedRuns counts generation runs where every attempted
	// summary failed. Any single success resets it to zero.
	ConsecutiveFailedRuns int `json:"consecutive_failed_runs"`

	// LastError is the error from the most recent failed run, collapsed to a
	// single line and truncated. This is the load-bearing field: a generic
	// "summaries aren't working" sends the user digging, whereas the actual
	// stderr usually names the problem outright.
	LastError string `json:"last_error,omitempty"`

	// LastCommand is the command that produced LastError, so the user can see
	// which of several configured commands is the broken one.
	LastCommand []string `json:"last_command,omitempty"`

	// NoTextSkips is how many sessions the last generation run had to skip
	// because their provider returned no text at all, and NoTextExample is one
	// of their IDs so the hint can point at something concrete.
	NoTextSkips   int    `json:"no_text_skips,omitempty"`
	NoTextExample string `json:"no_text_example,omitempty"`

	// Diagnostic only — nothing reads these back. No omitempty: it has no
	// effect on a struct value like time.Time, so a zero time serializes
	// either way and the tag would only be misleading.
	LastFailure time.Time `json:"last_failure"`
	LastSuccess time.Time `json:"last_success"`
}

// NewHealth loads or creates the index health record.
func NewHealth() *Health {
	h := &Health{path: filepath.Join(CacheDir(), "index-health.json")}
	h.load()
	return h
}

// RecordRun folds the outcome of one generation run into the health record.
//
// attempted is how many summaries were tried, succeeded how many produced a
// result, and firstErr the first error encountered (nil if there were none).
// A run that attempted nothing is not evidence either way and is ignored.
func (h *Health) RecordRun(attempted, succeeded int, firstErr error, command []string) {
	if attempted == 0 {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// Anything attempted means text was found, so a previously recorded
	// all-empty run no longer describes the world.
	h.state.NoTextSkips = 0
	h.state.NoTextExample = ""

	if succeeded > 0 {
		// Something worked, so the command is fundamentally sound. Whatever
		// else failed is a per-session problem (an unparseable transcript, one
		// slow call hitting the timeout), not a broken configuration.
		h.state.ConsecutiveFailedRuns = 0
		h.state.LastError = ""
		h.state.LastCommand = nil
		h.state.LastSuccess = time.Now()
		return
	}

	h.state.ConsecutiveFailedRuns++
	h.state.LastFailure = time.Now()
	h.state.LastCommand = command
	if firstErr != nil {
		h.state.LastError = CondenseError(firstErr.Error())
	}
}

// RunRecorder folds one generation run's outcome into a Health record as soon
// as that outcome is determined, rather than after the run finishes.
//
// This exists because background generation is routinely killed mid-flight:
// sesh exits the moment the user picks a session, which is frequently sooner
// than a single summary takes to produce. An outcome recorded only at the end
// of the batch would rarely survive, and would miss exactly the fast-exit users
// the failure hint is meant to reach.
//
// The state machine is two-step. The first result settles the run; a later
// success upgrades a run that opened with a failure. A genuinely broken command
// fails on its first item, so the right answer is reached immediately in the
// case that matters.
type RunRecorder struct {
	health  *Health
	command []string
	healthy bool // a success has been recorded; nothing further can change it
	failed  bool // a failure has been recorded for this run
}

// NewRunRecorder returns a recorder that writes to h, attributing failures to
// command.
func NewRunRecorder(h *Health, command []string) *RunRecorder {
	return &RunRecorder{health: h, command: command}
}

// Observe folds one summary attempt's result into the run. Persisting is
// best-effort: a failed save costs one run's diagnostics and there is nowhere
// safe to report it from, since callers run underneath the TUI.
func (r *RunRecorder) Observe(err error) {
	if r.health == nil || r.healthy {
		return
	}

	switch {
	case err != nil:
		if r.failed {
			return // this run is already counted as failed
		}
		r.failed = true
		r.health.RecordRun(1, 0, err, r.command)
	default:
		r.healthy = true
		r.health.RecordRun(1, 1, nil, r.command)
	}

	_ = r.health.Save()
}

// Failing reports whether failures have accumulated past the point where they
// should be assumed sticky rather than transient.
func (h *Health) Failing() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.state.ConsecutiveFailedRuns >= FailureHintThreshold
}

// ConsecutiveFailedRuns returns the current run of total failures.
func (h *Health) ConsecutiveFailedRuns() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.state.ConsecutiveFailedRuns
}

// LastError returns the condensed error from the most recent failed run.
func (h *Health) LastError() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.state.LastError
}

// LastCommand returns the command that produced the most recent failure.
func (h *Health) LastCommand() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.state.LastCommand...)
}

// Reset clears the failure record. Called when generation succeeds outside a
// normal run, such as after the user repairs their configuration.
func (h *Health) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.state = healthState{LastSuccess: h.state.LastSuccess}
}

// Save persists the health record to disk.
func (h *Health) Save() error {
	h.mu.Lock()
	data, err := json.MarshalIndent(h.state, "", "  ")
	h.mu.Unlock()
	if err != nil {
		return fmt.Errorf("marshal index health: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(h.path), 0755); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}

	tmp := h.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("write index health: %w", err)
	}
	if err := os.Rename(tmp, h.path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename index health: %w", err)
	}
	return nil
}

func (h *Health) load() {
	data, err := os.ReadFile(h.path)
	if err != nil {
		return // no record yet, that's fine
	}
	var state healthState
	if err := json.Unmarshal(data, &state); err != nil {
		// A corrupt health record is not worth warning about — it only holds
		// derived diagnostics, and starting fresh rebuilds it within a few
		// runs. Silence here is deliberate, unlike in lazyIndex.
		return
	}
	h.state = state
}

// CondenseError collapses an error message to a single truncated line so it can
// be shown as one row of terminal output. Command failures carry raw stderr,
// which is frequently multi-line.
func CondenseError(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	// Truncate by rune, not by byte: error text can carry non-ASCII (file
	// paths, model output), and slicing mid-rune yields invalid UTF-8.
	if r := []rune(s); len(r) > maxRecordedErrorLen {
		s = string(r[:maxRecordedErrorLen-1]) + "…"
	}
	return s
}

// NoTextHintThreshold is how many textless sessions a run must skip before the
// hint fires. A couple of genuinely empty sessions is ordinary; a whole run
// finding nothing to read is not.
const NoTextHintThreshold = 5

// RecordNoUsableText notes a run that attempted nothing because every session
// it wanted to summarize read back with no text.
//
// This exists because RecordRun cannot see it. RecordRun ignores runs with
// attempted == 0 on purpose — "nothing needed summarizing" is not evidence
// about anything — but "sessions needed summarizing and every one of them read
// back empty" is strong evidence, and it lands in that same branch. Without
// this, a provider whose stored format changed underneath us is indis-
// tinguishable from an idle index: the reads succeed, return nothing, and the
// picker goes on advising 'sesh index', which skips them all and says nothing.
//
// Deliberately provider-agnostic. The failure it catches is "a source we read
// stopped giving us text", which is as true of OpenCode's SQLite schema as of
// Claude's JSONL, and neither provider has to participate.
func (h *Health) RecordNoUsableText(skipped int, exampleID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.state.NoTextSkips = skipped
	h.state.NoTextExample = exampleID
}

// NoUsableText reports whether the last run had sessions to summarize and could
// not read text for any of them.
func (h *Health) NoUsableText() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.state.NoTextSkips >= NoTextHintThreshold
}

// NoTextSkips and NoTextExample return the detail behind NoUsableText.
func (h *Health) NoTextSkips() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.state.NoTextSkips
}

func (h *Health) NoTextExample() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.state.NoTextExample
}
