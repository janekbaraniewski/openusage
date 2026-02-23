package main

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"
)

func ensureTelemetryDaemonRunning(ctx context.Context, socketPath string, verbose bool) (*telemetryDaemonClient, error) {
	return ensureTelemetryDaemonRunningWithMode(ctx, socketPath, verbose, false)
}

func ensureTelemetryDaemonRunningInteractive(ctx context.Context, socketPath string, verbose bool) (*telemetryDaemonClient, error) {
	return ensureTelemetryDaemonRunningWithMode(ctx, socketPath, verbose, true)
}

func ensureTelemetryDaemonRunningWithMode(
	ctx context.Context,
	socketPath string,
	verbose bool,
	allowInstallPrompt bool,
) (*telemetryDaemonClient, error) {
	socketPath = strings.TrimSpace(socketPath)
	if socketPath == "" {
		return nil, fmt.Errorf("daemon socket path is empty")
	}
	interactive := allowInstallPrompt && isInteractiveTerminal()
	client := newTelemetryDaemonClient(socketPath)
	statusf := func(format string, args ...any) {
		if !interactive {
			return
		}
		fmt.Fprintf(os.Stdout, format+"\n", args...)
	}

	statusf("Checking telemetry daemon...")

	if err := waitForTelemetryDaemonHealth(ctx, client, 1200*time.Millisecond); err == nil {
		statusf("Telemetry daemon is running.")
		return client, nil
	}
	statusf("Telemetry daemon not reachable at %s.", socketPath)

	manager, managerErr := newDaemonServiceManager(socketPath)
	if managerErr != nil {
		return nil, managerErr
	}
	if manager.isSupported() {
		if !manager.isInstalled() {
			if !allowInstallPrompt {
				return nil, fmt.Errorf("telemetry daemon service is not installed; run `%s`", manager.installHint())
			}
			approved, promptErr := promptInstallDaemonService(manager)
			if promptErr != nil {
				return nil, promptErr
			}
			if !approved {
				return nil, fmt.Errorf("telemetry daemon service installation declined; run `%s` to install manually", manager.installHint())
			}
			statusf("Installing telemetry daemon service...")
			if err := manager.install(); err != nil {
				return nil, fmt.Errorf("install telemetry daemon service: %w", err)
			}
			statusf("Telemetry daemon service installed.")
		} else {
			statusf("Telemetry daemon service is installed.")
		}
		statusf("Starting telemetry daemon service...")
		if err := manager.start(); err != nil {
			return nil, fmt.Errorf("start telemetry daemon service: %w\n%s", err, daemonStartupDiagnostics(manager, socketPath))
		}
		var waitErr error
		if interactive {
			waitErr = waitForTelemetryDaemonHealthWithProgress(ctx, client, 25*time.Second, "Waiting for telemetry daemon to become ready")
		} else {
			waitErr = waitForTelemetryDaemonHealth(ctx, client, 25*time.Second)
		}
		if waitErr != nil {
			return nil, fmt.Errorf("%w\n%s", waitErr, daemonStartupDiagnostics(manager, socketPath))
		}
		statusf("Telemetry daemon is ready.")
		return client, nil
	}

	// Unsupported OS: fallback to ad-hoc local process spawn.
	statusf("Starting telemetry daemon process...")
	if err := spawnTelemetryDaemonProcess(socketPath, verbose); err != nil {
		return nil, fmt.Errorf("start telemetry daemon: %w", err)
	}
	var waitErr error
	if interactive {
		waitErr = waitForTelemetryDaemonHealthWithProgress(ctx, client, 25*time.Second, "Waiting for telemetry daemon to become ready")
	} else {
		waitErr = waitForTelemetryDaemonHealth(ctx, client, 25*time.Second)
	}
	if waitErr != nil {
		return nil, waitErr
	}
	statusf("Telemetry daemon is ready.")
	return client, nil
}

func waitForTelemetryDaemonHealth(ctx context.Context, client *telemetryDaemonClient, timeout time.Duration) error {
	if client == nil {
		return fmt.Errorf("daemon client is nil")
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
		err := client.Health(hc)
		hcCancel()
		if err == nil {
			return nil
		}
		lastErr = err
		time.Sleep(220 * time.Millisecond)
	}
	if pingCtx.Err() != nil && pingCtx.Err() != context.Canceled {
		return pingCtx.Err()
	}
	if lastErr != nil {
		return fmt.Errorf("telemetry daemon did not become ready at %s: %w", client.socketPath, lastErr)
	}
	return fmt.Errorf("telemetry daemon did not become ready at %s", client.socketPath)
}

func waitForTelemetryDaemonHealthWithProgress(
	ctx context.Context,
	client *telemetryDaemonClient,
	timeout time.Duration,
	label string,
) error {
	if client == nil {
		return fmt.Errorf("daemon client is nil")
	}
	if strings.TrimSpace(label) == "" {
		label = "Waiting for telemetry daemon"
	}

	pingCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	deadline := time.Now().Add(timeout)
	started := time.Now()
	frames := []string{"|", "/", "-", "\\"}
	frame := 0
	var lastErr error

	for time.Now().Before(deadline) {
		if pingCtx.Err() != nil {
			break
		}
		hc, hcCancel := context.WithTimeout(pingCtx, 700*time.Millisecond)
		err := client.Health(hc)
		hcCancel()
		if err == nil {
			fmt.Fprintf(os.Stdout, "\r%s... done in %.1fs\n", label, time.Since(started).Seconds())
			return nil
		}
		lastErr = err
		elapsed := time.Since(started).Seconds()
		fmt.Fprintf(os.Stdout, "\r%s... %s %.1fs", label, frames[frame], elapsed)
		frame = (frame + 1) % len(frames)
		time.Sleep(220 * time.Millisecond)
	}
	fmt.Fprintln(os.Stdout)
	if pingCtx.Err() != nil && pingCtx.Err() != context.Canceled {
		return pingCtx.Err()
	}
	if lastErr != nil {
		return fmt.Errorf("telemetry daemon did not become ready at %s: %w", client.socketPath, lastErr)
	}
	return fmt.Errorf("telemetry daemon did not become ready at %s", client.socketPath)
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

func tailFile(path string, lines int) string {
	raw, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return ""
	}
	return tailTextLines(string(raw), lines)
}

func tailTextLines(text string, maxLines int) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimSpace(text)
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
