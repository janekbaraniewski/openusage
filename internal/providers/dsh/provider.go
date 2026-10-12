// Package dsh implements a local-data provider for dsh (DeepSeek Harness),
// a coding agent that logs every session to a Zstandard-compressed JSONL
// file at <dsh home>/sessions/<project-slug>/<session-id>/session.v4.jsonl.zstd.
//
// The provider reads token usage per model response and every completed tool
// call from those logs. Fetch aggregates them into the dsh tile; the
// telemetry source in telemetry.go emits them as events, attributed to the
// service that hosted each request. No network calls are made and no
// authentication is required.
package dsh

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

// ID is the canonical provider identifier registered in the providers
// registry.
const ID = "dsh"

// DefaultAccountID is the account ID used by the auto-detector when it
// registers a local install.
const DefaultAccountID = "dsh"

const allTimeWindow = "all-time"

// Provider is a thin wrapper around providerbase.Base. Fetch and Collect
// share one parse cache so a session log is decompressed once per change.
type Provider struct {
	providerbase.Base
	clock core.Clock

	sessions sessionCache

	// emitted records the file state each session log was last collected
	// at, so Collect returns a session's events only when its log changed.
	emittedMu sync.Mutex
	emitted   map[string]shared.FileSignature
}

// New constructs a dsh provider with sensible widget defaults.
func New() *Provider {
	return &Provider{
		Base: providerbase.New(core.ProviderSpec{
			ID: ID,
			Info: core.ProviderInfo{
				Name:         "dsh",
				Capabilities: []string{"local_stats", "session_tracking", "model_tokens"},
			},
			Auth: core.ProviderAuthSpec{
				Type:             core.ProviderAuthTypeLocal,
				DefaultAccountID: DefaultAccountID,
			},
			Setup: core.ProviderSetupSpec{
				Quickstart: []string{
					"Run at least one dsh session.",
					"openusage auto-detects session logs under ~/.dsh/sessions (or $DSH_HOME/sessions); no configuration required.",
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

// HasChanged reports whether any session log, or a directory holding one,
// was modified after since. Appending to a log does not touch its parent
// directories, so the logs themselves are checked.
func (p *Provider) HasChanged(acct core.AccountConfig, since time.Time) (bool, error) {
	dir := resolveSessionsDir(acct)
	if dir == "" {
		return false, nil
	}
	changed := false
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && d.Name() != sessionFileName {
			return nil
		}
		if info, statErr := d.Info(); statErr == nil && info.ModTime().After(since) {
			changed = true
			return filepath.SkipAll
		}
		return nil
	})
	return changed, nil
}

// Fetch aggregates every local session into the dsh tile.
//
// Missing-directory is not an error: we return an Unknown-status snapshot.
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
		snap.Message = "dsh sessions directory not found"
		return snap, nil
	}
	snap.Raw["sessions_dir"] = dir

	loaded, err := p.sessions.load(ctx, dir)
	if err != nil {
		snap.SetDiagnostic("walk_error", err.Error())
		snap.Status = core.StatusError
		snap.Message = "Failed to read dsh sessions directory"
		return snap, err
	}

	sessions := make([]*sessionData, 0, len(loaded))
	for _, l := range loaded {
		sessions = append(sessions, l.session)
	}
	populateSnapshot(&snap, sessions, p.now())

	snap.Status = core.StatusOK
	snap.Message = buildStatusMessage(snap)
	return snap, nil
}

// modelKey separates usage of one model reached through different hosts.
type modelKey struct {
	model    string
	upstream string
}

type modelTotals struct {
	input      int64
	output     int64
	cacheRead  int64
	cacheWrite int64
	reasoning  int64
	total      int64
	requests   int64
}

// populateSnapshot folds the parsed sessions into the snapshot.
func populateSnapshot(snap *core.UsageSnapshot, sessions []*sessionData, now time.Time) {
	perModel := make(map[modelKey]*modelTotals)
	var all modelTotals
	var toolCalls int64

	today := now.UTC().Format("2006-01-02")
	cutoff7d := now.UTC().AddDate(0, 0, -7)
	var totalSessions, sessionsToday, sessions7d int64
	tokensByDay := make(map[string]float64)
	messagesByDay := make(map[string]float64)
	sessionsByDay := make(map[string]float64)

	for _, session := range sessions {
		if session == nil || len(session.Messages) == 0 {
			continue
		}
		totalSessions++
		toolCalls += int64(len(session.Tools))

		activeDays := make(map[string]bool)
		activeIn7d := false
		for _, msg := range session.Messages {
			key := modelKey{model: msg.Model, upstream: msg.Upstream}
			if key.model == "" {
				key.model = "unknown"
			}
			bucket, ok := perModel[key]
			if !ok {
				bucket = &modelTotals{}
				perModel[key] = bucket
			}
			for _, totals := range []*modelTotals{bucket, &all} {
				totals.input += msg.Input
				totals.output += msg.Output
				totals.cacheRead += msg.CacheRead
				totals.cacheWrite += msg.CacheWrite
				totals.reasoning += msg.Reasoning
				totals.total += msg.Total
				totals.requests++
			}

			if msg.Time.IsZero() {
				continue
			}
			day := msg.Time.UTC().Format("2006-01-02")
			tokensByDay[day] += float64(msg.Input + msg.Output)
			messagesByDay[day]++
			activeDays[day] = true
			if !msg.Time.Before(cutoff7d) {
				activeIn7d = true
			}
		}

		for day := range activeDays {
			sessionsByDay[day]++
		}
		if activeDays[today] {
			sessionsToday++
		}
		if activeIn7d {
			sessions7d++
		}
	}

	setUsedMetric(snap, "total_sessions", float64(totalSessions), "sessions", allTimeWindow)
	setUsedMetric(snap, "sessions_today", float64(sessionsToday), "sessions", "today")
	setUsedMetric(snap, "sessions_7d", float64(sessions7d), "sessions", "7d")
	setUsedMetric(snap, "total_messages", float64(all.requests), "messages", allTimeWindow)
	setUsedMetric(snap, "total_tool_calls", float64(toolCalls), "calls", allTimeWindow)
	setUsedMetric(snap, "total_tokens", float64(all.input+all.output), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_input_tokens", float64(all.input), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_output_tokens", float64(all.output), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_cache_read", float64(all.cacheRead), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_cache_write", float64(all.cacheWrite), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_reasoning_tokens", float64(all.reasoning), "tokens", allTimeWindow)

	if len(sessionsByDay) > 0 {
		snap.DailySeries["sessions"] = core.SortedTimePoints(sessionsByDay)
	}
	if len(messagesByDay) > 0 {
		snap.DailySeries["messages"] = core.SortedTimePoints(messagesByDay)
	}
	if len(tokensByDay) > 0 {
		snap.DailySeries["tokens"] = core.SortedTimePoints(tokensByDay)
	}

	for key, bucket := range perModel {
		rec := core.ModelUsageRecord{
			RawModelID:      key.model,
			RawSource:       "jsonl",
			Window:          allTimeWindow,
			InputTokens:     core.Float64Ptr(float64(bucket.input)),
			OutputTokens:    core.Float64Ptr(float64(bucket.output)),
			CachedTokens:    core.Float64Ptr(float64(bucket.cacheRead)),
			ReasoningTokens: core.Float64Ptr(float64(bucket.reasoning)),
			TotalTokens:     core.Float64Ptr(float64(bucket.total)),
			Requests:        core.Float64Ptr(float64(bucket.requests)),
		}
		if key.upstream != "" {
			rec.SetDimension("upstream_provider", key.upstream)
		}
		snap.AppendModelUsage(rec)
	}
}

func buildStatusMessage(snap core.UsageSnapshot) string {
	parts := make([]string, 0, 3)
	if m, ok := snap.Metrics["total_sessions"]; ok && m.Used != nil && *m.Used > 0 {
		parts = append(parts, formatCount(*m.Used, "session"))
	}
	if m, ok := snap.Metrics["total_messages"]; ok && m.Used != nil && *m.Used > 0 {
		parts = append(parts, formatCount(*m.Used, "message"))
	}
	if m, ok := snap.Metrics["total_tokens"]; ok && m.Used != nil && *m.Used > 0 {
		parts = append(parts, shared.FormatTokenCount(int(*m.Used))+" tokens")
	}
	if len(parts) == 0 {
		return "No dsh sessions recorded"
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
