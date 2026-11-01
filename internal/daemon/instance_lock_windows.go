//go:build windows

package daemon

import "errors"

// errInstanceLocked means another daemon process holds the instance lock.
var errInstanceLocked = errors.New("telemetry daemon instance lock is held by another process")

// acquireInstanceLock is a no-op on Windows; the daemon runs as a single
// scheduled task there and falls back to the socket dial-probe.
func acquireInstanceLock(string) (func(), error) {
	return func() {}, nil
}
