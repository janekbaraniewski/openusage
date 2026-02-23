package daemon

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"golang.org/x/mod/semver"

	"github.com/janekbaraniewski/openusage/internal/version"
)

func ClassifyEnsureError(err error) DaemonState {
	if err == nil {
		return DaemonState{Status: DaemonStatusRunning}
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "not installed"):
		return DaemonState{
			Status:      DaemonStatusNotInstalled,
			Message:     "Daemon service is not installed.",
			InstallHint: "openusage telemetry daemon install",
		}
	case strings.Contains(msg, "out of date"):
		return DaemonState{
			Status:  DaemonStatusOutdated,
			Message: msg,
		}
	case strings.Contains(msg, "unsupported on"):
		return DaemonState{
			Status:  DaemonStatusError,
			Message: msg,
		}
	default:
		return DaemonState{
			Status:  DaemonStatusError,
			Message: msg,
		}
	}
}

func EnsureRunning(ctx context.Context, socketPath string, verbose bool) (*Client, error) {
	socketPath = strings.TrimSpace(socketPath)
	if socketPath == "" {
		return nil, fmt.Errorf("daemon socket path is empty")
	}
	client := NewClient(socketPath)

	health, healthErr := WaitForHealthInfo(ctx, client, 1200*time.Millisecond)
	if healthErr == nil && HealthCurrent(health) {
		return client, nil
	}

	needsUpgrade := healthErr == nil
	return ensureViaServiceManager(ctx, client, socketPath, verbose, needsUpgrade, health)
}

func ensureViaServiceManager(
	ctx context.Context,
	client *Client,
	socketPath string,
	verbose bool,
	needsUpgrade bool,
	health HealthResponse,
) (*Client, error) {
	manager, err := NewServiceManager(socketPath)
	if err != nil {
		return nil, err
	}

	if needsUpgrade && !manager.IsSupported() {
		return nil, fmt.Errorf(
			"telemetry daemon is out of date (running=%s expected=%s) and auto-upgrade is unsupported on %s",
			HealthVersion(health), strings.TrimSpace(version.Version), runtime.GOOS,
		)
	}

	if manager.IsSupported() {
		return startViaManagedService(ctx, client, manager, needsUpgrade, socketPath)
	}

	if err := spawnDaemonProcess(socketPath, verbose); err != nil {
		return nil, fmt.Errorf("start telemetry daemon: %w", err)
	}
	if err := waitAndVerifyDaemon(ctx, client, socketPath); err != nil {
		return nil, err
	}
	return client, nil
}

func startViaManagedService(
	ctx context.Context,
	client *Client,
	manager ServiceManager,
	needsUpgrade bool,
	socketPath string,
) (*Client, error) {
	if needsUpgrade {
		if err := manager.Install(); err != nil {
			return nil, fmt.Errorf("upgrade telemetry daemon service: %w", err)
		}
	}
	if !manager.IsInstalled() {
		return nil, fmt.Errorf("telemetry daemon service is not installed; run `%s`", manager.InstallHint())
	}
	if err := manager.Start(); err != nil {
		return nil, fmt.Errorf("start telemetry daemon service: %w\n%s", err, StartupDiagnostics(manager, socketPath))
	}
	if err := waitAndVerifyDaemon(ctx, client, socketPath); err != nil {
		return nil, fmt.Errorf("%w\n%s", err, StartupDiagnostics(manager, socketPath))
	}
	return client, nil
}

func waitAndVerifyDaemon(ctx context.Context, client *Client, socketPath string) error {
	if err := WaitForHealth(ctx, client, 25*time.Second); err != nil {
		return err
	}
	health, err := WaitForHealthInfo(ctx, client, 1500*time.Millisecond)
	if err != nil {
		return nil
	}
	if !HealthCurrent(health) {
		return fmt.Errorf(
			"telemetry daemon is out of date (running=%s expected=%s)",
			HealthVersion(health), strings.TrimSpace(version.Version),
		)
	}
	return nil
}

func HealthVersion(health HealthResponse) string {
	if v := strings.TrimSpace(health.DaemonVersion); v != "" {
		return v
	}
	return "unknown"
}

func HealthCurrent(health HealthResponse) bool {
	expected := strings.TrimSpace(version.Version)
	if expected == "" || strings.EqualFold(expected, "dev") || !IsReleaseSemver(expected) {
		return HealthAPICompatible(health)
	}
	return strings.TrimSpace(health.DaemonVersion) == expected && HealthAPICompatible(health)
}

func HealthAPICompatible(health HealthResponse) bool {
	apiVersion := strings.TrimSpace(health.APIVersion)
	return apiVersion == "" || apiVersion == APIVersion
}

func IsReleaseSemver(value string) bool {
	v := strings.TrimSpace(value)
	if !semver.IsValid(v) {
		return false
	}
	if semver.Prerelease(v) != "" || semver.Build(v) != "" {
		return false
	}
	return v == semver.Canonical(v)
}

func WaitForHealth(ctx context.Context, client *Client, timeout time.Duration) error {
	_, err := WaitForHealthInfo(ctx, client, timeout)
	return err
}

func WaitForHealthInfo(
	ctx context.Context,
	client *Client,
	timeout time.Duration,
) (HealthResponse, error) {
	if client == nil {
		return HealthResponse{}, fmt.Errorf("daemon client is nil")
	}
	pingCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		if pingCtx.Err() != nil {
			break
		}
		hc, hcCancel := context.WithTimeout(pingCtx, 700*time.Millisecond)
		health, err := client.HealthInfo(hc)
		hcCancel()
		if err == nil {
			return health, nil
		}
		lastErr = err
		time.Sleep(220 * time.Millisecond)
	}
	if pingCtx.Err() != nil && pingCtx.Err() != context.Canceled {
		return HealthResponse{}, pingCtx.Err()
	}
	if lastErr != nil {
		return HealthResponse{}, fmt.Errorf("telemetry daemon did not become ready at %s: %w", client.SocketPath, lastErr)
	}
	return HealthResponse{}, fmt.Errorf("telemetry daemon did not become ready at %s", client.SocketPath)
}

func StartupDiagnostics(manager ServiceManager, socketPath string) string {
	lines := []string{
		fmt.Sprintf("socket_path=%s", strings.TrimSpace(socketPath)),
	}
	if hint := strings.TrimSpace(manager.StatusHint()); hint != "" {
		lines = append(lines, "status_cmd="+hint)
	}
	if stderrPath := strings.TrimSpace(manager.StderrLogPath()); stderrPath != "" {
		lines = append(lines, "stderr_log="+stderrPath)
		if tail := TailFile(stderrPath, 30); strings.TrimSpace(tail) != "" {
			lines = append(lines, "stderr_tail:\n"+tail)
		}
	}
	if stdoutPath := strings.TrimSpace(manager.StdoutLogPath()); stdoutPath != "" {
		lines = append(lines, "stdout_log="+stdoutPath)
	}
	if manager.Kind == "darwin" {
		domain := fmt.Sprintf("gui/%d/%s", os.Getuid(), LaunchdDaemonLabel)
		if out, err := RunCommand("launchctl", "print", domain); err == nil {
			if tail := TailTextLines(out, 30); strings.TrimSpace(tail) != "" {
				lines = append(lines, "launchctl_print_tail:\n"+tail)
			}
		}
	}
	return strings.Join(lines, "\n")
}

func TailFile(path string, maxLines int) string {
	raw, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return ""
	}
	return TailTextLines(string(raw), maxLines)
}

func TailTextLines(text string, maxLines int) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	if text == "" {
		return ""
	}
	if maxLines <= 0 {
		maxLines = 20
	}
	parts := strings.Split(text, "\n")
	if len(parts) <= maxLines {
		return strings.Join(parts, "\n")
	}
	return strings.Join(parts[len(parts)-maxLines:], "\n")
}

func spawnDaemonProcess(socketPath string, verbose bool) error {
	_ = verbose
	_ = socketPath
	return fmt.Errorf("daemon process auto-spawn is unsupported on %s without a managed service", runtime.GOOS)
}
