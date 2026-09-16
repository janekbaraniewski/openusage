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

// Fake gh logs "$GH_HOST :: $@" per invocation, then answers the subset of
// commands Fetch needs. Auth output varies by host so cache cross-talk
// between accounts would be visible in snapshot values.
const ghHostFakeGH = `
echo "GH_HOST=${GH_HOST-unset} :: $@" >> "$OU_ARGV_LOG"
if [ "$1" = "copilot" ] && [ "$2" = "--version" ]; then
  echo "gh: unknown command copilot" >&2
  exit 1
fi
if [ "$1" = "auth" ] && [ "$2" = "status" ]; then
  echo "Logged in to ${GH_HOST-unset} as octocat"
  exit 0
fi
if [ "$1" = "api" ]; then
  endpoint=""
  for arg in "$@"; do endpoint="$arg"; done
  case "$endpoint" in
    "/user")
      echo '{"login":"octocat","name":"Octo Cat","plan":{"name":"free"}}'
      exit 0
      ;;
    "/copilot_internal/user")
      echo '{"login":"octocat","access_type_sku":"copilot_pro","copilot_plan":"individual","chat_enabled":true,"is_mcp_enabled":false,"organization_login_list":[],"organization_list":[]}'
      exit 0
      ;;
    "/rate_limit")
      echo '{"resources":{"core":{"limit":5000,"remaining":4999,"reset":2000000000,"used":1}}}'
      exit 0
      ;;
  esac
fi
echo "unsupported gh args: $*" >&2
exit 1
`

func setupGHHostFetch(t *testing.T) (ghBin, argvLog string, newAcct func(id, host string) core.AccountConfig) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("test uses shell scripts")
	}
	tmp := t.TempDir()
	configDir := filepath.Join(t.TempDir(), ".copilot")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	argvLog = filepath.Join(tmp, "argv.log")
	t.Setenv("OU_ARGV_LOG", argvLog)
	// Ensure the child env carries no GH_HOST unless ghCLI sets one.
	// (Unset, not emptied: ${VAR-default} only falls back when unset.)
	if prev, ok := os.LookupEnv("GH_HOST"); ok {
		t.Cleanup(func() { _ = os.Setenv("GH_HOST", prev) })
		if err := os.Unsetenv("GH_HOST"); err != nil {
			t.Fatalf("unset GH_HOST: %v", err)
		}
	}

	ghBin = writeTestExe(t, tmp, "gh", ghHostFakeGH)
	copilotBin := writeTestExe(t, tmp, "copilot", `
if [ "$1" = "--version" ]; then
  echo "copilot 1.2.3"
  exit 0
fi
exit 1
`)
	t.Setenv("PATH", tmp)

	newAcct = func(id, host string) core.AccountConfig {
		acct := testCopilotAccount(ghBin, configDir, copilotBin)
		acct.ID = id
		if host != "" {
			acct.SetOption("gh_host", host)
		}
		return acct
	}
	return ghBin, argvLog, newAcct
}

func readArgvLog(t *testing.T, argvLog string) []string {
	t.Helper()
	raw, err := os.ReadFile(argvLog)
	if err != nil {
		t.Fatalf("read argv log: %v", err)
	}
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// The gh_host account option must reach every gh invocation as GH_HOST
// (the single choke point in ghCLI.run) — never as --hostname flags.
func TestFetch_GHHostOptionSetsEnv(t *testing.T) {
	_, argvLog, newAcct := setupGHHostFetch(t)
	p := New()
	snap, err := p.Fetch(context.Background(), newAcct("copilot-ghe", "my-org.ghe.com"))
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Fatalf("Status = %q, want ok", snap.Status)
	}
	if got := snap.Raw["gh_host"]; got != "my-org.ghe.com" {
		t.Fatalf(`snap.Raw["gh_host"] = %q, want %q`, got, "my-org.ghe.com")
	}

	lines := readArgvLog(t, argvLog)
	var sawAuth, sawAPI bool
	for _, line := range lines {
		if strings.Contains(line, "--hostname") {
			t.Errorf("host must travel as GH_HOST, not --hostname: %q", line)
		}
		if strings.Contains(line, "--version") {
			// Host-independent version probe: must not carry a host.
			if !strings.HasPrefix(line, "GH_HOST=unset :: ") {
				t.Errorf("version probe must not set GH_HOST: %q", line)
			}
			continue
		}
		if !strings.HasPrefix(line, "GH_HOST=my-org.ghe.com :: ") {
			t.Errorf("invocation missing GH_HOST=my-org.ghe.com: %q", line)
			continue
		}
		argv := strings.TrimPrefix(line, "GH_HOST=my-org.ghe.com :: ")
		switch {
		case strings.HasPrefix(argv, "auth status"):
			sawAuth = true
		case strings.HasPrefix(argv, "api "):
			fields := strings.Fields(argv)
			if last := fields[len(fields)-1]; !strings.HasPrefix(last, "/") {
				t.Errorf("endpoint not last argv word: %q", line)
			} else {
				sawAPI = true
			}
		}
	}
	if !sawAuth {
		t.Error("no `gh auth status` invocation recorded")
	}
	if !sawAPI {
		t.Error("no `gh api` invocation recorded")
	}
}

// Without the option, ghCLI must not inject GH_HOST at all.
func TestFetch_DefaultHostLeavesEnvUnset(t *testing.T) {
	_, argvLog, newAcct := setupGHHostFetch(t)
	p := New()
	snap, err := p.Fetch(context.Background(), newAcct("copilot", ""))
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Fatalf("Status = %q, want ok", snap.Status)
	}
	if _, ok := snap.Raw["gh_host"]; ok {
		t.Errorf(`snap.Raw["gh_host"] set for default host`)
	}
	for _, line := range readArgvLog(t, argvLog) {
		if !strings.HasPrefix(line, "GH_HOST=unset :: ") {
			t.Errorf("default invocation must not set GH_HOST: %q", line)
		}
		if strings.Contains(line, "--hostname") {
			t.Errorf("unexpected --hostname in default argv: %q", line)
		}
	}
}

// One Provider serves all copilot accounts: auth/snapshot caches are keyed
// per account, so a github.com account and a GHE account must each trigger
// their own subprocess calls and never see each other's values.
func TestFetch_PerAccountCacheIsolation(t *testing.T) {
	_, argvLog, newAcct := setupGHHostFetch(t)
	p := New()
	ctx := context.Background()

	snapA, err := p.Fetch(ctx, newAcct("copilot", ""))
	if err != nil {
		t.Fatalf("Fetch(A) error: %v", err)
	}
	snapB, err := p.Fetch(ctx, newAcct("copilot-acme", "acme.ghe.com"))
	if err != nil {
		t.Fatalf("Fetch(B) error: %v", err)
	}
	if snapA.Status != core.StatusOK || snapB.Status != core.StatusOK {
		t.Fatalf("statuses = %q/%q, want ok/ok", snapA.Status, snapB.Status)
	}

	// Auth output varies by host in the fixture: equal values would prove
	// B was served A's cached entry.
	authA, authB := snapA.Raw["auth_status"], snapB.Raw["auth_status"]
	if !strings.Contains(authA, "unset") || !strings.Contains(authB, "acme.ghe.com") {
		t.Fatalf("auth cross-talk: A=%q B=%q", authA, authB)
	}

	authCalls := map[string]int{}
	for _, line := range readArgvLog(t, argvLog) {
		if strings.Contains(line, "auth status") {
			host := strings.SplitN(line, " :: ", 2)[0]
			authCalls[host]++
		}
	}
	if authCalls["GH_HOST=unset"] != 1 || authCalls["GH_HOST=acme.ghe.com"] != 1 {
		t.Fatalf("each account must auth once against its own host: %v", authCalls)
	}

	// Re-fetching A hits A's snapshot cache: no new subprocess calls.
	nBefore := len(readArgvLog(t, argvLog))
	snapA2, err := p.Fetch(ctx, newAcct("copilot", ""))
	if err != nil {
		t.Fatalf("Fetch(A) #2 error: %v", err)
	}
	if snapA2.Raw["auth_status"] != authA {
		t.Fatalf("cached A changed auth: %q vs %q", snapA2.Raw["auth_status"], authA)
	}
	if nAfter := len(readArgvLog(t, argvLog)); nAfter != nBefore {
		t.Fatalf("cached re-fetch spawned %d new gh calls", nAfter-nBefore)
	}
}

// The provider must declare gh_host so the settings UI can render it.
func TestSpec_DeclaresGHHostOption(t *testing.T) {
	spec := New().Spec()
	found := false
	for _, opt := range spec.Options {
		if opt.Key == "gh_host" {
			found = true
			if opt.Required {
				t.Error("gh_host must be optional (empty = github.com)")
			}
		}
	}
	if !found {
		t.Fatalf("Spec().Options lacks gh_host: %+v", spec.Options)
	}
}
