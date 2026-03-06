package tui

import (
	"errors"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestDaemonInstallResultSuccess(t *testing.T) {
	m := NewModel(0.2, 0.1, false, config.DashboardConfig{}, nil, core.TimeWindow30d)
	m.daemon.status = DaemonNotInstalled
	m.daemon.installing = true

	updated, _ := m.Update(daemonInstallResultMsg{err: nil})
	got, ok := updated.(Model)
	if !ok {
		t.Fatalf("updated model type = %T, want tui.Model", updated)
	}

	if got.daemon.installing {
		t.Fatal("expected daemonInstalling=false after successful install")
	}
	if got.daemon.status != DaemonStarting {
		t.Fatalf("daemonStatus = %q, want %q", got.daemon.status, DaemonStarting)
	}
	if !got.daemon.installDone {
		t.Fatal("expected daemonInstallDone=true after successful install")
	}
}

func TestDaemonInstallResultFailure(t *testing.T) {
	m := NewModel(0.2, 0.1, false, config.DashboardConfig{}, nil, core.TimeWindow30d)
	m.daemon.status = DaemonNotInstalled
	m.daemon.installing = true

	installErr := errors.New("failed to install daemon")
	updated, _ := m.Update(daemonInstallResultMsg{err: installErr})
	got, ok := updated.(Model)
	if !ok {
		t.Fatalf("updated model type = %T, want tui.Model", updated)
	}

	if got.daemon.installing {
		t.Fatal("expected daemonInstalling=false after failed install")
	}
	if got.daemon.status != DaemonError {
		t.Fatalf("daemonStatus = %q, want %q", got.daemon.status, DaemonError)
	}
	if got.daemon.message != "failed to install daemon" {
		t.Fatalf("daemonMessage = %q, want %q", got.daemon.message, "failed to install daemon")
	}
}
