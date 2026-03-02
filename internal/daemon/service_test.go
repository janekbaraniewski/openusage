package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLastErrorLine_ReturnsMostRecentError(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "daemon.stderr.log")
	content := strings.Join([]string{
		"hint: missing integration",
		"Error: telemetry daemon already running on socket /tmp/openusage.sock",
		"Usage:",
		"Error: open daemon telemetry store: telemetry: opening DB: permission denied",
		"tail line",
	}, "\n")
	if err := os.WriteFile(logPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	got := LastErrorLine(logPath)
	want := "open daemon telemetry store: telemetry: opening DB: permission denied"
	if got != want {
		t.Fatalf("LastErrorLine() = %q, want %q", got, want)
	}
}
