package main

import (
	"testing"

	"github.com/janekbaraniewski/openusage/internal/version"
)

func TestIsReleaseSemverVersion(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		wantOK bool
	}{
		{name: "release", input: "v0.4.0", wantOK: true},
		{name: "release with spaces", input: "  v1.2.3  ", wantOK: true},
		{name: "dev", input: "dev", wantOK: false},
		{name: "dirty snapshot", input: "v0.4.0-11-g0aa98a4-dirty", wantOK: false},
		{name: "missing patch", input: "v0.4", wantOK: false},
		{name: "missing v", input: "0.4.0", wantOK: false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			if got := isReleaseSemverVersion(tt.input); got != tt.wantOK {
				t.Fatalf("isReleaseSemverVersion(%q) = %v, want %v", tt.input, got, tt.wantOK)
			}
		})
	}
}

func TestDaemonHealthCurrent(t *testing.T) {
	origVersion := version.Version
	t.Cleanup(func() {
		version.Version = origVersion
	})

	t.Run("release requires exact daemon version", func(t *testing.T) {
		version.Version = "v0.4.0"
		health := daemonHealthResponse{DaemonVersion: "dev", APIVersion: telemetryDaemonAPIVersion}
		if daemonHealthCurrent(health) {
			t.Fatal("daemonHealthCurrent() = true, want false")
		}
	})

	t.Run("release accepts exact daemon version", func(t *testing.T) {
		version.Version = "v0.4.0"
		health := daemonHealthResponse{DaemonVersion: "v0.4.0", APIVersion: telemetryDaemonAPIVersion}
		if !daemonHealthCurrent(health) {
			t.Fatal("daemonHealthCurrent() = false, want true")
		}
	})

	t.Run("local snapshot accepts running dev daemon", func(t *testing.T) {
		version.Version = "v0.4.0-11-g0aa98a4-dirty"
		health := daemonHealthResponse{DaemonVersion: "dev", APIVersion: telemetryDaemonAPIVersion}
		if !daemonHealthCurrent(health) {
			t.Fatal("daemonHealthCurrent() = false, want true")
		}
	})

	t.Run("api mismatch stays incompatible", func(t *testing.T) {
		version.Version = "v0.4.0-11-g0aa98a4-dirty"
		health := daemonHealthResponse{DaemonVersion: "dev", APIVersion: "v2"}
		if daemonHealthCurrent(health) {
			t.Fatal("daemonHealthCurrent() = true, want false")
		}
	})
}
