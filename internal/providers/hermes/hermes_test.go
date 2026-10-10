package hermes

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/janekbaraniewski/openusage/internal/core"
)

type fixedClock struct{ t time.Time }

func (f fixedClock) Now() time.Time { return f.t }

// makeTempDB creates a minimal sessions table mirroring the upstream Hermes
// schema (subset of columns the provider actually reads).
func makeTempDB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.db")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	ddl := `CREATE TABLE sessions (
		id TEXT PRIMARY KEY,
		model TEXT,
		billing_provider TEXT,
		started_at REAL,
		message_count INTEGER,
		input_tokens INTEGER,
		output_tokens INTEGER,
		cache_read_tokens INTEGER,
		cache_write_tokens INTEGER,
		reasoning_tokens INTEGER,
		estimated_cost_usd REAL,
		actual_cost_usd REAL
	)`
	if _, err := db.Exec(ddl); err != nil {
		t.Fatalf("ddl: %v", err)
	}
	return dbPath
}

type hermesRow struct {
	ID            string
	Model         string
	Provider      string
	StartedAt     float64 // seconds since epoch
	MessageCount  int
	Input         int64
	Output        int64
	CacheRead     int64
	CacheWrite    int64
	Reasoning     int64
	EstimatedCost float64
	ActualCost    float64
}

func insertRow(t *testing.T, dbPath string, r hermesRow) {
	t.Helper()
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open insert: %v", err)
	}
	defer db.Close()
	_, err = db.Exec(
		`INSERT INTO sessions (id, model, billing_provider, started_at,
			message_count, input_tokens, output_tokens, cache_read_tokens,
			cache_write_tokens, reasoning_tokens, estimated_cost_usd, actual_cost_usd)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.Model, r.Provider, r.StartedAt,
		r.MessageCount, r.Input, r.Output, r.CacheRead,
		r.CacheWrite, r.Reasoning, nullableFloat(r.EstimatedCost), nullableFloat(r.ActualCost),
	)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
}

func nullableFloat(v float64) any {
	if v == 0 {
		return nil
	}
	return v
}

func TestProvider_BasicMetadata(t *testing.T) {
	p := New()
	if p.ID() != ID {
		t.Errorf("ID = %q, want %q", p.ID(), ID)
	}
	info := p.Describe()
	if info.Name == "" {
		t.Error("Describe().Name is empty")
	}
	if p.Spec().Auth.Type != core.ProviderAuthTypeLocal {
		t.Errorf("auth type = %v, want local", p.Spec().Auth.Type)
	}
	if p.DashboardWidget().IsZero() {
		t.Error("DashboardWidget is zero")
	}
}

// isolateHermesHome points every default Hermes location at an empty temp
// dir so resolveDBPath cannot fall back to a real ~/.hermes/state.db on
// machines where Hermes is installed.
func isolateHermesHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("HERMES_HOME", "")
}

func TestProvider_Fetch_MissingDB(t *testing.T) {
	isolateHermesHome(t)
	p := New()
	p.clock = fixedClock{t: time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)}
	acct := core.AccountConfig{ID: "hermes", Provider: "hermes", Auth: "local"}
	acct.SetPath("db_path", filepath.Join(t.TempDir(), "missing.db"))

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if snap.Status != core.StatusUnknown {
		t.Errorf("status = %v, want %v", snap.Status, core.StatusUnknown)
	}
	if len(snap.Metrics) != 0 {
		t.Errorf("metrics non-empty: %v", snap.Metrics)
	}
}

func TestProvider_Fetch_HappyPath(t *testing.T) {
	dbPath := makeTempDB(t)

	// started_at = seconds since epoch for 2026-05-18 10:00 UTC.
	day1 := float64(time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC).Unix())
	day2 := float64(time.Date(2026, 5, 17, 15, 0, 0, 0, time.UTC).Unix())

	insertRow(t, dbPath, hermesRow{
		ID: "s1", Model: "claude-opus-4-7", Provider: "anthropic",
		StartedAt: day1, MessageCount: 4,
		Input: 1000, Output: 500, Reasoning: 100, ActualCost: 0.04,
	})
	insertRow(t, dbPath, hermesRow{
		ID: "s2", Model: "claude-opus-4-7", Provider: "anthropic",
		StartedAt: day2, MessageCount: 2,
		Input: 2000, Output: 1000, EstimatedCost: 0.07,
	})
	insertRow(t, dbPath, hermesRow{
		ID: "s3", Model: "gpt-4o", Provider: "openai",
		StartedAt: day1, MessageCount: 3,
		Input: 500, Output: 250, ActualCost: 0.01,
	})

	p := New()
	p.clock = fixedClock{t: time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)}
	acct := core.AccountConfig{ID: "hermes", Provider: "hermes", Auth: "local"}
	acct.SetPath("db_path", dbPath)

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Fatalf("status = %v want OK; msg=%q", snap.Status, snap.Message)
	}

	expect := func(key string, want float64) {
		t.Helper()
		m, ok := snap.Metrics[key]
		if !ok {
			t.Errorf("missing metric %s", key)
			return
		}
		if m.Used == nil || *m.Used != want {
			got := -1.0
			if m.Used != nil {
				got = *m.Used
			}
			t.Errorf("metric %s = %v, want %v", key, got, want)
		}
	}

	expect("total_sessions", 3)
	expect("sessions_today", 2)
	expect("total_input_tokens", 3500)
	expect("total_output_tokens", 1750)
	expect("total_reasoning_tokens", 100)
	// total_cost: actual 0.04 + estimated 0.07 + actual 0.01 = 0.12
	if m := snap.Metrics["total_cost_usd"]; m.Used == nil {
		t.Error("missing total_cost_usd")
	} else if diff := *m.Used - 0.12; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("total_cost_usd = %v, want ~0.12", *m.Used)
	}

	if len(snap.ModelUsage) != 2 {
		t.Fatalf("len(ModelUsage) = %d, want 2", len(snap.ModelUsage))
	}
	byModel := map[string]core.ModelUsageRecord{}
	for _, rec := range snap.ModelUsage {
		byModel[rec.RawModelID] = rec
	}
	claude := byModel["claude-opus-4-7"]
	if claude.Requests == nil || *claude.Requests != 2 {
		t.Errorf("claude requests = %v, want 2", claude.Requests)
	}
	if claude.Dimensions["upstream_provider"] != "anthropic" {
		t.Errorf("claude upstream_provider = %q, want anthropic", claude.Dimensions["upstream_provider"])
	}
	if claude.RawSource != "sqlite" {
		t.Errorf("claude raw_source = %q, want sqlite", claude.RawSource)
	}
}

func TestProvider_Fetch_EmptyDB(t *testing.T) {
	dbPath := makeTempDB(t)
	p := New()
	p.clock = fixedClock{t: time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)}
	acct := core.AccountConfig{ID: "hermes", Provider: "hermes", Auth: "local"}
	acct.SetPath("db_path", dbPath)

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Errorf("status = %v want OK", snap.Status)
	}
	if got, want := snap.Message, "No Hermes sessions recorded"; got != want {
		t.Errorf("message = %q want %q", got, want)
	}
}

func TestQueryHermesSessions_ZeroTokensFiltered(t *testing.T) {
	dbPath := makeTempDB(t)
	day := float64(time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC).Unix())

	// All-zero row, no cost: filtered out.
	insertRow(t, dbPath, hermesRow{
		ID: "z", Model: "noop", Provider: "x", StartedAt: day,
	})
	// Cost-only row: kept.
	insertRow(t, dbPath, hermesRow{
		ID: "c", Model: "costonly", Provider: "x", StartedAt: day, ActualCost: 0.005,
	})
	// Empty-model row: filtered.
	insertRow(t, dbPath, hermesRow{
		ID: "e", Model: "", Provider: "x", StartedAt: day, Input: 100,
	})

	sessions, err := queryHermesSessions(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1 (cost-only)", len(sessions))
	}
	if sessions[0].ID != "c" {
		t.Errorf("survivor = %q, want %q", sessions[0].ID, "c")
	}
}

func TestConvertStartedAt_SecondsAndMillis(t *testing.T) {
	// 1.7e9 seconds → 2023-11-15 UTC-ish
	secs := sql.NullFloat64{Float64: 1700000000.5, Valid: true}
	ts, ok := convertStartedAt(secs)
	if !ok {
		t.Fatal("seconds path returned !ok")
	}
	if y := ts.Year(); y < 2023 || y > 2026 {
		t.Errorf("seconds path produced unexpected year %d", y)
	}

	// 1.7e12 ms → same UNIX time.
	ms := sql.NullFloat64{Float64: 1700000000500, Valid: true}
	ts2, ok := convertStartedAt(ms)
	if !ok {
		t.Fatal("ms path returned !ok")
	}
	if delta := ts2.Sub(ts); delta > time.Second || delta < -time.Second {
		t.Errorf("seconds vs ms mismatch: %v", delta)
	}

	// Invalid input.
	if _, ok := convertStartedAt(sql.NullFloat64{}); ok {
		t.Error("invalid null returned ok=true")
	}
	if _, ok := convertStartedAt(sql.NullFloat64{Float64: 0, Valid: true}); ok {
		t.Error("zero returned ok=true")
	}
}

// Sanity check that the upstream-style query handles many rows without
// blowing up; ensures the column-probing path stays warm.
func TestQueryHermesSessions_ManyRows(t *testing.T) {
	dbPath := makeTempDB(t)
	day := float64(time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC).Unix())
	for i := 0; i < 50; i++ {
		insertRow(t, dbPath, hermesRow{
			ID: fmt.Sprintf("s%d", i), Model: "m", Provider: "p",
			StartedAt: day + float64(i), Input: int64(i + 1),
		})
	}
	sessions, err := queryHermesSessions(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(sessions) != 50 {
		t.Fatalf("got %d sessions, want 50", len(sessions))
	}
}
