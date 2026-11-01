package shared

import (
	"context"
	"os/exec"
	"time"
)

// SubprocessWaitDelay bounds how long cmd.Wait keeps reading a killed
// command's pipes. A grandchild that escaped the group kill (or any child on
// Windows) can hold stdout open forever; after this delay Wait closes the
// pipes and returns.
const SubprocessWaitDelay = time.Second

// CommandContext is exec.CommandContext for short-lived CLI probes: when ctx
// is done it kills the command's whole process group, not just the direct
// child, and Wait gives up on leaked pipes after SubprocessWaitDelay.
func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	StartInOwnProcessGroup(cmd)
	cmd.WaitDelay = SubprocessWaitDelay
	return cmd
}
