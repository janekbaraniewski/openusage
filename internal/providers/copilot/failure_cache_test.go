package copilot

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// fakeGHFailingScript logs every invocation to $GH_ARGS_LOG, has no
// `gh copilot` extension, reports a rate-limited auth status (a non-OK
// snapshot), lists two orgs, and answers org billing with a 403.
const fakeGHFailingScript = `
echo "$*" >> "$GH_ARGS_LOG"
case "$*" in
  "copilot --version"*) echo "unknown command" >&2; exit 1 ;;
  "auth status"*) echo "Logged in; API rate limit exceeded"; exit 0 ;;
  *"/copilot_internal/user"*)
    echo '{"login":"x","copilot_plan":"business","organization_login_list":["acme","beta"],"organization_list":[]}'
    exit 0 ;;
  *" /user"*) echo '{"login":"x"}'; exit 0 ;;
  *"/rate_limit"*) echo '{"resources":{}}'; exit 0 ;;
  *"/copilot/billing"*) echo 'HTTP 403: Must be an org admin' >&2; exit 1 ;;
  *"/copilot/metrics"*) echo '[]'; exit 0 ;;
esac
exit 1
`

func countInvocations(lines []string, substr string) int {
	n := 0
	for _, l := range lines {
		if strings.Contains(l, substr) {
			n++
		}
	}
	return n
}

func TestFetch_CachesFailuresAndOrgData(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses shell scripts")
	}
	binDir := t.TempDir()
	t.Setenv("PATH", binDir) // no real copilot binary
	logPath := filepath.Join(t.TempDir(), "gh-args.log")
	t.Setenv("GH_ARGS_LOG", logPath)
	configDir := filepath.Join(t.TempDir(), ".copilot")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	env := fakeGHEnv{
		ghBin:     writeTestExe(t, binDir, "gh", fakeGHFailingScript),
		configDir: configDir,
		logPath:   logPath,
	}

	p := New()
	acct := testCopilotAccount(env.ghBin, env.configDir, "")
	key := accountCacheKey(acct)
	ctx := context.Background()

	snap1, err := p.Fetch(ctx, acct)
	if err != nil {
		t.Fatalf("Fetch #1: %v", err)
	}
	if snap1.Status == core.StatusOK {
		t.Fatalf("Fetch #1 status = OK, want a non-OK status from the rate-limited auth output")
	}
	first := env.invocations(t)
	if got := countInvocations(first, "/orgs/"); got != 4 {
		t.Fatalf("Fetch #1 made %d org calls, want 4 (billing+metrics for 2 orgs): %v", got, first)
	}

	// A non-OK snapshot is served from cache for ttlSnapshotFailure.
	env.reset(t)
	snap2, err := p.Fetch(ctx, acct)
	if err != nil {
		t.Fatalf("Fetch #2: %v", err)
	}
	if !snap2.Timestamp.Equal(snap1.Timestamp) {
		t.Fatal("Fetch #2 refetched; a non-OK snapshot must be cached briefly")
	}
	if _, err := os.Stat(logPath); err == nil {
		t.Fatalf("Fetch #2 ran gh: %v", env.invocations(t))
	}

	// After the snapshot expires, the failed version probe and the org
	// responses (including the 403) still come from cache.
	p.cacheMu.Lock()
	p.cacheLocked(key).lastSnapAt = time.Now().Add(-ttlSnapshotFailure - time.Second)
	p.cacheMu.Unlock()
	env.reset(t)
	snap3, err := p.Fetch(ctx, acct)
	if err != nil {
		t.Fatalf("Fetch #3: %v", err)
	}
	third := env.invocations(t)
	if got := countInvocations(third, "copilot --version"); got != 0 {
		t.Fatalf("Fetch #3 re-ran `gh copilot --version` within ttlVersionFailure: %v", third)
	}
	if got := countInvocations(third, "/orgs/"); got != 0 {
		t.Fatalf("Fetch #3 re-fetched org data within ttlOrgData: %v", third)
	}
	if snap3.Raw["copilot_version_error"] == "" {
		t.Fatal("cached version failure must still be reported")
	}
	if snap3.Raw["org_beta_billing_plan"] != "" || snap3.Raw["copilot_orgs"] != "acme, beta" {
		t.Fatalf("unexpected org raw data: %v", snap3.Raw)
	}

	// Once the failure TTLs pass, both are probed again.
	p.cacheMu.Lock()
	c := p.cacheLocked(key)
	c.lastSnapAt = time.Time{}
	c.versionFetchedAt = time.Now().Add(-ttlVersionFailure - time.Second)
	for k, r := range c.orgResponses {
		r.fetchedAt = time.Now().Add(-ttlOrgData - time.Second)
		c.orgResponses[k] = r
	}
	p.cacheMu.Unlock()
	env.reset(t)
	if _, err := p.Fetch(ctx, acct); err != nil {
		t.Fatalf("Fetch #4: %v", err)
	}
	fourth := env.invocations(t)
	if countInvocations(fourth, "copilot --version") != 1 || countInvocations(fourth, "/orgs/") != 4 {
		t.Fatalf("Fetch #4 should re-probe the version and org data after expiry: %v", fourth)
	}
}
