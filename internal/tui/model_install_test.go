package tui

import (
	"errors"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestDaemonInstallResultSuccess(t *testing.T) {
	m := NewModel(0.2, 0.1, false, config.DashboardConfig{}, nil, core.TimeWindow30d)
	m.daemonStatus = DaemonNotInstalled
	m.daemonInstalling = true

	updated, _ := m.Update(daemonInstallResultMsg{err: nil})
	got, ok := updated.(Model)
	if !ok {
		t.Fatalf("updated model type = %T, want tui.Model", updated)
	}

	if got.daemonInstalling {
		t.Fatal("expected daemonInstalling=false after successful install")
	}
	if got.daemonStatus != DaemonStarting {
		t.Fatalf("daemonStatus = %q, want %q", got.daemonStatus, DaemonStarting)
	}
	if !got.daemonInstallDone {
		t.Fatal("expected daemonInstallDone=true after successful install")
	}
}

func TestDaemonInstallResultFailure(t *testing.T) {
	m := NewModel(0.2, 0.1, false, config.DashboardConfig{}, nil, core.TimeWindow30d)
	m.daemonStatus = DaemonNotInstalled
	m.daemonInstalling = true

	installErr := errors.New("failed to install daemon")
	updated, _ := m.Update(daemonInstallResultMsg{err: installErr})
	got, ok := updated.(Model)
	if !ok {
		t.Fatalf("updated model type = %T, want tui.Model", updated)
	}

	if got.daemonInstalling {
		t.Fatal("expected daemonInstalling=false after failed install")
	}
	if got.daemonStatus != DaemonError {
		t.Fatalf("daemonStatus = %q, want %q", got.daemonStatus, DaemonError)
	}
	if got.daemonMessage != "failed to install daemon" {
		t.Fatalf("daemonMessage = %q, want %q", got.daemonMessage, "failed to install daemon")
	}
}
