//go:build !windows

package daemon

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// errInstanceLocked means another daemon process holds the instance lock.
var errInstanceLocked = errors.New("telemetry daemon instance lock is held by another process")

// acquireInstanceLock takes a non-blocking exclusive flock on path and returns
// a func that releases it. The kernel drops the lock when the process exits,
// so a crashed daemon never leaves a stale lock behind.
func acquireInstanceLock(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open daemon lock %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("%w (%s)", errInstanceLocked, path)
		}
		return nil, fmt.Errorf("lock daemon lock %s: %w", path, err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
