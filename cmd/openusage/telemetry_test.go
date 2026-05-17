package main

import "testing"

func TestTelemetryDaemonRunCommandExposesRunFlags(t *testing.T) {
	cmd := newTelemetryDaemonCommand()
	runCmd, _, err := cmd.Find([]string{"run"})
	if err != nil {
		t.Fatalf("find run command: %v", err)
	}
	if runCmd == nil || runCmd.Use != "run" {
		t.Fatalf("expected run command, got %#v", runCmd)
	}

	for _, name := range []string{
		"db-path",
		"spool-dir",
		"interval",
		"collect-interval",
		"poll-interval",
		"verbose",
	} {
		if runCmd.Flags().Lookup(name) == nil {
			t.Fatalf("run command missing %q flag", name)
		}
	}
}
