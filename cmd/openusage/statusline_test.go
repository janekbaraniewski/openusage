package main

import (
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/report"
)

func TestRenderStatusline_FullLine(t *testing.T) {
	now := time.Date(2026, 6, 1, 13, 0, 0, 0, time.Local)
	mk := func(h, m int, cost float64, in, cacheRead int) report.Event {
		return report.Event{
			Time:      time.Date(2026, 6, 1, h, m, 0, 0, time.Local),
			Provider:  "claude_code",
			Model:     "claude-opus-4-8",
			Session:   "sess-1",
			Cost:      cost,
			Input:     in,
			CacheRead: cacheRead,
		}
	}
	events := []report.Event{
		mk(12, 0, 10, 1000, 50_000),
		mk(13, 0, 10, 2000, 100_000), // last session turn -> context = 2000+100000
	}
	in := statuslineInput{SessionID: "sess-1"}
	in.Model.DisplayName = "Opus 4.8"

	opts := statuslineOptions{offline: true, mode: "calculate", color: false, contextMedium: 50, contextHigh: 80}
	line := renderStatusline(in, events, now, opts)

	for _, want := range []string{"Opus 4.8", "sess", "today", "block", "/hr", "🧠"} {
		if !strings.Contains(line, want) {
			t.Errorf("status line missing %q:\n%s", want, line)
		}
	}
	// session cost = 20, today (both today) = 20
	if !strings.Contains(line, "$20.00 sess") {
		t.Errorf("expected session cost $20.00:\n%s", line)
	}
	// context = 102000 tokens of 200k window ~= 51%
	if !strings.Contains(line, "51%") {
		t.Errorf("expected context 51%%:\n%s", line)
	}
}

func TestRenderStatusline_NoLogsFallsBackToInputCost(t *testing.T) {
	in := statuslineInput{SessionID: "x"}
	in.Model.DisplayName = "Sonnet"
	in.Cost.TotalCostUSD = 3.21
	line := renderStatusline(in, nil, time.Now(), statuslineOptions{color: false, contextMedium: 50, contextHigh: 80})
	if !strings.Contains(line, "Sonnet") || !strings.Contains(line, "$3.21 sess") {
		t.Errorf("fallback line wrong: %s", line)
	}
}

func TestReadStatuslineInput_ParsesJSON(t *testing.T) {
	payload := `{"session_id":"abc","model":{"id":"claude-opus-4-8","display_name":"Opus 4.8"},"cost":{"total_cost_usd":1.5}}`
	in := readStatuslineInput(strings.NewReader(payload))
	if in.SessionID != "abc" || in.Model.DisplayName != "Opus 4.8" || in.Cost.TotalCostUSD != 1.5 {
		t.Errorf("parsed input wrong: %+v", in)
	}
}

func TestContextWindowFor(t *testing.T) {
	if got := contextWindowFor("claude-opus-4-8"); got != 200_000 {
		t.Errorf("default window = %d, want 200000", got)
	}
	if got := contextWindowFor("claude-sonnet-4-8[1m]"); got != 1_000_000 {
		t.Errorf("1m window = %d, want 1000000", got)
	}
}

func TestInstallUninstallStatusline_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	settings := dir + "/settings.json"
	t.Setenv("CLAUDE_SETTINGS_FILE", settings)
	if err := os.WriteFile(settings, []byte(`{"model":"opus","keep":true}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := installStatusline(io.Discard, statuslineOptions{offline: true, color: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	cfg, err := readJSONObject(settings)
	if err != nil {
		t.Fatal(err)
	}
	if cfg["keep"] != true || cfg["model"] != "opus" {
		t.Errorf("install clobbered existing keys: %+v", cfg)
	}
	sl, ok := cfg["statusLine"].(map[string]any)
	if !ok {
		t.Fatalf("statusLine not written: %+v", cfg)
	}
	if cmd, _ := sl["command"].(string); !strings.Contains(cmd, "statusline") {
		t.Errorf("statusLine command wrong: %v", sl["command"])
	}
	// a backup of the original should exist
	if _, err := os.Stat(settings + ".bak"); err != nil {
		t.Errorf("expected backup file: %v", err)
	}

	if err := uninstallStatusline(io.Discard); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	cfg, _ = readJSONObject(settings)
	if _, exists := cfg["statusLine"]; exists {
		t.Errorf("statusLine not removed: %+v", cfg)
	}
	if cfg["keep"] != true {
		t.Errorf("uninstall clobbered existing keys: %+v", cfg)
	}
}

func TestUninstallStatusline_LeavesForeignStatusLine(t *testing.T) {
	dir := t.TempDir()
	settings := dir + "/settings.json"
	t.Setenv("CLAUDE_SETTINGS_FILE", settings)
	if err := os.WriteFile(settings, []byte(`{"statusLine":{"type":"command","command":"some-other-tool"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := uninstallStatusline(io.Discard); err != nil {
		t.Fatal(err)
	}
	cfg, _ := readJSONObject(settings)
	if _, exists := cfg["statusLine"]; !exists {
		t.Error("uninstall removed a statusLine it does not manage")
	}
}

func TestAssembleStatuslineSegments(t *testing.T) {
	v := sampleStatuslineValues()
	all := assembleStatusline(v, statuslineOptions{color: false})
	for _, want := range []string{"Opus 4.8", "sess", "today", "block", "/hr", "🧠"} {
		if !strings.Contains(all, want) {
			t.Fatalf("full statusline missing %q: %s", want, all)
		}
	}
	// Only today + context.
	sub := assembleStatusline(v, statuslineOptions{color: false, segments: []string{"today", "context"}})
	if strings.Contains(sub, "sess") || strings.Contains(sub, "block") || strings.Contains(sub, "/hr") || strings.Contains(sub, "Opus") {
		t.Fatalf("subset leaked disabled segments: %s", sub)
	}
	if !strings.Contains(sub, "today") || !strings.Contains(sub, "🧠") {
		t.Fatalf("subset missing enabled segments: %s", sub)
	}
}

func TestStatuslineCommandStringBakesOptions(t *testing.T) {
	// All segments + defaults => bare command, no flags.
	full := statuslineCommandString(statuslineOptions{offline: true, color: true, mode: "calculate", segments: allStatuslineSegmentKeys()})
	if strings.Contains(full, "--segments") || strings.Contains(full, "--color") || strings.Contains(full, "--offline") || strings.Contains(full, "--mode") {
		t.Fatalf("default command should carry no flags: %s", full)
	}
	// Subset + non-defaults => flags baked in.
	custom := statuslineCommandString(statuslineOptions{offline: false, color: false, mode: "auto", segments: []string{"today", "burn"}})
	for _, want := range []string{"--segments today,burn", "--color=false", "--offline=false", "--mode auto"} {
		if !strings.Contains(custom, want) {
			t.Fatalf("custom command missing %q: %s", want, custom)
		}
	}
}

func TestStatuslineConfiguratorChoices(t *testing.T) {
	m := newStatuslineConfigModel()
	ch := m.choices()
	if len(ch.segments) != len(statuslineSegmentDefs) {
		t.Fatalf("default should enable all segments, got %v", ch.segments)
	}
	if !ch.color || !ch.offline {
		t.Fatalf("defaults: color and embedded pricing on, got color=%v offline=%v", ch.color, ch.offline)
	}
	if strings.TrimSpace(m.preview()) == "" {
		t.Fatal("preview is empty")
	}
	// options() round-trips into a usable statuslineOptions.
	if !ch.options().segmentEnabled("model") {
		t.Fatal("options() lost segment selection")
	}
}
