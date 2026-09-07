// Package muse_code implements a local-data provider that scans the Muse
// Code CLI's session logs (session.jsonl files under the muse data dir) and
// aggregates per-model token totals, priced through the shared pricing
// engine. No network calls are made.
//
// Muse Code publishes no quota or spend API (every plausible api.meta.ai
// usage/billing path returns 404): there are no quota meters by design, only
// spend and activity. If Meta ever documents a quota endpoint, its meters
// belong in Fetch next to the spend aggregation below.
package muse_code

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

const ID = "muse_code"

const DefaultAccountID = "muse-code"

const allTimeWindow = "all-time"

type Provider struct {
	providerbase.Base
	clock core.Clock
}

func New() *Provider {
	return &Provider{
		Base: providerbase.New(core.ProviderSpec{
			ID: ID,
			Info: core.ProviderInfo{
				Name:         "Muse Code",
				Capabilities: []string{"local_stats", "session_tracking", "model_tokens"},
				DocURL:       "https://developer.meta.com/ai/models/muse-spark/",
			},
			Auth: core.ProviderAuthSpec{
				Type:             core.ProviderAuthTypeLocal,
				DefaultAccountID: DefaultAccountID,
			},
			Setup: core.ProviderSetupSpec{
				Quickstart: []string{
					"Install Muse Code, run `muse login`, and complete at least one session.",
					"openusage auto-detects the sessions dir and auth file; no configuration required.",
				},
			},
			Dashboard: dashboardWidget(),
		}),
		clock: core.SystemClock{},
	}
}

func (p *Provider) DetailWidget() core.DetailWidget {
	return detailWidget()
}

func (p *Provider) now() time.Time {
	if p != nil && p.clock != nil {
		return p.clock.Now()
	}
	return time.Now()
}

// HasCredential reports whether any Muse Code credential signal exists on
// this machine: an exported META_API_KEY, an explicit MUSE_AUTH_PATH file, or
// the auth.json written by `muse login` / `muse auth set`. Secrets are never
// read for content, only presence.
func HasCredential(acct core.AccountConfig) bool {
	if strings.TrimSpace(os.Getenv("META_API_KEY")) != "" {
		return true
	}
	return fileExists(authFilePath(acct))
}

func (p *Provider) HasChanged(acct core.AccountConfig, since time.Time) (bool, error) {
	dirs := resolveSessionsDirs(acct)
	if len(dirs) == 0 {
		return false, nil
	}
	return shared.AnyPathModifiedAfter(dirs, since), nil
}

func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
	if strings.TrimSpace(acct.Provider) == "" {
		acct.Provider = p.ID()
	}

	snap := core.NewUsageSnapshot(p.ID(), acct.ID)
	snap.Timestamp = p.now()
	snap.DailySeries = make(map[string][]core.TimePoint)

	dirs := resolveSessionsDirs(acct)
	if len(dirs) == 0 && !HasCredential(acct) {
		snap.Status = core.StatusAuth
		snap.Message = "Muse Code not detected (run `muse login` and complete a session)"
		return snap, nil
	}
	if len(dirs) > 0 {
		snap.Raw["sessions_dirs"] = strings.Join(dirs, string(os.PathListSeparator))
	}

	entries, err := readAllSessions(ctx, dirs)
	if err != nil {
		snap.SetDiagnostic("walk_error", err.Error())
		snap.Status = core.StatusError
		snap.Message = "Failed to read Muse sessions directory"
		return snap, err
	}
	if len(entries) == 0 {
		snap.Status = core.StatusOK
		snap.Message = "No Muse sessions recorded"
		return snap, nil
	}

	populateSnapshot(ctx, &snap, entries, p.now())
	snap.Status = core.StatusOK
	snap.Message = buildStatusMessage(snap)
	return snap, nil
}

func readAllSessions(ctx context.Context, dirs []string) ([]museModelEntry, error) {
	var all []museModelEntry
	seen := make(map[string]struct{})
	for _, dir := range dirs {
		walkErr := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if filepath.Ext(path) != ".jsonl" {
				return nil
			}
			if isSubagentTranscript(path) {
				return nil
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			canonical := canonicalPath(path)
			if _, dup := seen[canonical]; dup {
				return nil
			}
			seen[canonical] = struct{}{}

			entries, perFileErr := readMuseSessionFile(path)
			if perFileErr != nil {
				return nil
			}
			all = append(all, entries...)
			return nil
		})
		if walkErr != nil {
			return all, walkErr
		}
	}
	return all, nil
}

func canonicalPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		if abs, err := filepath.Abs(resolved); err == nil {
			return abs
		}
		return resolved
	}
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

// isSubagentTranscript reports whether path is a nested subagent transcript.
// Subagent steps are already mirrored into the parent session log, so
// counting both would double-count every delegated step.
func isSubagentTranscript(path string) bool {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for _, part := range parts {
		if part == "subagent" {
			return true
		}
	}
	return false
}

func populateSnapshot(ctx context.Context, snap *core.UsageSnapshot, entries []museModelEntry, now time.Time) {
	type modelTotals struct {
		input      int64
		output     int64
		reasoning  int64
		cacheRead  int64
		cacheWrite int64
		requests   int64
		cost       float64
		priced     bool
	}

	perModel := make(map[string]*modelTotals)
	sessions := make(map[string]struct{})
	unpriced := make(map[string]struct{})

	var (
		totalInput      int64
		totalOutput     int64
		totalReasoning  int64
		totalCacheRead  int64
		totalCacheWrite int64
		totalTokens     int64
		totalCost       float64
		todayCost       float64
	)

	today := now.UTC().Format("2006-01-02")
	cutoff7d := now.UTC().AddDate(0, 0, -7)
	var sessionsToday int64
	recentSessions := make(map[string]struct{})
	tokensByDay := make(map[string]float64)
	costByDay := make(map[string]float64)
	sessionsByDay := make(map[string]float64)
	sessionsSeenPerDay := make(map[string]map[string]struct{})

	for _, e := range entries {
		bucket, ok := perModel[e.Model]
		if !ok {
			bucket = &modelTotals{}
			perModel[e.Model] = bucket
		}
		bucket.input += e.Input
		bucket.output += e.Output
		bucket.reasoning += e.Reasoning
		bucket.cacheRead += e.CacheRead
		bucket.cacheWrite += e.CacheWrite
		bucket.requests++

		totalInput += e.Input
		totalOutput += e.Output
		totalReasoning += e.Reasoning
		totalCacheRead += e.CacheRead
		totalCacheWrite += e.CacheWrite
		totalTokens += e.TotalTokens

		if cost, ok := estimateEntryCost(ctx, e); ok {
			bucket.cost += cost
			bucket.priced = true
			totalCost += cost
		} else if e.TotalTokens > 0 {
			unpriced[e.Model] = struct{}{}
		}

		if e.SessionID != "" {
			sessions[e.SessionID] = struct{}{}
		}

		if e.Timestamp.IsZero() {
			continue
		}
		day := e.Timestamp.UTC().Format("2006-01-02")
		tokensByDay[day] += float64(e.TotalTokens)
		if cost, ok := estimateEntryCost(ctx, e); ok {
			costByDay[day] += cost
			if day == today {
				todayCost += cost
			}
		}
		seen, ok := sessionsSeenPerDay[day]
		if !ok {
			seen = make(map[string]struct{})
			sessionsSeenPerDay[day] = seen
		}
		if e.SessionID != "" {
			if _, dup := seen[e.SessionID]; !dup {
				seen[e.SessionID] = struct{}{}
				sessionsByDay[day]++
				if day == today {
					sessionsToday++
				}
			}
			if !e.Timestamp.Before(cutoff7d) {
				recentSessions[e.SessionID] = struct{}{}
			}
		}
	}

	setUsedMetric(snap, "total_sessions", float64(len(sessions)), "sessions", allTimeWindow)
	setUsedMetric(snap, "sessions_today", float64(sessionsToday), "sessions", "today")
	setUsedMetric(snap, "sessions_7d", float64(len(recentSessions)), "sessions", "7d")
	setUsedMetric(snap, "total_tokens", float64(totalTokens), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_input_tokens", float64(totalInput), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_output_tokens", float64(totalOutput), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_cache_read", float64(totalCacheRead), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_cache_write", float64(totalCacheWrite), "tokens", allTimeWindow)
	if totalCost > 0 {
		v := totalCost
		snap.Metrics["total_cost_usd"] = core.Metric{Used: &v, Unit: "USD", Window: allTimeWindow}
	}
	if todayCost > 0 {
		v := todayCost
		snap.Metrics["today_cost"] = core.Metric{Used: &v, Unit: "USD", Window: "today"}
	}
	if len(unpriced) > 0 {
		names := make([]string, 0, len(unpriced))
		for name := range unpriced {
			names = append(names, name)
		}
		snap.SetAttribute("unpriced_models", strings.Join(names, ", "))
	}

	if len(sessionsByDay) > 0 {
		snap.DailySeries["sessions"] = core.SortedTimePoints(sessionsByDay)
	}
	if len(tokensByDay) > 0 {
		snap.DailySeries["tokens"] = core.SortedTimePoints(tokensByDay)
	}
	if len(costByDay) > 0 {
		snap.DailySeries["cost"] = core.SortedTimePoints(costByDay)
	}

	for model, bucket := range perModel {
		rec := core.ModelUsageRecord{
			RawModelID:      model,
			RawSource:       "jsonl",
			Window:          allTimeWindow,
			InputTokens:     core.Float64Ptr(float64(bucket.input)),
			OutputTokens:    core.Float64Ptr(float64(bucket.output)),
			ReasoningTokens: core.Float64Ptr(float64(bucket.reasoning)),
			CachedTokens:    core.Float64Ptr(float64(bucket.cacheRead)),
			TotalTokens: core.Float64Ptr(float64(bucket.input + bucket.output +
				bucket.reasoning + bucket.cacheRead + bucket.cacheWrite)),
			Requests: core.Float64Ptr(float64(bucket.requests)),
		}
		if bucket.priced {
			rec.CostUSD = core.Float64Ptr(bucket.cost)
		}
		snap.AppendModelUsage(rec)
	}
}

func buildStatusMessage(snap core.UsageSnapshot) string {
	parts := make([]string, 0, 3)
	if m, ok := snap.Metrics["total_sessions"]; ok && m.Used != nil && *m.Used > 0 {
		parts = append(parts, formatCount(*m.Used, "session"))
	}
	if m, ok := snap.Metrics["total_tokens"]; ok && m.Used != nil && *m.Used > 0 {
		parts = append(parts, shared.FormatTokenCount(int(*m.Used))+" tokens")
	}
	if m, ok := snap.Metrics["total_cost_usd"]; ok && m.Used != nil && *m.Used > 0 {
		parts = append(parts, formatCostUSD(*m.Used))
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
	return shared.FormatTokenCount(int(v)) + " " + noun + "s"
}

func formatCostUSD(v float64) string {
	if v >= 1 {
		return fmt.Sprintf("$%.2f", v)
	}
	return fmt.Sprintf("$%.4f", v)
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
