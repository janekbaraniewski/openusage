package daemon

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/version"
)

func TestCompareBuildVersions(t *testing.T) {
	tests := []struct {
		a, b   string
		want   int
		wantOK bool
	}{
		{"v0.25.1", "0.25.1", 0, true},
		{"v0.25.1-6-g611051b", "0.24.4", 1, true},
		{"v0.25.1-6-g611051b-dirty", "v0.25.1", 1, true},
		{"v0.25.1-6-g611051b", "v0.25.1-7-gabcdef0", -1, true},
		{"v0.25.1-6-g611051b", "v0.26.0", -1, true},
		{"0.24.4", "v0.26.0", -1, true},
		{"dev", "v0.26.0", 0, false},
		{"v0.26.0", "unknown", 0, false},
	}
	for _, tt := range tests {
		got, ok := compareBuildVersions(tt.a, tt.b)
		if ok != tt.wantOK || (ok && got != tt.want) {
			t.Errorf("compareBuildVersions(%q, %q) = (%d, %v), want (%d, %v)", tt.a, tt.b, got, ok, tt.want, tt.wantOK)
		}
	}
}

func TestAutoUpgradeAllowed(t *testing.T) {
	origVersion := version.Version
	t.Cleanup(func() {
		version.Version = origVersion
		resetAutoUpgradeForTest()
	})

	compatible := func(v string) HealthResponse {
		return HealthResponse{DaemonVersion: v, APIVersion: APIVersion, ProviderRegistry: "stale"}
	}

	tests := []struct {
		name      string
		ours      string
		health    HealthResponse
		preMarked bool
		want      bool
		wantWarn  string
	}{
		{name: "local build upgrades older release daemon", ours: "v0.25.1-6-g611051b", health: compatible("0.24.4"), want: true},
		{name: "release upgrades older release daemon", ours: "v0.26.0", health: compatible("0.25.1"), want: true},
		{name: "never downgrades newer daemon", ours: "v0.24.4", health: compatible("0.26.0"), wantWarn: "not downgrading"},
		{name: "dev build leaves release daemon alone", ours: "dev", health: compatible("0.26.0"), wantWarn: "openusage telemetry daemon install"},
		{name: "second upgrade in one process is refused", ours: "v0.25.1-6-g611051b", health: compatible("0.24.4"), preMarked: true, wantWarn: "keeps reinstalling"},
		{name: "incompatible API still upgrades once", ours: "v0.24.4", health: HealthResponse{DaemonVersion: "0.26.0", APIVersion: "v999"}, want: true},
		{name: "incompatible API after flap reports out of date", ours: "v0.24.4", health: HealthResponse{DaemonVersion: "0.26.0", APIVersion: "v999"}, preMarked: true, wantWarn: "out of date"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetAutoUpgradeForTest()
			if tt.preMarked {
				markAutoUpgradeAttempted()
			}
			version.Version = tt.ours
			got, warn := autoUpgradeAllowed(tt.health)
			if got != tt.want {
				t.Fatalf("autoUpgradeAllowed() = %v (%q), want %v", got, warn, tt.want)
			}
			if tt.wantWarn != "" && !strings.Contains(warn, tt.wantWarn) {
				t.Fatalf("warning %q does not contain %q", warn, tt.wantWarn)
			}
		})
	}
}

// fakeHealthDaemon serves /healthz on a unix socket with the given response.
func fakeHealthDaemon(t *testing.T, health HealthResponse) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "ou-sock")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socketPath := filepath.Join(dir, "d.sock")
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(health)
	})}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return socketPath
}

// An incompatible-but-readable daemon that this process must not replace
// (newer build, or replaced again by another openusage process) must yield a
// usable client plus a warning, never an error that parks the TUI on the
// startup screen. None of these paths touch the service manager.
func TestEnsureRunning_DegradesInsteadOfFighting(t *testing.T) {
	origVersion := version.Version
	t.Cleanup(func() {
		version.Version = origVersion
		resetAutoUpgradeForTest()
	})

	t.Run("newer daemon is kept", func(t *testing.T) {
		resetAutoUpgradeForTest()
		version.Version = "v0.24.4"
		socket := fakeHealthDaemon(t, HealthResponse{Status: "ok", DaemonVersion: "0.26.0", APIVersion: APIVersion, ProviderRegistry: "other"})
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		client, warning, err := EnsureRunning(ctx, socket, false)
		if err != nil || client == nil {
			t.Fatalf("EnsureRunning() = (%v, %q, %v), want client", client, warning, err)
		}
		if !strings.Contains(warning, "0.26.0") {
			t.Fatalf("warning %q should name the running daemon version", warning)
		}
	})

	t.Run("daemon reverted by another process is kept", func(t *testing.T) {
		resetAutoUpgradeForTest()
		markAutoUpgradeAttempted()
		version.Version = "v0.25.1-6-g611051b"
		socket := fakeHealthDaemon(t, HealthResponse{Status: "ok", DaemonVersion: "0.24.4", APIVersion: APIVersion, ProviderRegistry: "old"})
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		client, warning, err := EnsureRunning(ctx, socket, false)
		if err != nil || client == nil {
			t.Fatalf("EnsureRunning() = (%v, %q, %v), want client", client, warning, err)
		}
		if !strings.Contains(warning, "keeps reinstalling") {
			t.Fatalf("warning %q should explain the reinstall loop", warning)
		}
	})
}
