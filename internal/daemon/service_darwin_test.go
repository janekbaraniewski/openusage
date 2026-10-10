//go:build darwin

package daemon

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLaunchdPlist_UsesDaemonRunSubcommand(t *testing.T) {
	plist := launchdPlist(
		"/tmp/openusage",
		"/tmp/openusage.sock",
		"/tmp/openusage.stdout.log",
		"/tmp/openusage.stderr.log",
		map[string]string{"OPENAI_API_KEY": "sk-test"},
	)

	if !strings.Contains(plist, "<string>daemon</string>\n\t\t<string>run</string>") {
		t.Fatalf("launchd plist does not include daemon run subcommand:\n%s", plist)
	}
	if !strings.Contains(plist, "<key>EnvironmentVariables</key>") || !strings.Contains(plist, "<key>OPENAI_API_KEY</key>") {
		t.Fatalf("launchd plist does not include env vars:\n%s", plist)
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

type fakeLaunchctl struct {
	calls []string
	// handlers keyed by subcommand; each call pops the first result.
	results map[string][]error
}

func (f *fakeLaunchctl) run(args ...string) (string, error) {
	f.calls = append(f.calls, strings.Join(args, " "))
	queue := f.results[args[0]]
	if len(queue) == 0 {
		return "", nil
	}
	err := queue[0]
	f.results[args[0]] = queue[1:]
	return "", err
}

func withFakeLaunchctl(t *testing.T, f *fakeLaunchctl) {
	t.Helper()
	origCmd, origSleep := launchctlCommand, launchctlSleep
	launchctlCommand = f.run
	launchctlSleep = func(time.Duration) {}
	t.Cleanup(func() {
		launchctlCommand = origCmd
		launchctlSleep = origSleep
	})
}

func TestReloadLaunchdService_WaitsForUnloadAndRetriesEIO(t *testing.T) {
	notLoaded := assertErr("launchctl print failed: exit status 113 (Could not find service)")
	eio := assertErr("launchctl bootstrap gui/501 x.plist failed: exit status 5 (Bootstrap failed: 5: Input/output error)")
	f := &fakeLaunchctl{results: map[string][]error{
		// still loaded twice after first bootout, then gone; gone immediately after the retry bootout.
		"print":     {nil, nil, notLoaded, notLoaded},
		"bootstrap": {eio, nil},
	}}
	withFakeLaunchctl(t, f)

	m := ServiceManager{unitPath: "/tmp/x.plist"}
	if err := m.reloadLaunchdService("gui/501"); err != nil {
		t.Fatalf("reloadLaunchdService() error = %v", err)
	}
	want := []string{
		"bootout gui/501/" + LaunchdDaemonLabel,
		"print gui/501/" + LaunchdDaemonLabel,
		"print gui/501/" + LaunchdDaemonLabel,
		"print gui/501/" + LaunchdDaemonLabel,
		"bootstrap gui/501 /tmp/x.plist",
		"bootout gui/501/" + LaunchdDaemonLabel,
		"print gui/501/" + LaunchdDaemonLabel,
		"bootstrap gui/501 /tmp/x.plist",
		"kickstart gui/501/" + LaunchdDaemonLabel,
	}
	if strings.Join(f.calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("launchctl calls:\n%s\nwant:\n%s", strings.Join(f.calls, "\n"), strings.Join(want, "\n"))
	}
}

func TestReloadLaunchdService_NonRetryableFailsFast(t *testing.T) {
	notLoaded := assertErr("Could not find service")
	f := &fakeLaunchctl{results: map[string][]error{
		"print":     {notLoaded},
		"bootstrap": {assertErr("Bootstrap failed: 122: Path had bad ownership/permissions")},
	}}
	withFakeLaunchctl(t, f)

	m := ServiceManager{unitPath: "/tmp/x.plist"}
	err := m.reloadLaunchdService("gui/501")
	if err == nil || !strings.Contains(err.Error(), "122") {
		t.Fatalf("reloadLaunchdService() error = %v, want the bootstrap error", err)
	}
	bootstraps := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, "bootstrap") {
			bootstraps++
		}
	}
	if bootstraps != 1 {
		t.Fatalf("bootstrap attempted %d times, want 1", bootstraps)
	}
}

// When every domain fails, the gui/ error must not be hidden behind the
// user/ fallback error (that is what made the original report unreadable).
func TestInstallLaunchd_ReportsErrorsFromAllDomains(t *testing.T) {
	notLoaded := assertErr("Could not find service")
	eio := func(domain string) error {
		return assertErr("launchctl bootstrap " + domain + " failed: exit status 5 (Bootstrap failed: 5: Input/output error)")
	}
	uid := strconv.Itoa(os.Getuid())
	var bootstrapErrs []error
	for i := 0; i < launchdBootstrapAttempts; i++ {
		bootstrapErrs = append(bootstrapErrs, eio("gui/"+uid))
	}
	for i := 0; i < launchdBootstrapAttempts; i++ {
		bootstrapErrs = append(bootstrapErrs, eio("user/"+uid))
	}
	prints := make([]error, 2*launchdBootstrapAttempts)
	for i := range prints {
		prints[i] = notLoaded
	}
	f := &fakeLaunchctl{results: map[string][]error{"print": prints, "bootstrap": bootstrapErrs}}
	withFakeLaunchctl(t, f)
	t.Setenv("HOME", t.TempDir())

	dir := t.TempDir()
	m := ServiceManager{
		Kind:       "darwin",
		exePath:    "/usr/local/bin/openusage",
		socketPath: filepath.Join(dir, "d.sock"),
		stateDir:   dir,
		unitPath:   filepath.Join(dir, "agents", LaunchdDaemonLabel+".plist"),
	}
	err := m.installLaunchd()
	if err == nil {
		t.Fatal("installLaunchd() error = nil, want failure")
	}
	if !strings.Contains(err.Error(), "gui/"+uid) || !strings.Contains(err.Error(), "user/"+uid) {
		t.Fatalf("error should mention both domains, got: %v", err)
	}
}
