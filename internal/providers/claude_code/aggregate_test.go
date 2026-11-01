package claude_code

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/pricing"
)

// TestEstimateCost_AppliesTierAbove200k locks in the cost-accuracy fix: the
// per-message context length must be forwarded to the pricing layer so the
// long-context tier override is applied instead of the base rate.
func TestEstimateCost_AppliesTierAbove200k(t *testing.T) {
	prev := priceLookup
	var gotCtxLen int
	aboveInput := 6.0
	priceLookup = func(_ context.Context, _ string, ctxLen int) (*pricing.Price, error) {
		gotCtxLen = ctxLen
		return &pricing.Price{
			Source:               pricing.SourceHardcoded,
			InputCostPerMillion:  3.0,
			OutputCostPerMillion: 15.0,
			Tiers: pricing.TierOverrides{
				Above200k: &pricing.TierRates{InputCostPerMillion: &aboveInput},
			},
		}, nil
	}
	t.Cleanup(func() { priceLookup = prev })

	u := &jsonlUsage{InputTokens: 250_000} // 250k context -> above 200k tier
	cost := estimateCost("claude-sonnet-4-5", u)

	if gotCtxLen != 250_000 {
		t.Fatalf("priceLookup received ctxLen=%d, want 250000", gotCtxLen)
	}
	want := 250_000 * aboveInput / 1_000_000 // 1.5 at the tier rate, not 0.75 at base
	if math.Abs(cost-want) > 1e-6 {
		t.Errorf("estimateCost = %.6f, want %.6f (tier rate)", cost, want)
	}
}

// TestEstimateCost_ContextLenIncludesCacheTokens verifies cache tokens count
// toward the tier threshold.
func TestEstimateCost_ContextLenIncludesCacheTokens(t *testing.T) {
	prev := priceLookup
	var gotCtxLen int
	priceLookup = func(_ context.Context, _ string, ctxLen int) (*pricing.Price, error) {
		gotCtxLen = ctxLen
		return &pricing.Price{Source: pricing.SourceHardcoded, InputCostPerMillion: 1}, nil
	}
	t.Cleanup(func() { priceLookup = prev })

	u := &jsonlUsage{InputTokens: 50_000, CacheReadInputTokens: 100_000, CacheCreationInputTokens: 80_000}
	_ = estimateCost("claude-sonnet-4-5", u)
	if want := 230_000; gotCtxLen != want {
		t.Errorf("ctxLen = %d, want %d (input+cacheRead+cacheCreate)", gotCtxLen, want)
	}
}

func TestCostForRecord_Modes(t *testing.T) {
	c := 2.5
	withCost := conversationRecord{model: "claude-sonnet-4-5", usage: &jsonlUsage{InputTokens: 1_000_000}, costUSD: &c}
	noCost := conversationRecord{model: "claude-sonnet-4-5", usage: &jsonlUsage{InputTokens: 1_000_000}}

	// display trusts the logged cost, or 0 when absent.
	if got := costForRecord(withCost, CostModeDisplay, true); got != 2.5 {
		t.Errorf("display+cost = %v, want 2.5", got)
	}
	if got := costForRecord(noCost, CostModeDisplay, true); got != 0 {
		t.Errorf("display-no-cost = %v, want 0", got)
	}
	// auto prefers logged cost, falls back to computed.
	if got := costForRecord(withCost, CostModeAuto, true); got != 2.5 {
		t.Errorf("auto+cost = %v, want 2.5", got)
	}
	// offline calculate uses the embedded sonnet input rate ($3/M).
	if got := costForRecord(noCost, CostModeCalculate, true); math.Abs(got-3.0) > 1e-6 {
		t.Errorf("calculate offline = %v, want 3.0", got)
	}
	if got := costForRecord(noCost, CostModeAuto, true); math.Abs(got-3.0) > 1e-6 {
		t.Errorf("auto-no-cost offline = %v, want 3.0", got)
	}
}

func TestParseCostMode(t *testing.T) {
	cases := map[string]CostMode{
		"display":   CostModeDisplay,
		"auto":      CostModeAuto,
		"calculate": CostModeCalculate,
		"":          CostModeCalculate,
		"garbage":   CostModeCalculate,
	}
	for in, want := range cases {
		if got := ParseCostMode(in); got != want {
			t.Errorf("ParseCostMode(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestAggregateConversations_DedupAndSyntheticFilter(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "session.jsonl")
	lines := "" +
		// real turn
		`{"type":"assistant","sessionId":"s1","timestamp":"2026-06-01T10:00:00Z","requestId":"r1","cwd":"/home/u/proj","message":{"id":"m1","model":"claude-sonnet-4-5","usage":{"input_tokens":1000,"output_tokens":500}}}` + "\n" +
		// duplicate of the same turn (same requestId) -> must be deduped
		`{"type":"assistant","sessionId":"s1","timestamp":"2026-06-01T10:00:00Z","requestId":"r1","cwd":"/home/u/proj","message":{"id":"m1","model":"claude-sonnet-4-5","usage":{"input_tokens":1000,"output_tokens":500}}}` + "\n" +
		// synthetic turn -> must be skipped
		`{"type":"assistant","sessionId":"s1","timestamp":"2026-06-01T10:05:00Z","requestId":"r2","cwd":"/home/u/proj","message":{"id":"m2","model":"<synthetic>","usage":{"input_tokens":0,"output_tokens":0}}}` + "\n" +
		// second real turn, different request
		`{"type":"assistant","sessionId":"s1","timestamp":"2026-06-01T10:06:00Z","requestId":"r3","cwd":"/home/u/proj","message":{"id":"m3","model":"claude-opus-4-5","usage":{"input_tokens":2000,"output_tokens":100}}}` + "\n"
	if err := os.WriteFile(file, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}

	stats, err := AggregateConversations(AggregateOptions{TranscriptPath: file, Offline: true})
	if err != nil {
		t.Fatalf("AggregateConversations: %v", err)
	}
	if len(stats) != 2 {
		t.Fatalf("got %d stats, want 2 (dedup + synthetic filter)", len(stats))
	}
	if stats[0].Project != "proj" {
		t.Errorf("project label = %q, want proj", stats[0].Project)
	}
	if stats[0].Model != "claude-sonnet-4-5" || stats[1].Model != "claude-opus-4-5" {
		t.Errorf("models = %q,%q", stats[0].Model, stats[1].Model)
	}
	// offline sonnet: 1000*3/1e6 + 500*15/1e6 = 0.003 + 0.0075 = 0.0105
	if math.Abs(stats[0].Cost-0.0105) > 1e-6 {
		t.Errorf("sonnet turn cost = %.6f, want 0.0105", stats[0].Cost)
	}
}

// TestAggregateConversations_ModifiedSince covers the statusline/tmux fast
// path: files untouched since the cutoff are skipped unless KeepPath keeps
// them (the active session's logs).
func TestAggregateConversations_ModifiedSince(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	turn := func(sid, req string) string {
		return `{"type":"assistant","sessionId":"` + sid + `","timestamp":"2026-06-01T10:00:00Z","requestId":"` + req + `","message":{"id":"` + req + `","model":"claude-sonnet-4-5","usage":{"input_tokens":10,"output_tokens":5}}}` + "\n"
	}
	write := func(name, body string, mtime time.Time) {
		p := filepath.Join(proj, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	write("recent.jsonl", turn("recent", "r1"), now)
	write("old.jsonl", turn("old", "r2"), now.Add(-72*time.Hour))
	write("old-active-session.jsonl", turn("active", "r3"), now.Add(-72*time.Hour))

	sessions := func(opts AggregateOptions) map[string]bool {
		opts.ProjectsDir, opts.AltProjectsDir, opts.Offline = root, filepath.Join(root, "none"), true
		stats, err := AggregateConversations(opts)
		if err != nil {
			t.Fatalf("AggregateConversations: %v", err)
		}
		got := map[string]bool{}
		for _, s := range stats {
			got[s.Session] = true
		}
		return got
	}

	if got := sessions(AggregateOptions{}); len(got) != 3 {
		t.Fatalf("no cutoff: got sessions %v, want all 3", got)
	}
	got := sessions(AggregateOptions{ModifiedSince: now.Add(-24 * time.Hour)})
	if len(got) != 1 || !got["recent"] {
		t.Fatalf("cutoff: got sessions %v, want only recent", got)
	}
	got = sessions(AggregateOptions{
		ModifiedSince: now.Add(-24 * time.Hour),
		KeepPath:      func(p string) bool { return strings.Contains(p, "active-session") },
	})
	if len(got) != 2 || !got["recent"] || !got["active"] {
		t.Fatalf("cutoff+keep: got sessions %v, want recent and active", got)
	}
}

// TestConversationRecordFromLine_PrefilterKeepsAssistant guards the cheap
// byte pre-filter: assistant lines (compact or spaced JSON) still parse, and
// non-assistant lines are rejected.
func TestConversationRecordFromLine_PrefilterKeepsAssistant(t *testing.T) {
	cases := []struct {
		line string
		want bool
	}{
		{`{"type":"assistant","timestamp":"2026-06-01T10:00:00Z","message":{"model":"m","usage":{"input_tokens":1}}}`, true},
		{`{"type": "assistant", "timestamp": "2026-06-01T10:00:00Z", "message": {"model": "m"}}`, true},
		{`{"type":"user","timestamp":"2026-06-01T10:00:00Z","message":{"role":"user","content":"hi"}}`, false},
	}
	for _, c := range cases {
		if _, ok := conversationRecordFromLine([]byte(c.line), 1, "f.jsonl", ""); ok != c.want {
			t.Errorf("conversationRecordFromLine(%s) ok=%v, want %v", c.line, ok, c.want)
		}
	}
}
