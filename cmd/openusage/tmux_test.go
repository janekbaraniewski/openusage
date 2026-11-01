package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samber/lo"

	"github.com/janekbaraniewski/openusage/internal/config"
)

// isolateHome points the home directory at a fresh temp dir. os.UserHomeDir
// reads USERPROFILE on Windows, so HOME alone does not isolate tests there.
func isolateHome(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	t.Setenv("HOME", d)
	t.Setenv("USERPROFILE", d)
	return d
}

func TestAssembleTemplate(t *testing.T) {
	// Pieces render in canonical palette order regardless of selection order,
	// and the result is a valid template.
	tmpl := assembleTemplate([]string{"today", "icon", "block"})
	if !strings.HasPrefix(tmpl, "{tool:icon:brand}") {
		t.Fatalf("icon should render first, got %q", tmpl)
	}
	if err := validateTemplate(tmpl); err != nil {
		t.Fatalf("assembled template should be valid: %v (%q)", err, tmpl)
	}
	// Empty selection and unknown keys yield an empty template (caller falls back).
	if got := assembleTemplate(nil); got != "" {
		t.Fatalf("empty selection = %q, want empty", got)
	}
	if got := assembleTemplate([]string{"not-a-component"}); got != "" {
		t.Fatalf("unknown key = %q, want empty", got)
	}
}

func TestValidateTemplate(t *testing.T) {
	// Valid templates pass.
	for _, ok := range []string{
		"{tool:icon}",
		"{tool:icon:brand} 5h {block_pct:pct:color} {today_cost:money}/today",
		"plain text",
	} {
		if err := validateTemplate(ok); err != nil {
			t.Errorf("validateTemplate(%q) = %v, want nil", ok, err)
		}
	}
	// Empty is rejected.
	if err := validateTemplate("   "); err == nil {
		t.Error("validateTemplate(blank) should error")
	}
	// A malformed template (unterminated brace) is rejected.
	if err := validateTemplate("{tool:icon"); err == nil {
		t.Error("validateTemplate with an unterminated brace should error")
	}
}

func TestLastStatusRoundTrip(t *testing.T) {
	isolateHome(t)

	if _, ok := readLastStatus(""); ok {
		t.Fatal("expected no cached status in a fresh HOME")
	}

	writeLastStatus("", "🤖 5h 15% $6.79/today")
	got, ok := readLastStatus("")
	if !ok {
		t.Fatal("expected a cached status after write")
	}
	if got != "🤖 5h 15% $6.79/today" {
		t.Fatalf("round-trip mismatch: %q", got)
	}

	// A blank render must not be treated as a usable cached status.
	writeLastStatus("", "   ")
	if _, ok := readLastStatus(""); ok {
		t.Fatal("blank status should not count as a cache hit")
	}
}

// ageLastStatus backdates the cache file far past any TTL.
func ageLastStatus(t *testing.T, key string) {
	t.Helper()
	old := time.Now().Add(-24 * time.Hour)
	if err := os.Chtimes(lastStatusPath(key), old, old); err != nil {
		t.Fatal(err)
	}
}

func TestEmitTmuxStatus_DegradedServesOldCache(t *testing.T) {
	isolateHome(t)
	const good = "🤖 5h 15% $6.79/today"
	writeLastStatus("", good)
	ageLastStatus(t, "")

	var buf bytes.Buffer
	emitTmuxStatus(&buf, "", "✨", true, nil)
	if got := buf.String(); got != good+"\n" {
		t.Fatalf("degraded render with old cache: got %q, want last good status", got)
	}
	if last, _ := readLastStatus(""); last != good {
		t.Fatalf("degraded render overwrote the cache: %q", last)
	}
}

func TestEmitTmuxStatus_DegradedWithoutCacheIsEmpty(t *testing.T) {
	isolateHome(t)

	var buf bytes.Buffer
	emitTmuxStatus(&buf, "", "✨", true, nil)
	if got := buf.String(); got != "\n" {
		t.Fatalf("degraded render without cache: got %q, want empty line", got)
	}
	if _, ok := readLastStatus(""); ok {
		t.Fatal("degraded render must not be written to the cache")
	}
}

func TestEmitTmuxStatus_ErrorWithoutCacheIsEmpty(t *testing.T) {
	isolateHome(t)

	var buf bytes.Buffer
	emitTmuxStatus(&buf, "", "", false, errors.New("daemon read timed out"))
	if got := buf.String(); got != "\n" {
		t.Fatalf("failed render without cache: got %q, want empty line", got)
	}
}

func TestEmitTmuxStatus_ErrorServesOldCache(t *testing.T) {
	isolateHome(t)
	writeLastStatus("", "good")
	ageLastStatus(t, "")

	var buf bytes.Buffer
	emitTmuxStatus(&buf, "", "", false, errors.New("boom"))
	if got := buf.String(); got != "good\n" {
		t.Fatalf("failed render with old cache: got %q", got)
	}
}

func TestEmitTmuxStatus_CleanRenderUpdatesCache(t *testing.T) {
	isolateHome(t)
	writeLastStatus("", "old")

	var buf bytes.Buffer
	emitTmuxStatus(&buf, "", "new", false, nil)
	if got := buf.String(); got != "new\n" {
		t.Fatalf("clean render: got %q", got)
	}
	if last, _ := readLastStatus(""); last != "new" {
		t.Fatalf("clean render did not refresh the cache: %q", last)
	}
}

// captureStdout runs fn with os.Stdout redirected and returns what it wrote.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()
	fn()
	_ = w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestRunTmuxRender_FreshCacheShortCircuits(t *testing.T) {
	home := isolateHome(t)
	t.Setenv("TMUX", "/tmp/tmux-test,1,0")
	// Point the daemon socket somewhere that does not exist, so if the
	// short-circuit failed the render could only produce "?" or a fallback.
	t.Setenv("OPENUSAGE_TELEMETRY_SOCKET", filepath.Join(home, "missing.sock"))

	const cached = "#[fg=#FF6600]cached 5h 42%"
	writeLastStatus("", cached)

	cmd := newTmuxCommand()
	cmd.SetArgs([]string{})
	out := captureStdout(t, func() {
		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute: %v", err)
		}
	})
	if out != cached+"\n" {
		t.Fatalf("fresh cache not served verbatim: got %q", out)
	}
}

func TestReadFreshStatus(t *testing.T) {
	isolateHome(t)

	writeLastStatus("", "fresh")
	now := time.Now()
	if got, ok := readFreshStatus("", now); !ok || got != "fresh" {
		t.Fatalf("expected fresh hit, got %q ok=%v", got, ok)
	}

	// Different flags use a different file.
	if _, ok := readFreshStatus("abcd", now); ok {
		t.Fatal("a different flag fingerprint must not share the cache")
	}

	// Past tmuxFreshTTL: no short-circuit, but still usable as a fallback.
	if _, ok := readFreshStatus("", now.Add(tmuxFreshTTL+time.Second)); ok {
		t.Fatal("expired cache must not short-circuit")
	}
	path := lastStatusPath("")
	old := now.Add(-tmuxFreshTTL - time.Second)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if _, ok := readFreshStatus("", now); ok {
		t.Fatal("expired cache must not short-circuit")
	}
	if _, ok := readLastStatus(""); !ok {
		t.Fatal("expired fresh cache should still serve as the degraded fallback")
	}

	// Editing settings.json after the render invalidates the short-circuit.
	writeLastStatus("", "fresh")
	cfgPath := config.ConfigPath()
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Second)
	if err := os.Chtimes(cfgPath, future, future); err != nil {
		t.Fatal(err)
	}
	if _, ok := readFreshStatus("", time.Now()); ok {
		t.Fatal("a settings.json newer than the cache must bypass the short-circuit")
	}
}

func TestTmuxCacheKeyDependsOnFlags(t *testing.T) {
	plain := newTmuxCommand()
	if err := plain.ParseFlags([]string{}); err != nil {
		t.Fatal(err)
	}
	if got := tmuxCacheKey(plain); got != "" {
		t.Fatalf("plain invocation should use the legacy key, got %q", got)
	}
	a := newTmuxCommand()
	_ = a.ParseFlags([]string{"--preset", "minimal"})
	b := newTmuxCommand()
	_ = b.ParseFlags([]string{"--preset", "claude-focused"})
	if tmuxCacheKey(a) == "" || tmuxCacheKey(a) == tmuxCacheKey(b) {
		t.Fatalf("distinct flags must produce distinct keys: %q vs %q", tmuxCacheKey(a), tmuxCacheKey(b))
	}
}

func TestNewTmuxCommandHasSubcommands(t *testing.T) {
	cmd := newTmuxCommand()
	want := []string{"install", "uninstall", "presets", "variables", "doctor", "preview", "watch"}
	have := map[string]bool{}
	for _, c := range cmd.Commands() {
		have[c.Name()] = true
	}
	for _, name := range want {
		if !have[name] {
			t.Errorf("subcommand %q missing from tmux", name)
		}
	}
}

func TestTmuxFlagsDefaults(t *testing.T) {
	cmd := newTmuxCommand()
	// Defaults from the renderer flags struct.
	if v, _ := cmd.Flags().GetString("color-mode"); v != "truecolor" {
		t.Errorf("color-mode default = %q, want truecolor", v)
	}
	if v, _ := cmd.Flags().GetString("source"); v != "auto" {
		t.Errorf("source default = %q, want auto", v)
	}
	if v, _ := cmd.Flags().GetDuration("max-runtime"); v.String() != "800ms" {
		t.Errorf("max-runtime default = %s, want 800ms", v)
	}
}

func TestTmuxFlagMutualExclusion(t *testing.T) {
	cmd := newTmuxCommand()
	cmd.SetArgs([]string{"--preset", "compact", "--format", "x"})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected mutual exclusion error for --preset + --format")
	} else if !strings.Contains(err.Error(), "preset") {
		t.Fatalf("expected error mentioning preset, got %v", err)
	}
}

func TestResolveTemplatePrecedence(t *testing.T) {
	// Segment wins over format and preset.
	out, err := resolveTemplate(tmuxOptions{preset: "compact", format: "x", segment: "cost"})
	if err != nil {
		t.Fatalf("resolveTemplate: %v", err)
	}
	if out != "{cost}" {
		t.Fatalf("segment precedence broken: %q", out)
	}
	// Format wins over preset when no segment.
	out, err = resolveTemplate(tmuxOptions{preset: "compact", format: "explicit"})
	if err != nil {
		t.Fatalf("resolveTemplate: %v", err)
	}
	if out != "explicit" {
		t.Fatalf("format precedence broken: %q", out)
	}
}

func TestCollectKnownVariablesNonEmpty(t *testing.T) {
	vars := collectKnownVariables()
	if len(vars) < 5 {
		t.Fatalf("expected many variables, got %d (%v)", len(vars), vars)
	}
	seen := map[string]bool{}
	for _, v := range vars {
		seen[v] = true
	}
	for _, want := range []string{"tool", "today_cost", "block"} {
		if !seen[want] {
			t.Errorf("variable %q missing from list", want)
		}
	}
}

func TestOrStringPicksFirstNonEmpty(t *testing.T) {
	if v := orString("", " ", "x", "y"); v != "x" {
		t.Fatalf("orString = %q, want x", v)
	}
	if v := orString("", ""); v != "" {
		t.Fatalf("orString = %q, want empty", v)
	}
}

func TestConfiguratorDefaultChoices(t *testing.T) {
	ch := newConfiguratorModel().choices()
	if ch.position != "right" {
		t.Errorf("default position = %q, want right", ch.position)
	}
	if ch.mode != "dynamic" {
		t.Errorf("default mode = %q, want dynamic", ch.mode)
	}
	// Default components are icon + block + today, in canonical order.
	if got := assembleTemplate(ch.components); !strings.HasPrefix(got, "{tool:icon:brand}") {
		t.Errorf("default template = %q, want icon first", got)
	}
	if err := validateTemplate(assembleTemplate(ch.components)); err != nil {
		t.Errorf("default template invalid: %v", err)
	}
	// Dynamic mode pins no provider.
	if got := ch.providersForMode(); got != nil {
		t.Errorf("dynamic providersForMode = %v, want nil", got)
	}
}

func TestConfiguratorModeSwitchingRebuildsRows(t *testing.T) {
	m := newConfiguratorModel()
	base := len(m.rows)

	// Switch to "several": one toggle row per provider appears.
	m.modeIdx = lo.IndexOf(cfgModes, "several")
	m.rebuildRows()
	if len(m.rows) <= base {
		t.Fatalf("several mode should add provider rows: base=%d now=%d", base, len(m.rows))
	}
	nProviders := lo.CountBy(m.rows, func(r cfgRow) bool { return r.kind == rowProviderToggle })
	if nProviders != len(m.provIDs) {
		t.Fatalf("several mode = %d provider rows, want %d", nProviders, len(m.provIDs))
	}

	// "pinned" mode adds a single cycle row for the pinned tool, not per-provider toggles.
	m.modeIdx = lo.IndexOf(cfgModes, "pinned")
	m.rebuildRows()
	if lo.CountBy(m.rows, func(r cfgRow) bool { return r.kind == rowProviderToggle }) != 0 {
		t.Fatalf("pinned mode should have no provider toggle rows")
	}
}

func TestConfiguratorProvidersForMode(t *testing.T) {
	pinned := tmuxChoices{mode: "pinned", pinned: "cursor"}
	if got := pinned.providersForMode(); len(got) != 1 || got[0] != "cursor" {
		t.Errorf("pinned providersForMode = %v, want [cursor]", got)
	}
	several := tmuxChoices{mode: "several", several: []string{"claude_code", "codex"}}
	if got := several.providersForMode(); len(got) != 2 {
		t.Errorf("several providersForMode = %v, want 2", got)
	}
}

func TestConfiguratorPreviewRenders(t *testing.T) {
	m := newConfiguratorModel()
	// Dynamic preview is a single claude_code segment.
	if out := m.preview(); strings.TrimSpace(out) == "" {
		t.Fatal("dynamic preview is empty")
	}
	// Several preview with two tools is joined by the separator.
	m.modeIdx = lo.IndexOf(cfgModes, "several")
	m.several = map[string]bool{"claude_code": true, "cursor": true}
	m.rebuildRows()
	if out := m.preview(); !strings.Contains(out, "│") {
		t.Errorf("several preview should join segments with separator, got %q", out)
	}
}
