package tmux

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// updateGoldens is true when the test binary is invoked with `-update`. It
// regenerates the golden files instead of comparing against them.
var updateGoldens = flag.Bool("update", false, "regenerate preset golden files")

// canonicalContextForPreset returns the rendering context every preset golden
// is compared against. It mirrors a Claude Code workflow with an active
// billing block, today_cost, model name, and context tokens populated so all
// 12 presets produce meaningful, non-empty output.
func canonicalContextForPreset(p Preset) Context {
	c := newTestContext()
	c.Glyphs = ParseGlyphTier(p.Glyphs)
	return c
}

func TestPresets_LoadAll(t *testing.T) {
	all := Presets()
	if len(all) != 12 {
		t.Fatalf("expected 12 presets, got %d (%v)", len(all), allNames(all))
	}
	wanted := []string{
		"ascii-safe", "burn", "claude-focused", "compact", "cost-only",
		"emoji-rich", "minimal", "multi-tool", "nerdfont", "powerline",
		"themed", "verbose",
	}
	for _, name := range wanted {
		if _, err := SamplePreset(name); err != nil {
			t.Errorf("missing preset %q: %v", name, err)
		}
	}
}

func TestPresets_RenderCleanly(t *testing.T) {
	// Every preset must render without an error against the canonical
	// fixture, and produce non-empty output.
	for _, p := range Presets() {
		t.Run(p.Name, func(t *testing.T) {
			ctx := canonicalContextForPreset(p)
			out, err := Render(p.Format, ctx)
			if err != nil {
				t.Fatalf("Render(%s): %v", p.Name, err)
			}
			if strings.TrimSpace(out) == "" {
				t.Errorf("preset %q rendered empty: format=%q", p.Name, p.Format)
			}
		})
	}
}

func TestPresets_Golden(t *testing.T) {
	dir := filepath.Join("testdata", "presets")
	if *updateGoldens {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
	}
	for _, p := range Presets() {
		t.Run(p.Name, func(t *testing.T) {
			ctx := canonicalContextForPreset(p)
			out, err := Render(p.Format, ctx)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			path := filepath.Join(dir, p.Name+".golden")
			if *updateGoldens {
				if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden %s: %v (run `go test -update` to create)", path, err)
			}
			if string(want) != out {
				t.Errorf("preset %q mismatch:\n  got:  %q\n  want: %q", p.Name, out, string(want))
			}
		})
	}
}

func TestSamplePreset_Unknown(t *testing.T) {
	if _, err := SamplePreset("not-a-real-preset"); err == nil {
		t.Errorf("expected error for unknown preset")
	}
}

func allNames(ps []Preset) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.Name)
	}
	return out
}
