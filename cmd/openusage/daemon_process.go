package main

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/version"
)

func ensureTelemetryDaemonRunning(ctx context.Context, socketPath string, verbose bool) (*telemetryDaemonClient, error) {
	socketPath = strings.TrimSpace(socketPath)
	if socketPath == "" {
		return nil, fmt.Errorf("daemon socket path is empty")
	}
	client := newTelemetryDaemonClient(socketPath)

	health, healthErr := waitForTelemetryDaemonHealthInfo(ctx, client, 1200*time.Millisecond)
	if healthErr == nil && daemonHealthCurrent(health) {
		return client, nil
	}

	needsUpgrade := healthErr == nil
	return ensureViaServiceManager(ctx, client, socketPath, verbose, needsUpgrade, health)
}

func ensureViaServiceManager(
	ctx context.Context,
	client *telemetryDaemonClient,
	socketPath string,
	verbose bool,
	needsUpgrade bool,
	health daemonHealthResponse,
) (*telemetryDaemonClient, error) {
	manager, err := newDaemonServiceManager(socketPath)
	if err != nil {
		return nil, err
	}

	if needsUpgrade && !manager.isSupported() {
		return nil, fmt.Errorf(
			"telemetry daemon is out of date (running=%s expected=%s) and auto-upgrade is unsupported on %s",
			daemonHealthVersion(health), strings.TrimSpace(version.Version), runtime.GOOS,
		)
	}

	if manager.isSupported() {
		return startViaManagedService(ctx, client, manager, needsUpgrade, socketPath)
	}

	if err := spawnTelemetryDaemonProcess(socketPath, verbose); err != nil {
		return nil, fmt.Errorf("start telemetry daemon: %w", err)
	}
	if err := waitAndVerifyDaemon(ctx, client, socketPath); err != nil {
		return nil, err
	}
	return client, nil
}

func startViaManagedService(
	ctx context.Context,
	client *telemetryDaemonClient,
	manager daemonServiceManager,
	needsUpgrade bool,
	socketPath string,
) (*telemetryDaemonClient, error) {
	if needsUpgrade {
		if err := manager.install(); err != nil {
			return nil, fmt.Errorf("upgrade telemetry daemon service: %w", err)
		}
	}
	if !manager.isInstalled() {
		return nil, fmt.Errorf("telemetry daemon service is not installed; run `%s`", manager.installHint())
	}
	if err := manager.start(); err != nil {
		return nil, fmt.Errorf("start telemetry daemon service: %w\n%s", err, daemonStartupDiagnostics(manager, socketPath))
	}
	if err := waitAndVerifyDaemon(ctx, client, socketPath); err != nil {
		return nil, fmt.Errorf("%w\n%s", err, daemonStartupDiagnostics(manager, socketPath))
	}
	return client, nil
}

func waitAndVerifyDaemon(ctx context.Context, client *telemetryDaemonClient, socketPath string) error {
	if err := waitForTelemetryDaemonHealth(ctx, client, 25*time.Second); err != nil {
		return err
	}
	health, err := waitForTelemetryDaemonHealthInfo(ctx, client, 1500*time.Millisecond)
	if err != nil {
		return nil
	}
	if !daemonHealthCurrent(health) {
		return fmt.Errorf(
			"telemetry daemon is out of date (running=%s expected=%s)",
			daemonHealthVersion(health), strings.TrimSpace(version.Version),
		)
	}
	return nil
}

func daemonHealthVersion(health daemonHealthResponse) string {
	if v := strings.TrimSpace(health.DaemonVersion); v != "" {
		return v
	}
	return "unknown"
}

func daemonHealthCurrent(health daemonHealthResponse) bool {
	expected := strings.TrimSpace(version.Version)
	if expected == "" || strings.EqualFold(expected, "dev") || !isReleaseSemverVersion(expected) {
		return daemonHealthAPICompatible(health)
	}
	return strings.TrimSpace(health.DaemonVersion) == expected && daemonHealthAPICompatible(health)
}

func daemonHealthAPICompatible(health daemonHealthResponse) bool {
	apiVersion := strings.TrimSpace(health.APIVersion)
	return apiVersion == "" || apiVersion == telemetryDaemonAPIVersion
}

func isReleaseSemverVersion(value string) bool {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "v") {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(value, "v"), ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, ch := range part {
			if ch < '0' || ch > '9' {
				return false
			}
		}
	}
	return true
}

func waitForTelemetryDaemonHealth(ctx context.Context, client *telemetryDaemonClient, timeout time.Duration) error {
	_, err := waitForTelemetryDaemonHealthInfo(ctx, client, timeout)
	return err
}

func waitForTelemetryDaemonHealthInfo(
	ctx context.Context,
	client *telemetryDaemonClient,
	timeout time.Duration,
) (daemonHealthResponse, error) {
	if client == nil {
		return daemonHealthResponse{}, fmt.Errorf("daemon client is nil")
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
		return daemonHealthResponse{}, pingCtx.Err()
	}
	if lastErr != nil {
		return daemonHealthResponse{}, fmt.Errorf("telemetry daemon did not become ready at %s: %w", client.socketPath, lastErr)
	}
	return daemonHealthResponse{}, fmt.Errorf("telemetry daemon did not become ready at %s", client.socketPath)
}

func daemonStartupDiagnostics(manager daemonServiceManager, socketPath string) string {
	lines := []string{
		fmt.Sprintf("socket_path=%s", strings.TrimSpace(socketPath)),
	}
	if hint := strings.TrimSpace(manager.statusHint()); hint != "" {
		lines = append(lines, "status_cmd="+hint)
	}
	if stderrPath := strings.TrimSpace(manager.stderrLogPath()); stderrPath != "" {
		lines = append(lines, "stderr_log="+stderrPath)
		if tail := tailFile(stderrPath, 30); strings.TrimSpace(tail) != "" {
			lines = append(lines, "stderr_tail:\n"+tail)
		}
	}
	if stdoutPath := strings.TrimSpace(manager.stdoutLogPath()); stdoutPath != "" {
		lines = append(lines, "stdout_log="+stdoutPath)
	}
	if manager.kind == "darwin" {
		domain := fmt.Sprintf("gui/%d/%s", os.Getuid(), launchdDaemonLabel)
		if out, err := runCommand("launchctl", "print", domain); err == nil {
			if tail := tailTextLines(out, 30); strings.TrimSpace(tail) != "" {
				lines = append(lines, "launchctl_print_tail:\n"+tail)
			}
		}
	}
	return strings.Join(lines, "\n")
}

func tailFile(path string, maxLines int) string {
	raw, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return ""
	}
	return tailTextLines(string(raw), maxLines)
}

func tailTextLines(text string, maxLines int) string {
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

func spawnTelemetryDaemonProcess(socketPath string, verbose bool) error {
	_ = verbose
	_ = socketPath
	return fmt.Errorf("daemon process auto-spawn is unsupported on %s without a managed service", runtime.GOOS)
}
