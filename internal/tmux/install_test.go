package tmux

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallPrintModeEmitsSnippet(t *testing.T) {
	var buf bytes.Buffer
	if _, err := Install(&buf, InstallOptions{Preset: "compact"}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, sentinelStart) || !strings.Contains(out, sentinelEnd) {
		t.Fatalf("snippet missing sentinels:\n%s", out)
	}
	if !strings.Contains(out, "openusage tmux --preset compact") {
		t.Fatalf("snippet missing status-line command:\n%s", out)
	}
}

func TestInstallWritesNewConf(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "") // force ~/.config path

	var out bytes.Buffer
	path, err := Install(&out, InstallOptions{Write: true, Preset: "compact"})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if path == "" {
		t.Fatalf("Install returned empty path")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !bytes.Contains(data, []byte(sentinelStart)) {
		t.Fatalf("conf missing sentinel start:\n%s", data)
	}
}

func TestInstallReplacesExistingSentinelBlock(t *testing.T) {
	home := t.TempDir()
	confPath := filepath.Join(home, "tmux.conf")
	pre := "set -g default-terminal \"screen-256color\"\n\n" +
		sentinelStart + "\nset -g status-interval 99\n" + sentinelEnd + "\n" +
		"# something after\n"
	if err := os.WriteFile(confPath, []byte(pre), 0o644); err != nil {
		t.Fatalf("seed conf: %v", err)
	}

	var out bytes.Buffer
	if _, err := Install(&out, InstallOptions{
		Write:    true,
		ConfPath: confPath,
		Preset:   "compact",
		Interval: 7,
	}); err != nil {
		t.Fatalf("Install: %v", err)
	}

	data, err := os.ReadFile(confPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	s := string(data)
	if strings.Count(s, sentinelStart) != 1 {
		t.Fatalf("expected exactly one sentinel block, got:\n%s", s)
	}
	if !strings.Contains(s, "set -g status-interval 7") {
		t.Fatalf("new interval not applied:\n%s", s)
	}
	if !strings.Contains(s, "# something after") {
		t.Fatalf("post-sentinel content was clobbered:\n%s", s)
	}

	// Backup must exist with the original content.
	bak, err := os.ReadFile(confPath + ".bak")
	if err != nil {
		t.Fatalf("backup not created: %v", err)
	}
	if !strings.Contains(string(bak), "status-interval 99") {
		t.Fatalf("backup missing original content:\n%s", bak)
	}
}

func TestInstallAppendsToExistingConfWithoutSentinels(t *testing.T) {
	home := t.TempDir()
	confPath := filepath.Join(home, "tmux.conf")
	pre := "# existing user config\nset -g mouse on\n"
	if err := os.WriteFile(confPath, []byte(pre), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var out bytes.Buffer
	if _, err := Install(&out, InstallOptions{Write: true, ConfPath: confPath, Preset: "compact"}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	data, _ := os.ReadFile(confPath)
	s := string(data)
	if !strings.Contains(s, "set -g mouse on") {
		t.Fatalf("user content lost:\n%s", s)
	}
	if !strings.Contains(s, sentinelStart) {
		t.Fatalf("sentinel not appended:\n%s", s)
	}
}

func TestDetectTmuxConfXDGPreference(t *testing.T) {
	home := t.TempDir()
	xdg := filepath.Join(home, "xdg")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", xdg)

	// No conf yet: XDG path wins.
	path, err := DetectTmuxConf(nil)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if !strings.HasPrefix(path, xdg) {
		t.Fatalf("expected XDG path, got %s", path)
	}

	// Once ~/.tmux.conf exists and XDG path does not, the legacy path wins
	// only if XDG is unset; with XDG set the XDG candidate stays preferred
	// when neither exists, but a real legacy file should win if XDG missing.
	legacy := filepath.Join(home, ".tmux.conf")
	if err := os.WriteFile(legacy, []byte("# legacy\n"), 0o644); err != nil {
		t.Fatalf("seed legacy: %v", err)
	}
	t.Setenv("XDG_CONFIG_HOME", "")

	path, err = DetectTmuxConf(nil)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if path != legacy {
		t.Fatalf("expected legacy path %s, got %s", legacy, path)
	}
}

func TestUninstallRemovesSentinelBlock(t *testing.T) {
	home := t.TempDir()
	confPath := filepath.Join(home, "tmux.conf")
	pre := "# before\n" + sentinelStart + "\nset -g status-interval 5\n" + sentinelEnd + "\n# after\n"
	if err := os.WriteFile(confPath, []byte(pre), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var out bytes.Buffer
	if err := Uninstall(&out, confPath); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	data, _ := os.ReadFile(confPath)
	s := string(data)
	if strings.Contains(s, sentinelStart) {
		t.Fatalf("sentinel still present:\n%s", s)
	}
	if !strings.Contains(s, "# before") || !strings.Contains(s, "# after") {
		t.Fatalf("user content lost:\n%s", s)
	}
	if _, err := os.Stat(confPath + ".bak"); err != nil {
		t.Fatalf("backup missing: %v", err)
	}
}

func TestUninstallMissingConfIsNoop(t *testing.T) {
	var out bytes.Buffer
	if err := Uninstall(&out, filepath.Join(t.TempDir(), "nope.conf")); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if !strings.Contains(out.String(), "nothing to uninstall") {
		t.Fatalf("expected no-op message, got %q", out.String())
	}
}

func TestSentinelPresent(t *testing.T) {
	dir := t.TempDir()
	with := filepath.Join(dir, "with.conf")
	if err := os.WriteFile(with, []byte(sentinelStart+"\nx\n"+sentinelEnd+"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if ok, err := SentinelPresent(with); err != nil || !ok {
		t.Fatalf("expected sentinel present, got ok=%v err=%v", ok, err)
	}

	without := filepath.Join(dir, "without.conf")
	if err := os.WriteFile(without, []byte("# plain\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if ok, _ := SentinelPresent(without); ok {
		t.Fatalf("expected sentinel absent")
	}

	if ok, err := SentinelPresent(filepath.Join(dir, "missing.conf")); err != nil || ok {
		t.Fatalf("missing conf should report false,nil; got ok=%v err=%v", ok, err)
	}
}

func TestBuildSnippetMultipleProviders(t *testing.T) {
	snip := BuildSnippet(InstallOptions{
		Position:  "right",
		Preset:    "compact",
		Providers: []string{"claude_code", "cursor"},
	})
	// One pinned segment per provider, each with --provider.
	for _, want := range []string{"--provider claude_code --preset compact", "--provider cursor --preset compact"} {
		if !strings.Contains(snip, want) {
			t.Fatalf("multi-provider snippet missing %q:\n%s", want, snip)
		}
	}
	// Still the safe prepend shape: a run-shell loop, no literal "#(" that tmux
	// would expand at parse time, and the idempotency guard.
	if strings.Contains(snip, "#(") {
		t.Fatalf("snippet has a literal #( that tmux would expand:\n%s", snip)
	}
	if !strings.Contains(snip, "for c in") || !strings.Contains(snip, `printf "#%s"`) {
		t.Fatalf("snippet not using the runtime printf loop:\n%s", snip)
	}
	// No providers → a single auto-detecting segment (no --provider).
	single := BuildSnippet(InstallOptions{Position: "right", Preset: "compact"})
	if strings.Contains(single, "--provider") {
		t.Fatalf("dynamic snippet should not pin a provider:\n%s", single)
	}
}

func TestBuildSnippetPositionVariants(t *testing.T) {
	right := BuildSnippet(InstallOptions{Position: "right"})
	if !strings.Contains(right, "status-right") || strings.Contains(right, "status-left ") {
		t.Fatalf("right-only snippet wrong:\n%s", right)
	}
	left := BuildSnippet(InstallOptions{Position: "left"})
	if !strings.Contains(left, "status-left ") || strings.Contains(left, "status-right ") {
		t.Fatalf("left-only snippet wrong:\n%s", left)
	}
	both := BuildSnippet(InstallOptions{Position: "both"})
	if !strings.Contains(both, "status-left ") || !strings.Contains(both, "status-right ") {
		t.Fatalf("both-position snippet missing one side:\n%s", both)
	}
}

// TestBuildSnippetRightPrepends asserts the right-side segment is inserted at
// the inner edge of status-right (prepended) rather than appended to the
// far-right edge, and that it never writes a literal "#(" that tmux would
// expand at parse time.
func TestBuildSnippetRightPrepends(t *testing.T) {
	for _, pos := range []string{"right", "both"} {
		snip := BuildSnippet(InstallOptions{Position: pos})

		// The right segment must be installed via run-shell prepend, not a
		// plain `set -ga status-right` append.
		if strings.Contains(snip, "set -ga status-right") {
			t.Fatalf("%s: still appends to status-right (far-right edge):\n%s", pos, snip)
		}
		if !strings.Contains(snip, "run-shell") || !strings.Contains(snip, `tmux set -g status-right "$seg │ $cur"`) {
			t.Fatalf("%s: missing prepend run-shell line:\n%s", pos, snip)
		}
		// The idempotency guard must be present.
		if !strings.Contains(snip, `case "$cur" in *"$seg"*) exit 0`) {
			t.Fatalf("%s: missing idempotency guard:\n%s", pos, snip)
		}

		// Isolate the run-shell line: a literal "#(" inside a run-shell
		// argument is expanded by tmux at parse time, so the "#" must be
		// rebuilt at runtime via printf instead. (A "#(" in a plain
		// `set status-left/right` line is fine; tmux stores it unexpanded.)
		var runLine string
		for _, line := range strings.Split(snip, "\n") {
			if strings.HasPrefix(line, "run-shell") {
				runLine = line
				break
			}
		}
		if runLine == "" {
			t.Fatalf("%s: no run-shell line found:\n%s", pos, snip)
		}
		if strings.Contains(runLine, "#(") {
			t.Fatalf("%s: run-shell line contains a literal #( that tmux expands at parse time:\n%s", pos, runLine)
		}
		if !strings.Contains(runLine, `printf "#%s"`) {
			t.Fatalf("%s: segment prefix is not rebuilt at runtime via printf:\n%s", pos, runLine)
		}
	}
}

func TestBuildSnippetBindings(t *testing.T) {
	snip := BuildSnippet(InstallOptions{BindPopup: "u", BindRefresh: "r"})
	if !strings.Contains(snip, "bind-key u display-popup") {
		t.Fatalf("bind-popup missing:\n%s", snip)
	}
	if !strings.Contains(snip, "bind-key r run-shell") {
		t.Fatalf("bind-refresh missing:\n%s", snip)
	}
}
