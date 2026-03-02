//go:build darwin

package daemon

import (
	"strings"
	"testing"
)

func TestLaunchdPlist_UsesDaemonRunSubcommand(t *testing.T) {
	plist := launchdPlist(
		"/tmp/openusage",
		"/tmp/openusage.sock",
		"/tmp/openusage.stdout.log",
		"/tmp/openusage.stderr.log",
	)

	if !strings.Contains(plist, "<string>daemon</string>\n\t\t<string>run</string>") {
		t.Fatalf("launchd plist does not include daemon run subcommand:\n%s", plist)
	}
}

func TestIsLaunchctlAlreadyRunning(t *testing.T) {
	if !isLaunchctlAlreadyRunning(assertErr("launchctl kickstart failed: service already running")) {
		t.Fatal("expected service already running error to be detected")
	}
	if !isLaunchctlAlreadyRunning(assertErr("launchctl kickstart failed: already running")) {
		t.Fatal("expected already running error to be detected")
	}
	if isLaunchctlAlreadyRunning(assertErr("launchctl kickstart failed: no such process")) {
		t.Fatal("did not expect no such process error to be treated as already running")
	}
}

func assertErr(msg string) error {
	return &testErr{msg: msg}
}

type testErr struct {
	msg string
}

func (e *testErr) Error() string {
	return e.msg
}
