package muse_code

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/pricing"
)

type fixedClock struct{ t time.Time }

func (f fixedClock) Now() time.Time { return f.t }

func testRecord(t *testing.T, ts time.Time, model string) []byte {
	t.Helper()
	micros := ts.UnixMicro()
	return []byte(fmt.Sprintf(
		`{"schema_version":1,"id":"r1","stream":{"kind":"session","id":"s1"},"sequence":1,`+
			`"recorded_at":%d,"record_type":"event","payload_type":"runtime.session",`+
			`"payload":{"kind":"run","event":{"kind":"model_completed",`+
			`"usage":{"input_tokens":10000,"output_tokens":500,"cached_tokens":8000,`+
			`"cache_read_tokens":8000,"cache_write_tokens":0,"reasoning_tokens":100},`+
			`"duration_ms":100,"model":%q}}}`,
		micros, model))
}

func writeSession(t *testing.T, root, rel string, records ...[]byte) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	var data []byte
	for i, rec := range records {
		if i > 0 {
			data = append(data, '\n')
		}
		data = append(data, rec...)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func wrappedRecord(t *testing.T, inner []byte) []byte {
	t.Helper()
	obj := map[string]any{
		"retained_frame": "x",
		"children": []map[string]any{
			{"child_index": 0, "record_json": string(inner)},
		},
	}
	data, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("marshal wrapper: %v", err)
	}
	return data
}

func stubPricing(t *testing.T) {
	t.Helper()
	old := priceLookup
	priceLookup = func(_ context.Context, model string) (*pricing.Price, error) {
		switch model {
		case "muse-spark-1.3":
			return &pricing.Price{
				ModelID:                  "muse-spark-1.3",
				InputCostPerMillion:      1.25,
				OutputCostPerMillion:     4.25,
				CacheReadCostPerMillion:  0.15,
				CacheWriteCostPerMillion: 1.25,
			}, nil
		case "muse-spark-1.3-contributor":
			return &pricing.Price{
				ModelID:              "muse-spark-1.3-contributor",
				InputCostPerMillion:  0.1,
				OutputCostPerMillion: 0.2,
			}, nil
		default:
			return nil, fmt.Errorf("no price for %s", model)
		}
	}
	t.Cleanup(func() { priceLookup = old })
}

func TestProvider_BasicMetadata(t *testing.T) {
	p := New()
	if p.ID() != ID || ID != "muse_code" {
		t.Errorf("ID = %q, want muse_code", p.ID())
	}
	if p.Spec().Info.Name != "Muse Code" {
		t.Errorf("name = %q, want Muse Code", p.Spec().Info.Name)
	}
	if p.Spec().Auth.Type != core.ProviderAuthTypeLocal {
		t.Errorf("auth type = %v, want local", p.Spec().Auth.Type)
	}
	if p.Spec().Info.DocURL == "" {
		t.Error("DocURL is empty")
	}
	if p.DashboardWidget().IsZero() {
		t.Error("DashboardWidget is zero")
	}
}

func TestProvider_Fetch_AuthRequired(t *testing.T) {
	t.Setenv("META_API_KEY", "")
	t.Setenv("MUSE_AUTH_PATH", filepath.Join(t.TempDir(), "missing-auth.json"))
	// Isolate from real ~/.config/openusage/muse.json which now also counts as
	// a credential for quota (muse.json file-only quota). Without this, a
	// developer with a saved key would make this "no credential" test pass
	// incorrectly as quota-capable.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	p := New()
	acct := core.AccountConfig{ID: "muse-code", Provider: "muse_code", Auth: "local"}
	acct.SetPath("sessions_dir", filepath.Join(t.TempDir(), "missing"))

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if snap.Status != core.StatusAuth {
		t.Errorf("status = %v, want auth-required", snap.Status)
	}
}

func TestProvider_Fetch_HappyPath(t *testing.T) {
	stubPricing(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("MUSE_AUTH_PATH", filepath.Join(t.TempDir(), "missing.json"))
	t.Setenv("META_API_KEY", "")
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	root := t.TempDir()
	writeSession(t, root, "2026/09/07/s1/session.jsonl", testRecord(t, now, "muse-spark-1.3"))

	p := New()
	p.clock = fixedClock{t: now}
	acct := core.AccountConfig{ID: "muse-code", Provider: "muse_code", Auth: "local"}
	acct.SetPath("sessions_dir", root)

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Fatalf("status = %v, message = %q", snap.Status, snap.Message)
	}
	used := func(key string) float64 {
		m, ok := snap.Metrics[key]
		if !ok || m.Used == nil {
			t.Fatalf("metric %q missing", key)
		}
		return *m.Used
	}
	if got := used("total_tokens"); got != 10600 {
		t.Errorf("total_tokens = %v, want 10600", got)
	}
	// (2000 x 1.25 + 600 x 4.25 + 8000 x 0.15) / 1e6.
	if got := used("total_cost_usd"); got < 0.006249 || got > 0.006251 {
		t.Errorf("total_cost_usd = %v, want 0.00625", got)
	}
	if got := used("total_sessions"); got != 1 {
		t.Errorf("total_sessions = %v, want 1", got)
	}
	if len(snap.ModelUsage) != 1 || snap.ModelUsage[0].RawModelID != "muse-spark-1.3" {
		t.Errorf("model usage = %+v, want one muse-spark-1.3 record", snap.ModelUsage)
	}
}

func TestProvider_Fetch_SkipsSubagentTranscripts(t *testing.T) {
	stubPricing(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("MUSE_AUTH_PATH", filepath.Join(t.TempDir(), "missing.json"))
	t.Setenv("META_API_KEY", "")
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	root := t.TempDir()
	rec := testRecord(t, now, "muse-spark-1.3")
	writeSession(t, root, "2026/09/07/s1/session.jsonl", rec)
	writeSession(t, root, "2026/09/07/s1/subagent/child/session.jsonl", rec)

	p := New()
	p.clock = fixedClock{t: now}
	acct := core.AccountConfig{ID: "muse-code", Provider: "muse_code", Auth: "local"}
	acct.SetPath("sessions_dir", root)

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	m := snap.Metrics["total_tokens"]
	if m.Used == nil || *m.Used != 10600 {
		t.Errorf("total_tokens = %v, want single-counted 10600", *m.Used)
	}
}

func TestProvider_Fetch_Sessions7dCountsDistinctSessions(t *testing.T) {
	stubPricing(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("MUSE_AUTH_PATH", filepath.Join(t.TempDir(), "missing.json"))
	t.Setenv("META_API_KEY", "")
	day1 := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	root := t.TempDir()
	// Same session active on two days counts once in the 7d window.
	writeSession(t, root, "2026/09/06/s1/session.jsonl", testRecord(t, day1, "muse-spark-1.3"))
	writeSession(t, root, "2026/09/07/s1/session.jsonl", testRecord(t, day2, "muse-spark-1.3"))

	p := New()
	p.clock = fixedClock{t: day2}
	acct := core.AccountConfig{ID: "muse-code", Provider: "muse_code", Auth: "local"}
	acct.SetPath("sessions_dir", root)

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if m := snap.Metrics["sessions_7d"]; m.Used == nil || *m.Used != 1 {
		t.Errorf("sessions_7d = %v, want distinct 1", *m.Used)
	}
}

func TestProvider_Fetch_UnpricedModelKeepsTokensOmitsCost(t *testing.T) {
	stubPricing(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("MUSE_AUTH_PATH", filepath.Join(t.TempDir(), "missing.json"))
	t.Setenv("META_API_KEY", "")
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	root := t.TempDir()
	writeSession(t, root, "2026/09/07/s1/session.jsonl", testRecord(t, now, "muse-spark-9.9"))

	p := New()
	p.clock = fixedClock{t: now}
	acct := core.AccountConfig{ID: "muse-code", Provider: "muse_code", Auth: "local"}
	acct.SetPath("sessions_dir", root)

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if m := snap.Metrics["total_tokens"]; m.Used == nil || *m.Used != 10600 {
		t.Errorf("total_tokens = %v, want counted 10600", *m.Used)
	}
	if _, ok := snap.Metrics["total_cost_usd"]; ok {
		t.Error("total_cost_usd present for unpriced model, want omitted")
	}
}
