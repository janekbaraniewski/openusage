package main

import (
	"strings"
	"testing"
)

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
