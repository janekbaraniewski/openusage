package dsh

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func testAccount(sessionsDir string) core.AccountConfig {
	acct := core.AccountConfig{ID: "dsh", Provider: "dsh", Auth: "local"}
	acct.SetPath(PathHintSessionsDirKey, sessionsDir)
	return acct
}

// writeFixtureSessions lays out three sessions: two with usage across three
// routes and two days, one without any model response.
func writeFixtureSessions(t *testing.T, root string) {
	t.Helper()
	writeSession(t, root, "proj-alpha", "sess-1",
		headerLine("sess-1", fixtureDay1, "/work/alpha", false),
		contextLine(1, fixtureDay1, "deepseek-official", "deepseek-flash"),
		messageLine(2, fixtureDay1+1000, "m1", 0, usageJSON(100, 50, 1000)),
		toolCallLine(3, fixtureDay1+1100, "call-1", "read", `{"file_path":"/work/alpha/main.go"}`),
		toolResultLine(4, fixtureDay1+1200, "call-1", false),
		messageLine(5, fixtureDay2+1000, "m2", 1, usageJSON(200, 100, 2000)),
	)
	writeSession(t, root, "proj-beta", "session-2",
		headerLine("session-2", fixtureDay2, "/work/beta", false),
		contextLine(1, fixtureDay2, "commandcode", "deepseek/deepseek-v4.1-flash"),
		messageLine(2, fixtureDay2+1000, "m3", 0,
			`{"inputTokens":400,"outputTokens":200,"cacheReadTokens":0,"reasoningTokens":80,"totalTokens":600}`),
		toolCallLine(3, fixtureDay2+1100, "call-2", "bash", `{"command":"true"}`),
		toolResultLine(4, fixtureDay2+1200, "call-2", true),
		contextLine(5, fixtureDay2+2000, "zai", "glm-5.3"),
		messageLine(6, fixtureDay2+3000, "m4", 1, usageJSON(10, 5, 0)),
	)
	writeSession(t, root, "proj-beta", "session-3",
		headerLine("session-3", fixtureDay2, "/work/beta", false),
		contextLine(1, fixtureDay2, "zai", "glm-5.3"),
	)
}

func metricUsed(t *testing.T, snap core.UsageSnapshot, key string) float64 {
	t.Helper()
	m, ok := snap.Metrics[key]
	if !ok || m.Used == nil {
		t.Fatalf("metric %q missing", key)
	}
	return *m.Used
}

func TestProvider_BasicMetadata(t *testing.T) {
	p := New()
	if p.ID() != ID {
		t.Errorf("ID = %q, want %q", p.ID(), ID)
	}
	if p.Spec().Auth.Type != core.ProviderAuthTypeLocal {
		t.Errorf("auth type = %v, want local", p.Spec().Auth.Type)
	}
	if p.Spec().Info.Name != "dsh" {
		t.Errorf("name = %q, want %q", p.Spec().Info.Name, "dsh")
	}
	if p.DashboardWidget().IsZero() {
		t.Error("DashboardWidget is zero")
	}
}

func TestProvider_Fetch_MissingDir(t *testing.T) {
	setHome(t, t.TempDir())
	p := New()

	snap, err := p.Fetch(context.Background(), testAccount(filepath.Join(t.TempDir(), "missing")))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if snap.Status != core.StatusUnknown {
		t.Errorf("status = %v want UNKNOWN", snap.Status)
	}
	if len(snap.Metrics) != 0 {
		t.Errorf("metrics non-empty: %v", snap.Metrics)
	}
}

func TestProvider_Fetch_EmptyDir(t *testing.T) {
	setHome(t, t.TempDir())
	p := New()

	snap, err := p.Fetch(context.Background(), testAccount(t.TempDir()))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Errorf("status = %v want OK", snap.Status)
	}
	if snap.Message != "No dsh sessions recorded" {
		t.Errorf("message = %q", snap.Message)
	}
}

func TestProvider_Fetch_HappyPath(t *testing.T) {
	setHome(t, t.TempDir())
	root := t.TempDir()
	writeFixtureSessions(t, root)

	p := New()
	p.clock = fixedClock{t: time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)}

	snap, err := p.Fetch(context.Background(), testAccount(root))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Fatalf("status = %v want OK", snap.Status)
	}
	if snap.Raw["sessions_dir"] != root {
		t.Errorf("Raw[sessions_dir] = %q, want %q", snap.Raw["sessions_dir"], root)
	}

	want := map[string]float64{
		// session-3 has no model response and is not counted.
		"total_sessions":         2,
		"sessions_today":         2,
		"sessions_7d":            2,
		"total_messages":         4,
		"total_tool_calls":       2,
		"total_input_tokens":     710,
		"total_output_tokens":    355,
		"total_tokens":           1065,
		"total_cache_read":       3000,
		"total_reasoning_tokens": 80,
	}
	for key, wantValue := range want {
		if got := metricUsed(t, snap, key); got != wantValue {
			t.Errorf("%s = %v, want %v", key, got, wantValue)
		}
	}
	if _, ok := snap.Metrics["total_cache_write"]; ok {
		t.Error("total_cache_write should be absent while zero")
	}
	if snap.Message != "2 sessions, 4 messages, 1.1K tokens" {
		t.Errorf("message = %q", snap.Message)
	}

	wantSeries := map[string][]core.TimePoint{
		"sessions": {{Date: "2026-05-17", Value: 1}, {Date: "2026-05-18", Value: 2}},
		"messages": {{Date: "2026-05-17", Value: 1}, {Date: "2026-05-18", Value: 3}},
		"tokens":   {{Date: "2026-05-17", Value: 150}, {Date: "2026-05-18", Value: 915}},
	}
	for key, wantPoints := range wantSeries {
		got := snap.DailySeries[key]
		if len(got) != len(wantPoints) {
			t.Errorf("series %s = %v, want %v", key, got, wantPoints)
			continue
		}
		for i := range wantPoints {
			if got[i] != wantPoints[i] {
				t.Errorf("series %s[%d] = %v, want %v", key, i, got[i], wantPoints[i])
			}
		}
	}

	type modelWant struct {
		upstream string
		input    float64
		output   float64
		cached   float64
		total    float64
		requests float64
	}
	wantModels := map[string]modelWant{
		"deepseek-flash":               {upstream: "deepseek", input: 300, output: 150, cached: 3000, total: 3450, requests: 2},
		"deepseek/deepseek-v4.1-flash": {upstream: "command_code", input: 400, output: 200, cached: 0, total: 600, requests: 1},
		"glm-5.3":                      {upstream: "zai", input: 10, output: 5, cached: 0, total: 15, requests: 1},
	}
	if len(snap.ModelUsage) != len(wantModels) {
		t.Fatalf("len(ModelUsage) = %d, want %d", len(snap.ModelUsage), len(wantModels))
	}
	for _, rec := range snap.ModelUsage {
		w, ok := wantModels[rec.RawModelID]
		if !ok {
			t.Errorf("unexpected model %q", rec.RawModelID)
			continue
		}
		if got := rec.Dimensions["upstream_provider"]; got != w.upstream {
			t.Errorf("%s upstream_provider = %q, want %q", rec.RawModelID, got, w.upstream)
		}
		if *rec.InputTokens != w.input || *rec.OutputTokens != w.output || *rec.CachedTokens != w.cached ||
			*rec.TotalTokens != w.total || *rec.Requests != w.requests {
			t.Errorf("%s = (%v,%v,%v,%v,%v), want %+v", rec.RawModelID,
				*rec.InputTokens, *rec.OutputTokens, *rec.CachedTokens, *rec.TotalTokens, *rec.Requests, w)
		}
	}
}

func TestProvider_Fetch_DefaultsToDshHome(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	writeFixtureSessions(t, filepath.Join(home, ".dsh", "sessions"))

	snap, err := New().Fetch(context.Background(), core.AccountConfig{ID: "dsh", Provider: "dsh"})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got := metricUsed(t, snap, "total_sessions"); got != 2 {
		t.Errorf("total_sessions = %v, want 2", got)
	}
}

func TestProvider_HasChanged(t *testing.T) {
	setHome(t, t.TempDir())
	root := t.TempDir()
	path := writeSession(t, root, "proj", "sess-1",
		headerLine("sess-1", fixtureDay1, "/work/alpha", false),
	)
	p := New()
	acct := testAccount(root)

	old := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	for _, dir := range []string{root, filepath.Join(root, "proj"), filepath.Join(root, "proj", "sess-1")} {
		if err := os.Chtimes(dir, old, old); err != nil {
			t.Fatalf("chtimes: %v", err)
		}
	}
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	changed, err := p.HasChanged(acct, old.Add(time.Hour))
	if err != nil || changed {
		t.Errorf("HasChanged = (%v, %v), want (false, nil)", changed, err)
	}

	// An append touches only the log, not the directories above it.
	appended := old.Add(2 * time.Hour)
	if err := os.Chtimes(path, appended, appended); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	changed, err = p.HasChanged(acct, old.Add(time.Hour))
	if err != nil || !changed {
		t.Errorf("HasChanged after append = (%v, %v), want (true, nil)", changed, err)
	}

	changed, err = p.HasChanged(testAccount(filepath.Join(t.TempDir(), "missing")), old)
	if err != nil || changed {
		t.Errorf("HasChanged for missing dir = (%v, %v), want (false, nil)", changed, err)
	}
}

func TestResolveSessionsDir(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)

	if got := resolveSessionsDir(core.AccountConfig{}); got != "" {
		t.Errorf("no install: got %q, want \"\"", got)
	}

	def := filepath.Join(home, ".dsh", "sessions")
	if err := os.MkdirAll(def, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if got := resolveSessionsDir(core.AccountConfig{}); got != def {
		t.Errorf("default: got %q, want %q", got, def)
	}

	relocated := filepath.Join(t.TempDir(), "dsh-home")
	if err := os.MkdirAll(filepath.Join(relocated, "sessions"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Setenv(homeEnvVar, relocated)
	if got := resolveSessionsDir(core.AccountConfig{}); got != filepath.Join(relocated, "sessions") {
		t.Errorf("DSH_HOME: got %q, want %q", got, filepath.Join(relocated, "sessions"))
	}

	override := t.TempDir()
	if got := resolveSessionsDir(testAccount(override)); got != override {
		t.Errorf("override: got %q, want %q", got, override)
	}
	// A stale override falls back to the default location.
	if got := resolveSessionsDir(testAccount(filepath.Join(override, "gone"))); got != filepath.Join(relocated, "sessions") {
		t.Errorf("stale override: got %q, want %q", got, filepath.Join(relocated, "sessions"))
	}
}
