package muse_code

import (
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestApplyPlanNameOverride_WinsOverTier(t *testing.T) {
	acct := core.AccountConfig{ID: "muse-code", Provider: "muse_code"}
	acct.SetPath(PlanNamePathKey, "Everyday Usage")
	snap := core.NewUsageSnapshot("muse_code", "muse-code")
	snap.Raw["plan_name"] = "27681393394859588"

	applyPlanNameOverride(acct, &snap)

	if got := snap.Raw["plan_name"]; got != "Everyday Usage" {
		t.Fatalf("plan_name = %q, want user-attested override", got)
	}
}

func TestApplyPlanNameOverride_EmptyLeavesTier(t *testing.T) {
	acct := core.AccountConfig{ID: "muse-code", Provider: "muse_code"}
	snap := core.NewUsageSnapshot("muse_code", "muse-code")
	snap.Raw["plan_name"] = "27681393394859588"

	applyPlanNameOverride(acct, &snap)

	if got := snap.Raw["plan_name"]; got != "27681393394859588" {
		t.Fatalf("plan_name = %q, want tier passthrough untouched", got)
	}
}
