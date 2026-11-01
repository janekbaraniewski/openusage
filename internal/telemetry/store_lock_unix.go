//go:build !windows

package telemetry

import (
	"errors"
	"os"
	"syscall"
)

// tryLockFile takes a non-blocking flock on f. flock locks belong to the open
// file description, so two descriptors conflict even within one process.
func tryLockFile(f *os.File, exclusive bool) error {
	how := syscall.LOCK_SH
	if exclusive {
		how = syscall.LOCK_EX
	}
	for {
		err := syscall.Flock(int(f.Fd()), how|syscall.LOCK_NB)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return errLockBusy
		}
		return err
	}
}

func unlockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
