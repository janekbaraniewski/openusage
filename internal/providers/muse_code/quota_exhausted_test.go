package muse_code

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func seedQuotaMemory(t *testing.T, mem museQuotaMemory) {
	t.Helper()
	museQuotaMemoryMu.Lock()
	old := museQuotaMemoryState
	museQuotaMemoryState = mem
	museQuotaMemoryMu.Unlock()
	t.Cleanup(func() {
		museQuotaMemoryMu.Lock()
		museQuotaMemoryState = old
		museQuotaMemoryMu.Unlock()
	})
}

func stub429(t *testing.T, resetsAt int64) {
	t.Helper()
	stubMuseAPIKey(t, "test-key", true)
	body := fmt.Sprintf(`{"error":{"code":"rate_limit_exceeded","message":"Subscription quota exhausted.","resets_at":%d}}`, resetsAt)
	stubQuotaTransport(t, func(r *http.Request) (*http.Response, error) {
		return quotaTestResponse(http.StatusTooManyRequests, body), nil
	})
}

func metricUsed(t *testing.T, snap *core.UsageSnapshot, key string) float64 {
	t.Helper()
	m, ok := snap.Metrics[key]
	if !ok || m.Used == nil {
		t.Fatalf("metric %q missing", key)
	}
	return *m.Used
}

// A 429 whose reset is far sooner than the remembered weekly reset is the
// session window exhausting, not the week: the weekly bar must keep its
// remembered value (marked stale) instead of flipping to 100%.
func TestTrySubscriptionUsage429_SessionExhaustedKeepsWeeklyMemory(t *testing.T) {
	now := time.Now()
	seedQuotaMemory(t, museQuotaMemory{
		weeklyUsed:      68,
		weeklyResetUnix: now.Add(6 * 24 * time.Hour).Unix(),
		tier:            "tier-123",
		observedAt:      now.Add(-time.Hour),
	})
	stub429(t, now.Add(3*time.Hour).Unix()) // session reset, hours out

	snap := core.NewUsageSnapshot("muse_code", "muse-code")
	if !trySubscriptionUsage(context.Background(), &snap) {
		t.Fatal("expected decided outcome")
	}

	if got := metricUsed(t, &snap, "muse.session"); got != 100 {
		t.Fatalf("muse.session = %v, want 100 (exhausted window)", got)
	}
	if got := metricUsed(t, &snap, "muse.weekly"); got != 68 {
		t.Fatalf("muse.weekly = %v, want remembered 68", got)
	}
	if _, ok := snap.Diagnostics["muse_quota_weekly_stale"]; !ok {
		t.Fatal("expected muse_quota_weekly_stale diagnostic")
	}
	if got := snap.Resets["muse.session"].Unix(); got != now.Add(3*time.Hour).Unix() {
		t.Fatalf("muse.session reset = %v, want 429 reset", got)
	}
}

// A 429 resetting at (about) the remembered weekly reset is genuinely the
// week: weekly goes to 100% as before.
func TestTrySubscriptionUsage429_WeeklyExhausted(t *testing.T) {
	now := time.Now()
	weeklyReset := now.Add(6 * 24 * time.Hour).Unix()
	seedQuotaMemory(t, museQuotaMemory{
		weeklyUsed:      99,
		weeklyResetUnix: weeklyReset,
		observedAt:      now.Add(-time.Hour),
	})
	stub429(t, weeklyReset)

	snap := core.NewUsageSnapshot("muse_code", "muse-code")
	if !trySubscriptionUsage(context.Background(), &snap) {
		t.Fatal("expected decided outcome")
	}
	if got := metricUsed(t, &snap, "muse.weekly"); got != 100 {
		t.Fatalf("muse.weekly = %v, want 100", got)
	}
	if _, ok := snap.Diagnostics["muse_quota_weekly_stale"]; ok {
		t.Fatal("unexpected stale diagnostic on genuine weekly exhaustion")
	}
}

// No memory (first poll is a 429): keep the legacy weekly-100% fallback
// rather than guessing the window.
func TestTrySubscriptionUsage429_NoMemoryFallsBackToWeekly(t *testing.T) {
	seedQuotaMemory(t, museQuotaMemory{})
	stub429(t, time.Now().Add(3*time.Hour).Unix())

	snap := core.NewUsageSnapshot("muse_code", "muse-code")
	if !trySubscriptionUsage(context.Background(), &snap) {
		t.Fatal("expected decided outcome")
	}
	if got := metricUsed(t, &snap, "muse.weekly"); got != 100 {
		t.Fatalf("muse.weekly = %v, want legacy 100 fallback", got)
	}
}

// Expired memory (remembered weekly reset already passed) must not classify.
func TestTrySubscriptionUsage429_ExpiredMemoryIgnored(t *testing.T) {
	now := time.Now()
	seedQuotaMemory(t, museQuotaMemory{
		weeklyUsed:      68,
		weeklyResetUnix: now.Add(-time.Hour).Unix(),
		observedAt:      now.Add(-7 * 24 * time.Hour),
	})
	stub429(t, now.Add(3*time.Hour).Unix())

	snap := core.NewUsageSnapshot("muse_code", "muse-code")
	if !trySubscriptionUsage(context.Background(), &snap) {
		t.Fatal("expected decided outcome")
	}
	if got := metricUsed(t, &snap, "muse.weekly"); got != 100 {
		t.Fatalf("muse.weekly = %v, want legacy 100 fallback", got)
	}
}
