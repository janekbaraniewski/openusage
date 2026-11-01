//go:build !windows

package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstanceLockRejectsSecondHolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.sock.lock")

	release, err := acquireInstanceLock(path)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if _, err := acquireInstanceLock(path); !errors.Is(err, errInstanceLocked) {
		t.Fatalf("second acquire err = %v, want errInstanceLocked", err)
	}

	release()
	release2, err := acquireInstanceLock(path)
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	release2()
}

// A second daemon must refuse to start while the first holds the lock, and
// must not touch the first daemon's socket file on the way out.
func TestStartSocketServerRejectsSecondDaemon(t *testing.T) {
	// Short dir: unix socket paths are length-limited on macOS.
	dir, err := os.MkdirTemp("", "oud")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "d.sock")

	release, err := acquireInstanceLock(InstanceLockPath(socket))
	if err != nil {
		t.Fatalf("hold lock: %v", err)
	}
	defer release()
	// Stand-in for the first daemon's socket. Without the lock, the second
	// daemon would probe it, find nothing listening and delete it.
	if err := os.WriteFile(socket, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	second := &Service{cfg: Config{SocketPath: socket}}
	err = second.startSocketServer(context.Background())
	if err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("second daemon start err = %v, want already running", err)
	}
	if _, statErr := os.Stat(socket); statErr != nil {
		t.Fatalf("second daemon removed the first daemon's socket: %v", statErr)
	}
}
