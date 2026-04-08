package copilot

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestFetchCache_ReturnsCachedSnapshotWithinTTL(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses shell scripts")
	}

	tmp := t.TempDir()
	configDir := filepath.Join(t.TempDir(), ".copilot")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}

	callCount := 0
	ghBin := writeTestExe(t, tmp, "gh", `
if [ "$1" = "copilot" ] && [ "$2" = "--version" ]; then
  echo "gh copilot 1.0.0"
  exit 0
fi
if [ "$1" = "auth" ] && [ "$2" = "status" ]; then
  echo "Logged in to github.com as testuser"
  exit 0
fi
if [ "$1" = "api" ]; then
  endpoint=""
  for arg in "$@"; do endpoint="$arg"; done
  case "$endpoint" in
    "/user")
      echo '{"login":"testuser","name":"Test User","plan":{"name":"free"}}'
      exit 0
      ;;
    "/copilot_internal/user")
      echo '{"login":"testuser","access_type_sku":"copilot_pro","copilot_plan":"individual","chat_enabled":true,"is_mcp_enabled":false,"organization_login_list":[],"organization_list":[]}'
      exit 0
      ;;
    "/rate_limit")
      echo '{"resources":{"core":{"limit":5000,"remaining":4999,"reset":2000000000,"used":1}}}'
      exit 0
      ;;
  esac
fi
exit 1
`)
	_ = callCount

	p := New()
	acct := testCopilotAccount(ghBin, configDir, "")

	// First call should populate the cache.
	snap1, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() #1 error: %v", err)
	}
	if snap1.Status != core.StatusOK {
		t.Fatalf("Fetch() #1 status = %q, want %q", snap1.Status, core.StatusOK)
	}

	// Second call should return the cached snapshot (same timestamp).
	snap2, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() #2 error: %v", err)
	}
	if snap2.Timestamp != snap1.Timestamp {
		t.Fatalf("expected cached snapshot (same timestamp), got different: %v vs %v", snap2.Timestamp, snap1.Timestamp)
	}
	if snap2.Raw["github_login"] != "testuser" {
		t.Fatalf("cached snapshot lost github_login: %q", snap2.Raw["github_login"])
	}
}

func TestFetchCache_DoesNotCacheErrorStatus(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses shell scripts")
	}

	tmp := t.TempDir()
	configDir := filepath.Join(t.TempDir(), ".copilot")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}

	// A gh binary that fails auth.
	ghBin := writeTestExe(t, tmp, "gh", `
if [ "$1" = "copilot" ] && [ "$2" = "--version" ]; then
  echo "gh copilot 1.0.0"
  exit 0
fi
if [ "$1" = "auth" ] && [ "$2" = "status" ]; then
  echo "not logged in" >&2
  exit 1
fi
exit 1
`)

	p := New()
	acct := testCopilotAccount(ghBin, configDir, "")

	snap1, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() #1 error: %v", err)
	}
	if snap1.Status != core.StatusAuth {
		t.Fatalf("Fetch() #1 status = %q, want %q", snap1.Status, core.StatusAuth)
	}

	// Second call should NOT return cached data (error status).
	snap2, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() #2 error: %v", err)
	}
	// The second call should go through the full fetch flow again, producing
	// a fresh timestamp.
	if snap2.Timestamp == snap1.Timestamp {
		t.Fatal("expected fresh fetch for error status, got cached timestamp")
	}
}

func TestFetchCache_ExpiredSnapshotRefetches(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses shell scripts")
	}

	tmp := t.TempDir()
	configDir := filepath.Join(t.TempDir(), ".copilot")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}

	ghBin := writeTestExe(t, tmp, "gh", `
if [ "$1" = "copilot" ] && [ "$2" = "--version" ]; then
  echo "gh copilot 1.0.0"
  exit 0
fi
if [ "$1" = "auth" ] && [ "$2" = "status" ]; then
  echo "Logged in to github.com as testuser"
  exit 0
fi
if [ "$1" = "api" ]; then
  endpoint=""
  for arg in "$@"; do endpoint="$arg"; done
  case "$endpoint" in
    "/user")
      echo '{"login":"testuser","name":"Test User","plan":{"name":"free"}}'
      exit 0
      ;;
    "/copilot_internal/user")
      echo '{"login":"testuser","access_type_sku":"copilot_pro","copilot_plan":"individual","chat_enabled":true,"is_mcp_enabled":false,"organization_login_list":[],"organization_list":[]}'
      exit 0
      ;;
    "/rate_limit")
      echo '{"resources":{"core":{"limit":5000,"remaining":4999,"reset":2000000000,"used":1}}}'
      exit 0
      ;;
  esac
fi
exit 1
`)

	p := New()
	acct := testCopilotAccount(ghBin, configDir, "")

	snap1, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() #1 error: %v", err)
	}
	if snap1.Status != core.StatusOK {
		t.Fatalf("Fetch() #1 status = %q, want %q", snap1.Status, core.StatusOK)
	}

	// Manually expire the snapshot cache.
	p.cacheMu.Lock()
	p.apiCache.lastSnapAt = time.Now().Add(-3 * time.Minute)
	p.cacheMu.Unlock()

	snap2, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() #2 error: %v", err)
	}
	if snap2.Timestamp == snap1.Timestamp {
		t.Fatal("expected fresh fetch after TTL expiry, got cached timestamp")
	}
	if snap2.Status != core.StatusOK {
		t.Fatalf("Fetch() #2 status = %q, want %q", snap2.Status, core.StatusOK)
	}
}

func TestFetchCache_BinaryResolutionCached(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses shell scripts")
	}

	tmp := t.TempDir()
	copilotBin := writeTestExe(t, tmp, "copilot", `
if [ "$1" = "--version" ]; then
  echo "copilot 1.2.3"
  exit 0
fi
exit 1
`)

	configDir := filepath.Join(t.TempDir(), ".copilot")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}

	p := New()
	acct := testCopilotAccount(copilotBin, configDir, "")

	// First resolution.
	gh1, cp1 := p.resolveAndCacheBinaries(acct)

	// Second call should hit cache.
	gh2, cp2 := p.resolveAndCacheBinaries(acct)
	if gh1 != gh2 || cp1 != cp2 {
		t.Fatalf("binary resolution mismatch: (%q,%q) vs (%q,%q)", gh1, cp1, gh2, cp2)
	}

	// Verify cache was populated.
	p.cacheMu.Lock()
	if p.apiCache == nil || p.apiCache.binaryResolvedAt.IsZero() {
		t.Fatal("expected binaryResolvedAt to be set")
	}
	p.cacheMu.Unlock()
}

func TestFetchCache_VersionDetectionCached(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses shell scripts")
	}

	tmp := t.TempDir()
	copilotBin := writeTestExe(t, tmp, "copilot", `
if [ "$1" = "--version" ]; then
  echo "copilot 1.2.3"
  exit 0
fi
exit 1
`)

	p := New()
	ctx := context.Background()

	// First detection spawns subprocess.
	v1, s1, err := p.detectAndCacheVersion(ctx, "", copilotBin)
	if err != nil {
		t.Fatalf("detectAndCacheVersion() #1 error: %v", err)
	}
	if v1 != "copilot 1.2.3" || s1 != "copilot" {
		t.Fatalf("version = (%q, %q), want (%q, %q)", v1, s1, "copilot 1.2.3", "copilot")
	}

	// Second call should return from cache.
	v2, s2, err := p.detectAndCacheVersion(ctx, "", copilotBin)
	if err != nil {
		t.Fatalf("detectAndCacheVersion() #2 error: %v", err)
	}
	if v1 != v2 || s1 != s2 {
		t.Fatalf("version mismatch: (%q,%q) vs (%q,%q)", v1, s1, v2, s2)
	}

	// Verify cache timestamps.
	p.cacheMu.Lock()
	if p.apiCache == nil || p.apiCache.versionFetchedAt.IsZero() {
		t.Fatal("expected versionFetchedAt to be set")
	}
	p.cacheMu.Unlock()
}

func TestFetchCache_AuthStatusCached(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses shell scripts")
	}

	tmp := t.TempDir()
	ghBin := writeTestExe(t, tmp, "gh", `
if [ "$1" = "auth" ] && [ "$2" = "status" ]; then
  echo "Logged in to github.com as testuser"
  exit 0
fi
exit 1
`)

	p := New()
	ctx := context.Background()

	out1, ok1 := p.checkAndCacheAuth(ctx, ghBin)
	if !ok1 {
		t.Fatal("checkAndCacheAuth() #1 expected ok=true")
	}
	if !strings.Contains(out1, "Logged in") {
		t.Fatalf("auth output = %q, want Logged in", out1)
	}

	// Second call returns from cache.
	out2, ok2 := p.checkAndCacheAuth(ctx, ghBin)
	if !ok2 || out1 != out2 {
		t.Fatalf("auth cache mismatch: (%q,%v) vs (%q,%v)", out1, ok1, out2, ok2)
	}

	// Expire and re-check.
	p.cacheMu.Lock()
	p.apiCache.authFetchedAt = time.Now().Add(-10 * time.Minute)
	p.cacheMu.Unlock()

	out3, ok3 := p.checkAndCacheAuth(ctx, ghBin)
	if !ok3 {
		t.Fatal("checkAndCacheAuth() #3 expected ok=true after expiry")
	}
	if !strings.Contains(out3, "Logged in") {
		t.Fatalf("auth output after expiry = %q", out3)
	}
}

func TestFetchCache_ConcurrentAccess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses shell scripts")
	}

	tmp := t.TempDir()
	configDir := filepath.Join(t.TempDir(), ".copilot")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}

	ghBin := writeTestExe(t, tmp, "gh", `
if [ "$1" = "copilot" ] && [ "$2" = "--version" ]; then
  echo "gh copilot 1.0.0"
  exit 0
fi
if [ "$1" = "auth" ] && [ "$2" = "status" ]; then
  echo "Logged in to github.com as testuser"
  exit 0
fi
if [ "$1" = "api" ]; then
  endpoint=""
  for arg in "$@"; do endpoint="$arg"; done
  case "$endpoint" in
    "/user")
      echo '{"login":"testuser","name":"Test","plan":{"name":"free"}}'
      exit 0
      ;;
    "/copilot_internal/user")
      echo '{"login":"testuser","access_type_sku":"copilot_pro","copilot_plan":"individual","chat_enabled":true,"is_mcp_enabled":false,"organization_login_list":[],"organization_list":[]}'
      exit 0
      ;;
    "/rate_limit")
      echo '{"resources":{"core":{"limit":5000,"remaining":4999,"reset":2000000000,"used":1}}}'
      exit 0
      ;;
  esac
fi
exit 1
`)

	p := New()
	acct := testCopilotAccount(ghBin, configDir, "")

	// Populate cache first.
	if _, err := p.Fetch(context.Background(), acct); err != nil {
		t.Fatalf("initial Fetch() error: %v", err)
	}

	// Concurrent reads should not race.
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			snap, err := p.Fetch(context.Background(), acct)
			if err != nil {
				t.Errorf("concurrent Fetch() error: %v", err)
				return
			}
			if snap.Status != core.StatusOK {
				t.Errorf("concurrent Fetch() status = %q", snap.Status)
			}
		}()
	}
	wg.Wait()
}
