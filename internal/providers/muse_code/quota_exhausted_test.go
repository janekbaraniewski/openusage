package muse_code

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func seedQuotaMemory(t *testing.T, mem museQuotaMemory) {
	t.Helper()
	// Hermetic file layer: every test gets a fresh dir so the real
	// ~/.local/state file can neither leak in nor out.
	t.Setenv("MUSE_QUOTA_MEMORY_PATH", t.TempDir()+"/muse-quota-memory.json")
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

// A restart during a session block must not flip the week to 100%: with
// cold process memory but a persisted weekly reset far out, the 429 still
// attributes to the session window.
func TestTrySubscriptionUsage429_RestartKeepsWeeklyMemory(t *testing.T) {
	now := time.Now()
	stub429(t, now.Add(3*time.Hour).Unix()) // session reset, hours out
	seedQuotaMemory(t, museQuotaMemory{})   // cold process, hermetic file
	rememberQuotaMemory(&subscriptionUsage{
		Tier: "tier-123",
	})
	// Fill the rest the way a real event would (rememberQuotaMemory only
	// persists when Weekly.ResetsAt is set).
	persistQuotaMemory(museQuotaMemory{
		weeklyUsed:      68,
		weeklyResetUnix: now.Add(6 * 24 * time.Hour).Unix(),
		tier:            "tier-123",
		observedAt:      now.Add(-time.Hour),
	})

	snap := core.NewUsageSnapshot("muse_code", "muse-code")
	if !trySubscriptionUsage(context.Background(), &snap) {
		t.Fatal("expected decided outcome")
	}
	if got := metricUsed(t, &snap, "muse.session"); got != 100 {
		t.Fatalf("muse.session = %v, want 100 (exhausted window)", got)
	}
	if got := metricUsed(t, &snap, "muse.weekly"); got != 68 {
		t.Fatalf("muse.weekly = %v, want persisted 68", got)
	}
}

// A corrupt state file is indistinguishable from no memory: legacy fallback.
func TestTrySubscriptionUsage429_CorruptFileFallsBack(t *testing.T) {
	stub429(t, time.Now().Add(3*time.Hour).Unix())
	seedQuotaMemory(t, museQuotaMemory{})
	if err := os.WriteFile(os.Getenv("MUSE_QUOTA_MEMORY_PATH"), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	snap := core.NewUsageSnapshot("muse_code", "muse-code")
	if !trySubscriptionUsage(context.Background(), &snap) {
		t.Fatal("expected decided outcome")
	}
	if got := metricUsed(t, &snap, "muse.weekly"); got != 100 {
		t.Fatalf("muse.weekly = %v, want legacy 100 fallback", got)
	}
}

// A transient probe failure (not a 429, not a rejected key) must not wipe
// the gauges: remembered meters carry forward marked stale, like /usage.
func TestTrySubscriptionUsage_ProbeErrorCarriesMemory(t *testing.T) {
	now := time.Now()
	seedQuotaMemory(t, museQuotaMemory{
		weeklyUsed:      32,
		weeklyResetUnix: now.Add(6 * 24 * time.Hour).Unix(),
		windowUsed:      60,
		windowResetUnix: now.Add(2 * time.Hour).Unix(),
		tier:            "tier-123",
		observedAt:      now.Add(-10 * time.Minute),
	})
	stubMuseAPIKey(t, "test-key", true)
	stubQuotaTransport(t, func(r *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("transient network failure")
	})

	snap := core.NewUsageSnapshot("muse_code", "muse-code")
	if !trySubscriptionUsage(context.Background(), &snap) {
		t.Fatal("expected decided outcome")
	}
	if got := metricUsed(t, &snap, "muse.session"); got != 60 {
		t.Fatalf("muse.session = %v, want remembered 60", got)
	}
	if got := metricUsed(t, &snap, "muse.weekly"); got != 32 {
		t.Fatalf("muse.weekly = %v, want remembered 32", got)
	}
	if _, ok := snap.Diagnostics["muse_quota_stale"]; !ok {
		t.Fatal("expected muse_quota_stale diagnostic")
	}
	if got := snap.Message; !strings.Contains(got, "60%") || !strings.Contains(got, "32%") {
		t.Fatalf("message = %q, want remembered percents", got)
	}
}

// Same transient failure with no memory anywhere: today's behavior stands
// (diagnostic only, no fabricated meters).
func TestTrySubscriptionUsage_ProbeErrorWithoutMemory(t *testing.T) {
	seedQuotaMemory(t, museQuotaMemory{})
	stubMuseAPIKey(t, "test-key", true)
	stubQuotaTransport(t, func(r *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("transient network failure")
	})

	snap := core.NewUsageSnapshot("muse_code", "muse-code")
	if !trySubscriptionUsage(context.Background(), &snap) {
		t.Fatal("expected decided outcome")
	}
	if _, ok := snap.Metrics["muse.session"]; ok {
		t.Fatal("unexpected fabricated session meter")
	}
	if _, ok := snap.Metrics["muse.weekly"]; ok {
		t.Fatal("unexpected fabricated weekly meter")
	}
}

// A rejected key is not transient: no carry-forward, even with memory
// (the account behind the key may have changed).
func TestTrySubscriptionUsage_AuthRejectedIgnoresMemory(t *testing.T) {
	now := time.Now()
	seedQuotaMemory(t, museQuotaMemory{
		weeklyUsed:      32,
		weeklyResetUnix: now.Add(6 * 24 * time.Hour).Unix(),
		observedAt:      now.Add(-time.Hour),
	})
	stubMuseAPIKey(t, "test-key", true)
	stubQuotaTransport(t, func(r *http.Request) (*http.Response, error) {
		return quotaTestResponse(http.StatusUnauthorized, `{"error":"invalid key"}`), nil
	})

	snap := core.NewUsageSnapshot("muse_code", "muse-code")
	if !trySubscriptionUsage(context.Background(), &snap) {
		t.Fatal("expected decided outcome")
	}
	if _, ok := snap.Metrics["muse.weekly"]; ok {
		t.Fatal("unexpected carried meter on rejected key")
	}
	if _, ok := snap.Diagnostics["muse_quota_auth"]; !ok {
		t.Fatal("expected auth diagnostic")
	}
}

// Successful events persist the memory so the next process starts warm.
func TestRememberQuotaMemory_PersistsToFile(t *testing.T) {
	seedQuotaMemory(t, museQuotaMemory{})
	now := time.Now()
	sub := &subscriptionUsage{Tier: "tier-123"}
	sub.Weekly.UsedPercent = 68
	sub.Weekly.ResetsAt = now.Add(6 * 24 * time.Hour).Unix()
	sub.Window.UsedPercent = 18
	rememberQuotaMemory(sub)

	raw, err := os.ReadFile(os.Getenv("MUSE_QUOTA_MEMORY_PATH"))
	if err != nil {
		t.Fatalf("expected state file: %v", err)
	}
	var decoded struct {
		WeeklyUsed      float64 `json:"weekly_used"`
		WeeklyResetUnix int64   `json:"weekly_reset_unix"`
		Tier            string  `json:"tier"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("state file not JSON: %v", err)
	}
	if decoded.WeeklyUsed != 68 || decoded.WeeklyResetUnix != sub.Weekly.ResetsAt || decoded.Tier != "tier-123" {
		t.Fatalf("state file = %+v, want persisted values", decoded)
	}
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

// Percentage deduction: a 429 with a FRESH session window at 0% is weekly
// by elimination, even when the reset instant looks like a session reset.
// Reported integers are floored, so ≤98 cannot be exhausted.
func TestTrySubscriptionUsage429_FreshWindowZeroDeducesWeekly(t *testing.T) {
	now := time.Now()
	seedQuotaMemory(t, museQuotaMemory{
		weeklyUsed:      99,
		weeklyResetUnix: now.Add(6 * 24 * time.Hour).Unix(),
		windowUsed:      0,
		windowResetUnix: now.Add(4 * time.Hour).Unix(),
		tier:            "tier-123",
		observedAt:      now.Add(-time.Minute),
	})
	stub429(t, now.Add(3*time.Hour).Unix()) // reset-distance alone would say session

	snap := core.NewUsageSnapshot("muse_code", "muse-code")
	if !trySubscriptionUsage(context.Background(), &snap) {
		t.Fatal("expected decided outcome")
	}
	if got := metricUsed(t, &snap, "muse.weekly"); got != 100 {
		t.Fatalf("muse.weekly = %v, want 100 (fresh 0%% window cannot be exhausted)", got)
	}
}

// Symmetric direction: a fresh week at 50% cannot be exhausted either.
func TestTrySubscriptionUsage429_FreshWeeklyLowDeducesSession(t *testing.T) {
	now := time.Now()
	seedQuotaMemory(t, museQuotaMemory{
		weeklyUsed:      50,
		weeklyResetUnix: now.Add(6 * 24 * time.Hour).Unix(),
		windowUsed:      99,
		windowResetUnix: now.Add(4 * time.Hour).Unix(),
		tier:            "tier-123",
		observedAt:      now.Add(-time.Minute),
	})
	stub429(t, now.Add(6*24*time.Hour).Unix()) // reset-distance alone would say weekly

	snap := core.NewUsageSnapshot("muse_code", "muse-code")
	if !trySubscriptionUsage(context.Background(), &snap) {
		t.Fatal("expected decided outcome")
	}
	if got := metricUsed(t, &snap, "muse.session"); got != 100 {
		t.Fatalf("muse.session = %v, want 100 (fresh 50%% week cannot be exhausted)", got)
	}
}

// Stale memory must not place the block: same shape as the weekly case but
// observed an hour ago falls back to reset-distance (session here).
func TestTrySubscriptionUsage429_StaleMemorySkipsDeduction(t *testing.T) {
	now := time.Now()
	seedQuotaMemory(t, museQuotaMemory{
		weeklyUsed:      99,
		weeklyResetUnix: now.Add(6 * 24 * time.Hour).Unix(),
		windowUsed:      0,
		windowResetUnix: now.Add(4 * time.Hour).Unix(),
		tier:            "tier-123",
		observedAt:      now.Add(-time.Hour),
	})
	stub429(t, now.Add(3*time.Hour).Unix())

	snap := core.NewUsageSnapshot("muse_code", "muse-code")
	if !trySubscriptionUsage(context.Background(), &snap) {
		t.Fatal("expected decided outcome")
	}
	if got := metricUsed(t, &snap, "muse.session"); got != 100 {
		t.Fatalf("muse.session = %v, want reset-distance verdict without fresh meters", got)
	}
}

// v1 files carry the week only: a zero windowUsed with no window reset must
// not force the weekly verdict.
func TestTrySubscriptionUsage429_WeekOnlyMemorySkipsWindowRule(t *testing.T) {
	now := time.Now()
	seedQuotaMemory(t, museQuotaMemory{
		weeklyUsed:      99,
		weeklyResetUnix: now.Add(6 * 24 * time.Hour).Unix(),
		tier:            "tier-123",
		observedAt:      now.Add(-time.Minute),
	})
	stub429(t, now.Add(3*time.Hour).Unix())

	snap := core.NewUsageSnapshot("muse_code", "muse-code")
	if !trySubscriptionUsage(context.Background(), &snap) {
		t.Fatal("expected decided outcome")
	}
	if got := metricUsed(t, &snap, "muse.session"); got != 100 {
		t.Fatalf("muse.session = %v, want reset-distance verdict for week-only memory", got)
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
