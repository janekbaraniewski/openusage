package daemon

import (
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/telemetry"
)

const (
	LaunchdDaemonLabel = "com.openusage.telemetryd"
	SystemdDaemonUnit  = "openusage-telemetry.service"
)

type ServiceManager struct {
	Kind       string
	exePath    string
	socketPath string
	stateDir   string
	unitPath   string
}

func (m ServiceManager) StdoutLogPath() string {
	if strings.TrimSpace(m.stateDir) == "" {
		return ""
	}
	return filepath.Join(m.stateDir, "daemon.stdout.log")
}

func (m ServiceManager) StderrLogPath() string {
	if strings.TrimSpace(m.stateDir) == "" {
		return ""
	}
	return filepath.Join(m.stateDir, "daemon.stderr.log")
}

func (m ServiceManager) StatusHint() string {
	switch m.Kind {
	case "darwin":
		return "launchctl print gui/$(id -u)/" + LaunchdDaemonLabel
	case "linux":
		return "systemctl --user status " + SystemdDaemonUnit
	default:
		return ""
	}
}

func NewServiceManager(socketPath string) (ServiceManager, error) {
	exePath, err := os.Executable()
	if err != nil {
		return ServiceManager{}, fmt.Errorf("resolve executable path: %w", err)
	}
	stateDir, err := telemetry.DefaultStateDir()
	if err != nil {
		return ServiceManager{}, err
	}

	manager := ServiceManager{
		Kind:       runtime.GOOS,
		exePath:    exePath,
		socketPath: strings.TrimSpace(socketPath),
		stateDir:   stateDir,
	}

	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return ServiceManager{}, fmt.Errorf("resolve home dir: %w", err)
		}
		manager.unitPath = filepath.Join(home, "Library", "LaunchAgents", LaunchdDaemonLabel+".plist")
	case "linux":
		home, err := os.UserHomeDir()
		if err != nil {
			return ServiceManager{}, fmt.Errorf("resolve home dir: %w", err)
		}
		manager.unitPath = filepath.Join(home, ".config", "systemd", "user", SystemdDaemonUnit)
	default:
		manager.Kind = "unsupported"
	}
	return manager, nil
}

func (m ServiceManager) IsSupported() bool {
	return m.Kind == "darwin" || m.Kind == "linux"
}

func (m ServiceManager) IsInstalled() bool {
	if strings.TrimSpace(m.unitPath) == "" {
		return false
	}
	_, err := os.Stat(m.unitPath)
	return err == nil
}

func (m ServiceManager) InstallHint() string {
	return "openusage telemetry daemon install"
}

func (m ServiceManager) Install() error {
	switch m.Kind {
	case "darwin":
		return m.installLaunchd()
	case "linux":
		return m.installSystemdUser()
	default:
		return fmt.Errorf("daemon service install is unsupported on %s", runtime.GOOS)
	}
}

func (m ServiceManager) Uninstall() error {
	switch m.Kind {
	case "darwin":
		return m.uninstallLaunchd()
	case "linux":
		return m.uninstallSystemdUser()
	default:
		return fmt.Errorf("daemon service uninstall is unsupported on %s", runtime.GOOS)
	}
}

func (m ServiceManager) Start() error {
	switch m.Kind {
	case "darwin":
		return m.startLaunchd()
	case "linux":
		_, err := RunCommand("systemctl", "--user", "start", SystemdDaemonUnit)
		return err
	default:
		return fmt.Errorf("daemon service start is unsupported on %s", runtime.GOOS)
	}
}

func (m ServiceManager) domainCandidates() []string {
	uid := fmt.Sprintf("%d", os.Getuid())
	return []string{"gui/" + uid, "user/" + uid}
}

func (m ServiceManager) installLaunchd() error {
	if err := os.MkdirAll(filepath.Dir(m.unitPath), 0o755); err != nil {
		return fmt.Errorf("create launch agents dir: %w", err)
	}
	if err := os.MkdirAll(m.stateDir, 0o755); err != nil {
		return fmt.Errorf("create telemetry state dir: %w", err)
	}

	stdoutPath := filepath.Join(m.stateDir, "daemon.stdout.log")
	stderrPath := filepath.Join(m.stateDir, "daemon.stderr.log")
	content := launchdPlist(m.exePath, m.socketPath, stdoutPath, stderrPath)
	if err := os.WriteFile(m.unitPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write launchd plist: %w", err)
	}

	var lastErr error
	for _, domain := range m.domainCandidates() {
		_, _ = RunCommand("launchctl", "bootout", domain+"/"+LaunchdDaemonLabel)
		if _, err := RunCommand("launchctl", "bootstrap", domain, m.unitPath); err != nil {
			lastErr = err
			continue
		}
		if _, err := RunCommand("launchctl", "kickstart", "-k", domain+"/"+LaunchdDaemonLabel); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("launchd bootstrap failed")
}

func (m ServiceManager) uninstallLaunchd() error {
	var lastErr error
	for _, domain := range m.domainCandidates() {
		_, err := RunCommand("launchctl", "bootout", domain+"/"+LaunchdDaemonLabel)
		if err != nil {
			if isLaunchctlNoSuchProcess(err) {
				continue
			}
			lastErr = err
		}
	}
	if err := os.Remove(m.unitPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove launchd plist: %w", err)
	}
	if lastErr != nil {
		return lastErr
	}
	return nil
}

func isLaunchctlNoSuchProcess(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(msg, "no such process") || strings.Contains(msg, "boot-out failed: 3")
}

func (m ServiceManager) startLaunchd() error {
	var lastErr error
	for _, domain := range m.domainCandidates() {
		if _, err := RunCommand("launchctl", "kickstart", "-k", domain+"/"+LaunchdDaemonLabel); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	if !m.IsInstalled() {
		return fmt.Errorf("launchd service is not installed")
	}
	var bootstrapErr error
	for _, domain := range m.domainCandidates() {
		if _, err := RunCommand("launchctl", "bootstrap", domain, m.unitPath); err != nil {
			bootstrapErr = err
			continue
		}
		if _, err := RunCommand("launchctl", "kickstart", "-k", domain+"/"+LaunchdDaemonLabel); err == nil {
			return nil
		} else {
			bootstrapErr = err
		}
	}
	if bootstrapErr != nil {
		return bootstrapErr
	}
	return lastErr
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

func RunCommand(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	trimmed := strings.TrimSpace(string(output))
	if err != nil {
		if trimmed != "" {
			return trimmed, fmt.Errorf("%s %s failed: %w (%s)", name, strings.Join(args, " "), err, trimmed)
		}
		return trimmed, fmt.Errorf("%s %s failed: %w", name, strings.Join(args, " "), err)
	}
	return trimmed, nil
}

func launchdPlist(exePath, socketPath, stdoutPath, stderrPath string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>telemetry</string>
		<string>daemon</string>
		<string>--socket-path</string>
		<string>%s</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>StandardOutPath</key>
	<string>%s</string>
	<key>StandardErrorPath</key>
	<string>%s</string>
</dict>
</plist>
`, LaunchdDaemonLabel, xmlEscape(exePath), xmlEscape(socketPath), xmlEscape(stdoutPath), xmlEscape(stderrPath))
}

func systemdUnit(exePath, socketPath string) string {
	return fmt.Sprintf(`[Unit]
Description=OpenUsage Telemetry Daemon
After=default.target

[Service]
Type=simple
ExecStart=%s telemetry daemon --socket-path %s
Restart=always
RestartSec=2
WorkingDirectory=%%h

[Install]
WantedBy=default.target
`, exePath, socketPath)
}

func xmlEscape(in string) string {
	var b strings.Builder
	if err := xml.EscapeText(&b, []byte(in)); err != nil {
		return in
	}
	return b.String()
}

func InstallService(socketPath string) error {
	manager, err := NewServiceManager(socketPath)
	if err != nil {
		return err
	}
	if !manager.IsSupported() {
		return fmt.Errorf("daemon service install is unsupported on %s", runtime.GOOS)
	}
	if err := manager.Install(); err != nil {
		return err
	}
	fmt.Printf("telemetry daemon service installed (%s)\n", manager.Kind)
	return nil
}

func UninstallService(socketPath string) error {
	manager, err := NewServiceManager(socketPath)
	if err != nil {
		return err
	}
	if !manager.IsSupported() {
		return fmt.Errorf("daemon service uninstall is unsupported on %s", runtime.GOOS)
	}
	if err := manager.Uninstall(); err != nil {
		return err
	}
	fmt.Printf("telemetry daemon service uninstalled (%s)\n", manager.Kind)
	return nil
}

func ServiceStatus(socketPath string) error {
	manager, err := NewServiceManager(socketPath)
	if err != nil {
		return err
	}
	client := NewClient(socketPath)
	health, healthErr := client.HealthInfo(context.Background())

	fmt.Printf("daemon kind=%s installed=%t running=%t socket=%s\n",
		manager.Kind,
		manager.IsInstalled(),
		healthErr == nil,
		socketPath,
	)
	if healthErr != nil {
		fmt.Printf("daemon health_error=%v\n", healthErr)
	} else {
		fmt.Printf(
			"daemon version=%s api=%s integration=%s\n",
			strings.TrimSpace(health.DaemonVersion),
			strings.TrimSpace(health.APIVersion),
			strings.TrimSpace(health.IntegrationVersion),
		)
	}
	return nil
}
