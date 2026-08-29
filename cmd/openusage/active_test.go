package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestActiveJSONFallsBackWhenDaemonUnavailable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var out strings.Builder
	err := runActive(activeOptions{
		socketPath: t.TempDir() + "/nonexistent.sock",
		asJSON:     true,
		out:        &out,
	})
	if err != nil {
		t.Fatalf("runActive: %v", err)
	}
	var sel map[string]any
	if err := json.Unmarshal([]byte(out.String()), &sel); err != nil {
		t.Fatalf("output is not JSON: %q", out.String())
	}
	status, _ := sel["status"].(string)
	if status != "ok" && status != "no_data" {
		t.Errorf("status = %q, want ok or no_data", status)
	}
	if source, _ := sel["source"].(string); source == "events" {
		t.Error("source should not be events when the daemon is unreachable")
	}
}

func TestActiveCommandHasPinControls(t *testing.T) {
	cmd := newActiveCommand()
	if cmd.Commands() == nil {
		t.Fatal("active command has no subcommands")
	}
	want := map[string]bool{"pin": false, "unpin": false, "detail": false, "list": false}
	for _, child := range cmd.Commands() {
		if _, ok := want[child.Name()]; ok {
			want[child.Name()] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("missing active %s subcommand", name)
		}
	}
}

func TestActiveExplainNamesDegradedPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var out strings.Builder
	err := runActive(activeOptions{
		socketPath: t.TempDir() + "/nonexistent.sock",
		explain:    true,
		out:        &out,
	})
	if err != nil {
		t.Fatalf("runActive: %v", err)
	}
	if !strings.Contains(out.String(), "daemon unavailable") {
		t.Errorf("explanation = %q, want daemon unavailable notice", out.String())
	}
}
