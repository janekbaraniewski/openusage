package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"
)

func ensureTelemetryDaemonRunning(ctx context.Context, socketPath string, verbose bool) (*telemetryDaemonClient, error) {
	socketPath = strings.TrimSpace(socketPath)
	if socketPath == "" {
		return nil, fmt.Errorf("daemon socket path is empty")
	}
	client := newTelemetryDaemonClient(socketPath)

	healthCtx, cancel := context.WithTimeout(ctx, 1200*time.Millisecond)
	healthErr := client.Health(healthCtx)
	cancel()
	if healthErr == nil {
		return client, nil
	}

	if err := spawnTelemetryDaemonProcess(socketPath, verbose); err != nil {
		return nil, fmt.Errorf("start telemetry daemon: %w", err)
	}

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		pingCtx, pingCancel := context.WithTimeout(ctx, 700*time.Millisecond)
		err := client.Health(pingCtx)
		pingCancel()
		if err == nil {
			return client, nil
		}
		time.Sleep(220 * time.Millisecond)
	}
	return nil, fmt.Errorf("telemetry daemon did not become ready at %s", socketPath)
}

func spawnTelemetryDaemonProcess(socketPath string, verbose bool) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}

	args := []string{"telemetry", "daemon", "--socket-path", socketPath}
	if verbose {
		args = append(args, "--verbose")
	}

	cmd := exec.Command(exe, args...)
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err == nil {
		cmd.Stdout = devNull
		cmd.Stderr = devNull
		cmd.Stdin = devNull
		defer devNull.Close()
	}
	if runtime.GOOS != "windows" {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	}

	if err := cmd.Start(); err != nil {
		return err
	}
	_ = cmd.Process.Release()
	return nil
}
