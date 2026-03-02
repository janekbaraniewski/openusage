//go:build linux

package daemon

import (
	"fmt"
	"os"
	"path/filepath"
)

func (m ServiceManager) Install() error {
	if isTransientExecutablePath(m.exePath) {
		return fmt.Errorf(
			"refusing to install telemetry daemon service from transient executable %q (likely from `go run`); build a stable binary first, then run `./bin/openusage telemetry daemon install`",
			m.exePath,
		)
	}
	return m.installSystemdUser()
}

func (m ServiceManager) Uninstall() error {
	return m.uninstallSystemdUser()
}

func (m ServiceManager) Start() error {
	_, err := RunCommand("systemctl", "--user", "start", SystemdDaemonUnit)
	return err
}

func (m ServiceManager) installSystemdUser() error {
	if err := os.MkdirAll(filepath.Dir(m.unitPath), 0o755); err != nil {
		return fmt.Errorf("create systemd user dir: %w", err)
	}
	if err := os.MkdirAll(m.stateDir, 0o755); err != nil {
		return fmt.Errorf("create telemetry state dir: %w", err)
	}

	content := systemdUnit(m.exePath, m.socketPath)
	if err := os.WriteFile(m.unitPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write systemd unit: %w", err)
	}
	if _, err := RunCommand("systemctl", "--user", "daemon-reload"); err != nil {
		return err
	}
	if _, err := RunCommand("systemctl", "--user", "enable", "--now", SystemdDaemonUnit); err != nil {
		return err
	}
	return nil
}

func (m ServiceManager) uninstallSystemdUser() error {
	_, _ = RunCommand("systemctl", "--user", "disable", "--now", SystemdDaemonUnit)
	if err := os.Remove(m.unitPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove systemd unit: %w", err)
	}
	_, _ = RunCommand("systemctl", "--user", "daemon-reload")
	return nil
}

func systemdUnit(exePath, socketPath string) string {
	return fmt.Sprintf(`[Unit]
Description=OpenUsage Telemetry Daemon
After=default.target

[Service]
Type=simple
ExecStart=%s telemetry daemon run --socket-path %s
Restart=always
RestartSec=2
WorkingDirectory=%%h

[Install]
WantedBy=default.target
`, exePath, socketPath)
}
