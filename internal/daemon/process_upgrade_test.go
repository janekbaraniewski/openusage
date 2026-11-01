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
		name       string
		ours       string
		health     HealthResponse
		preMarked  bool
		want       bool
		wantWarn   string
		wantNoWarn bool
	}{
		{name: "newer release upgrades older release daemon", ours: "v0.26.0", health: compatible("0.25.1"), want: true},
		{name: "older release never downgrades newer daemon", ours: "v0.24.4", health: compatible("0.26.0"), wantWarn: "not downgrading"},
		{name: "equal version is a silent no-op", ours: "v0.26.0", health: compatible("0.26.0"), wantNoWarn: true},
		{name: "equal version without v prefix is a no-op", ours: "0.26.0", health: compatible("v0.26.0"), wantNoWarn: true},
		{name: "dev build leaves release daemon alone", ours: "dev", health: compatible("0.26.0"), wantWarn: "openusage telemetry daemon install"},
		{name: "local make build leaves older release daemon alone", ours: "v0.25.1-6-g611051b", health: compatible("0.24.4"), wantWarn: "openusage telemetry daemon install"},
		{name: "dirty build leaves release daemon alone", ours: "v0.25.1-6-g611051b-dirty", health: compatible("0.25.1"), wantWarn: "openusage telemetry daemon install"},
		{name: "release leaves dev daemon alone", ours: "v0.26.0", health: compatible("v0.25.1-6-g611051b"), wantWarn: "openusage telemetry daemon install"},
		{name: "release leaves unknown daemon alone", ours: "v0.26.0", health: compatible(""), wantWarn: "unknown"},
		{name: "second upgrade within cooldown is refused", ours: "v0.26.0", health: compatible("0.24.4"), preMarked: true, wantWarn: "keeps reinstalling"},
		{name: "incompatible API upgrades older daemon", ours: "v0.26.0", health: HealthResponse{DaemonVersion: "0.24.4", APIVersion: "v999"}, want: true},
		{name: "incompatible API from dev build still upgrades", ours: "dev", health: HealthResponse{DaemonVersion: "0.24.4", APIVersion: "v999"}, want: true},
		{name: "incompatible API never downgrades newer daemon", ours: "v0.24.4", health: HealthResponse{DaemonVersion: "0.26.0", APIVersion: "v999"}, wantWarn: "upgrade openusage"},
		{name: "incompatible API within cooldown reports out of date", ours: "v0.26.0", health: HealthResponse{DaemonVersion: "0.24.4", APIVersion: "v999"}, preMarked: true, wantWarn: "out of date"},
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
			if tt.wantNoWarn && warn != "" {
				t.Fatalf("warning = %q, want none", warn)
			}
		})
	}
}

func TestAutoUpgradeDebounce(t *testing.T) {
	origVersion := version.Version
	t.Cleanup(func() {
		version.Version = origVersion
		resetAutoUpgradeForTest()
	})
	resetAutoUpgradeForTest()
	version.Version = "v0.26.0"
	health := HealthResponse{DaemonVersion: "0.25.1", APIVersion: APIVersion}

	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	autoUpgradeNow = func() time.Time { return now }

	if ok, warn := autoUpgradeAllowed(health); !ok {
		t.Fatalf("first upgrade refused: %q", warn)
	}
	now = now.Add(autoUpgradeCooldown - time.Second)
	if ok, _ := autoUpgradeAllowed(health); ok {
		t.Fatal("second upgrade inside cooldown was allowed")
	}
	// The refused attempt must not extend the window.
	now = now.Add(2 * time.Second)
	if ok, warn := autoUpgradeAllowed(health); !ok {
		t.Fatalf("upgrade after cooldown refused: %q", warn)
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
		version.Version = "v0.26.0"
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

	t.Run("equal version with different registry is a no-op", func(t *testing.T) {
		resetAutoUpgradeForTest()
		version.Version = "v0.26.0"
		socket := fakeHealthDaemon(t, HealthResponse{Status: "ok", DaemonVersion: "0.26.0", APIVersion: APIVersion, ProviderRegistry: "other"})
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		client, warning, err := EnsureRunning(ctx, socket, false)
		if err != nil || client == nil || warning != "" {
			t.Fatalf("EnsureRunning() = (%v, %q, %v), want client and no warning", client, warning, err)
		}
	})

	t.Run("local build keeps release daemon and hints", func(t *testing.T) {
		resetAutoUpgradeForTest()
		version.Version = "v0.26.0-3-gabc1234-dirty"
		socket := fakeHealthDaemon(t, HealthResponse{Status: "ok", DaemonVersion: "0.25.1", APIVersion: APIVersion, ProviderRegistry: "other"})
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		client, warning, err := EnsureRunning(ctx, socket, false)
		if err != nil || client == nil {
			t.Fatalf("EnsureRunning() = (%v, %q, %v), want client", client, warning, err)
		}
		if !strings.Contains(warning, "daemon install") {
			t.Fatalf("warning %q should tell the user how to switch", warning)
		}
	})
}

func TestVerifyInstalledDaemon(t *testing.T) {
	origVersion := version.Version
	t.Cleanup(func() { version.Version = origVersion })
	version.Version = "v0.26.0"
	ctx := context.Background()

	stale := fakeHealthDaemon(t, HealthResponse{Status: "ok", DaemonVersion: "0.24.4", APIVersion: APIVersion, ProviderRegistry: "old"})
	err := verifyInstalledDaemon(ctx, NewClient(stale), 2*time.Second)
	if err == nil || !strings.Contains(err.Error(), "0.24.4") || !strings.Contains(err.Error(), "another openusage process") {
		t.Fatalf("stale daemon: err = %v, want reinstall-conflict error", err)
	}

	current := fakeHealthDaemon(t, HealthResponse{Status: "ok", DaemonVersion: "v0.26.0", APIVersion: APIVersion, ProviderRegistry: ProviderRegistryHash()})
	if err := verifyInstalledDaemon(ctx, NewClient(current), 2*time.Second); err != nil {
		t.Fatalf("current daemon: err = %v", err)
	}

	if err := verifyInstalledDaemon(ctx, NewClient(filepath.Join(t.TempDir(), "none.sock")), 2*time.Second); err != nil {
		t.Fatalf("unreachable daemon should defer to startup diagnostics, got %v", err)
	}
}

func TestUnitProgram(t *testing.T) {
	dir := t.TempDir()
	plist := filepath.Join(dir, "a.plist")
	_ = os.WriteFile(plist, []byte(launchdPlistForTest("/opt/homebrew/bin/openusage")), 0o644)
	if got := unitProgram(plist); got != "/opt/homebrew/bin/openusage" {
		t.Fatalf("plist: unitProgram = %q", got)
	}
	unit := filepath.Join(dir, "a.service")
	_ = os.WriteFile(unit, []byte("[Service]\nExecStart=/usr/local/bin/openusage telemetry daemon run\n"), 0o644)
	if got := unitProgram(unit); got != "/usr/local/bin/openusage" {
		t.Fatalf("systemd: unitProgram = %q", got)
	}
	if got := unitProgram(filepath.Join(dir, "missing")); got != "" {
		t.Fatalf("missing: unitProgram = %q", got)
	}
}

func launchdPlistForTest(exe string) string {
	return `<plist version="1.0"><dict><key>Label</key><string>x</string>
<key>ProgramArguments</key><array><string>` + exe + `</string><string>telemetry</string></array></dict></plist>`
}
