package kilocode

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/roocode"
)

type fixedClock struct{ t time.Time }

func (f fixedClock) Now() time.Time { return f.t }

// TestProvider_BasicMetadata sanity-checks the Kilo Code provider's
// surface (ID, auth type, widget shape). Mirrors the roocode parity test
// so the two providers stay aligned.
func TestProvider_BasicMetadata(t *testing.T) {
	p := New()
	if got, want := p.ID(), ID; got != want {
		t.Errorf("ID = %q, want %q", got, want)
	}
	if got, want := p.ID(), "kilo_code"; got != want {
		t.Errorf("ID = %q, want %q", got, want)
	}
	info := p.Describe()
	if info.Name == "" {
		t.Error("Describe().Name is empty")
	}
	spec := p.Spec()
	if spec.Auth.Type != core.ProviderAuthTypeLocal {
		t.Errorf("auth type = %v, want local", spec.Auth.Type)
	}
	if p.DashboardWidget().IsZero() {
		t.Error("DashboardWidget is zero")
	}
}

// TestProvider_Fetch_OverrideTasksDir drives Kilo Code's Fetch through
// the same shared parser as roocode, with the per-account tasks_dir
// override pointing at a synthetic task tree. Confirms wiring is intact
// without depending on a real Kilo Code install.
func TestProvider_Fetch_OverrideTasksDir(t *testing.T) {
	tasksRoot := t.TempDir()
	taskDir := filepath.Join(tasksRoot, "task-xyz")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, roocode.UIMessagesFile),
		[]byte(`[{"say":"api_req_started","ts":1716033600000,"text":"{\"cost\":0.05,\"tokensIn\":100,\"tokensOut\":50,\"apiProtocol\":\"anthropic\"}"}]`),
		0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, roocode.APIConversationHistoryFile),
		[]byte(`[{"content":"<model>kilo-test-model</model>"}]`),
		0o600); err != nil {
		t.Fatal(err)
	}

	p := New()
	p.clock = fixedClock{t: time.Date(2024, 5, 19, 12, 0, 0, 0, time.UTC)}
	acct := core.AccountConfig{ID: "kilo_code", Provider: "kilo_code", Auth: "local"}
	acct.SetPath("tasks_dir", tasksRoot)

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Fatalf("status = %v (msg=%q), want OK", snap.Status, snap.Message)
	}
	if m, ok := snap.Metrics["total_tasks"]; !ok || m.Used == nil || *m.Used != 1 {
		t.Errorf("total_tasks: ok=%v val=%v, want 1", ok, m.Used)
	}
	if len(snap.ModelUsage) != 1 || snap.ModelUsage[0].RawModelID != "kilo-test-model" {
		t.Errorf("model usage = %v, want [kilo-test-model]", snap.ModelUsage)
	}
}

// TestProvider_Fetch_NoData returns UNKNOWN when neither override nor
// extension data is present.
func TestProvider_Fetch_NoData(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	p := New()
	p.clock = fixedClock{t: time.Date(2025, 5, 18, 12, 0, 0, 0, time.UTC)}
	acct := core.AccountConfig{ID: "kilo_code", Provider: "kilo_code", Auth: "local"}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if snap.Status != core.StatusUnknown {
		t.Errorf("status = %v, want UNKNOWN", snap.Status)
	}
}
