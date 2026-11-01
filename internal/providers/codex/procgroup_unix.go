//go:build !windows

package codex

import (
	"os/exec"
	"syscall"
)

// startInOwnProcessGroup runs cmd as the leader of a new process group and
// makes context cancellation kill the whole group. npm installs codex as a
// node wrapper that spawns the native app-server as a child; killing only the
// wrapper orphans that child, which keeps the stdio pipes open and blocks the
// reader and cmd.Wait forever (#446).
func startInOwnProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return killProcessGroup(cmd) }
}

// killProcessGroup kills every process in cmd's group (falling back to the
// process itself if the group signal fails).
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err == nil {
		return nil
	}
	return cmd.Process.Kill()
}
