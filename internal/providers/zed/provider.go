// Package zed implements a local-data provider that reads usage telemetry
// from Zed's thread store (a SQLite database). Threads owned by the hosted
// "zed.dev" model provider contribute to the surfaced metrics; threads
// targeting local or self-hosted models are skipped because they have no
// billing implication.
//
// No network calls are made and no authentication is required. The threads
// database is opened in read-only, immutable SQLite mode so we never compete
// for the file lock with the live Zed process. The schema and path layout
// referenced here come from Zed's published data-locations documentation
// (zed-industries/zed).
package zed

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

// ID is the canonical provider identifier registered in the providers
// registry.
const ID = "zed"

// DefaultAccountID is the account ID used by the auto-detector when it
// registers a local install.
const DefaultAccountID = "zed"

const allTimeWindow = "all-time"

// Provider is a thin wrapper around providerbase.Base.
type Provider struct {
	providerbase.Base
	clock core.Clock
}

// New constructs a Zed provider with sensible widget defaults.
func New() *Provider {
	return &Provider{
		Base: providerbase.New(core.ProviderSpec{
			ID: ID,
			Info: core.ProviderInfo{
				Name:         "Zed",
				Capabilities: []string{"local_stats", "thread_tracking", "model_tokens"},
				DocURL:       "https://zed.dev/docs/",
			},
			Auth: core.ProviderAuthSpec{
				Type:             core.ProviderAuthTypeLocal,
				DefaultAccountID: DefaultAccountID,
			},
			Setup: core.ProviderSetupSpec{
				Quickstart: []string{
					"Open Zed and use the Agent panel against the hosted zed.dev models so the threads database is populated.",
					"openusage auto-detects threads.db at the OS-appropriate Zed data directory; no configuration required.",
				},
			},
			Dashboard: dashboardWidget(),
		}),
		clock: core.SystemClock{},
	}
}

// DetailWidget returns the standard coding-tool detail layout.
func (p *Provider) DetailWidget() core.DetailWidget {
	return detailWidget()
}

func (p *Provider) now() time.Time {
	if p != nil && p.clock != nil {
		return p.clock.Now()
	}
	return time.Now()
}

// HasChanged reports whether threads.db has been modified since the given
// time.
func (p *Provider) HasChanged(acct core.AccountConfig, since time.Time) (bool, error) {
	dbPath := resolveDBPath(acct)
	if dbPath == "" {
		return false, nil
	}
	return shared.AnyPathModifiedAfter([]string{dbPath}, since), nil
}

// Fetch reads threads.db (if present) and produces a UsageSnapshot.
//
// Missing-file is not an error: we return an OK-ish snapshot with no metrics
// and a friendly message so the dashboard surfaces the provider as
// detected-but-quiet rather than failing.
func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
	if strings.TrimSpace(acct.Provider) == "" {
		acct.Provider = p.ID()
	}

	snap := core.NewUsageSnapshot(p.ID(), acct.ID)
	snap.Timestamp = p.now()
	snap.DailySeries = make(map[string][]core.TimePoint)

	dbPath := resolveDBPath(acct)
	if dbPath == "" {
		snap.Status = core.StatusUnknown
		snap.Message = "Zed threads.db not found"
		return snap, nil
	}
	snap.Raw["db_path"] = dbPath

	threads, err := queryZedThreads(ctx, dbPath)
	if err != nil {
		snap.SetDiagnostic("query_error", err.Error())
		snap.Status = core.StatusError
		snap.Message = "Failed to read Zed threads.db"
		return snap, err
	}
	if len(threads) == 0 {
		snap.Status = core.StatusOK
		snap.Message = "No Zed threads recorded"
		return snap, nil
	}

	populateSnapshot(&snap, threads, p.now())
	snap.Status = core.StatusOK
	snap.Message = buildStatusMessage(snap)
	return snap, nil
}

// populateSnapshot folds per-thread records into the snapshot.
func populateSnapshot(snap *core.UsageSnapshot, threads []zedThread, now time.Time) {
	type modelTotals struct {
		input      int64
		output     int64
		cacheRead  int64
		cacheWrite int64
		reasoning  int64
		messages   int64
		threads    int64
	}

	perModel := make(map[string]*modelTotals)

	var (
		totalInput      int64
		totalOutput     int64
		totalCacheRead  int64
		totalCacheWrite int64
		totalReasoning  int64
		totalMessages   int64
	)

	today := now.UTC().Format("2006-01-02")
	cutoff7d := now.UTC().AddDate(0, 0, -7)
	var threadsToday, threads7d int64
	threadsByDay := make(map[string]float64)
	tokensByDay := make(map[string]float64)

	for _, t := range threads {
		bucket, ok := perModel[t.Model]
		if !ok {
			bucket = &modelTotals{}
			perModel[t.Model] = bucket
		}
		bucket.input += t.Input
		bucket.output += t.Output
		bucket.cacheRead += t.CacheRead
		bucket.cacheWrite += t.CacheWrite
		bucket.reasoning += t.Reasoning
		bucket.messages += t.MessageCount
		bucket.threads++

		totalInput += t.Input
		totalOutput += t.Output
		totalCacheRead += t.CacheRead
		totalCacheWrite += t.CacheWrite
		totalReasoning += t.Reasoning
		totalMessages += t.MessageCount

		if !t.Timestamp.IsZero() {
			day := t.Timestamp.UTC().Format("2006-01-02")
			threadsByDay[day]++
			tokensByDay[day] += float64(t.Input + t.Output + t.Reasoning)
			if day == today {
				threadsToday++
			}
			if !t.Timestamp.Before(cutoff7d) {
				threads7d++
			}
		}
	}

	totalTokens := totalInput + totalOutput + totalReasoning

	setUsedMetric(snap, "total_threads", float64(len(threads)), "threads", allTimeWindow)
	setUsedMetric(snap, "threads_today", float64(threadsToday), "threads", "today")
	setUsedMetric(snap, "threads_7d", float64(threads7d), "threads", "7d")
	setUsedMetric(snap, "total_tokens", float64(totalTokens), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_input_tokens", float64(totalInput), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_output_tokens", float64(totalOutput), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_cache_read", float64(totalCacheRead), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_cache_write", float64(totalCacheWrite), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_reasoning_tokens", float64(totalReasoning), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_messages", float64(totalMessages), "messages", allTimeWindow)

	if len(threadsByDay) > 0 {
		snap.DailySeries["threads"] = core.SortedTimePoints(threadsByDay)
	}
	if len(tokensByDay) > 0 {
		snap.DailySeries["tokens"] = core.SortedTimePoints(tokensByDay)
	}

	for model, bucket := range perModel {
		rec := core.ModelUsageRecord{
			RawModelID:      model,
			RawSource:       "sqlite",
			Window:          allTimeWindow,
			InputTokens:     core.Float64Ptr(float64(bucket.input)),
			OutputTokens:    core.Float64Ptr(float64(bucket.output)),
			CachedTokens:    core.Float64Ptr(float64(bucket.cacheRead)),
			ReasoningTokens: core.Float64Ptr(float64(bucket.reasoning)),
			TotalTokens:     core.Float64Ptr(float64(bucket.input + bucket.output + bucket.cacheRead + bucket.cacheWrite + bucket.reasoning)),
			Requests:        core.Float64Ptr(float64(bucket.threads)),
		}
		rec.SetDimension("upstream_provider", "zed.dev")
		snap.AppendModelUsage(rec)
	}
}

func buildStatusMessage(snap core.UsageSnapshot) string {
	parts := make([]string, 0, 3)
	if m, ok := snap.Metrics["total_threads"]; ok && m.Used != nil && *m.Used > 0 {
		parts = append(parts, formatCount(*m.Used, "thread"))
	}
	if m, ok := snap.Metrics["total_tokens"]; ok && m.Used != nil && *m.Used > 0 {
		parts = append(parts, shared.FormatTokenCount(int(*m.Used))+" tokens")
	}
	if len(parts) == 0 {
		return "OK"
	}
	return strings.Join(parts, ", ")
}

func setUsedMetric(snap *core.UsageSnapshot, key string, value float64, unit, window string) {
	if value <= 0 {
		return
	}
	v := value
	snap.Metrics[key] = core.Metric{
		Used:   &v,
		Unit:   unit,
		Window: window,
	}
}

func formatCount(v float64, noun string) string {
	if v == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%s %ss", shared.FormatTokenCount(int(v)), noun)
}
