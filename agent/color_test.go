package agent

import "testing"

func TestColorIndex_Deterministic(t *testing.T) {
	// Verify the same name always produces the same index.
	for i := 0; i < 100; i++ {
		if ColorIndex("opencode") != ColorIndex("opencode") {
			t.Fatal("ColorIndex is not deterministic")
		}
	}
}

func TestColorIndex_StableMapping(t *testing.T) {
	// Pin known agents to expected indices. If the hash or palette size
	// changes, this test fails — which is the point. Agent colors should
	// not silently change.
	tests := []struct {
		name string
		want int
	}{
		{"opencode", 0},
		{"claude", 1},
		{"pi-mono", 4},
		{"omp", 1},
		{"cursor", 5},
		{"aider", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ColorIndex(tt.name)
			if got != tt.want {
				t.Errorf("ColorIndex(%q) = %d, want %d", tt.name, got, tt.want)
			}
		})
	}
}

func TestANSIColor_Range(t *testing.T) {
	// ANSI color must be in 1-6 for any input.
	names := []string{"opencode", "claude", "pi-mono", "omp", "cursor", "windsurf", "copilot", "aider", "", "x", "a-very-long-agent-name"}
	for _, name := range names {
		c := ANSIColor(name)
		if c < 1 || c > 6 {
			t.Errorf("ANSIColor(%q) = %d, want 1-6", name, c)
		}
	}
}

func TestANSIColor_MatchesColorIndex(t *testing.T) {
	names := []string{"opencode", "claude", "omp"}
	for _, name := range names {
		if ANSIColor(name) != ColorIndex(name)+1 {
			t.Errorf("ANSIColor(%q) != ColorIndex(%q)+1", name, name)
		}
	}
}
