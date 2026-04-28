// Package testhelper provides utilities for cross-platform test scripts.
package testhelper

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// WriteMockScript creates an executable script in a temp directory and returns
// the command slice to invoke it. On Unix it writes a POSIX shell script and
// returns [path]. On Windows it writes a PowerShell script and returns
// [powershell -ExecutionPolicy Bypass -NonInteractive -NoProfile -File path].
//
// shContent is the full POSIX shell script text (including shebang).
// psContent is the equivalent PowerShell script text.
func WriteMockScript(t *testing.T, shContent, psContent string) []string {
	t.Helper()
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		path := filepath.Join(dir, "mock.ps1")
		if err := os.WriteFile(path, []byte(psContent), 0644); err != nil {
			t.Fatal(err)
		}
		return []string{
			"powershell",
			"-ExecutionPolicy", "Bypass",
			"-NonInteractive",
			"-NoProfile",
			"-File", path,
		}
	}
	path := filepath.Join(dir, "mock.sh")
	if err := os.WriteFile(path, []byte(shContent), 0755); err != nil {
		t.Fatal(err)
	}
	return []string{path}
}
