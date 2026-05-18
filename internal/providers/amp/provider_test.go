package amp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestProviderID(t *testing.T) {
	p := New()
	if p.ID() != "amp" {
		t.Errorf("expected ID 'amp', got %q", p.ID())
	}
}

func TestDescribe(t *testing.T) {
	p := New()
	info := p.Describe()
	if info.Name != "Amp" {
		t.Errorf("expected name 'Amp', got %q", info.Name)
	}
	if len(info.Capabilities) == 0 {
		t.Error("expected at least one capability")
	}
}

func TestSpecAuthIsLocal(t *testing.T) {
	p := New()
	spec := p.Spec()
	if spec.Auth.Type != core.ProviderAuthTypeLocal {
		t.Errorf("expected auth type 'local', got %q", spec.Auth.Type)
	}
}

func TestDashboardWidgetWiresThroughCodingToolDefaults(t *testing.T) {
	w := dashboardWidget()
	if !w.ShowClientComposition {
		t.Error("expected ShowClientComposition=true via CodingToolDashboard")
	}
	if !w.ShowLanguageComposition {
		t.Error("expected ShowLanguageComposition=true via CodingToolDashboard")
	}
	if !w.ShowCodeStatsComposition {
		t.Error("expected ShowCodeStatsComposition=true via CodingToolDashboard")
	}
	if len(w.CompactRows) < 3 {
		t.Errorf("expected >= 3 compact rows, got %d", len(w.CompactRows))
	}
}

func TestDetailWidgetIsCodingToolWithoutMCP(t *testing.T) {
	p := New()
	w := p.DetailWidget()
	for _, s := range w.Sections {
		if s.Name == "MCP Usage" {
			t.Errorf("expected no MCP section, got %+v", s)
		}
	}
}

// TestFetchMissingDataDir verifies the graceful-degradation contract: when
// Amp's data directory cannot be found, Fetch must return a non-error
// snapshot with StatusUnknown.
func TestFetchMissingDataDir(t *testing.T) {
	tmp := t.TempDir()
	// Point the provider at a path that does not exist.
	acct := core.AccountConfig{
		ID:       "amp-test",
		Provider: "amp",
		Auth:     "local",
		Binary:   filepath.Join(tmp, "no-such-dir"),
	}

	p := New()
	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if snap.Status != core.StatusUnknown {
		t.Errorf("expected StatusUnknown for missing data dir, got %q", snap.Status)
	}
	if snap.Message == "" {
		t.Error("expected human-readable message")
	}
}

func TestFetchEmptyThreadsDir(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "threads"), 0o755); err != nil {
		t.Fatal(err)
	}
	acct := core.AccountConfig{
		ID:       "amp-test",
		Provider: "amp",
		Auth:     "local",
		Binary:   tmp,
	}
	snap, err := New().Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if snap.Status != core.StatusUnknown {
		t.Errorf("expected StatusUnknown for empty threads dir, got %q", snap.Status)
	}
}

func TestFetchHappyPathWithLedgerReconciliation(t *testing.T) {
	tmp := t.TempDir()
	threadsDir := filepath.Join(tmp, "threads")
	if err := os.MkdirAll(threadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Copy fixtures into the synthetic data dir.
	if err := copyFile(filepath.Join("testdata", "thread_basic.json"), filepath.Join(threadsDir, "thread-basic-001.json")); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(filepath.Join("testdata", "thread_camelcase.json"), filepath.Join(threadsDir, "thread-cc-001.json")); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(filepath.Join("testdata", "ledger_basic.jsonl"), filepath.Join(tmp, "ledger.jsonl")); err != nil {
		t.Fatal(err)
	}

	acct := core.AccountConfig{
		ID:       "amp-test",
		Provider: "amp",
		Auth:     "local",
		Binary:   tmp,
	}
	snap, err := New().Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Fatalf("expected StatusOK, got %q (msg: %s diag: %v)", snap.Status, snap.Message, snap.Diagnostics)
	}

	// Cost should equal ledger credits for the 2 matched messages + 1 orphan
	// ledger row = 0.025 + 0.031 + 0.011 = 0.067.
	costMetric, ok := snap.Metrics["total_cost"]
	if !ok || costMetric.Used == nil {
		t.Fatal("expected total_cost metric")
	}
	if want := 0.067; absDiff(*costMetric.Used, want) > 1e-9 {
		t.Errorf("expected total_cost %v, got %v", want, *costMetric.Used)
	}

	// 2 matched (per-field max) input tokens: 1200 + 1500 from thread side
	// (ledger tokens for asst-1 were 1100, so max wins at 1200). Plus orphan
	// 300 input. Plus the camelCase thread's 500.
	inputMetric, ok := snap.Metrics["total_input_tokens"]
	if !ok || inputMetric.Used == nil {
		t.Fatal("expected total_input_tokens metric")
	}
	if want := float64(1200 + 1500 + 300 + 500); absDiff(*inputMetric.Used, want) > 1e-9 {
		t.Errorf("expected total_input_tokens %v, got %v", want, *inputMetric.Used)
	}

	// Sessions = distinct thread ids = 2 (thread-basic + thread-cc). The
	// orphan ledger event has no ThreadID, so it does not bump the count.
	sessions, ok := snap.Metrics["total_sessions"]
	if !ok || sessions.Used == nil || *sessions.Used != 2 {
		t.Errorf("expected total_sessions=2, got %+v", sessions)
	}

	// Per-model breakdown: 1 row per model present.
	if len(snap.ModelUsage) == 0 {
		t.Error("expected at least one ModelUsage record")
	}

	if snap.Raw["data_dir"] != tmp {
		t.Errorf("expected data_dir=%q raw, got %q", tmp, snap.Raw["data_dir"])
	}
	if snap.Raw["thread_count"] != "2" {
		t.Errorf("expected thread_count=2, got %q", snap.Raw["thread_count"])
	}

	// Malformed ledger lines should surface as a non-fatal diagnostic.
	if _, ok := snap.Diagnostics["amp_ledger_skipped_lines"]; !ok {
		t.Error("expected amp_ledger_skipped_lines diagnostic from malformed/keyless lines")
	}
}

func TestFetchPrefersProviderPathsOverBinary(t *testing.T) {
	tmp := t.TempDir()
	customThreads := filepath.Join(tmp, "custom-threads")
	if err := os.MkdirAll(customThreads, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(filepath.Join("testdata", "thread_basic.json"), filepath.Join(customThreads, "t.json")); err != nil {
		t.Fatal(err)
	}

	acct := core.AccountConfig{
		ID:       "amp-test",
		Provider: "amp",
		Auth:     "local",
		Binary:   filepath.Join(tmp, "ignored"),
	}
	acct.SetPath("data_dir", tmp)
	acct.SetPath("threads_dir", customThreads)

	snap, err := New().Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Fatalf("expected StatusOK, got %q (%s)", snap.Status, snap.Message)
	}
	if snap.Raw["threads_dir"] != customThreads {
		t.Errorf("expected threads_dir=%q, got %q", customThreads, snap.Raw["threads_dir"])
	}
}

func TestHasChangedRespondsToMTime(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "threads"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(filepath.Join("testdata", "thread_basic.json"), filepath.Join(tmp, "threads", "t.json")); err != nil {
		t.Fatal(err)
	}
	acct := core.AccountConfig{
		ID:       "amp-test",
		Provider: "amp",
		Auth:     "local",
		Binary:   tmp,
	}
	p := New()
	// Far-future "since" -> nothing has changed.
	since := mustParseTime("2099-01-01T00:00:00Z")
	changed, err := p.HasChanged(acct, since)
	if err != nil {
		t.Fatalf("HasChanged error: %v", err)
	}
	if changed {
		t.Error("expected changed=false for far-future since")
	}

	// Far-past "since" -> threads dir mtime is newer.
	since = mustParseTime("1990-01-01T00:00:00Z")
	changed, err = p.HasChanged(acct, since)
	if err != nil {
		t.Fatalf("HasChanged error: %v", err)
	}
	if !changed {
		t.Error("expected changed=true for far-past since")
	}
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

func absDiff(a, b float64) float64 {
	if a > b {
		return a - b
	}
	return b - a
}
