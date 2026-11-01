//go:build !windows

package shared

import (
	"os/exec"
	"syscall"
)

// StartInOwnProcessGroup runs cmd as the leader of a new process group and
// makes context cancellation kill the whole group. Many CLIs are wrappers
// that spawn the real binary as a child (npm's codex.js, `gh copilot`
// launching the copilot CLI); killing only the wrapper orphans that child,
// which keeps the stdio pipes open and blocks the reader and cmd.Wait
// forever (#446). Call it before cmd.Start.
func StartInOwnProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return KillProcessGroup(cmd) }
}

// KillProcessGroup kills every process in cmd's group (falling back to the
// process itself if the group signal fails).
func KillProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err == nil {
		return nil
	}
	return cmd.Process.Kill()
}
