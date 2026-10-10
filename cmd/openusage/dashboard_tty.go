package main

import (
	"errors"
	"os"

	"golang.org/x/term"
)

// errDashboardNeedsTerminal is returned when the default command is run
// without an interactive terminal on stdin and stdout. Bubble Tea would
// otherwise open /dev/tty behind the caller's back, put the user's terminal
// into raw mode, and never exit (issue #405).
var errDashboardNeedsTerminal = errors.New(
	"openusage: the dashboard needs an interactive terminal (stdin and stdout must be a TTY); " +
		"for non-interactive use try `openusage export`, `openusage daily`, or `openusage statusline`")

// checkDashboardTerminal reports errDashboardNeedsTerminal unless both stdin
// and stdout are terminals. isTerminal is injected so tests can exercise the
// decision without a real TTY.
func checkDashboardTerminal(isTerminal func(fd int) bool, stdinFd, stdoutFd int) error {
	if !isTerminal(stdinFd) || !isTerminal(stdoutFd) {
		return errDashboardNeedsTerminal
	}
	return nil
}

// requireDashboardTerminal applies checkDashboardTerminal to the process's
// real stdin and stdout.
func requireDashboardTerminal() error {
	return checkDashboardTerminal(term.IsTerminal, int(os.Stdin.Fd()), int(os.Stdout.Fd()))
}
