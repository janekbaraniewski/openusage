// Package droid implements a local-data provider that reads usage
// telemetry from Droid's per-session settings files at
// ~/.factory/sessions/<uuid>.settings.json.
//
// No network calls are made and no authentication is required. Each
// settings.json file corresponds to one session (UUID encoded in the
// filename) and is paired with a companion <uuid>.jsonl event log we only
// touch when the model name is missing from settings.
package droid

import (
	"context"
	"errors"
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
// registry.
const ID = "droid"

// DefaultAccountID is the account ID used by the auto-detector when it
// registers a local install.
const DefaultAccountID = "droid"

const allTimeWindow = "all-time"

// Provider is a thin wrapper around providerbase.Base.
type Provider struct {
	providerbase.Base
	clock core.Clock
}

// New constructs a Droid provider with sensible widget defaults.
func New() *Provider {
	return &Provider{
		Base: providerbase.New(core.ProviderSpec{
			ID: ID,
			Info: core.ProviderInfo{
				Name:         "Droid",
				Capabilities: []string{"local_stats", "session_tracking", "model_tokens"},
				DocURL:       "https://docs.factory.ai/",
			},
			Auth: core.ProviderAuthSpec{
				Type:             core.ProviderAuthTypeLocal,
				DefaultAccountID: DefaultAccountID,
			},
			Setup: core.ProviderSetupSpec{
				Quickstart: []string{
					"Install the Droid CLI and run at least one session.",
					"openusage auto-detects ~/.factory/sessions/*.settings.json; no configuration required.",
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

// HasChanged reports whether the sessions directory has been modified since
// the given time.
func (p *Provider) HasChanged(acct core.AccountConfig, since time.Time) (bool, error) {
	dir := resolveSessionsDir(acct)
	if dir == "" {
		return false, nil
	}
	return shared.AnyPathModifiedAfter([]string{dir}, since), nil
}

// Fetch walks the sessions directory and aggregates per-model totals.
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
		snap.Message = "Droid sessions directory not found"
		return snap, nil
	}
	snap.Raw["sessions_dir"] = dir

	sessions, parseErrors, err := readAllSessions(ctx, dir)
	if err != nil {
		snap.SetDiagnostic("walk_error", err.Error())
		snap.Status = core.StatusError
		snap.Message = "Failed to read Droid sessions directory"
		return snap, err
	}
	if parseErrors > 0 {
		snap.SetDiagnostic("parse_errors", fmt.Sprintf("%d", parseErrors))
	}
	if len(sessions) == 0 {
		snap.Status = core.StatusOK
		snap.Message = "No Droid sessions recorded"
		return snap, nil
	}

	populateSnapshot(&snap, sessions, p.now())
	snap.Status = core.StatusOK
	snap.Message = buildStatusMessage(snap)
	return snap, nil
}

// readAllSessions walks the sessions directory and parses every
// *.settings.json file it finds. The returned parseErrors count covers
// files that failed JSON unmarshalling.
func readAllSessions(ctx context.Context, dir string) ([]droidSession, int, error) {
	var (
		out         []droidSession
		parseErrors int
	)
	walkErr := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".settings.json") {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		sess, perFileErr := parseDroidSession(path)
		if perFileErr != nil {
			if errors.Is(perFileErr, errDroidParse) {
				parseErrors++
			}
			return nil
		}
		if sess == nil {
			return nil
		}
		out = append(out, *sess)
		return nil
	})
	if walkErr != nil {
		return out, parseErrors, walkErr
	}
	return out, parseErrors, nil
}

// populateSnapshot folds per-session records into the snapshot.
func populateSnapshot(snap *core.UsageSnapshot, sessions []droidSession, now time.Time) {
	type modelTotals struct {
		input      int64
		output     int64
		cacheRead  int64
		cacheWrite int64
		thinking   int64
		sessions   int64
	}

	perModel := make(map[string]*modelTotals)
	perProvider := make(map[string]string)

	var (
		totalInput      int64
		totalOutput     int64
		totalCacheRead  int64
		totalCacheWrite int64
		totalThinking   int64
	)

	today := now.UTC().Format("2006-01-02")
	cutoff7d := now.UTC().AddDate(0, 0, -7)
	var sessionsToday, sessions7d int64
	tokensByDay := make(map[string]float64)
	sessionsByDay := make(map[string]float64)

	for _, s := range sessions {
		bucket, ok := perModel[s.Model]
		if !ok {
			bucket = &modelTotals{}
			perModel[s.Model] = bucket
		}
		bucket.input += s.Input
		bucket.output += s.Output
		bucket.cacheRead += s.CacheRead
		bucket.cacheWrite += s.CacheWrite
		bucket.thinking += s.Thinking
		bucket.sessions++
		if perProvider[s.Model] == "" && s.Provider != "" {
			perProvider[s.Model] = s.Provider
		}

		totalInput += s.Input
		totalOutput += s.Output
		totalCacheRead += s.CacheRead
		totalCacheWrite += s.CacheWrite
		totalThinking += s.Thinking

		if !s.Timestamp.IsZero() {
			day := s.Timestamp.UTC().Format("2006-01-02")
			sessionsByDay[day]++
			tokensByDay[day] += float64(s.Input + s.Output + s.Thinking)
			if day == today {
				sessionsToday++
			}
			if !s.Timestamp.Before(cutoff7d) {
				sessions7d++
			}
		}
	}

	totalTokens := totalInput + totalOutput + totalThinking

	setUsedMetric(snap, "total_sessions", float64(len(sessions)), "sessions", allTimeWindow)
	setUsedMetric(snap, "sessions_today", float64(sessionsToday), "sessions", "today")
	setUsedMetric(snap, "sessions_7d", float64(sessions7d), "sessions", "7d")
	setUsedMetric(snap, "total_tokens", float64(totalTokens), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_input_tokens", float64(totalInput), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_output_tokens", float64(totalOutput), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_cache_read", float64(totalCacheRead), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_cache_write", float64(totalCacheWrite), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_reasoning_tokens", float64(totalThinking), "tokens", allTimeWindow)

	if len(sessionsByDay) > 0 {
		snap.DailySeries["sessions"] = core.SortedTimePoints(sessionsByDay)
	}
	if len(tokensByDay) > 0 {
		snap.DailySeries["tokens"] = core.SortedTimePoints(tokensByDay)
	}

	for model, bucket := range perModel {
		rec := core.ModelUsageRecord{
			RawModelID:      model,
			RawSource:       "json",
			Window:          allTimeWindow,
			InputTokens:     core.Float64Ptr(float64(bucket.input)),
			OutputTokens:    core.Float64Ptr(float64(bucket.output)),
			CachedTokens:    core.Float64Ptr(float64(bucket.cacheRead)),
			ReasoningTokens: core.Float64Ptr(float64(bucket.thinking)),
			TotalTokens:     core.Float64Ptr(float64(bucket.input + bucket.output + bucket.cacheRead + bucket.cacheWrite + bucket.thinking)),
			Requests:        core.Float64Ptr(float64(bucket.sessions)),
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
