package telemetry

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// legacyPrimaryCreditMetric is the pre-optimization query (a DISTINCT over
// every observation of the account), kept to prove the indexed version picks
// the same metric and semantics.
func legacyPrimaryCreditMetric(t *testing.T, s *Store, providerID, accountID string) (string, string, bool) {
	t.Helper()
	rows, err := s.DB().Query(`
		SELECT DISTINCT metric_key, semantics
		FROM balance_observations
		WHERE provider_id = ? AND account_id = ?
	`, providerID, accountID)
	if err != nil {
		t.Fatalf("legacy query: %v", err)
	}
	defer rows.Close()
	present := map[string]string{}
	for rows.Next() {
		var k, sem string
		if err := rows.Scan(&k, &sem); err != nil {
			t.Fatalf("legacy scan: %v", err)
		}
		present[k] = sem
	}
	for _, k := range creditMetricPriority {
		if sem, ok := present[k]; ok {
			return k, sem, true
		}
	}
	return "", "", false
}

func TestPrimaryCreditMetricMatchesLegacyQuery(t *testing.T) {
	s := newObsStore(t)
	now := time.Now().UTC().Truncate(time.Minute)

	// claude-style account: many non-money metrics, no money metric.
	for i := range 50 {
		seedObs(t, s, "claude_code", "claude-code", fmt.Sprintf("client_%d_tokens", i), balanceSemanticsCumulative, now.Add(-time.Duration(i)*time.Minute), f64(float64(i)), nil)
	}
	// openrouter-style account: two priority keys plus noise; the higher
	// priority one (credit_balance) must win.
	for i := range 5 {
		at := now.Add(-time.Duration(i) * time.Hour)
		seedObs(t, s, "openrouter", "openrouter", "credits", balanceSemanticsCumulative, at, f64(1), nil)
		seedObs(t, s, "openrouter", "openrouter", "credit_balance", balanceSemanticsCumulative, at, f64(2), nil)
		seedObs(t, s, "openrouter", "openrouter", "requests_today", balanceSemanticsCumulative, at, f64(3), nil)
	}
	// Single lower-priority key.
	seedObs(t, s, "deepseek", "deepseek", "total_balance", "remaining", now, nil, f64(9))

	cases := [][2]string{
		{"claude_code", "claude-code"},
		{"openrouter", "openrouter"},
		{"deepseek", "deepseek"},
		{"missing", "missing"},
	}
	for _, c := range cases {
		wantKey, wantSem, wantOK := legacyPrimaryCreditMetric(t, s, c[0], c[1])
		gotKey, gotSem, gotOK, err := s.PrimaryCreditMetric(context.Background(), c[0], c[1])
		if err != nil {
			t.Fatalf("%s: PrimaryCreditMetric: %v", c[1], err)
		}
		if gotKey != wantKey || gotSem != wantSem || gotOK != wantOK {
			t.Errorf("%s: got (%q, %q, %v), legacy (%q, %q, %v)", c[1], gotKey, gotSem, gotOK, wantKey, wantSem, wantOK)
		}
	}
}

func TestIncrementalChangeProbeMatchesCombinedAggregate(t *testing.T) {
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if err := NewStore(db).Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}

	const (
		legacy = `SELECT COALESCE(MIN(seq), 0), COALESCE(MAX(seq), 0) FROM usage_event_changes`
		probe  = `SELECT COALESCE((SELECT MIN(seq) FROM usage_event_changes), 0), COALESCE((SELECT MAX(seq) FROM usage_event_changes), 0)`
	)
	check := func(label string) {
		t.Helper()
		var lMin, lMax, pMin, pMax int64
		if err := db.QueryRow(legacy).Scan(&lMin, &lMax); err != nil {
			t.Fatalf("%s legacy: %v", label, err)
		}
		if err := db.QueryRow(probe).Scan(&pMin, &pMax); err != nil {
			t.Fatalf("%s probe: %v", label, err)
		}
		if lMin != pMin || lMax != pMax {
			t.Fatalf("%s: probe (%d,%d) != legacy (%d,%d)", label, pMin, pMax, lMin, lMax)
		}
	}

	check("empty")
	for i := range 20 {
		if _, err := db.Exec(`INSERT INTO usage_event_changes(event_id, operation) VALUES (?, 'insert')`, fmt.Sprintf("e%d", i)); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	check("filled")
	if _, err := db.Exec(`DELETE FROM usage_event_changes WHERE seq <= 7`); err != nil {
		t.Fatalf("prune: %v", err)
	}
	check("pruned")
}
