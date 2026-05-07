//go:build linux

package daemon

import (
	"strings"
	"testing"
)

func TestSystemdUnit_UsesDaemonRunSubcommand(t *testing.T) {
	unit := systemdUnit("/usr/local/bin/openusage", "/tmp/openusage.sock", "/tmp/openusage.env")

	if !strings.Contains(unit, "ExecStart=/usr/local/bin/openusage telemetry daemon run --socket-path /tmp/openusage.sock") {
		t.Fatalf("systemd unit does not include daemon run subcommand:\n%s", unit)
	}
	if !strings.Contains(unit, "EnvironmentFile=-/tmp/openusage.env") {
		t.Fatalf("systemd unit does not include env file:\n%s", unit)
	}
}
