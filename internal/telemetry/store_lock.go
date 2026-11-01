package telemetry

import (
	"errors"
	"fmt"
	"os"
	"time"
)

// errLockBusy is returned by tryLockFile when another open file (in this or
// another process) holds a conflicting lock.
var errLockBusy = errors.New("lock busy")

// openGateTimeout bounds how long OpenStore waits for another opener that is
// in the middle of opening (and possibly recovering) the same database.
var openGateTimeout = 30 * time.Second

// dbOpenLock coordinates OpenStore callers across processes so that the
// destructive recovery steps (removing a stale -shm, quarantining a corrupt
// DB, pruning old corrupt backups) only run when this process is provably the
// only one with the database open.
//
// Two advisory lock files sit next to the database:
//
//   - <db>.lock is held SHARED by every Store for its whole lifetime. An
//     opener that can take it EXCLUSIVE knows no other Store has the DB open.
//   - <db>.open.lock is a short-lived gate held EXCLUSIVE while an opener
//     probes <db>.lock and, if it is the sole opener, while it recovers. It
//     makes the EXCLUSIVE to SHARED conversion on <db>.lock race-free: flock
//     conversion is not atomic on Linux and LockFileEx has no conversion at
//     all, so without the gate a second opener could grab EXCLUSIVE in the gap.
type dbOpenLock struct {
	life *os.File
	gate *os.File
	sole bool
}

// acquireDBOpenLock takes the gate and probes for sole ownership. On success
// the gate is still held; call finishOpen once the database is open.
func acquireDBOpenLock(dbPath string) (*dbOpenLock, error) {
	gate, err := os.OpenFile(dbPath+".open.lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("telemetry: opening open-gate lock: %w", err)
	}
	deadline := time.Now().Add(openGateTimeout)
	for {
		err := tryLockFile(gate, true)
		if err == nil {
			break
		}
		if !errors.Is(err, errLockBusy) || time.Now().After(deadline) {
			_ = gate.Close()
			return nil, fmt.Errorf("telemetry: acquiring open-gate lock: %w", err)
		}
		time.Sleep(50 * time.Millisecond)
	}

	life, err := os.OpenFile(dbPath+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		_ = unlockFile(gate)
		_ = gate.Close()
		return nil, fmt.Errorf("telemetry: opening store lock: %w", err)
	}
	l := &dbOpenLock{life: life, gate: gate}

	switch err := tryLockFile(life, true); {
	case err == nil:
		l.sole = true
	case errors.Is(err, errLockBusy):
		// Other Stores are open. Exclusive holders always hold the gate and we
		// hold the gate, so a shared lock is available immediately.
		if err := tryLockFile(life, false); err != nil {
			l.release()
			return nil, fmt.Errorf("telemetry: acquiring shared store lock: %w", err)
		}
	default:
		l.release()
		return nil, fmt.Errorf("telemetry: acquiring store lock: %w", err)
	}
	return l, nil
}

// soleOpener reports whether no other Store had the database open when the
// lock was acquired. While the gate is held no other opener can join.
func (l *dbOpenLock) soleOpener() bool { return l != nil && l.sole }

// finishOpen converts an exclusive store lock to shared and releases the gate
// so other openers may proceed.
func (l *dbOpenLock) finishOpen() error {
	if l == nil || l.gate == nil {
		return nil
	}
	defer func() {
		_ = unlockFile(l.gate)
		_ = l.gate.Close()
		l.gate = nil
	}()
	if !l.sole {
		return nil
	}
	l.sole = false
	if err := unlockFile(l.life); err != nil {
		return fmt.Errorf("telemetry: releasing exclusive store lock: %w", err)
	}
	if err := tryLockFile(l.life, false); err != nil {
		return fmt.Errorf("telemetry: downgrading store lock: %w", err)
	}
	return nil
}

// release drops every lock held and closes the lock files.
func (l *dbOpenLock) release() {
	if l == nil {
		return
	}
	if l.life != nil {
		_ = unlockFile(l.life)
		_ = l.life.Close()
		l.life = nil
	}
	if l.gate != nil {
		_ = unlockFile(l.gate)
		_ = l.gate.Close()
		l.gate = nil
	}
}
