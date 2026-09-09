package provider

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"os"
)

// Every agent surface sesh reads stores its history and transcripts as JSONL,
// one JSON object per line — so a single line carries a whole message, and a
// pasted file, a large tool result, or a long assistant response makes that
// line arbitrarily big. bufio.Scanner's 64 KB default is nowhere near enough,
// and the caps this code used to carry (1 MB, and 256 KB in one place) were
// not either: the largest line in a 252-file sample of ~/.claude/projects was
// 1484 KB.
//
// Getting it wrong fails silently and expensively. Scanner does not skip an
// oversized line and continue — Scan() returns false and the read is over, so
// an undersized cap drops the entire rest of the file. The transcript that
// blew the 1 MB cap had its long line at 320 of 340, so titles, session text,
// and summary input all quietly lost the last 20 messages.
//
// Start small and grow: the buffer is allocated on demand, so a generous
// ceiling costs nothing on the overwhelming majority of files that never
// approach it.
const (
	jsonlScanBufferStart = 64 * 1024
	jsonlScanBufferMax   = 32 * 1024 * 1024
)

// newJSONLScanner returns a bufio.Scanner sized for agent history and
// transcript files. Nothing outside this file calls it: readers go through
// jsonlLines, which is what keeps the limits in one place. claude.go and
// claude_cowork.go read the same files, and a cap that disagrees between them
// means one reader sees a session the other truncates.
func newJSONLScanner(r io.Reader) *bufio.Scanner {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 0, jsonlScanBufferStart), jsonlScanBufferMax)
	return s
}

// warnScanErr reports a failed scan to stderr. Scanner surfaces errors only
// through Err(), so leaving it unchecked turns a partial read into an
// invisible one: the caller returns whatever it accumulated before the failure
// with no signal that anything is missing. Callers keep their partial data —
// a truncated title beats no session at all — but the user gets told.
func warnScanErr(err error, path string) {
	if err == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "sesh: warning: reading %s: %v\n", path, err)
}

// jsonlLinesFrom yields each line of a JSONL stream and reports a short read
// through warnScanErr when the scan stops early, so no reader has to remember
// the buffer sizing or the Err() check. name is used only in that warning.
//
// The yielded slice belongs to the scanner and is overwritten on the next
// iteration. Unmarshal it or copy it; never retain it past the loop body.
//
// Breaking out of the loop skips the warning: the caller ended the read
// deliberately, so a short read is not a failure. Both early-exit readers
// (firstUserPrompt, extractSlug) behaved this way before the iterator existed,
// since their warnScanErr call sat after the loop they returned out of.
//
// Callers that need to distinguish "unreadable" from "empty" open the file
// themselves and pass the reader here — see ListSessions, where an unreadable
// history.jsonl has to surface as a provider error rather than zero sessions.
func jsonlLinesFrom(r io.Reader, name string) iter.Seq[[]byte] {
	return func(yield func([]byte) bool) {
		scanner := newJSONLScanner(r)
		for scanner.Scan() {
			if !yield(scanner.Bytes()) {
				return
			}
		}
		warnScanErr(scanner.Err(), name)
	}
}

// jsonlLines opens path and yields its lines, closing the file when the loop
// ends by any route. A file that cannot be opened yields nothing: for
// transcripts that is the right answer, since every caller already treats a
// missing transcript as an absent session rather than an error.
func jsonlLines(path string) iter.Seq[[]byte] {
	return func(yield func([]byte) bool) {
		f, err := os.Open(path)
		if err != nil {
			return
		}
		defer f.Close()
		jsonlLinesFrom(f, path)(yield)
	}
}

// jsonlDecode adapts a line iterator into a record iterator, skipping lines
// that do not parse as T. Transcripts carry many record shapes and every
// reader wants one of them, so a line that does not fit is routine rather than
// an error worth reporting.
//
// Composed rather than opening files itself, so it works over both jsonlLines
// and jsonlLinesFrom without a second set of path/reader variants:
//
//	for rec := range jsonlDecode[slugRecord](jsonlLines(path)) { ... }
func jsonlDecode[T any](lines iter.Seq[[]byte]) iter.Seq[T] {
	return func(yield func(T) bool) {
		for line := range lines {
			var rec T
			if json.Unmarshal(line, &rec) != nil {
				continue
			}
			if !yield(rec) {
				return
			}
		}
	}
}
