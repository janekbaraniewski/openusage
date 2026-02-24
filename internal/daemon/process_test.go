package daemon

import (
	"testing"

	"github.com/janekbaraniewski/openusage/internal/version"
)

func TestIsReleaseSemver(t *testing.T) {
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
			if got := IsReleaseSemver(tt.input); got != tt.wantOK {
				t.Fatalf("IsReleaseSemver(%q) = %v, want %v", tt.input, got, tt.wantOK)
			}
		})
	}
}

func TestHealthCurrent(t *testing.T) {
	origVersion := version.Version
	t.Cleanup(func() {
		version.Version = origVersion
	})

	t.Run("release requires exact daemon version", func(t *testing.T) {
		version.Version = "v0.4.0"
		health := HealthResponse{DaemonVersion: "dev", APIVersion: APIVersion}
		if HealthCurrent(health) {
			t.Fatal("HealthCurrent() = true, want false")
		}
	})

	t.Run("release accepts exact daemon version", func(t *testing.T) {
		version.Version = "v0.4.0"
		health := HealthResponse{DaemonVersion: "v0.4.0", APIVersion: APIVersion}
		if !HealthCurrent(health) {
			t.Fatal("HealthCurrent() = false, want true")
		}
	})

	t.Run("local snapshot accepts running dev daemon", func(t *testing.T) {
		version.Version = "v0.4.0-11-g0aa98a4-dirty"
		health := HealthResponse{DaemonVersion: "dev", APIVersion: APIVersion}
		if !HealthCurrent(health) {
			t.Fatal("HealthCurrent() = false, want true")
		}
	})

	t.Run("api mismatch stays incompatible", func(t *testing.T) {
		version.Version = "v0.4.0-11-g0aa98a4-dirty"
		health := HealthResponse{DaemonVersion: "dev", APIVersion: "v2"}
		if HealthCurrent(health) {
			t.Fatal("HealthCurrent() = true, want false")
		}
	})
}
