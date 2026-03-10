package cursor

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestProvider_Fetch_ReadsComposerSessionsFromStateDB(t *testing.T) {
	stateDBPath := filepath.Join(t.TempDir(), "state.vscdb")
	db, err := sql.Open("sqlite3", stateDBPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.Exec(`CREATE TABLE IF NOT EXISTS ItemTable (key TEXT PRIMARY KEY, value TEXT)`)
	db.Exec(`INSERT INTO ItemTable (key, value) VALUES ('cursorAuth/cachedEmail', 'test@example.com')`)
	db.Exec(`INSERT INTO ItemTable (key, value) VALUES ('freeBestOfN.promptCount', '42')`)

	db.Exec(`CREATE TABLE IF NOT EXISTS cursorDiskKV (key TEXT PRIMARY KEY, value TEXT)`)
	now := time.Now()
	session1 := fmt.Sprintf(`{"usageData":{"claude-4.5-opus":{"costInCents":500,"amount":10},"gpt-4o":{"costInCents":100,"amount":5}},"unifiedMode":"agent","createdAt":%d,"totalLinesAdded":200,"totalLinesRemoved":50}`, now.Add(-1*time.Hour).UnixMilli())
	session2 := fmt.Sprintf(`{"usageData":{"claude-4.5-opus":{"costInCents":300,"amount":8}},"unifiedMode":"chat","createdAt":%d,"totalLinesAdded":100,"totalLinesRemoved":20}`, now.Add(-2*time.Hour).UnixMilli())
	sessionEmpty := `{"usageData":{},"unifiedMode":"agent","createdAt":1000}`
	db.Exec(`INSERT INTO cursorDiskKV (key, value) VALUES ('composerData:aaa', ?)`, session1)
	db.Exec(`INSERT INTO cursorDiskKV (key, value) VALUES ('composerData:bbb', ?)`, session2)
	db.Exec(`INSERT INTO cursorDiskKV (key, value) VALUES ('composerData:ccc', ?)`, sessionEmpty)
	db.Close()

	p := New()
	snap, err := p.Fetch(context.Background(), core.AccountConfig{
		ID:       "cursor-composer-test",
		Provider: "cursor",
		ExtraData: map[string]string{
			"state_db": stateDBPath,
		},
	})
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}

	if m, ok := snap.Metrics["composer_cost"]; !ok || m.Used == nil || *m.Used != 9.0 {
		t.Errorf("composer_cost: got %+v, want Used=9.0 (900 cents)", m)
	}
	if m, ok := snap.Metrics["composer_sessions"]; !ok || m.Used == nil || *m.Used != 2 {
		t.Errorf("composer_sessions: got %+v, want Used=2", m)
	}
	if m, ok := snap.Metrics["composer_requests"]; !ok || m.Used == nil || *m.Used != 23 {
		t.Errorf("composer_requests: got %+v, want Used=23", m)
	}
	if m, ok := snap.Metrics["composer_lines_added"]; !ok || m.Used == nil || *m.Used != 300 {
		t.Errorf("composer_lines_added: got %+v, want Used=300", m)
	}
	if m, ok := snap.Metrics["mode_agent_sessions"]; !ok || m.Used == nil || *m.Used != 1 {
		t.Errorf("mode_agent_sessions: got %+v, want Used=1", m)
	}
	if m, ok := snap.Metrics["mode_chat_sessions"]; !ok || m.Used == nil || *m.Used != 1 {
		t.Errorf("mode_chat_sessions: got %+v, want Used=1", m)
	}
	if m, ok := snap.Metrics["total_prompts"]; !ok || m.Used == nil || *m.Used != 42 {
		t.Errorf("total_prompts: got %+v, want Used=42", m)
	}
	if snap.Raw["account_email"] != "test@example.com" {
		t.Errorf("account_email: got %q, want test@example.com", snap.Raw["account_email"])
	}
	if snap.Raw["total_prompts"] != "42" {
		t.Errorf("total_prompts raw: got %q, want 42", snap.Raw["total_prompts"])
	}
}

func TestProvider_Fetch_ReadsScoredCommitsFromTrackingDB(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ai-code-tracking.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.Exec(`CREATE TABLE ai_code_hashes (hash TEXT PRIMARY KEY, source TEXT, createdAt INTEGER, model TEXT)`)
	db.Exec(`INSERT INTO ai_code_hashes VALUES ('h1', 'composer', ?, 'claude')`, time.Now().UnixMilli())

	db.Exec(`CREATE TABLE scored_commits (
		commitHash TEXT, branchName TEXT, scoredAt INTEGER,
		linesAdded INTEGER, linesDeleted INTEGER,
		tabLinesAdded INTEGER, tabLinesDeleted INTEGER,
		composerLinesAdded INTEGER, composerLinesDeleted INTEGER,
		humanLinesAdded INTEGER, humanLinesDeleted INTEGER,
		blankLinesAdded INTEGER, blankLinesDeleted INTEGER,
		commitMessage TEXT, commitDate TEXT,
		v1AiPercentage TEXT, v2AiPercentage TEXT,
		PRIMARY KEY (commitHash, branchName))`)
	db.Exec(`INSERT INTO scored_commits VALUES ('abc', 'main', ?, 100, 10, 20, 5, 60, 3, 20, 2, 0, 0, 'test', '2026-02-23', '50.0', '80.0')`, time.Now().UnixMilli())
	db.Exec(`INSERT INTO scored_commits VALUES ('def', 'main', ?, 200, 20, 40, 10, 120, 6, 40, 4, 0, 0, 'test2', '2026-02-22', '30.0', '60.0')`, time.Now().UnixMilli())
	db.Close()

	p := New()
	snap, err := p.Fetch(context.Background(), core.AccountConfig{
		ID:       "cursor-commits-test",
		Provider: "cursor",
		ExtraData: map[string]string{
			"tracking_db": dbPath,
		},
	})
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}

	if m, ok := snap.Metrics["scored_commits"]; !ok || m.Used == nil || *m.Used != 2 {
		t.Errorf("scored_commits: got %+v, want Used=2", m)
	}
	if m, ok := snap.Metrics["ai_code_percentage"]; !ok || m.Used == nil {
		t.Errorf("ai_code_percentage missing")
	} else if *m.Used != 70.0 {
		t.Errorf("ai_code_percentage: got %.1f, want 70.0 (avg of 80+60)", *m.Used)
	}
}

func TestCursorClientBucket(t *testing.T) {
	tests := []struct {
		source string
		want   string
	}{
		{source: "composer", want: "ide"},
		{source: "tab", want: "ide"},
		{source: "human", want: "ide"},
		{source: "cli", want: "cli_agents"},
		{source: "terminal", want: "cli_agents"},
		{source: "background-agent", want: "cloud_agents"},
		{source: "cloud", want: "cloud_agents"},
		{source: "web_agent", want: "cloud_agents"},
		{source: "unknown-source", want: "other"},
		{source: "", want: "other"},
	}

	for _, tt := range tests {
		if got := cursorClientBucket(tt.source); got != tt.want {
			t.Errorf("cursorClientBucket(%q) = %q, want %q", tt.source, got, tt.want)
		}
	}
}

type cursorTrackingRow struct {
	Hash      string
	Source    string
	Model     string
	CreatedAt int64
}

func createCursorTrackingDBForTest(t *testing.T, rows []cursorTrackingRow) string {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "ai-code-tracking.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE ai_code_hashes (
			hash TEXT PRIMARY KEY,
			source TEXT NOT NULL,
			fileExtension TEXT,
			fileName TEXT,
			requestId TEXT,
			conversationId TEXT,
			timestamp INTEGER,
			createdAt INTEGER NOT NULL,
			model TEXT
		)`)
	if err != nil {
		t.Fatalf("create ai_code_hashes table: %v", err)
	}

	stmt, err := db.Prepare(`
		INSERT INTO ai_code_hashes (
			hash, source, fileExtension, fileName, requestId, conversationId, timestamp, createdAt, model
		) VALUES (?, ?, '', '', '', '', ?, ?, ?)`)
	if err != nil {
		t.Fatalf("prepare insert: %v", err)
	}
	defer stmt.Close()

	for _, row := range rows {
		ts := row.CreatedAt
		if ts == 0 {
			ts = time.Now().UnixMilli()
		}
		if _, err := stmt.Exec(row.Hash, row.Source, ts, ts, row.Model); err != nil {
			t.Fatalf("insert row %q: %v", row.Hash, err)
		}
	}

	return dbPath
}

func TestProvider_Fetch_PlanSpendGaugeUsesIncludedAmountWhenNoLimit(t *testing.T) {
	// When the plan has no hard limit (pu.Limit=0) and no pooled team limit,
	// plan_spend should use the plan's included amount as the gauge reference.
	mux := http.NewServeMux()
	mux.HandleFunc("/aiserver.v1.DashboardService/GetCurrentPeriodUsage", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(currentPeriodUsageResp{
			BillingCycleStart: "1768055295000",
			BillingCycleEnd:   "1770733695000",
			PlanUsage: planUsage{
				TotalSpend:       36470, // $364.70
				IncludedSpend:    2000,
				Limit:            0, // No hard limit
				TotalPercentUsed: 0,
			},
			DisplayMessage: "Usage-based billing",
		})
	})
	mux.HandleFunc("/aiserver.v1.DashboardService/GetPlanInfo", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(planInfoResp{
			PlanInfo: struct {
				PlanName            string  `json:"planName"`
				IncludedAmountCents float64 `json:"includedAmountCents"`
				Price               string  `json:"price"`
				BillingCycleEnd     string  `json:"billingCycleEnd"`
			}{
				PlanName:            "Pro",
				IncludedAmountCents: 2000, // $20 included
				Price:               "$20/mo",
			},
		})
	})
	mux.HandleFunc("/aiserver.v1.DashboardService/GetAggregatedUsageEvents", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(aggregatedUsageResp{})
	})
	mux.HandleFunc("/aiserver.v1.DashboardService/GetHardLimit", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(hardLimitResp{})
	})
	mux.HandleFunc("/auth/full_stripe_profile", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(stripeProfileResp{})
	})
	mux.HandleFunc("/aiserver.v1.DashboardService/GetUsageLimitPolicyStatus", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(usageLimitPolicyResp{})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	prevBase := cursorAPIBase
	cursorAPIBase = server.URL
	defer func() { cursorAPIBase = prevBase }()

	p := New()
	snap, err := p.Fetch(context.Background(), core.AccountConfig{
		ID:       "cursor-gauge-test",
		Provider: "cursor",
		Token:    "test-token",
	})
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}

	m, ok := snap.Metrics["plan_spend"]
	if !ok {
		t.Fatal("plan_spend metric missing")
	}
	if m.Used == nil || *m.Used != 364.70 {
		t.Fatalf("plan_spend.Used = %v, want 364.70", m.Used)
	}
	if m.Limit == nil || *m.Limit != 20.0 {
		t.Fatalf("plan_spend.Limit = %v, want 20.0 (from IncludedAmountCents)", m.Limit)
	}
}

func TestProvider_Fetch_CachedBillingMetricsRestoreOnAPIFailure(t *testing.T) {
	// First call: API available → caches billing metrics.
	// Second call: API fails → billing metrics restored from cache.
	var periodCalls int
	mux := http.NewServeMux()
	mux.HandleFunc("/aiserver.v1.DashboardService/GetCurrentPeriodUsage", func(w http.ResponseWriter, r *http.Request) {
		periodCalls++
		if periodCalls > 1 {
			http.Error(w, "service unavailable", http.StatusServiceUnavailable)
			return
		}
		json.NewEncoder(w).Encode(currentPeriodUsageResp{
			BillingCycleStart: "1768055295000",
			BillingCycleEnd:   "1770733695000",
			PlanUsage: planUsage{
				TotalSpend:       40700,
				Limit:            0,
				TotalPercentUsed: 85.0,
				AutoPercentUsed:  60.0,
				APIPercentUsed:   25.0,
			},
			SpendLimitUsage: spendLimitUsage{
				PooledLimit:     360000,
				PooledUsed:      40700,
				PooledRemaining: 319300,
			},
		})
	})
	mux.HandleFunc("/aiserver.v1.DashboardService/GetPlanInfo", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(planInfoResp{
			PlanInfo: struct {
				PlanName            string  `json:"planName"`
				IncludedAmountCents float64 `json:"includedAmountCents"`
				Price               string  `json:"price"`
				BillingCycleEnd     string  `json:"billingCycleEnd"`
			}{
				PlanName:            "Business",
				IncludedAmountCents: 50000,
			},
		})
	})
	mux.HandleFunc("/aiserver.v1.DashboardService/GetAggregatedUsageEvents", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(aggregatedUsageResp{
			Aggregations: []modelAggregation{
				{ModelIntent: "test-model", TotalCents: 100},
			},
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	prevBase := cursorAPIBase
	cursorAPIBase = server.URL
	defer func() { cursorAPIBase = prevBase }()

	// Create state DB with composer cost data.
	stateDBPath := filepath.Join(t.TempDir(), "state.vscdb")
	db, err := sql.Open("sqlite3", stateDBPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.Exec(`CREATE TABLE IF NOT EXISTS ItemTable (key TEXT PRIMARY KEY, value TEXT)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS cursorDiskKV (key TEXT PRIMARY KEY, value TEXT)`)
	session := fmt.Sprintf(`{"usageData":{"test-model":{"costInCents":7500,"amount":15}},"unifiedMode":"agent","createdAt":%d}`, time.Now().Add(-1*time.Hour).UnixMilli())
	db.Exec(`INSERT INTO cursorDiskKV (key, value) VALUES ('composerData:aaa', ?)`, session)
	db.Close()

	p := New()
	acct := core.AccountConfig{
		ID:       "cursor-cache-billing",
		Provider: "cursor",
		Token:    "test-token",
		ExtraData: map[string]string{
			"state_db": stateDBPath,
		},
	}

	// First fetch: API works, caches billing metrics.
	snap1, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("first Fetch returned error: %v", err)
	}
	// Verify API-derived billing metrics exist.
	if m, ok := snap1.Metrics["spend_limit"]; !ok || m.Limit == nil || *m.Limit != 3600.0 {
		t.Fatalf("spend_limit after API call: got %+v, want Limit=3600", snap1.Metrics["spend_limit"])
	}
	if m, ok := snap1.Metrics["plan_percent_used"]; !ok || m.Used == nil || *m.Used != 85.0 {
		t.Fatalf("plan_percent_used after API call: got %+v, want Used=85", snap1.Metrics["plan_percent_used"])
	}

	// Second fetch: API fails → billing metrics should be restored from cache.
	snap2, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("second Fetch returned error: %v", err)
	}

	// spend_limit should be restored from cache.
	if m, ok := snap2.Metrics["spend_limit"]; !ok {
		t.Fatal("spend_limit missing after API failure (should be restored from cache)")
	} else {
		if m.Limit == nil || *m.Limit != 3600.0 {
			t.Fatalf("spend_limit.Limit = %v, want 3600 (from cache)", m.Limit)
		}
		if m.Used == nil || *m.Used != 407.0 {
			t.Fatalf("spend_limit.Used = %v, want 407 (from cache)", m.Used)
		}
	}

	// plan_percent_used should be restored from cache.
	if m, ok := snap2.Metrics["plan_percent_used"]; !ok {
		t.Fatal("plan_percent_used missing after API failure (should be restored from cache)")
	} else {
		if m.Used == nil || *m.Used != 85.0 {
			t.Fatalf("plan_percent_used.Used = %v, want 85 (from cache)", m.Used)
		}
	}

	// plan_spend should be restored from cache.
	if m, ok := snap2.Metrics["plan_spend"]; !ok {
		t.Fatal("plan_spend missing after API failure (should be restored from cache)")
	} else {
		if m.Used == nil {
			t.Fatal("plan_spend.Used is nil (should be restored from cache)")
		}
	}
}

func TestProvider_Fetch_PartialAPIFailure_PeriodUsageDown(t *testing.T) {
	// GetCurrentPeriodUsage fails, but GetAggregatedUsageEvents succeeds.
	// After a first successful call caches billing metrics, the second call
	// with GetCurrentPeriodUsage failing should still show billing gauges
	// AND model aggregation data from the live API.
	var periodCalls int
	mux := http.NewServeMux()
	mux.HandleFunc("/aiserver.v1.DashboardService/GetCurrentPeriodUsage", func(w http.ResponseWriter, r *http.Request) {
		periodCalls++
		if periodCalls > 1 {
			http.Error(w, "rate limited", http.StatusTooManyRequests)
			return
		}
		json.NewEncoder(w).Encode(currentPeriodUsageResp{
			BillingCycleStart: "1768055295000",
			BillingCycleEnd:   "1770733695000",
			PlanUsage: planUsage{
				TotalSpend:       40700,
				TotalPercentUsed: 85.0,
			},
			SpendLimitUsage: spendLimitUsage{
				PooledLimit:     360000,
				PooledUsed:      40700,
				PooledRemaining: 319300,
			},
		})
	})
	mux.HandleFunc("/aiserver.v1.DashboardService/GetAggregatedUsageEvents", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(aggregatedUsageResp{
			Aggregations: []modelAggregation{
				{ModelIntent: "claude-opus", TotalCents: 30000, InputTokens: "1000000"},
			},
			TotalCostCents: 30000,
		})
	})
	mux.HandleFunc("/aiserver.v1.DashboardService/GetPlanInfo", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(planInfoResp{})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	prevBase := cursorAPIBase
	cursorAPIBase = server.URL
	defer func() { cursorAPIBase = prevBase }()

	p := New()
	acct := core.AccountConfig{
		ID:       "cursor-partial",
		Provider: "cursor",
		Token:    "test-token",
	}

	// First fetch: everything works.
	snap1, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("first Fetch: %v", err)
	}
	if _, ok := snap1.Metrics["spend_limit"]; !ok {
		t.Fatal("spend_limit missing after successful API call")
	}

	// Second fetch: GetCurrentPeriodUsage fails, but aggregation succeeds.
	snap2, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("second Fetch: %v", err)
	}

	// Model aggregation from live API should still work.
	if _, ok := snap2.Metrics["billing_total_cost"]; !ok {
		t.Fatal("billing_total_cost missing — aggregation endpoint should still work")
	}

	// Billing gauge should be restored from cache.
	if m, ok := snap2.Metrics["spend_limit"]; !ok {
		t.Fatal("spend_limit missing — should be restored from billing cache")
	} else if m.Limit == nil || *m.Limit != 3600.0 {
		t.Fatalf("spend_limit.Limit = %v, want 3600 (from cached billing)", m.Limit)
	}

	// plan_percent_used should also be restored.
	if m, ok := snap2.Metrics["plan_percent_used"]; !ok {
		t.Fatal("plan_percent_used missing — should be restored from billing cache")
	} else if m.Used == nil || *m.Used != 85.0 {
		t.Fatalf("plan_percent_used.Used = %v, want 85 (from cached billing)", m.Used)
	}
}

func TestProvider_Fetch_NoPeriodUsage_AggregationCreatesGauge(t *testing.T) {
	// GetCurrentPeriodUsage always fails, no billing cache exists.
	// GetAggregatedUsageEvents succeeds with cost data.
	// GetPlanInfo returns IncludedAmountCents.
	// Should create a plan_spend gauge from billing_total_cost + plan limit.
	mux := http.NewServeMux()
	mux.HandleFunc("/aiserver.v1.DashboardService/GetCurrentPeriodUsage", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
	mux.HandleFunc("/aiserver.v1.DashboardService/GetAggregatedUsageEvents", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(aggregatedUsageResp{
			Aggregations: []modelAggregation{
				{ModelIntent: "claude-opus", TotalCents: 36470},
			},
			TotalCostCents: 36470,
		})
	})
	mux.HandleFunc("/aiserver.v1.DashboardService/GetPlanInfo", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(planInfoResp{
			PlanInfo: struct {
				PlanName            string  `json:"planName"`
				IncludedAmountCents float64 `json:"includedAmountCents"`
				Price               string  `json:"price"`
				BillingCycleEnd     string  `json:"billingCycleEnd"`
			}{
				PlanName:            "Pro",
				IncludedAmountCents: 2000,
			},
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	prevBase := cursorAPIBase
	cursorAPIBase = server.URL
	defer func() { cursorAPIBase = prevBase }()

	p := New()
	snap, err := p.Fetch(context.Background(), core.AccountConfig{
		ID:       "cursor-no-period",
		Provider: "cursor",
		Token:    "test-token",
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	// billing_total_cost should exist from aggregation.
	if m, ok := snap.Metrics["billing_total_cost"]; !ok || m.Used == nil {
		t.Fatal("billing_total_cost missing from aggregation")
	}

	// plan_spend should be created from billing_total_cost + plan included amount.
	m, ok := snap.Metrics["plan_spend"]
	if !ok {
		t.Fatal("plan_spend missing — should be built from billing_total_cost + plan limit")
	}
	if m.Used == nil || *m.Used != 364.70 {
		t.Fatalf("plan_spend.Used = %v, want 364.70", m.Used)
	}
	if m.Limit == nil || *m.Limit != 20.0 {
		t.Fatalf("plan_spend.Limit = %v, want 20.0 (from IncludedAmountCents)", m.Limit)
	}
}

// TestProvider_Fetch_LocalOnlyComposerCostCreatesCreditsTag verifies that
// when the API is completely unavailable (no token) but local composer
// sessions have cost data, ensureCreditGauges creates plan_total_spend_usd
// so the Credits tag renders in the TUI.
func TestProvider_Fetch_LocalOnlyComposerCostCreatesCreditsTag(t *testing.T) {
	p := New()

	// Set up a state DB with composer sessions that have cost data.
	stateDir := t.TempDir()
	stateDBPath := filepath.Join(stateDir, "state.vscdb")
	sdb, err := sql.Open("sqlite3", stateDBPath)
	if err != nil {
		t.Fatalf("open state db: %v", err)
	}
	_, err = sdb.Exec(`CREATE TABLE IF NOT EXISTS ItemTable (key TEXT PRIMARY KEY, value TEXT)`)
	if err != nil {
		t.Fatalf("create ItemTable: %v", err)
	}
	_, err = sdb.Exec(`CREATE TABLE IF NOT EXISTS cursorDiskKV (key TEXT PRIMARY KEY, value TEXT)`)
	if err != nil {
		t.Fatalf("create cursorDiskKV: %v", err)
	}

	// Insert composer session with cost data.
	usage := map[string]composerModelUsage{
		"claude-4-5-opus-high-thinking": {CostInCents: 15000, Amount: 20},
	}
	usageJSON, _ := json.Marshal(usage)
	createdAt := time.Now().Add(-1 * time.Hour).UnixMilli()
	sessionVal := fmt.Sprintf(`{"usageData":%s,"unifiedMode":"agent","createdAt":%d,"totalLinesAdded":100,"totalLinesRemoved":10}`, string(usageJSON), createdAt)
	_, err = sdb.Exec(`INSERT INTO cursorDiskKV (key, value) VALUES (?, ?)`, "composerData:session-1", sessionVal)
	if err != nil {
		t.Fatalf("insert composer session: %v", err)
	}
	sdb.Close()

	// Fetch with no token — API is completely unavailable.
	snap, err := p.Fetch(context.Background(), core.AccountConfig{
		ID: "test-local-only",
		ExtraData: map[string]string{
			"state_db": stateDBPath,
		},
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	// composer_cost should exist from local state DB.
	cm, ok := snap.Metrics["composer_cost"]
	if !ok || cm.Used == nil || *cm.Used <= 0 {
		t.Fatalf("composer_cost missing or zero, got: %+v", cm)
	}

	// plan_total_spend_usd should be synthesized by ensureCreditGauges.
	ptsu, ok := snap.Metrics["plan_total_spend_usd"]
	if !ok {
		t.Fatal("plan_total_spend_usd missing — ensureCreditGauges should create it from composer_cost")
	}
	if ptsu.Used == nil || *ptsu.Used != *cm.Used {
		t.Fatalf("plan_total_spend_usd.Used = %v, want %v (from composer_cost)", ptsu.Used, *cm.Used)
	}

	// Message should indicate API unavailable.
	if snap.Message == "" {
		t.Error("expected a local-only message, got empty")
	}
}

// TestProvider_Fetch_LocalOnlyCachedLimitCreatesPlanSpendGauge verifies that
// when the API previously provided a plan limit (cached), and later becomes
// unavailable, ensureCreditGauges creates plan_spend with the cached limit
// so the gauge bar renders.
func TestProvider_Fetch_LocalOnlyCachedLimitCreatesPlanSpendGauge(t *testing.T) {
	p := New()

	// Pre-populate the cache with an effective limit from a previous API call.
	p.mu.Lock()
	p.modelAggregationCache["test-cached"] = cachedModelAggregation{
		EffectiveLimitUSD: 500.0,
	}
	p.mu.Unlock()

	// Set up a state DB with composer sessions that have cost data.
	stateDir := t.TempDir()
	stateDBPath := filepath.Join(stateDir, "state.vscdb")
	sdb, err := sql.Open("sqlite3", stateDBPath)
	if err != nil {
		t.Fatalf("open state db: %v", err)
	}
	_, err = sdb.Exec(`CREATE TABLE IF NOT EXISTS ItemTable (key TEXT PRIMARY KEY, value TEXT)`)
	if err != nil {
		t.Fatalf("create ItemTable: %v", err)
	}
	_, err = sdb.Exec(`CREATE TABLE IF NOT EXISTS cursorDiskKV (key TEXT PRIMARY KEY, value TEXT)`)
	if err != nil {
		t.Fatalf("create cursorDiskKV: %v", err)
	}

	usage := map[string]composerModelUsage{
		"claude-4-5-opus": {CostInCents: 36470, Amount: 50},
	}
	usageJSON, _ := json.Marshal(usage)
	createdAt := time.Now().Add(-2 * time.Hour).UnixMilli()
	sessionVal := fmt.Sprintf(`{"usageData":%s,"unifiedMode":"agent","createdAt":%d,"totalLinesAdded":200,"totalLinesRemoved":20}`, string(usageJSON), createdAt)
	_, err = sdb.Exec(`INSERT INTO cursorDiskKV (key, value) VALUES (?, ?)`, "composerData:session-cached", sessionVal)
	if err != nil {
		t.Fatalf("insert composer session: %v", err)
	}
	sdb.Close()

	// Fetch with no token.
	snap, err := p.Fetch(context.Background(), core.AccountConfig{
		ID: "test-cached",
		ExtraData: map[string]string{
			"state_db": stateDBPath,
		},
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	// plan_spend should be created with cached limit.
	ps, ok := snap.Metrics["plan_spend"]
	if !ok {
		t.Fatal("plan_spend missing — ensureCreditGauges should create it from composer_cost + cached limit")
	}
	if ps.Used == nil || *ps.Used != 364.70 {
		t.Fatalf("plan_spend.Used = %v, want 364.70", ps.Used)
	}
	if ps.Limit == nil || *ps.Limit != 500.0 {
		t.Fatalf("plan_spend.Limit = %v, want 500.0 (from cached effective limit)", ps.Limit)
	}
}
