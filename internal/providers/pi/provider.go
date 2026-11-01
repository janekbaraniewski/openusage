// Package pi implements a local-data provider that reads JSONL session
// transcripts from per-workspace directories under the user's home and
// aggregates per-model token totals. No network calls are made and no
// authentication is required.
package pi

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

const ID = "pi"

const DefaultAccountID = "pi"

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
				Name:         "Pi",
				Capabilities: []string{"local_stats", "session_tracking", "model_tokens"},
				DocURL:       "https://github.com/badlogic/pi-mono",
			},
			Auth: core.ProviderAuthSpec{
				Type:             core.ProviderAuthTypeLocal,
				DefaultAccountID: DefaultAccountID,
			},
			Setup: core.ProviderSetupSpec{
				Quickstart: []string{
					"Install Pi and run at least one session.",
					"openusage auto-detects ~/.pi/agent/sessions and ~/.omp/agent/sessions; no configuration required.",
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
	if len(dirs) == 0 {
		snap.Status = core.StatusUnknown
		snap.Message = "Pi sessions directory not found"
		return snap, nil
	}
	snap.Raw["sessions_dirs"] = strings.Join(dirs, string(os.PathListSeparator))

	entries, err := readAllSessions(ctx, dirs)
	if err != nil {
		snap.SetDiagnostic("walk_error", err.Error())
		snap.Status = core.StatusError
		snap.Message = "Failed to read Pi sessions directory"
		return snap, err
	}
	if len(entries) == 0 {
		snap.Status = core.StatusOK
		snap.Message = "No Pi sessions recorded"
		return snap, nil
	}

	populateSnapshot(&snap, entries, p.now())
	snap.Status = core.StatusOK
	snap.Message = buildStatusMessage(snap)
	return snap, nil
}

func readAllSessions(ctx context.Context, dirs []string) ([]piModelEntry, error) {
	var all []piModelEntry
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
			if ctx.Err() != nil {
				return ctx.Err()
			}
			canonical := canonicalPath(path)
			if _, dup := seen[canonical]; dup {
				return nil
			}
			seen[canonical] = struct{}{}

			entries, _, perFileErr := readPiSessionFile(path)
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

func populateSnapshot(snap *core.UsageSnapshot, entries []piModelEntry, now time.Time) {
	type modelTotals struct {
		input      int64
		output     int64
		cacheRead  int64
		cacheWrite int64
		requests   int64
		cost       float64
		hasCost    bool
	}

	perModel := make(map[string]*modelTotals)
	perProvider := make(map[string]string)
	perWorkspace := make(map[string]string)
	sessions := make(map[string]struct{})

	var (
		totalInput      int64
		totalOutput     int64
		totalCacheRead  int64
		totalCacheWrite int64
		totalCost       float64
		hasAnyCost      bool
	)

	today := now.UTC().Format("2006-01-02")
	cutoff7d := now.UTC().AddDate(0, 0, -7)
	var sessionsToday, sessions7d int64
	tokensByDay := make(map[string]float64)
	sessionsByDay := make(map[string]float64)
	sessionsSeenPerDay := make(map[string]map[string]struct{})
	costByDay := make(map[string]float64)

	for _, e := range entries {
		bucket, ok := perModel[e.Model]
		if !ok {
			bucket = &modelTotals{}
			perModel[e.Model] = bucket
		}
		bucket.input += e.Input
		bucket.output += e.Output
		bucket.cacheRead += e.CacheRead
		bucket.cacheWrite += e.CacheWrite
		bucket.requests++
		if e.HasCost {
			bucket.cost += e.CostUSD
			bucket.hasCost = true
			totalCost += e.CostUSD
			hasAnyCost = true
		}
		if perProvider[e.Model] == "" && e.Provider != "" {
			perProvider[e.Model] = e.Provider
		}
		if perWorkspace[e.Model] == "" && e.WorkspaceLabel != "" {
			perWorkspace[e.Model] = e.WorkspaceLabel
		}

		totalInput += e.Input
		totalOutput += e.Output
		totalCacheRead += e.CacheRead
		totalCacheWrite += e.CacheWrite

		if e.SessionID != "" {
			sessions[e.SessionID] = struct{}{}
		}

		if !e.Timestamp.IsZero() {
			day := e.Timestamp.UTC().Format("2006-01-02")
			tokensByDay[day] += float64(e.Input + e.Output)
			if e.HasCost {
				costByDay[day] += e.CostUSD
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
					if !e.Timestamp.Before(cutoff7d) {
						sessions7d++
					}
				}
			}
		}
	}

	totalTokens := totalInput + totalOutput

	setUsedMetric(snap, "total_sessions", float64(len(sessions)), "sessions", allTimeWindow)
	setUsedMetric(snap, "sessions_today", float64(sessionsToday), "sessions", "today")
	setUsedMetric(snap, "sessions_7d", float64(sessions7d), "sessions", "7d")
	setUsedMetric(snap, "total_tokens", float64(totalTokens), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_input_tokens", float64(totalInput), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_output_tokens", float64(totalOutput), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_cache_read", float64(totalCacheRead), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_cache_write", float64(totalCacheWrite), "tokens", allTimeWindow)
	if hasAnyCost {
		setUsedMetric(snap, "total_cost_usd", totalCost, "USD", allTimeWindow)
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
			RawModelID:   model,
			RawSource:    "jsonl",
			Window:       allTimeWindow,
			InputTokens:  core.Float64Ptr(float64(bucket.input)),
			OutputTokens: core.Float64Ptr(float64(bucket.output)),
			CachedTokens: core.Float64Ptr(float64(bucket.cacheRead)),
			TotalTokens:  core.Float64Ptr(float64(bucket.input + bucket.output + bucket.cacheRead + bucket.cacheWrite)),
			Requests:     core.Float64Ptr(float64(bucket.requests)),
		}
		if bucket.hasCost {
			rec.CostUSD = core.Float64Ptr(bucket.cost)
		}
		if hint := perProvider[model]; hint != "" {
			rec.SetDimension("upstream_provider", hint)
		}
		if ws := perWorkspace[model]; ws != "" {
			rec.SetDimension("workspace_label", ws)
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
