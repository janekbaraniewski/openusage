//go:build !windows

package shared

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A wrapper that starts a child which ignores SIGTERM, never exits, and
// inherits stdout, then waits for it: the shape of `gh copilot` launching the
// copilot binary or npm's codex.js launching the native app-server (#446).
// Killing only the wrapper would leave the child holding stdout open.
const hangingWrapper = `#!/bin/sh
sh -c 'trap "" TERM; echo $$ > "$1"; while :; do sleep 1; done' child "$1" &
wait
`

func TestCommandContextKillsWholeGroupOnCancel(t *testing.T) {
	dir := t.TempDir()
	wrapper := filepath.Join(dir, "wrapper")
	if err := os.WriteFile(wrapper, []byte(hangingWrapper), 0o755); err != nil {
		t.Fatal(err)
	}
	pidFile := filepath.Join(dir, "child.pid")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		// Output reads stdout until EOF, so it blocks for as long as any
		// process in the tree still holds the pipe.
		_, err := CommandContext(ctx, wrapper, pidFile).Output()
		done <- err
	}()

	// Cancel (as a timeout would) once the child is up.
	for start := time.Now(); ; time.Sleep(20 * time.Millisecond) {
		if _, err := os.Stat(pidFile); err == nil {
			break
		}
		if time.Since(start) > 10*time.Second {
			t.Fatal("child never started")
		}
	}
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error from a command killed on timeout")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("command did not return after its deadline: the child kept the pipe open")
	}

	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("child never started: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("parse child pid: %v", err)
	}
	// The child must be gone, not orphaned. Allow a moment for the kernel to
	// reap it after SIGKILL.
	deadline := time.Now().Add(3 * time.Second)
	for {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("child %d survived the timeout (kill(0) = %v): only the wrapper was killed", pid, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
