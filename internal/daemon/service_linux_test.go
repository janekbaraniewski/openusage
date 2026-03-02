//go:build linux

package daemon

import (
	"strings"
	"testing"
)

func TestSystemdUnit_UsesDaemonRunSubcommand(t *testing.T) {
	unit := systemdUnit("/usr/local/bin/openusage", "/tmp/openusage.sock")

	if !strings.Contains(unit, "ExecStart=/usr/local/bin/openusage telemetry daemon run --socket-path /tmp/openusage.sock") {
		t.Fatalf("systemd unit does not include daemon run subcommand:\n%s", unit)
	}
}
