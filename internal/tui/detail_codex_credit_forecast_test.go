package tui

import (
	"strings"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestBuildDetailCodexCreditForecastSection(t *testing.T) {
	used := 2572.322
	limit := 7500.0
	rate := 200.0
	runout := 24.638
	snap := core.UsageSnapshot{
		Metrics: map[string]core.Metric{
			"codex_credit_limit": {
				Used:  &used,
				Limit: &limit,
				Unit:  "credits",
			},
			"codex_credit_burn_rate":    {Used: &rate, Unit: "credits/hour"},
			"codex_credit_runout_hours": {Used: &runout, Unit: "h"},
		},
	}

	lines := buildDetailCodexCreditForecastSection(snap, 100)
	output := strings.Join(lines, "\n")
	for _, want := range []string{"Credit Usage", "2572 / 7500 credits (34%)", "200 credits/hour", "Credit Forecast", "1.0 days left"} {
		if !strings.Contains(output, want) {
			t.Errorf("expected forecast output to contain %q, got %q", want, output)
		}
	}
}
