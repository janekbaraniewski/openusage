// Package qwen_cli implements a local-data provider that reads usage
// telemetry from Qwen CLI's per-project chat transcripts at
// ~/.qwen/projects/<project>/chats/*.jsonl.
//
// No network calls are made and no authentication is required. Each line of
// a transcript file represents one event; assistant messages carry the
// usageMetadata block that this provider aggregates into per-model totals.
package qwen_cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

const (
	ID               = "qwen_cli"
	DefaultAccountID = "qwen_cli"

	allTimeWindow    = "all-time"
	defaultProvider  = "qwen"
	defaultModel     = "unknown"
	chatsSubdir      = "chats"
	chatFileExt      = ".jsonl"
	chatFilenameMark = chatFileExt
)

type Provider struct {
	providerbase.Base
	clock core.Clock
}

func New() *Provider {
	return &Provider{
		Base: providerbase.New(core.ProviderSpec{
			ID: ID,
			Info: core.ProviderInfo{
				Name:         "Qwen CLI",
				Capabilities: []string{"local_stats", "session_tracking", "model_tokens"},
				DocURL:       "https://github.com/QwenLM/qwen-cli",
			},
			Auth: core.ProviderAuthSpec{
				Type:             core.ProviderAuthTypeLocal,
				DefaultAccountID: DefaultAccountID,
			},
			Setup: core.ProviderSetupSpec{
				Quickstart: []string{
					"Install Qwen CLI and run at least one chat.",
					"openusage auto-detects ~/.qwen/projects/<project>/chats/*.jsonl; no configuration required.",
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
	dir := resolveProjectsDir(acct)
	if dir == "" {
		return false, nil
	}
	return shared.AnyPathModifiedAfter([]string{dir}, since), nil
}

func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
	if strings.TrimSpace(acct.Provider) == "" {
		acct.Provider = p.ID()
	}

	snap := core.NewUsageSnapshot(p.ID(), acct.ID)
	snap.Timestamp = p.now()
	snap.DailySeries = make(map[string][]core.TimePoint)

	dir := resolveProjectsDir(acct)
	if dir == "" {
		snap.Status = core.StatusUnknown
		snap.Message = "Qwen CLI projects directory not found"
		return snap, nil
	}
	snap.Raw["projects_dir"] = dir

	entries, err := readAllChats(ctx, dir)
	if err != nil {
		snap.SetDiagnostic("walk_error", err.Error())
		snap.Status = core.StatusError
		snap.Message = "Failed to read Qwen CLI projects directory"
		return snap, err
	}
	if len(entries) == 0 {
		snap.Status = core.StatusOK
		snap.Message = "No Qwen CLI chats recorded"
		return snap, nil
	}

	populateSnapshot(&snap, entries, p.now())
	snap.Status = core.StatusOK
	snap.Message = buildStatusMessage(snap)
	return snap, nil
}

func readAllChats(ctx context.Context, dir string) ([]qwenModelEntry, error) {
	var all []qwenModelEntry
	walkErr := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), chatFilenameMark) {
			return nil
		}
		if !pathHasChatsSegment(path) {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		entries, perFileErr := readQwenChatFile(path)
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

func pathHasChatsSegment(p string) bool {
	parts := strings.Split(filepath.ToSlash(p), "/")
	if len(parts) < 2 {
		return false
	}
	for _, segment := range parts[:len(parts)-1] {
		if segment == chatsSubdir {
			return true
		}
	}
	return false
}

func populateSnapshot(snap *core.UsageSnapshot, entries []qwenModelEntry, now time.Time) {
	type modelTotals struct {
		input     int64
		output    int64
		cached    int64
		reasoning int64
		requests  int64
	}

	perModel := make(map[string]*modelTotals)
	perProvider := make(map[string]string)
	sessions := make(map[string]struct{})

	var (
		totalInput     int64
		totalOutput    int64
		totalCached    int64
		totalReasoning int64
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
		bucket.cached += e.Cached
		bucket.reasoning += e.Reasoning
		bucket.requests++
		if perProvider[e.Model] == "" && e.Provider != "" {
			perProvider[e.Model] = e.Provider
		}

		totalInput += e.Input
		totalOutput += e.Output
		totalCached += e.Cached
		totalReasoning += e.Reasoning

		if e.SessionID != "" {
			sessions[e.SessionID] = struct{}{}
		}

		if !e.Timestamp.IsZero() {
			day := e.Timestamp.UTC().Format("2006-01-02")
			tokensByDay[day] += float64(e.Input + e.Output + e.Reasoning)
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

	totalTokens := totalInput + totalOutput + totalReasoning

	setUsedMetric(snap, "total_sessions", float64(len(sessions)), "sessions", allTimeWindow)
	setUsedMetric(snap, "sessions_today", float64(sessionsToday), "sessions", "today")
	setUsedMetric(snap, "sessions_7d", float64(sessions7d), "sessions", "7d")
	setUsedMetric(snap, "total_tokens", float64(totalTokens), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_input_tokens", float64(totalInput), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_output_tokens", float64(totalOutput), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_cache_read", float64(totalCached), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_reasoning_tokens", float64(totalReasoning), "tokens", allTimeWindow)

	if len(sessionsByDay) > 0 {
		snap.DailySeries["sessions"] = core.SortedTimePoints(sessionsByDay)
	}
	if len(tokensByDay) > 0 {
		snap.DailySeries["tokens"] = core.SortedTimePoints(tokensByDay)
	}

	for model, bucket := range perModel {
		rec := core.ModelUsageRecord{
			RawModelID:      model,
			RawSource:       "jsonl",
			Window:          allTimeWindow,
			InputTokens:     core.Float64Ptr(float64(bucket.input)),
			OutputTokens:    core.Float64Ptr(float64(bucket.output)),
			CachedTokens:    core.Float64Ptr(float64(bucket.cached)),
			ReasoningTokens: core.Float64Ptr(float64(bucket.reasoning)),
			TotalTokens:     core.Float64Ptr(float64(bucket.input + bucket.output + bucket.cached + bucket.reasoning)),
			Requests:        core.Float64Ptr(float64(bucket.requests)),
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
		return "1 " + noun
	}
	return shared.FormatTokenCount(int(v)) + " " + noun + "s"
}
