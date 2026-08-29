package sketchybar

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func setTestHome(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	}
}

func TestBuildSnippetUsesNeutralAssetDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sketchybar is macOS-only; path expansion rewrites POSIX separators on windows")
	}

	snippet, err := BuildSnippet(InstallOptions{
		Preset:  DefaultPreset,
		Binary:  "/Applications/Open Usage/openusage",
		DataDir: "/tmp/openusage sketchybar",
	})
	if err != nil {
		t.Fatalf("BuildSnippet: %v", err)
	}
	for _, want := range []string{
		SentinelStart,
		SentinelEnd,
		"OPENUSAGE_SKETCHYBAR_DIR='/tmp/openusage sketchybar'",
		"ai-usage.sh",
		"provider-select.sh",
		"OPENUSAGE_SKETCHYBAR_USAGE_TRIGGER='click'",
		"--subscribe 'ai' mouse.clicked",
		"--subscribe 'ai_switcher' mouse.clicked",
	} {
		if !strings.Contains(snippet, want) {
			t.Fatalf("snippet missing %q:\n%s", want, snippet)
		}
	}
	if strings.Contains(snippet, ".config/sketchybar/plugins") && !strings.Contains(snippet, "outside ~/.config/sketchybar/plugins") {
		t.Fatalf("snippet points at the user's plugins directory:\n%s", snippet)
	}
	if strings.Contains(snippet, "\n+") {
		t.Fatalf("snippet contains an accidental generated '+' line:\n%s", snippet)
	}

	path := filepath.Join(t.TempDir(), "snippet.sh")
	if err := os.WriteFile(path, []byte(snippet), 0o600); err != nil {
		t.Fatalf("write snippet: %v", err)
	}
	if out, err := exec.Command("bash", "-n", path).CombinedOutput(); err != nil {
		t.Fatalf("bash -n snippet: %v\n%s", err, out)
	}
}

func TestBuildSnippetUsesConfiguredTriggers(t *testing.T) {
	snippet, err := BuildSnippet(InstallOptions{
		UsageTrigger:    "hover",
		SwitcherTrigger: "click",
	})
	if err != nil {
		t.Fatalf("BuildSnippet: %v", err)
	}

	for _, want := range []string{
		"OPENUSAGE_SKETCHYBAR_USAGE_TRIGGER='hover'",
		"OPENUSAGE_SKETCHYBAR_SWITCHER_TRIGGER='click'",
		"--subscribe 'ai' mouse.entered mouse.exited mouse.exited.global",
		"--subscribe 'ai_switcher' mouse.clicked",
	} {
		if !strings.Contains(snippet, want) {
			t.Fatalf("snippet missing configured trigger %q:\n%s", want, snippet)
		}
	}
}

func TestInstallWritesAssetsAndSentinel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix execute bit semantics do not apply on windows")
	}

	home := t.TempDir()
	setTestHome(t, home)
	configPath := filepath.Join(home, ".config", "sketchybar", "sketchybarrc")
	dataDir := filepath.Join(home, ".local", "share", "openusage", "sketchybar")
	pluginsDir := filepath.Join(home, ".config", "sketchybar", "plugins")

	var out bytes.Buffer
	path, err := Install(&out, InstallOptions{Write: true, ConfigPath: configPath, DataDir: dataDir})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if path != configPath {
		t.Fatalf("path = %q, want %q", path, configPath)
	}
	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !bytes.Contains(config, []byte(SentinelStart)) || !bytes.Contains(config, []byte(SentinelEnd)) {
		t.Fatalf("config missing managed block:\n%s", config)
	}
	if _, err := os.Stat(pluginsDir); !os.IsNotExist(err) {
		t.Fatalf("installer touched plugins directory: err=%v", err)
	}
	for _, name := range assetNames {
		path := filepath.Join(dataDir, name)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("asset %s: %v", name, err)
		}
		if info.Mode()&0o111 == 0 {
			t.Fatalf("asset %s is not executable: mode=%o", name, info.Mode())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read asset %s: %v", name, err)
		}
		if name == "ai-usage.sh" {
			// The cache fallbacks use two deliberately different predicates.
			// Collapsing them into one made a total CLI failure paint
			// "AI unavailable" instead of the last known reading, because the
			// strict quota check also gated the unreachable-CLI fallback.
			if !strings.Contains(string(data), `parsable_active <"$ACTIVE_CACHE"`) {
				t.Fatalf("asset %s: unreachable-CLI fallback must use the loose predicate", name)
			}
			if !strings.Contains(string(data), `quota_bearing <"$ACTIVE_CACHE"`) {
				t.Fatalf("asset %s: degraded-payload fallback must use the strict predicate", name)
			}
			for _, want := range []string{
				"OPENUSAGE_SKETCHYBAR_USAGE_TRIGGER='click'",
				"mouse.clicked)",
				"popup_state=$(sketchybar --query ai",
				"close_popups",
			} {
				if !strings.Contains(string(data), want) {
					t.Fatalf("asset %s missing click-toggle behavior %q", name, want)
				}
			}
			if !strings.Contains(string(data), "mouse.entered") || !strings.Contains(string(data), "mouse.exited|mouse.exited.global") {
				t.Fatalf("asset %s missing the configurable hover path", name)
			}
		}
		if strings.Contains(strings.ToLower(string(data)), "python") {
			t.Fatalf("asset %s reintroduced a Python dependency", name)
		}
		if output, err := exec.Command("bash", "-n", path).CombinedOutput(); err != nil {
			t.Fatalf("bash -n %s: %v\n%s", name, err, output)
		}
	}
}

func TestInstallReplacesBlockAndUninstallPreservesUserConfig(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	configPath := filepath.Join(home, "sketchybarrc")
	if err := os.WriteFile(configPath, []byte("# before\n"+SentinelStart+"\nsketchybar --update\n"+SentinelEnd+"\n# after\n"), 0o644); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	var out bytes.Buffer
	if _, err := Install(&out, InstallOptions{Write: true, ConfigPath: configPath, DataDir: filepath.Join(home, "scripts")}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read replacement: %v", err)
	}
	if got := strings.Count(string(data), SentinelStart); got != 1 {
		t.Fatalf("sentinel count = %d, want 1:\n%s", got, data)
	}
	if !bytes.Contains(data, []byte("# before")) || !bytes.Contains(data, []byte("# after")) {
		t.Fatalf("replacement clobbered user config:\n%s", data)
	}
	if strings.Index(string(data), SentinelEnd) > strings.Index(string(data), "# after") {
		t.Fatalf("replacement moved the managed block past trailing user config:\n%s", data)
	}
	if _, err := os.Stat(configPath + ".bak"); err != nil {
		t.Fatalf("backup missing: %v", err)
	}

	if err := Uninstall(&out, configPath); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	data, err = os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read uninstall: %v", err)
	}
	if bytes.Contains(data, []byte(SentinelStart)) || !bytes.Contains(data, []byte("# before")) || !bytes.Contains(data, []byte("# after")) {
		t.Fatalf("uninstall damaged config:\n%s", data)
	}
}

func TestInstallFollowsSymlinkedConfig(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	target := filepath.Join(home, "dotfiles", "sketchybarrc")
	link := filepath.Join(home, ".config", "sketchybar", "sketchybarrc")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatalf("mkdir link: %v", err)
	}
	if err := os.WriteFile(target, []byte("# dotfiles config\n"), 0o644); err != nil {
		t.Fatalf("seed target: %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	var out bytes.Buffer
	if _, err := Install(&out, InstallOptions{Write: true, ConfigPath: link, DataDir: filepath.Join(home, "scripts")}); err != nil {
		t.Fatalf("Install through symlink: %v", err)
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("lstat link: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("installer replaced symlink with regular file: mode=%v", info.Mode())
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if !bytes.Contains(data, []byte(SentinelStart)) {
		t.Fatalf("target missing managed block:\n%s", data)
	}
}

func TestDoctorReportsIntegrationState(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	configPath := filepath.Join(home, "sketchybarrc")
	dataDir := filepath.Join(home, "scripts")
	var out bytes.Buffer
	if err := Doctor(&out, DoctorOptions{ConfigPath: configPath, DataDir: dataDir, Binary: filepath.Join(home, "missing-openusage"), Sketchybar: filepath.Join(home, "missing-sketchybar")}); err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	if !strings.Contains(out.String(), "no openusage block") || !strings.Contains(out.String(), "generated script: missing") {
		t.Fatalf("doctor output missing checks:\n%s", out.String())
	}
}

func TestDoctorDetectsTriggerDrift(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	configPath := filepath.Join(home, "sketchybarrc")
	dataDir := filepath.Join(home, "scripts")

	// Install with both items click-driven.
	if _, err := Install(io.Discard, InstallOptions{
		Write: true, ConfigPath: configPath, DataDir: dataDir,
		Binary: "/usr/local/bin/openusage", UsageTrigger: "click", SwitcherTrigger: "click",
	}); err != nil {
		t.Fatalf("Install: %v", err)
	}

	// Doctor run with matching expectations reports agreement.
	var matching bytes.Buffer
	if err := Doctor(&matching, DoctorOptions{
		ConfigPath: configPath, DataDir: dataDir,
		UsageTrigger: "click", SwitcherTrigger: "click",
	}); err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	if strings.Contains(matching.String(), "[WARN] trigger") {
		t.Fatalf("unexpected drift warning for a matching install:\n%s", matching.String())
	}
	if !strings.Contains(matching.String(), "[ OK ] trigger: usage=click") {
		t.Fatalf("expected a trigger check line:\n%s", matching.String())
	}

	// The user edits settings.json to hover but forgets to reinstall.
	var drifted bytes.Buffer
	if err := Doctor(&drifted, DoctorOptions{
		ConfigPath: configPath, DataDir: dataDir,
		UsageTrigger: "hover", SwitcherTrigger: "click",
	}); err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	out := drifted.String()
	if !strings.Contains(out, "[WARN] trigger") {
		t.Fatalf("expected a drift warning:\n%s", out)
	}
	for _, want := range []string{"ai-usage.sh", "installed click", "configured hover", "sketchybar install --write"} {
		if !strings.Contains(out, want) {
			t.Fatalf("drift warning missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "switcher") && strings.Contains(out, "[WARN] trigger: provider-select.sh") {
		t.Fatalf("switcher matched config and must not warn:\n%s", out)
	}
}

func TestProviderSelectClosesPickerByExplicitItemName(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	dataDir := filepath.Join(home, "scripts")
	if _, err := Install(io.Discard, InstallOptions{
		Write: true, ConfigPath: filepath.Join(home, "sketchybarrc"),
		DataDir: dataDir, Binary: "/usr/local/bin/openusage",
	}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dataDir, "provider-select.sh"))
	if err != nil {
		t.Fatal(err)
	}
	// The row click_script runs with NAME set to the clicked row, so $SWITCHER
	// does not name the picker on that path.
	if strings.Contains(string(data), `sketchybar --set "$SWITCHER" popup.drawing=off`) {
		t.Fatalf("provider-select.sh closes the picker via $SWITCHER, which is the row on a click_script:\n%s", data)
	}
	if !strings.Contains(string(data), "close_popups") {
		t.Fatal("provider-select.sh should close popups through the shared helper")
	}
}

func TestSwitcherHoverSurvivesTheTripIntoItsOwnPopup(t *testing.T) {
	snippet, err := BuildSnippet(InstallOptions{UsageTrigger: "hover", SwitcherTrigger: "hover"})
	if err != nil {
		t.Fatalf("BuildSnippet: %v", err)
	}

	// mouse.exited fires when the pointer leaves the *item*, which includes
	// moving down into the item's own popup. The picker is a menu that has to
	// be clicked, so it must not subscribe to that; the read-only usage popup
	// still should.
	if !strings.Contains(snippet, "--subscribe 'ai' mouse.entered mouse.exited mouse.exited.global") {
		t.Fatalf("usage item should keep the item-scoped exit:\n%s", snippet)
	}
	if !strings.Contains(snippet, "--subscribe 'ai_switcher' mouse.entered mouse.exited.global") {
		t.Fatalf("picker should subscribe only to the bar-scoped exit:\n%s", snippet)
	}
	if strings.Contains(snippet, "--subscribe 'ai_switcher' mouse.entered mouse.exited ") {
		t.Fatalf("picker must not subscribe to the item-scoped exit:\n%s", snippet)
	}

	home := t.TempDir()
	setTestHome(t, home)
	dataDir := filepath.Join(home, "scripts")
	if _, err := Install(io.Discard, InstallOptions{
		Write: true, ConfigPath: filepath.Join(home, "sketchybarrc"), DataDir: dataDir,
		Binary: "/usr/local/bin/openusage", UsageTrigger: "hover", SwitcherTrigger: "hover",
	}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dataDir, "provider-select.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "mouse.exited|mouse.exited.global") {
		t.Fatalf("picker still closes on the item-scoped exit, which dismisses the menu being reached for:\n%s", data)
	}
	if !strings.Contains(string(data), "mouse.exited.global)") {
		t.Fatal("picker should still close when the pointer leaves the bar")
	}
}
