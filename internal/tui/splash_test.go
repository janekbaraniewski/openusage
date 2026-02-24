package tui

import (
	"strings"
	"testing"
)

func joinLines(lines []string) string {
	return strings.Join(lines, "\n")
}

func TestSplashProgressConnecting(t *testing.T) {
	m := Model{
		daemonStatus:  DaemonConnecting,
		providerOrder: []string{"openai", "anthropic", "cursor"},
		animFrame:     0,
	}
	lines := m.splashProgressLines()
	combined := joinLines(lines)

	if !strings.Contains(combined, "Configuration loaded") {
		t.Error("expected 'Configuration loaded' in output")
	}
	if !strings.Contains(combined, "3 providers detected") {
		t.Error("expected '3 providers detected' in output")
	}
	if !strings.Contains(combined, "Connecting to background helper") {
		t.Error("expected 'Connecting to background helper' in output")
	}
}

func TestSplashProgressNotInstalled(t *testing.T) {
	m := Model{
		daemonStatus:     DaemonNotInstalled,
		daemonInstalling: false,
		providerOrder:    []string{"openai"},
		animFrame:        0,
	}
	lines := m.splashProgressLines()
	combined := joinLines(lines)

	if !strings.Contains(combined, "Configuration loaded") {
		t.Error("expected 'Configuration loaded' in output")
	}
	if !strings.Contains(combined, "background helper") {
		t.Error("expected 'background helper' explanation in output")
	}
	if !strings.Contains(combined, "Press Enter to set it up") {
		t.Error("expected 'Press Enter to set it up' in output")
	}
}

func TestSplashProgressInstalling(t *testing.T) {
	m := Model{
		daemonStatus:     DaemonNotInstalled,
		daemonInstalling: true,
		providerOrder:    []string{"openai"},
		animFrame:        3,
	}
	lines := m.splashProgressLines()
	combined := joinLines(lines)

	if !strings.Contains(combined, "Configuration loaded") {
		t.Error("expected 'Configuration loaded' in output")
	}
	if !strings.Contains(combined, "Setting up background helper") {
		t.Error("expected 'Setting up background helper' in output")
	}
}

func TestSplashProgressStarting(t *testing.T) {
	m := Model{
		daemonStatus:  DaemonStarting,
		providerOrder: []string{"openai", "cursor"},
		animFrame:     5,
	}
	lines := m.splashProgressLines()
	combined := joinLines(lines)

	if !strings.Contains(combined, "Configuration loaded") {
		t.Error("expected 'Configuration loaded' in output")
	}
	if !strings.Contains(combined, "2 providers detected") {
		t.Error("expected '2 providers detected' in output")
	}
	if !strings.Contains(combined, "Starting background helper") {
		t.Error("expected 'Starting background helper' in output")
	}
}

func TestSplashProgressRunning(t *testing.T) {
	m := Model{
		daemonStatus:  DaemonRunning,
		hasData:       false,
		providerOrder: []string{"openai"},
		animFrame:     2,
	}
	lines := m.splashProgressLines()
	combined := joinLines(lines)

	if !strings.Contains(combined, "Configuration loaded") {
		t.Error("expected 'Configuration loaded' in output")
	}
	if !strings.Contains(combined, "Background helper running") {
		t.Error("expected 'Background helper running' in output")
	}
	if !strings.Contains(combined, "Fetching usage data") {
		t.Error("expected 'Fetching usage data' when hasData=false")
	}
}

func TestSplashProgressRunningWithData(t *testing.T) {
	m := Model{
		daemonStatus:  DaemonRunning,
		hasData:       true,
		providerOrder: []string{"openai"},
		animFrame:     2,
	}
	lines := m.splashProgressLines()
	combined := joinLines(lines)

	if !strings.Contains(combined, "Background helper running") {
		t.Error("expected 'Background helper running' in output")
	}
	if strings.Contains(combined, "Fetching usage data") {
		t.Error("should not contain 'Fetching usage data' when hasData=true")
	}
}

func TestSplashProgressError(t *testing.T) {
	m := Model{
		daemonStatus:  DaemonError,
		daemonMessage: "socket timeout after 5s",
		providerOrder: []string{"openai"},
		animFrame:     0,
	}
	lines := m.splashProgressLines()
	combined := joinLines(lines)

	if !strings.Contains(combined, "Configuration loaded") {
		t.Error("expected 'Configuration loaded' in output")
	}
	if !strings.Contains(combined, "socket timeout after 5s") {
		t.Error("expected error message in output")
	}
	if !strings.Contains(combined, "daemon status") {
		t.Error("expected 'daemon status' hint in output")
	}
}

func TestSplashProgressErrorDefault(t *testing.T) {
	m := Model{
		daemonStatus:  DaemonError,
		daemonMessage: "",
		providerOrder: []string{},
		animFrame:     0,
	}
	lines := m.splashProgressLines()
	combined := joinLines(lines)

	if !strings.Contains(combined, "Could not connect to background helper") {
		t.Error("expected default error message when daemonMessage is empty")
	}
	if !strings.Contains(combined, "No providers detected") {
		t.Error("expected 'No providers detected' with empty providerOrder")
	}
}

func TestSplashProgressOutdated(t *testing.T) {
	m := Model{
		daemonStatus:     DaemonOutdated,
		daemonInstalling: false,
		providerOrder:    []string{"openai", "anthropic"},
		animFrame:        0,
	}
	lines := m.splashProgressLines()
	combined := joinLines(lines)

	if !strings.Contains(combined, "Configuration loaded") {
		t.Error("expected 'Configuration loaded' in output")
	}
	if !strings.Contains(combined, "needs an update") {
		t.Error("expected 'needs an update' in output")
	}
	if !strings.Contains(combined, "Press Enter to update") {
		t.Error("expected 'Press Enter to update' in output")
	}
}

func TestSplashProgressOutdatedInstalling(t *testing.T) {
	m := Model{
		daemonStatus:     DaemonOutdated,
		daemonInstalling: true,
		providerOrder:    []string{"openai"},
		animFrame:        7,
	}
	lines := m.splashProgressLines()
	combined := joinLines(lines)

	if !strings.Contains(combined, "Updating background helper") {
		t.Error("expected 'Updating background helper' in output")
	}
	if strings.Contains(combined, "Press Enter to update") {
		t.Error("should not show 'Press Enter to update' when already installing")
	}
}

func TestSplashProgressNoProviders(t *testing.T) {
	m := Model{
		daemonStatus:  DaemonConnecting,
		providerOrder: nil,
		animFrame:     0,
	}
	lines := m.splashProgressLines()
	combined := joinLines(lines)

	if !strings.Contains(combined, "No providers detected") {
		t.Error("expected 'No providers detected' with nil providerOrder")
	}
}

func TestSplashProgressStartingAfterInstall(t *testing.T) {
	m := Model{
		daemonStatus:      DaemonStarting,
		daemonInstallDone: true,
		providerOrder:     []string{"openai"},
		animFrame:         0,
	}
	lines := m.splashProgressLines()
	combined := joinLines(lines)

	if !strings.Contains(combined, "Background helper installed") {
		t.Error("expected 'Background helper installed' step after install")
	}
	if !strings.Contains(combined, "Starting background helper") {
		t.Error("expected 'Starting background helper' spinner")
	}
}

func TestSplashProgressRunningAfterInstall(t *testing.T) {
	m := Model{
		daemonStatus:      DaemonRunning,
		daemonInstallDone: true,
		hasData:           false,
		providerOrder:     []string{"openai"},
		animFrame:         0,
	}
	lines := m.splashProgressLines()
	combined := joinLines(lines)

	if !strings.Contains(combined, "Background helper installed") {
		t.Error("expected 'Background helper installed' step after install")
	}
	if !strings.Contains(combined, "Background helper running") {
		t.Error("expected 'Background helper running' step")
	}
	if !strings.Contains(combined, "Fetching usage data") {
		t.Error("expected 'Fetching usage data' spinner")
	}
}

func TestSplashProgressErrorMultilineMessage(t *testing.T) {
	m := Model{
		daemonStatus:  DaemonError,
		daemonMessage: "context deadline exceeded\nsocket_path=/Users/test/.openusage/socket\nstatus_cmd=launchctl print",
		providerOrder: []string{"openai"},
		animFrame:     0,
	}
	lines := m.splashProgressLines()
	combined := joinLines(lines)

	if !strings.Contains(combined, "context deadline exceeded") {
		t.Error("expected first line of error message")
	}
	if strings.Contains(combined, "socket_path") {
		t.Error("diagnostic lines should be stripped from error display")
	}
}
