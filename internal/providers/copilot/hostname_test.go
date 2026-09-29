package copilot

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// fakeGHHostScript is a fake `gh` that records its argv (one invocation per
// line) to $GH_ARGS_LOG, fails `gh copilot --version` (extension missing),
// and answers /user with a login that depends on whether --hostname
// acme.ghe.com was passed.
const fakeGHHostScript = `
echo "$*" >> "$GH_ARGS_LOG"
case "$*" in
  "copilot --version"*)
    echo "unknown command \"copilot\" for \"gh\"" >&2
    exit 1
    ;;
  "auth status"*)
    echo "Logged in"
    exit 0
    ;;
esac
host="github.com"
case "$*" in
  *"--hostname acme.ghe.com"*) host="acme.ghe.com" ;;
esac
case "$*" in
  *" /user"*)
    if [ "$host" = "acme.ghe.com" ]; then
      echo '{"login":"ghe-user","name":"GHE User","plan":{"name":"enterprise"}}'
    else
      echo '{"login":"dotcom-user","name":"Dotcom User","plan":{"name":"free"}}'
    fi
    exit 0
    ;;
  *"/copilot_internal/user"*)
    echo '{"login":"x","access_type_sku":"copilot_pro","copilot_plan":"individual","chat_enabled":true,"organization_login_list":[],"organization_list":[]}'
    exit 0
    ;;
  *"/rate_limit"*)
    echo '{"resources":{"core":{"limit":5000,"remaining":4999,"reset":2000000000,"used":1}}}'
    exit 0
    ;;
esac
exit 1
`

type fakeGHEnv struct {
	ghBin     string
	configDir string
	logPath   string
}

func setupFakeGH(t *testing.T) fakeGHEnv {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("test uses shell scripts")
	}
	binDir := t.TempDir()
	// Restrict PATH so a real `copilot` binary on the machine is not picked up.
	t.Setenv("PATH", binDir)
	logPath := filepath.Join(t.TempDir(), "gh-args.log")
	t.Setenv("GH_ARGS_LOG", logPath)

	configDir := filepath.Join(t.TempDir(), ".copilot")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	return fakeGHEnv{
		ghBin:     writeTestExe(t, binDir, "gh", fakeGHHostScript),
		configDir: configDir,
		logPath:   logPath,
	}
}

func (e fakeGHEnv) invocations(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(e.logPath)
	if err != nil {
		t.Fatalf("read gh args log: %v", err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func (e fakeGHEnv) reset(t *testing.T) {
	t.Helper()
	if err := os.Remove(e.logPath); err != nil && !os.IsNotExist(err) {
		t.Fatalf("reset gh args log: %v", err)
	}
}

func TestFetch_PassesHostnameForGHEBaseURL(t *testing.T) {
	env := setupFakeGH(t)

	acct := testCopilotAccount(env.ghBin, env.configDir, "")
	acct.BaseURL = "https://acme.ghe.com"

	snap, err := New().Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	if got := snap.Raw["github_login"]; got != "ghe-user" {
		t.Fatalf("github_login = %q, want ghe-user", got)
	}

	sawAuth, sawAPI := false, false
	for _, inv := range env.invocations(t) {
		switch {
		case strings.HasPrefix(inv, "auth status"):
			sawAuth = true
			if !strings.Contains(inv, "--hostname acme.ghe.com") {
				t.Errorf("auth status call missing --hostname: %q", inv)
			}
		case strings.HasPrefix(inv, "api "):
			sawAPI = true
			if !strings.Contains(inv, "--hostname acme.ghe.com") {
				t.Errorf("api call missing --hostname: %q", inv)
			}
		}
	}
	if !sawAuth || !sawAPI {
		t.Fatalf("expected auth status and api calls, got %v", env.invocations(t))
	}
}

func TestFetch_OmitsHostnameForGitHubDotCom(t *testing.T) {
	for _, baseURL := range []string{"", "https://github.com", "https://api.github.com"} {
		t.Run(baseURL, func(t *testing.T) {
			env := setupFakeGH(t)

			acct := testCopilotAccount(env.ghBin, env.configDir, "")
			acct.BaseURL = baseURL

			snap, err := New().Fetch(context.Background(), acct)
			if err != nil {
				t.Fatalf("Fetch() error: %v", err)
			}
			if got := snap.Raw["github_login"]; got != "dotcom-user" {
				t.Fatalf("github_login = %q, want dotcom-user", got)
			}
			for _, inv := range env.invocations(t) {
				if strings.Contains(inv, "--hostname") {
					t.Errorf("unexpected --hostname for github.com account: %q", inv)
				}
			}
		})
	}
}

func TestFetch_VersionCheckFailureIsNonFatal(t *testing.T) {
	env := setupFakeGH(t)

	acct := testCopilotAccount(env.ghBin, env.configDir, "")

	snap, err := New().Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Fatalf("Status = %q (%s), want %q", snap.Status, snap.Message, core.StatusOK)
	}
	if snap.Raw["copilot_version_error"] == "" {
		t.Fatal("expected copilot_version_error to be recorded")
	}
	if _, ok := snap.Raw["copilot_version"]; ok {
		t.Fatalf("copilot_version should be unset, got %q", snap.Raw["copilot_version"])
	}
	// The API calls after the failed version check must still have run.
	if got := snap.Raw["github_login"]; got != "dotcom-user" {
		t.Fatalf("github_login = %q, want dotcom-user (fetch stopped after version check?)", got)
	}
}

func TestFetch_CacheIsolatedPerHost(t *testing.T) {
	env := setupFakeGH(t)
	p := New()
	ctx := context.Background()

	// Same account ID, different hosts: the cache key must still differ.
	dotcom := testCopilotAccount(env.ghBin, env.configDir, "")
	ghe := testCopilotAccount(env.ghBin, env.configDir, "")
	ghe.BaseURL = "https://acme.ghe.com"

	snap1, err := p.Fetch(ctx, dotcom)
	if err != nil {
		t.Fatalf("Fetch(dotcom) error: %v", err)
	}
	if snap1.Status != core.StatusOK || snap1.Raw["github_login"] != "dotcom-user" {
		t.Fatalf("dotcom snapshot = %q/%q, want OK/dotcom-user", snap1.Status, snap1.Raw["github_login"])
	}

	env.reset(t)
	snap2, err := p.Fetch(ctx, ghe)
	if err != nil {
		t.Fatalf("Fetch(ghe) error: %v", err)
	}
	if got := snap2.Raw["github_login"]; got != "ghe-user" {
		t.Fatalf("ghe github_login = %q, want ghe-user (served from github.com cache?)", got)
	}
	sawGHEAuth := false
	for _, inv := range env.invocations(t) {
		if strings.HasPrefix(inv, "auth status") && strings.Contains(inv, "--hostname acme.ghe.com") {
			sawGHEAuth = true
		}
	}
	if !sawGHEAuth {
		t.Fatal("expected a fresh `gh auth status --hostname acme.ghe.com`; auth result was shared across hosts")
	}

	// Each host keeps its own cached snapshot.
	again1, _ := p.Fetch(ctx, dotcom)
	again2, _ := p.Fetch(ctx, ghe)
	if again1.Raw["github_login"] != "dotcom-user" || again2.Raw["github_login"] != "ghe-user" {
		t.Fatalf("cached logins = %q/%q, want dotcom-user/ghe-user", again1.Raw["github_login"], again2.Raw["github_login"])
	}
	if !again1.Timestamp.Equal(snap1.Timestamp) || !again2.Timestamp.Equal(snap2.Timestamp) {
		t.Fatal("expected second fetches to be served from per-host caches")
	}

	if accountCacheKey(dotcom) == accountCacheKey(ghe) {
		t.Fatal("accountCacheKey must differ across hosts")
	}
	other := dotcom
	other.ID = "copilot-work"
	if accountCacheKey(dotcom) == accountCacheKey(other) {
		t.Fatal("accountCacheKey must differ across account IDs")
	}
}
