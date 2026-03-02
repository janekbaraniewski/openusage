//go:build darwin

package daemon

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (m ServiceManager) Install() error {
	if isTransientExecutablePath(m.exePath) {
		return fmt.Errorf(
			"refusing to install telemetry daemon service from transient executable %q (likely from `go run`); build a stable binary first, then run `./bin/openusage telemetry daemon install`",
			m.exePath,
		)
	}
	return m.installLaunchd()
}

func (m ServiceManager) Uninstall() error {
	return m.uninstallLaunchd()
}

func (m ServiceManager) Start() error {
	return m.startLaunchd()
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
		if _, err := RunCommand("launchctl", "kickstart", domain+"/"+LaunchdDaemonLabel); err != nil && !isLaunchctlAlreadyRunning(err) {
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

func isLaunchctlAlreadyRunning(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(msg, "already running") || strings.Contains(msg, "service already running")
}

func (m ServiceManager) startLaunchd() error {
	var lastErr error
	for _, domain := range m.domainCandidates() {
		if _, err := RunCommand("launchctl", "kickstart", domain+"/"+LaunchdDaemonLabel); err == nil || isLaunchctlAlreadyRunning(err) {
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
		if _, err := RunCommand("launchctl", "kickstart", domain+"/"+LaunchdDaemonLabel); err == nil || isLaunchctlAlreadyRunning(err) {
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
		<string>run</string>
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

func xmlEscape(in string) string {
	var b strings.Builder
	if err := xml.EscapeText(&b, []byte(in)); err != nil {
		return in
	}
	return b.String()
}
