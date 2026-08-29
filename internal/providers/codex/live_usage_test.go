package codex

import (
	"encoding/json"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestApplyUsagePayloadParsesSpendControlCreditLimit(t *testing.T) {
	var payload usagePayload
	if err := json.Unmarshal([]byte(`{
		"spend_control": {
			"individual_limit": {
				"limit": "5000",
				"used": "1200",
				"used_percent": 24,
				"reset_at": 1783027200
			}
		}
	}`), &payload); err != nil {
		t.Fatal(err)
	}

	snap := core.NewUsageSnapshot("codex", "test")
	summary := applyUsagePayload(&payload, &snap)
	if summary.limitMetricsApplied != 1 {
		t.Fatalf("expected one live quota metric, got %d", summary.limitMetricsApplied)
	}

	metric, ok := snap.Metrics["codex_credit_limit"]
	if !ok {
		t.Fatal("expected codex_credit_limit metric")
	}
	if metric.Limit == nil || *metric.Limit != 5000 {
		t.Fatalf("expected credit limit 5000, got %v", metric.Limit)
	}
	if metric.Used == nil || *metric.Used != 1200 {
		t.Fatalf("expected used credits 1200, got %v", metric.Used)
	}
	if got := snap.Resets["codex_credit_limit"].Unix(); got != 1783027200 {
		t.Fatalf("expected reset 1783027200, got %d", got)
	}
	if snap.Raw["credit_limit_source"] != "live" {
		t.Fatalf("expected live credit source, got %q", snap.Raw["credit_limit_source"])
	}
}
