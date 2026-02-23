package daemon

import (
	"fmt"
	"net"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func shortSocketPath(t *testing.T, suffix string) string {
	t.Helper()
	return fmt.Sprintf("/tmp/openusage-%d-%s.sock", time.Now().UnixNano(), strings.TrimSpace(suffix))
}

func TestEnsureSocketPathAvailable_ActiveSocketReturnsError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix sockets are not supported in this test")
	}

	socketPath := shortSocketPath(t, "active")
	_ = os.Remove(socketPath)
	t.Cleanup(func() { _ = os.Remove(socketPath) })
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}
	defer listener.Close()

	err = EnsureSocketPathAvailable(socketPath)
	if err == nil {
		t.Fatal("expected error for active daemon socket")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "already running") {
		t.Fatalf("error = %q, want already running message", err)
	}
}

func TestEnsureSocketPathAvailable_RemovesStaleSocket(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix sockets are not supported in this test")
	}

	socketPath := shortSocketPath(t, "stale")
	_ = os.Remove(socketPath)
	t.Cleanup(func() { _ = os.Remove(socketPath) })
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	if _, statErr := os.Stat(socketPath); statErr != nil && !os.IsNotExist(statErr) {
		t.Fatalf("stat socket before ensure: %v", statErr)
	}

	if err := EnsureSocketPathAvailable(socketPath); err != nil {
		t.Fatalf("ensure socket path available: %v", err)
	}

	if _, statErr := os.Stat(socketPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected stale socket to be removed, stat err = %v", statErr)
	}
}

func TestEnsureSocketPathAvailable_RejectsRegularFile(t *testing.T) {
	socketPath := shortSocketPath(t, "file")
	_ = os.Remove(socketPath)
	t.Cleanup(func() { _ = os.Remove(socketPath) })
	if err := os.WriteFile(socketPath, []byte("not-a-socket"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	err := EnsureSocketPathAvailable(socketPath)
	if err == nil {
		t.Fatal("expected error for regular file at socket path")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "not a socket") {
		t.Fatalf("error = %q, want not a socket message", err)
	}
}
