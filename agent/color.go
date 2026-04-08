// Package agent provides shared utilities for agent identity,
// including deterministic color assignment.
package agent

// ColorIndex returns a stable index into a 6-color palette for the given
// agent name. Uses djb2 hash for good distribution over short strings.
//
// The palette maps to ANSI colors 1-6 (red, green, yellow, blue, magenta, cyan).
func ColorIndex(name string) int {
	var h uint32 = 5381
	for i := 0; i < len(name); i++ {
		h = (h << 5) + h + uint32(name[i])
	}
	return int(h % 6)
}

// ANSIColor returns the ANSI color code (1-6) for the given agent name.
func ANSIColor(name string) int {
	return ColorIndex(name) + 1
}
