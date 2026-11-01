//go:build windows

package codex

import "os/exec"

// startInOwnProcessGroup is a no-op on Windows; cmd.WaitDelay still bounds
// Wait if a child outlives the killed process.
func startInOwnProcessGroup(*exec.Cmd) {}

func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
