// Package kimi_cli implements a local-data provider that reads usage
// telemetry from the per-session wire.jsonl files of Kimi CLI
// (~/.kimi/sessions/<group-id>/<session-uuid>/wire.jsonl) and Kimi Code CLI
// (~/.kimi-code/sessions/<group-id>/<session-uuid>/agents/<agent>/wire.jsonl).
//
// No network calls are made and no authentication is required. The
// companion config.json supplies the default model name when individual
// records don't include one.
package kimi_cli

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

// ID is the canonical provider identifier registered in the providers
// registry. Distinct from the API-key "moonshot" provider that reports
// quota for the Moonshot API directly.
const ID = "kimi_cli"

// DefaultAccountID is the account ID used by the auto-detector when it
// registers a local install.
const DefaultAccountID = "kimi_cli"

const allTimeWindow = "all-time"

// Provider is a thin wrapper around providerbase.Base.
type Provider struct {
	providerbase.Base
	clock core.Clock
}

// New constructs a Kimi CLI provider with sensible widget defaults.
func New() *Provider {
	return &Provider{
		Base: providerbase.New(core.ProviderSpec{
			ID: ID,
			Info: core.ProviderInfo{
				Name:         "Kimi CLI",
				Capabilities: []string{"local_stats", "session_tracking", "model_tokens"},
				DocURL:       "https://github.com/MoonshotAI/kimi-cli",
			},
			Auth: core.ProviderAuthSpec{
				Type:             core.ProviderAuthTypeLocal,
				DefaultAccountID: DefaultAccountID,
			},
			Setup: core.ProviderSetupSpec{
				Quickstart: []string{
					"Install Kimi CLI or Kimi Code CLI and run at least one session.",
					"openusage auto-detects wire.jsonl under ~/.kimi/sessions and ~/.kimi-code/sessions; no configuration required.",
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

// HasChanged reports whether the sessions directory or config file have
// been modified since the given time.
func (p *Provider) HasChanged(acct core.AccountConfig, since time.Time) (bool, error) {
	paths := make([]string, 0, 2)
	if dir := resolveSessionsDir(acct); dir != "" {
		paths = append(paths, dir)
	}
	if cfg := resolveConfigPath(acct); cfg != "" {
		paths = append(paths, cfg)
	}
	if len(paths) == 0 {
		return false, nil
	}
	return shared.AnyPathModifiedAfter(paths, since), nil
}

// Fetch walks the sessions directory and aggregates per-model totals.
//
// Missing-directory is not an error: we return an Unknown-status snapshot
// with a friendly message.
func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
	if strings.TrimSpace(acct.Provider) == "" {
		acct.Provider = p.ID()
	}

	snap := core.NewUsageSnapshot(p.ID(), acct.ID)
	snap.Timestamp = p.now()
	snap.DailySeries = make(map[string][]core.TimePoint)

	dir := resolveSessionsDir(acct)
	if dir == "" {
		snap.Status = core.StatusUnknown
		snap.Message = "Kimi CLI sessions directory not found"
		return snap, nil
	}
	snap.Raw["sessions_dir"] = dir

	fallbackModel := readKimiConfigModel(resolveConfigPath(acct))

	entries, err := readAllSessions(ctx, dir, fallbackModel)
	if err != nil {
		snap.SetDiagnostic("walk_error", err.Error())
		snap.Status = core.StatusError
		snap.Message = "Failed to read Kimi CLI sessions directory"
		return snap, err
	}
	if len(entries) == 0 {
		snap.Status = core.StatusOK
		snap.Message = "No Kimi CLI sessions recorded"
		return snap, nil
	}

	populateSnapshot(&snap, entries, p.now())
	snap.Status = core.StatusOK
	snap.Message = buildStatusMessage(snap)
	return snap, nil
}

// readAllSessions walks the sessions directory and decodes every
// wire.jsonl file it finds.
func readAllSessions(ctx context.Context, dir, fallbackModel string) ([]kimiModelEntry, error) {
	var all []kimiModelEntry
	walkErr := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			// A single unreadable subdir shouldn't abort the walk.
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Base(path) != "wire.jsonl" {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		entries, perFileErr := readKimiWireFileWithModel(path, fallbackModel)
		if perFileErr != nil {
			return nil
		}
		all = append(all, entries...)
		return nil
	})
	if walkErr != nil {
		return all, walkErr
	}
	return all, nil
}

// populateSnapshot folds the per-record entries into the snapshot.
func populateSnapshot(snap *core.UsageSnapshot, entries []kimiModelEntry, now time.Time) {
	type modelTotals struct {
		input      int64
		output     int64
		cacheRead  int64
		cacheWrite int64
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
	)

	today := now.UTC().Format("2006-01-02")
	cutoff7d := now.UTC().AddDate(0, 0, -7)
	var sessionsToday, sessions7d int64
	tokensByDay := make(map[string]float64)
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
		bucket.cacheRead += e.CacheRead
		bucket.cacheWrite += e.CacheWrite
		bucket.requests++
		if perProvider[e.Model] == "" && e.Provider != "" {
			perProvider[e.Model] = e.Provider
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

	if len(sessionsByDay) > 0 {
		snap.DailySeries["sessions"] = core.SortedTimePoints(sessionsByDay)
	}
	if len(tokensByDay) > 0 {
		snap.DailySeries["tokens"] = core.SortedTimePoints(tokensByDay)
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
		if hint := perProvider[model]; hint != "" {
			rec.SetDimension("upstream_provider", hint)
		}
		snap.AppendModelUsage(rec)
	}
}

func buildStatusMessage(snap core.UsageSnapshot) string {
	parts := make([]string, 0, 2)
	if m, ok := snap.Metrics["total_sessions"]; ok && m.Used != nil && *m.Used > 0 {
		parts = append(parts, formatCount(*m.Used, "session"))
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
		return fmt.Sprintf("1 %s", noun)
	}
	return shared.FormatTokenCount(int(v)) + " " + noun + "s"
}
