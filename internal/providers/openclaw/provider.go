// Package openclaw implements a local-data provider that reads transcript
// JSONL files written by the OpenClaw AI coding agent under
// ~/.openclaw/agents/. Legacy directory aliases (~/.clawdbot, ~/.moltbot,
// ~/.moldbot) are also walked when present.
package openclaw

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

const ID = "openclaw"

const DefaultAccountID = "openclaw"

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
				Name:         "OpenClaw",
				Capabilities: []string{"local_stats", "session_tracking", "model_tokens", "cost_estimation"},
				DocURL:       "https://openclaw.ai/",
			},
			Auth: core.ProviderAuthSpec{
				Type:             core.ProviderAuthTypeLocal,
				DefaultAccountID: DefaultAccountID,
			},
			Setup: core.ProviderSetupSpec{
				Quickstart: []string{
					"Install OpenClaw and run at least one agent session.",
					"openusage auto-detects ~/.openclaw/agents/ (or its legacy aliases); no configuration required.",
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

// HasChanged reports whether any resolved agents directory has been modified
// since the given time.
func (p *Provider) HasChanged(acct core.AccountConfig, since time.Time) (bool, error) {
	dirs := resolveAgentsDirs(acct)
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

	dirs := resolveAgentsDirs(acct)
	if len(dirs) == 0 {
		snap.Status = core.StatusUnknown
		snap.Message = "OpenClaw agents directory not found"
		return snap, nil
	}
	snap.Raw["agents_dirs"] = strings.Join(dirs, string(os.PathListSeparator))

	entries, err := readAllEntries(ctx, dirs)
	if err != nil {
		snap.SetDiagnostic("walk_error", err.Error())
		snap.Status = core.StatusError
		snap.Message = "Failed to read OpenClaw agents directory"
		return snap, err
	}
	if len(entries) == 0 {
		snap.Status = core.StatusOK
		snap.Message = "No OpenClaw sessions recorded"
		return snap, nil
	}

	populateSnapshot(&snap, entries, p.now())
	snap.Status = core.StatusOK
	snap.Message = buildStatusMessage(snap)
	return snap, nil
}

// readAllEntries discovers and parses every transcript reachable from the
// given directories. Layout 1 (sessions.json index) is preferred per dir;
// otherwise the directory is scanned for *.jsonl files.
func readAllEntries(ctx context.Context, dirs []string) ([]openClawEntry, error) {
	var all []openClawEntry
	seenTranscripts := make(map[string]struct{})

	for _, dir := range dirs {
		if ctx.Err() != nil {
			return all, ctx.Err()
		}
		indexPath := filepath.Join(dir, "sessions.json")
		if fileExists(indexPath) {
			indexEntries, err := readOpenClawIndex(indexPath)
			if err != nil {
				return all, err
			}
			for _, ie := range indexEntries {
				if _, dup := seenTranscripts[ie.SessionFile]; dup {
					continue
				}
				seenTranscripts[ie.SessionFile] = struct{}{}
				e, err := readOpenClawTranscript(ie.SessionFile, ie.SessionID)
				if err != nil {
					continue
				}
				all = append(all, e...)
			}
			continue
		}

		walkErr := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if !strings.EqualFold(filepath.Ext(path), ".jsonl") {
				return nil
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if _, dup := seenTranscripts[path]; dup {
				return nil
			}
			seenTranscripts[path] = struct{}{}
			e, perFileErr := readOpenClawTranscript(path, "")
			if perFileErr != nil {
				return nil
			}
			all = append(all, e...)
			return nil
		})
		if walkErr != nil {
			return all, walkErr
		}
	}
	return all, nil
}

func populateSnapshot(snap *core.UsageSnapshot, entries []openClawEntry, now time.Time) {
	type modelTotals struct {
		input      int64
		output     int64
		cacheRead  int64
		cacheWrite int64
		cost       float64
		hasCost    bool
		requests   int64
	}

	perModel := make(map[string]*modelTotals)
	perProvider := make(map[string]string)
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
	costByDay := make(map[string]float64)
	sessionsByDay := make(map[string]float64)
	sessionsSeenPerDay := make(map[string]map[string]struct{})

	for _, e := range entries {
		modelKey := e.Model
		if modelKey == "" {
			modelKey = "unknown"
		}
		bucket, ok := perModel[modelKey]
		if !ok {
			bucket = &modelTotals{}
			perModel[modelKey] = bucket
		}
		bucket.input += e.Input
		bucket.output += e.Output
		bucket.cacheRead += e.CacheRead
		bucket.cacheWrite += e.CacheWrite
		bucket.requests++
		if e.HasCost {
			bucket.cost += e.CostUSD
			bucket.hasCost = true
		}
		if perProvider[modelKey] == "" && e.Provider != "" {
			perProvider[modelKey] = e.Provider
		}

		totalInput += e.Input
		totalOutput += e.Output
		totalCacheRead += e.CacheRead
		totalCacheWrite += e.CacheWrite
		if e.HasCost {
			totalCost += e.CostUSD
			hasAnyCost = true
		}

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
