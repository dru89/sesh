package provider

import (
	"bufio"
	"fmt"
	"io"
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
// transcript files. Every JSONL reader in this package goes through it so the
// limits stay in one place — claude.go and claude_cowork.go read the same
// files, and a cap that disagrees between them means one reader sees a session
// the other truncates.
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
