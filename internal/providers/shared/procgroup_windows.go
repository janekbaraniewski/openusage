//go:build windows

package shared

import "os/exec"

// StartInOwnProcessGroup is a no-op on Windows; cmd.WaitDelay still bounds
// Wait if a child outlives the killed process.
func StartInOwnProcessGroup(*exec.Cmd) {}

// KillProcessGroup kills cmd's process. Windows has no POSIX process groups;
// cmd.WaitDelay bounds Wait if a grandchild keeps the pipes open.
func KillProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
