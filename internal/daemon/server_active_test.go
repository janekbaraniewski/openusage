package daemon

import (
	"context"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/active"
	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/telemetry"
)

func TestActiveSelectsMostRecentEventProvider(t *testing.T) {
	configureActiveTestConfig(t,
		core.AccountConfig{ID: "codex-default", Provider: "codex"},
		core.AccountConfig{ID: "claude-default", Provider: "claude_code"},
	)
	srv := newActiveTestService(t)
	ctx := context.Background()

	seedActiveEvent(t, srv, "codex", "codex-default", time.Now().Add(-2*time.Hour), "codex-old")
	seedActiveEvent(t, srv, "claude_code", "claude-default", time.Now().Add(-5*time.Minute), "claude-new")

	sel, err := srv.computeActive(ctx)
	if err != nil {
		t.Fatalf("computeActive: %v", err)
	}
	if sel.Selected != "claude_code:claude-default" {
		t.Errorf("selected = %q, want claude_code:claude-default", sel.Selected)
	}
	if sel.Source != "events" {
		t.Errorf("source = %q, want events", sel.Source)
	}
	if sel.Status != "ok" {
		t.Errorf("status = %q, want ok", sel.Status)
	}
}

func TestActivePinHoldsThenAutoReleases(t *testing.T) {
	configureActiveTestConfig(t,
		core.AccountConfig{ID: "codex-default", Provider: "codex"},
		core.AccountConfig{ID: "claude-default", Provider: "claude_code"},
	)
	srv := newActiveTestService(t)
	ctx := context.Background()

	seedActiveEvent(t, srv, "codex", "codex-default", time.Now().Add(-2*time.Hour), "codex-old")
	seedActiveEvent(t, srv, "claude_code", "claude-default", time.Now().Add(-1*time.Hour), "claude-old")

	if err := srv.setPin(ctx, "codex:codex-default"); err != nil {
		t.Fatalf("setPin: %v", err)
	}
	sel, err := srv.computeActive(ctx)
	if err != nil {
		t.Fatalf("computeActive while pinned: %v", err)
	}
	if sel.Selected != "codex:codex-default" || !sel.Pinned {
		t.Fatalf("pin not honored: %+v", sel)
	}

	seedActiveEvent(t, srv, "claude_code", "claude-default", time.Now().Add(1*time.Minute), "claude-new")
	sel, err = srv.computeActive(ctx)
	if err != nil {
		t.Fatalf("computeActive after release: %v", err)
	}
	if sel.Pinned {
		t.Errorf("pin should have auto-released: %+v", sel)
	}
	if sel.Selected != "claude_code:claude-default" {
		t.Errorf("selected = %q, want claude_code:claude-default", sel.Selected)
	}

	raw, _, err := srv.store.MetaGet(ctx, active.PinMetaKey)
	if err != nil {
		t.Fatalf("MetaGet after release: %v", err)
	}
	if raw != "" {
		t.Errorf("released pin persisted as %q, want empty", raw)
	}
}

func TestActiveNoDataStatus(t *testing.T) {
	configureActiveTestConfig(t)
	srv := newActiveTestService(t)

	sel, err := srv.computeActive(context.Background())
	if err != nil {
		t.Fatalf("computeActive: %v", err)
	}
	if sel.Status != "no_data" {
		t.Errorf("status = %q, want no_data", sel.Status)
	}
}

func TestActiveConfiguredWithoutEventsUsesPriorityFallback(t *testing.T) {
	configureActiveTestConfig(t,
		core.AccountConfig{ID: "openai-default", Provider: "openai"},
		core.AccountConfig{ID: "codex-default", Provider: "codex"},
	)
	srv := newActiveTestService(t)

	sel, err := srv.computeActive(context.Background())
	if err != nil {
		t.Fatalf("computeActive: %v", err)
	}
	if sel.Selected != "codex:codex-default" {
		t.Errorf("selected = %q, want codex:codex-default", sel.Selected)
	}
	if sel.Source != "local" {
		t.Errorf("source = %q, want local", sel.Source)
	}
}

func TestPinSurvivesRestart(t *testing.T) {
	configureActiveTestConfig(t,
		core.AccountConfig{ID: "codex-default", Provider: "codex"},
	)
	dbPath := filepath.Join(t.TempDir(), "telemetry.db")
	first := openActiveTestService(t, dbPath)
	if err := first.setPin(context.Background(), "codex:codex-default"); err != nil {
		t.Fatalf("setPin: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first service: %v", err)
	}

	second := openActiveTestService(t, dbPath)
	t.Cleanup(func() { _ = second.Close() })
	raw, _, err := second.store.MetaGet(context.Background(), active.PinMetaKey)
	if err != nil {
		t.Fatalf("MetaGet: %v", err)
	}
	state, err := active.DecodePinState(raw)
	if err != nil {
		t.Fatalf("DecodePinState: %v", err)
	}
	if state.Key != "codex:codex-default" {
		t.Errorf("pin after restart = %q, want codex:codex-default", state.Key)
	}
}

func TestActivePinReleasesWhenProviderDisappears(t *testing.T) {
	configureActiveTestConfig(t,
		core.AccountConfig{ID: "codex-default", Provider: "codex"},
		core.AccountConfig{ID: "claude-default", Provider: "claude_code"},
	)
	srv := newActiveTestService(t)
	ctx := context.Background()
	if err := srv.setPin(ctx, "codex:codex-default"); err != nil {
		t.Fatalf("setPin: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load active test config: %v", err)
	}
	cfg.Accounts = []core.AccountConfig{{ID: "claude-default", Provider: "claude_code"}}
	if err := config.Save(cfg); err != nil {
		t.Fatalf("save updated active test config: %v", err)
	}
	seedActiveEvent(t, srv, "claude_code", "claude-default", time.Now(), "claude-after-disappear")

	sel, err := srv.computeActive(ctx)
	if err != nil {
		t.Fatalf("computeActive: %v", err)
	}
	if sel.Pinned {
		t.Fatalf("missing provider pin still active: %+v", sel)
	}
	if sel.Selected != "claude_code:claude-default" {
		t.Errorf("selected = %q, want claude_code:claude-default", sel.Selected)
	}
	raw, _, err := srv.store.MetaGet(ctx, active.PinMetaKey)
	if err != nil {
		t.Fatalf("MetaGet after missing provider release: %v", err)
	}
	if raw != "" {
		t.Errorf("missing provider pin persisted as %q, want empty", raw)
	}
}

func TestComputeActiveConcurrent(t *testing.T) {
	configureActiveTestConfig(t,
		core.AccountConfig{ID: "codex-default", Provider: "codex"},
		core.AccountConfig{ID: "claude-default", Provider: "claude_code"},
	)
	srv := newActiveTestService(t)
	ctx := context.Background()
	seedActiveEvent(t, srv, "codex", "codex-default", time.Now().Add(-2*time.Hour), "codex-old")
	seedActiveEvent(t, srv, "claude_code", "claude-default", time.Now().Add(-1*time.Hour), "claude-old")
	if err := srv.setPin(ctx, "codex:codex-default"); err != nil {
		t.Fatalf("setPin: %v", err)
	}
	seedActiveEvent(t, srv, "claude_code", "claude-default", time.Now().Add(time.Minute), "claude-new")

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := srv.computeActive(ctx); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent computeActive: %v", err)
	}
}

func TestActiveHTTPAPI(t *testing.T) {
	configureActiveTestConfig(t,
		core.AccountConfig{ID: "codex-default", Provider: "codex"},
	)
	srv := newActiveTestService(t)
	seedActiveEvent(t, srv, "codex", "codex-default", time.Now(), "codex-api")

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/active", srv.handleActive)
	mux.HandleFunc("/v1/active/explain", srv.handleActiveExplain)
	mux.HandleFunc("/v1/active/list", srv.handleActiveList)
	mux.HandleFunc("/v1/active/detail", srv.handleActiveDetail)
	mux.HandleFunc("/v1/active/pin", srv.handleActivePin)
	listener, err := net.Listen("unix", filepath.Join(t.TempDir(), "active.sock"))
	if err != nil {
		t.Fatalf("listen active test socket: %v", err)
	}
	server := &http.Server{Handler: mux}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		_ = server.Close()
		_ = listener.Close()
	})

	client := NewClient(listener.Addr().String())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sel, err := client.Active(ctx)
	if err != nil {
		t.Fatalf("client Active: %v", err)
	}
	if sel.Selected != "codex:codex-default" || sel.Source != "events" {
		t.Fatalf("active selection = %+v", sel)
	}

	list, err := client.ActiveList(ctx)
	if err != nil {
		t.Fatalf("client ActiveList: %v", err)
	}
	if list.Selected != sel.Selected || len(list.Candidates) != 1 {
		t.Fatalf("active list = %+v", list)
	}
	if list.Candidates[0].Severity != active.SeverityWarn {
		t.Fatalf("candidate severity = %q, want %q", list.Candidates[0].Severity, active.SeverityWarn)
	}
	detail, err := client.ActiveDetail(ctx)
	if err != nil {
		t.Fatalf("client ActiveDetail: %v", err)
	}
	if detail.Selection.Selected != sel.Selected || detail.Status != "ok" {
		t.Fatalf("active detail = %+v", detail)
	}

	if err := client.SetPin(ctx, "codex:codex-default"); err != nil {
		t.Fatalf("client SetPin: %v", err)
	}
	sel, err = client.Active(ctx)
	if err != nil {
		t.Fatalf("client Active after pin: %v", err)
	}
	if !sel.Pinned || sel.Source != "pinned" {
		t.Fatalf("pinned selection = %+v", sel)
	}

	explanation, err := client.ActiveExplain(ctx)
	if err != nil {
		t.Fatalf("client ActiveExplain: %v", err)
	}
	if !strings.Contains(explanation, "pinned") {
		t.Errorf("explanation = %q, want pinned decision", explanation)
	}

	if err := client.SetPin(ctx, ""); err != nil {
		t.Fatalf("client clear pin: %v", err)
	}
}

func TestActiveDetailFollowsMetricResetKey(t *testing.T) {
	reset := time.Now().Add(time.Hour).UTC()
	used := 40.0
	limit := 100.0
	snap := core.NewUsageSnapshot("claude_code", "claude-default")
	snap.Metrics["usage_five_hour"] = core.Metric{
		Used: &used, Limit: &limit, Unit: "%", Window: "5h", ResetKey: "billing_block",
	}
	snap.Resets["billing_block"] = reset
	selection := active.Selection{
		Selected: "claude_code:claude-default",
		Display:  "claude",
		Status:   "ok",
	}
	response := buildActiveDetailResponse(selection, map[string]core.UsageSnapshot{
		selection.Selected: snap,
	}, time.Now().UTC())
	if len(response.Rows) != 1 {
		t.Fatalf("rows = %+v, want one row", response.Rows)
	}
	if response.Rows[0].ResetAt == nil || !response.Rows[0].ResetAt.Equal(reset) {
		t.Fatalf("reset = %v, want %v", response.Rows[0].ResetAt, reset)
	}
}

func TestActiveDetailOmitsCostAndMarksPrimary(t *testing.T) {
	cost := 26.46
	burn := 0.7362370835
	used := 15.0
	limit := 100.0
	tokens := 1188049.0

	snap := core.NewUsageSnapshot("codex", "codex-cli")
	snap.Metrics["all_time_api_cost"] = core.Metric{Used: &cost, Unit: "USD", Window: "all-time"}
	snap.Metrics["model_gpt_cost_usd"] = core.Metric{Used: &cost, Unit: "USD", Window: "30d"}
	snap.Metrics["quota_burn_rate"] = core.Metric{Used: &burn, Unit: "%/hour", Window: "current"}
	snap.Metrics["plan_percent_used"] = core.Metric{Used: &used, Limit: &limit, Unit: "%", Window: "7d"}
	snap.Metrics["client_cli_total_tokens"] = core.Metric{Used: &tokens, Unit: "tokens", Window: "30d"}

	selection := active.Selection{Selected: "codex:codex-cli", Display: "codex", Status: "ok"}
	response := buildActiveDetailResponse(selection, map[string]core.UsageSnapshot{
		selection.Selected: snap,
	}, time.Now().UTC())

	for _, row := range response.Rows {
		if strings.Contains(strings.ToLower(row.Name), "cost") || strings.Contains(row.Display, "USD") {
			t.Fatalf("cost row leaked into active detail: %+v", row)
		}
	}

	primary := map[string]bool{}
	for _, row := range response.Rows {
		if row.Primary {
			primary[row.Name] = true
		}
	}
	for _, want := range []string{"quota_burn_rate", "plan_percent_used"} {
		if !primary[want] {
			t.Fatalf("expected %q to be primary, got rows %+v", want, response.Rows)
		}
	}
	if primary["client_cli_total_tokens"] {
		t.Fatalf("client_cli_total_tokens should not be primary")
	}
}

func TestFormatActiveNumberRounds(t *testing.T) {
	cases := map[float64]string{
		115.45194054314815: "115.45",
		0.7362370835:       "0.74",
		29:                 "29",
		1188049:            "1188049",
	}
	for in, want := range cases {
		if got := formatActiveNumber(in); got != want {
			t.Fatalf("formatActiveNumber(%v) = %q, want %q", in, got, want)
		}
	}
}

func configureActiveTestConfig(t *testing.T, accounts ...core.AccountConfig) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	cfg := config.DefaultConfig()
	cfg.AutoDetect = false
	cfg.Accounts = accounts
	if err := config.Save(cfg); err != nil {
		t.Fatalf("save active test config: %v", err)
	}
}

func newActiveTestService(t *testing.T) *Service {
	t.Helper()
	return openActiveTestService(t, filepath.Join(t.TempDir(), "telemetry.db"))
}

func openActiveTestService(t *testing.T, dbPath string) *Service {
	t.Helper()
	store, err := telemetry.OpenStore(dbPath)
	if err != nil {
		t.Fatalf("open active test store: %v", err)
	}
	// Windows cannot unlink an open file, so t.TempDir cleanup fails unless
	// the SQLite handle is closed first.
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close active test store: %v", err)
		}
	})
	srv := &Service{
		cfg:     Config{DBPath: dbPath},
		store:   store,
		rmCache: newReadModelCache(),
	}
	return srv
}

func seedActiveEvent(t *testing.T, srv *Service, providerID, accountID string, occurredAt time.Time, messageID string) {
	t.Helper()
	_, err := srv.store.Ingest(context.Background(), telemetry.IngestRequest{
		SourceSystem:        telemetry.SourceSystem(providerID),
		SourceChannel:       telemetry.SourceChannelHook,
		SourceSchemaVersion: "v1",
		OccurredAt:          occurredAt,
		ProviderID:          providerID,
		AccountID:           accountID,
		AgentName:           providerID,
		EventType:           telemetry.EventTypeMessageUsage,
		MessageID:           messageID,
		Status:              telemetry.EventStatusOK,
	})
	if err != nil {
		t.Fatalf("seed %s event: %v", providerID, err)
	}
}
