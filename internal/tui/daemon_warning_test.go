package tui

import (
	"strings"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestWrapSplashText_KeepsTailOfLongErrors(t *testing.T) {
	msg := "upgrade telemetry daemon service: launchctl bootstrap user/501 /Users/me/Library/LaunchAgents/com.openusage.telemetryd.plist failed: exit status 5"
	lines := wrapSplashText(msg, 64, 4)
	if len(lines) < 2 {
		t.Fatalf("expected wrapped lines, got %q", lines)
	}
	joined := strings.Join(lines, " ")
	if !strings.Contains(joined, "exit status 5") {
		t.Fatalf("wrapped text lost the exit status: %q", lines)
	}
	for _, l := range lines {
		if len(l) > 64 {
			t.Fatalf("line exceeds width: %q", l)
		}
	}
}

func TestDaemonWarning_RunningWithWarningDoesNotBlockAndShowsInFooter(t *testing.T) {
	m := NewModel(0.2, 0.1, config.DashboardConfig{}, nil, core.TimeWindow30d)
	updated, _ := m.Update(DaemonStatusMsg{Status: DaemonRunning, Warning: "Background helper is 0.24.4, this openusage is v0.25.1"})
	m = updated.(Model)

	if got := m.daemonWarning(); !strings.Contains(got, "0.24.4") {
		t.Fatalf("daemonWarning() = %q", got)
	}
	splash := strings.Join(m.splashProgressLines(), "\n")
	if !strings.Contains(splash, "Background helper running") || !strings.Contains(splash, "0.24.4") {
		t.Fatalf("splash should show running plus warning:\n%s", splash)
	}
	if footer := m.renderFooterStatusLine(200); !strings.Contains(footer, "0.24.4") {
		t.Fatalf("footer should carry the warning, got %q", footer)
	}

	// Informational Message (hub view) must not be rendered as a warning.
	updated, _ = m.Update(DaemonStatusMsg{Status: DaemonRunning, Message: "hub x · 3 machine snapshots"})
	m = updated.(Model)
	if got := m.daemonWarning(); got != "" {
		t.Fatalf("daemonWarning() = %q, want empty for informational message", got)
	}
}
