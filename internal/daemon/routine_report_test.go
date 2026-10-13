package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/telemetry"
)

func TestRoutineReportRequests(t *testing.T) {
	report := routineUsageReport{
		RoutineID: "routine-a", RunID: "run-1", OccurredAt: time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC),
		Models: []routineModelReport{{Model: "claude-sonnet-4-5", InputTokens: 100, OutputTokens: 25, CacheReadTokens: 10}},
	}
	reqs, err := routineReportRequests(report, "claude")
	if err != nil || len(reqs) != 1 {
		t.Fatalf("routineReportRequests() = %d, %v; want 1 request", len(reqs), err)
	}
	req := reqs[0]
	if req.SourceSystem != "claude_code_routine_cloud" || req.AgentName != "claude_code_routine_cloud" || req.ProviderID != "claude_code" || req.AccountID != "claude" {
		t.Fatalf("unexpected routing: %+v", req)
	}
	if req.TotalTokens == nil || *req.TotalTokens != 135 {
		t.Fatalf("total_tokens = %v, want 135", req.TotalTokens)
	}
}

func TestRoutineReportRequestsRejectInvalidReports(t *testing.T) {
	base := routineUsageReport{RoutineID: "r", RunID: "run", OccurredAt: time.Now(), Models: []routineModelReport{{Model: "sonnet"}}}
	tests := []struct {
		name    string
		edit    func(*routineUsageReport)
		account string
	}{
		{"missing run", func(r *routineUsageReport) { r.RunID = "" }, "claude"},
		{"negative tokens", func(r *routineUsageReport) { r.Models[0].InputTokens = -1 }, "claude"},
		{"duplicate model", func(r *routineUsageReport) { r.Models = append(r.Models, r.Models[0]) }, "claude"},
		{"missing account", func(*routineUsageReport) {}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := base
			report.Models = append([]routineModelReport(nil), base.Models...)
			tt.edit(&report)
			if _, err := routineReportRequests(report, tt.account); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestHandleRoutineReportAuthAndMethod(t *testing.T) {
	svc := &Service{cfg: Config{RoutineToken: "secret", RoutineAccountID: "claude"}}
	for _, tt := range []struct {
		name, method, auth string
		want               int
	}{
		{"unauthorized", http.MethodPost, "", http.StatusUnauthorized},
		{"wrong token", http.MethodPost, "Bearer wrong", http.StatusUnauthorized},
		{"method", http.MethodGet, "Bearer secret", http.StatusMethodNotAllowed},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/v1/routines/claude-code/report", strings.NewReader(`{}`))
			if tt.auth != "" {
				req.Header.Set("Authorization", tt.auth)
			}
			w := httptest.NewRecorder()
			svc.handleRoutineReport(w, req)
			if w.Code != tt.want {
				t.Fatalf("status = %d, want %d", w.Code, tt.want)
			}
		})
	}
}

func TestRoutineReportDuplicateIngestDedupes(t *testing.T) {
	store, err := telemetry.OpenStore(t.TempDir() + "/telemetry.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.RunMigrations(context.Background()); err != nil {
		t.Fatal(err)
	}
	report := routineUsageReport{RoutineID: "r", RunID: "run", OccurredAt: time.Now(), Models: []routineModelReport{{Model: "sonnet", InputTokens: 20}}}
	reqs, err := routineReportRequests(report, "claude")
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.Ingest(context.Background(), reqs[0])
	if err != nil || first.Deduped {
		t.Fatalf("first ingest = %+v, %v", first, err)
	}
	second, err := store.Ingest(context.Background(), reqs[0])
	if err != nil || !second.Deduped {
		t.Fatalf("repeat ingest = %+v, %v; want deduped", second, err)
	}
}

func TestHandleRoutineReportPersistsAndDeduplicates(t *testing.T) {
	dbPath := t.TempDir() + "/telemetry.db"
	store, err := telemetry.OpenStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.RunMigrations(context.Background()); err != nil {
		t.Fatal(err)
	}
	svc := &Service{cfg: Config{RoutineToken: "secret", RoutineAccountID: "claude", DBPath: dbPath}, store: store}
	body := `{"routine_id":"r","run_id":"run-1","occurred_at":"2026-10-13T12:00:00Z","models":[{"model":"claude-sonnet-4-5","input_tokens":100,"output_tokens":20}]}`
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/v1/routines/claude-code/report", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer secret")
		w := httptest.NewRecorder()
		svc.handleRoutineReport(w, req)
		if w.Code != http.StatusAccepted {
			t.Fatalf("submission %d status=%d body=%s", i+1, w.Code, w.Body.String())
		}
	}
	var count int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM usage_events WHERE account_id = 'claude'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("stored usage events=%d, want 1 after duplicate report", count)
	}
	snapshots, err := svc.computeReadModel(context.Background(), ReadModelRequest{
		Accounts:   []ReadModelAccount{{AccountID: "claude", ProviderID: "claude_code"}},
		TimeWindow: "30d",
	})
	if err != nil {
		t.Fatalf("compute Routine Cloud read model: %v", err)
	}
	metric, ok := snapshots["claude"].Metrics["client_claude_code_routine_cloud_total_tokens"]
	if !ok || metric.Used == nil || *metric.Used != 120 {
		t.Fatalf("Routine Cloud total metric = %+v, exists=%v; want 120 tokens", metric, ok)
	}
}
