package goose

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

type fixedClock struct{ t time.Time }

func (f fixedClock) Now() time.Time { return f.t }

func TestProvider_BasicMetadata(t *testing.T) {
	p := New()
	if p.ID() != "goose" {
		t.Errorf("ID = %q, want goose", p.ID())
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

func TestProvider_Fetch_MissingDB(t *testing.T) {
	p := New()
	p.clock = fixedClock{t: time.Date(2025, 5, 18, 12, 0, 0, 0, time.UTC)}

	// Account with a non-existent override path; resolveDBPath returns ""
	// and Fetch should produce an OK-ish (StatusUnknown) snapshot with no
	// error rather than crashing the dashboard.
	acct := core.AccountConfig{
		ID:       "goose",
		Provider: "goose",
		Auth:     "local",
	}
	acct.SetPath("db_path", filepath.Join(t.TempDir(), "missing.db"))

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if snap.Status != core.StatusUnknown {
		t.Errorf("status = %v, want %v", snap.Status, core.StatusUnknown)
	}
	if len(snap.Metrics) != 0 {
		t.Errorf("metrics = %v, want empty", snap.Metrics)
	}
	if len(snap.ModelUsage) != 0 {
		t.Errorf("model usage = %v, want empty", snap.ModelUsage)
	}
}

func TestProvider_Fetch_EmptyDB(t *testing.T) {
	opts := schemaOpts{}
	dbPath := makeTempDB(t, opts)

	p := New()
	p.clock = fixedClock{t: time.Date(2025, 5, 18, 12, 0, 0, 0, time.UTC)}
	acct := core.AccountConfig{ID: "goose", Provider: "goose", Auth: "local"}
	acct.SetPath("db_path", dbPath)

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Errorf("status = %v, want OK", snap.Status)
	}
	if got, want := snap.Message, "No Goose sessions recorded"; got != want {
		t.Errorf("message = %q, want %q", got, want)
	}
}

func TestProvider_Fetch_HappyPath(t *testing.T) {
	opts := schemaOpts{}
	dbPath := makeTempDB(t, opts)

	// Two rows for the same model on different days; one with cost, one
	// without — both totals should aggregate, cost should sum.
	insertRow(t, dbPath, opts, rowValues{
		ID:             "20250518_1",
		CreatedAt:      "2025-05-18T10:00:00Z",
		ModelConfigCol: `{"model_name": "claude-opus-4-7"}`,
		ProviderName:   "anthropic",
		AccInput:       1000,
		AccOutput:      500,
		AccTotal:       1600, // reasoning = 100
		AccCost:        0.04,
	})
	insertRow(t, dbPath, opts, rowValues{
		ID:             "20250517_2",
		CreatedAt:      "2025-05-17T15:00:00Z",
		ModelConfigCol: `{"model_name": "claude-opus-4-7"}`,
		ProviderName:   "anthropic",
		AccInput:       2000,
		AccOutput:      1000,
		AccTotal:       3000,
	})
	// A second model.
	insertRow(t, dbPath, opts, rowValues{
		ID:             "20250518_3",
		CreatedAt:      "2025-05-18T11:00:00Z",
		ModelConfigCol: `{"model_name": "gpt-4o"}`,
		ProviderName:   "openai",
		AccInput:       500,
		AccOutput:      250,
		AccTotal:       750,
		AccCost:        0.01,
	})

	p := New()
	p.clock = fixedClock{t: time.Date(2025, 5, 18, 12, 0, 0, 0, time.UTC)}
	acct := core.AccountConfig{ID: "goose", Provider: "goose", Auth: "local"}
	acct.SetPath("db_path", dbPath)

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Fatalf("status = %v, want OK; message=%q", snap.Status, snap.Message)
	}

	expectMetric(t, snap, "total_sessions", 3)
	expectMetric(t, snap, "sessions_today", 2)
	expectMetric(t, snap, "sessions_7d", 3)
	expectMetric(t, snap, "total_tokens", 1600+3000+750)
	expectMetric(t, snap, "total_input_tokens", 1000+2000+500)
	expectMetric(t, snap, "total_output_tokens", 500+1000+250)
	expectMetric(t, snap, "total_reasoning_tokens", 100) // only the first row carries reasoning
	expectMetric(t, snap, "total_cost_usd", 0.05)

	// ModelUsage: 2 distinct models.
	if len(snap.ModelUsage) != 2 {
		t.Fatalf("len(ModelUsage) = %d, want 2", len(snap.ModelUsage))
	}
	byModel := make(map[string]core.ModelUsageRecord)
	for _, rec := range snap.ModelUsage {
		byModel[rec.RawModelID] = rec
	}
	claude, ok := byModel["claude-opus-4-7"]
	if !ok {
		t.Fatal("missing claude-opus-4-7 record")
	}
	if claude.InputTokens == nil || *claude.InputTokens != 3000 {
		t.Errorf("claude input = %v, want 3000", floatPtrValue(claude.InputTokens))
	}
	if claude.TotalTokens == nil || *claude.TotalTokens != 4600 {
		t.Errorf("claude total = %v, want 4600", floatPtrValue(claude.TotalTokens))
	}
	if claude.Requests == nil || *claude.Requests != 2 {
		t.Errorf("claude requests = %v, want 2", floatPtrValue(claude.Requests))
	}
	if claude.RawSource != "sqlite" {
		t.Errorf("claude raw_source = %q, want sqlite", claude.RawSource)
	}
	if claude.Dimensions["upstream_provider"] != "anthropic" {
		t.Errorf("claude upstream_provider = %q, want anthropic",
			claude.Dimensions["upstream_provider"])
	}

	gpt, ok := byModel["gpt-4o"]
	if !ok {
		t.Fatal("missing gpt-4o record")
	}
	if gpt.CostUSD == nil || *gpt.CostUSD != 0.01 {
		t.Errorf("gpt cost = %v, want 0.01", floatPtrValue(gpt.CostUSD))
	}

	// Daily series sanity.
	if len(snap.DailySeries["sessions"]) == 0 {
		t.Error("DailySeries[sessions] is empty")
	}
	if len(snap.DailySeries["tokens"]) == 0 {
		t.Error("DailySeries[tokens] is empty")
	}
}

func TestProvider_Fetch_SkippedRowsDiagnostic(t *testing.T) {
	opts := schemaOpts{}
	dbPath := makeTempDB(t, opts)

	insertRow(t, dbPath, opts, rowValues{
		ID:             "good",
		CreatedAt:      "2025-05-18T10:00:00Z",
		ModelConfigCol: `{"model_name": "claude-opus-4-7"}`,
		ProviderName:   "anthropic",
		AccInput:       1000,
		AccOutput:      500,
		AccTotal:       1500,
	})
	insertRow(t, dbPath, opts, rowValues{
		ID:             "bad_ts",
		CreatedAt:      "definitely not a date",
		ModelConfigCol: `{"model_name": "claude-opus-4-7"}`,
		ProviderName:   "anthropic",
		AccInput:       100,
		AccOutput:      50,
		AccTotal:       150,
	})

	p := New()
	p.clock = fixedClock{t: time.Date(2025, 5, 18, 12, 0, 0, 0, time.UTC)}
	acct := core.AccountConfig{ID: "goose", Provider: "goose", Auth: "local"}
	acct.SetPath("db_path", dbPath)

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Fatalf("status = %v, want OK; message=%q", snap.Status, snap.Message)
	}
	if got := snap.Diagnostics["skipped_rows"]; got != "1" {
		t.Errorf("skipped_rows diagnostic = %q, want %q", got, "1")
	}
	if m, ok := snap.Metrics["total_sessions"]; !ok || m.Used == nil || *m.Used != 1 {
		got := -1.0
		if ok && m.Used != nil {
			got = *m.Used
		}
		t.Errorf("total_sessions = %v, want 1", got)
	}
}

func TestProvider_HasChanged(t *testing.T) {
	opts := schemaOpts{}
	dbPath := makeTempDB(t, opts)

	p := New()
	acct := core.AccountConfig{ID: "goose", Provider: "goose", Auth: "local"}
	acct.SetPath("db_path", dbPath)

	// A long-ago "since" should report changed=true; the file mtime is now.
	changed, err := p.HasChanged(acct, time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("HasChanged: %v", err)
	}
	if !changed {
		t.Error("changed=false, want true (since=2000)")
	}

	// A "since" in the far future should report not-changed.
	changed, err = p.HasChanged(acct, time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("HasChanged: %v", err)
	}
	if changed {
		t.Error("changed=true, want false (since=future)")
	}
}

func TestProvider_HasChanged_NoDB(t *testing.T) {
	p := New()
	acct := core.AccountConfig{ID: "goose", Provider: "goose", Auth: "local"}
	// No db_path hint; resolveDBPath returns "" and HasChanged returns false.
	changed, err := p.HasChanged(acct, time.Now())
	if err != nil {
		t.Fatalf("HasChanged: %v", err)
	}
	if changed {
		t.Error("changed=true, want false (no DB resolved)")
	}
}

func expectMetric(t *testing.T, snap core.UsageSnapshot, key string, want float64) {
	t.Helper()
	m, ok := snap.Metrics[key]
	if !ok {
		t.Errorf("missing metric %s", key)
		return
	}
	if m.Used == nil {
		t.Errorf("metric %s has nil Used", key)
		return
	}
	if *m.Used != want {
		t.Errorf("metric %s = %v, want %v", key, *m.Used, want)
	}
}

func floatPtrValue(p *float64) float64 {
	if p == nil {
		return -1
	}
	return *p
}
