// Package codebuff implements a local-data provider that reads chat history
// from the Codebuff coding agent (formerly named manicode internally).
//
// Data lives under ~/.config/manicode{,-dev,-staging}/projects/<project>/
// chats/<chatId>/chat-messages.json. Each file is a JSON array of message
// objects; assistant messages carry token usage in one of several metadata
// locations.
package codebuff

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

const ID = "codebuff"

const DefaultAccountID = "codebuff"

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
				Name:         "Codebuff",
				Capabilities: []string{"local_stats", "session_tracking", "model_tokens"},
				DocURL:       "https://codebuff.com/",
			},
			Auth: core.ProviderAuthSpec{
				Type:             core.ProviderAuthTypeLocal,
				DefaultAccountID: DefaultAccountID,
			},
			Setup: core.ProviderSetupSpec{
				Quickstart: []string{
					"Install Codebuff and run at least one chat session.",
					"openusage auto-detects ~/.config/manicode/; no configuration required.",
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
	dirs := resolveDataDirs(acct)
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

	dirs := resolveDataDirs(acct)
	if len(dirs) == 0 {
		snap.Status = core.StatusUnknown
		snap.Message = "Codebuff data directory not found"
		return snap, nil
	}
	snap.Raw["data_dirs"] = strings.Join(dirs, ",")

	entries, err := readAllChats(ctx, dirs)
	if err != nil {
		snap.SetDiagnostic("walk_error", err.Error())
		snap.Status = core.StatusError
		snap.Message = "Failed to read Codebuff data directory"
		return snap, err
	}
	if len(entries) == 0 {
		snap.Status = core.StatusOK
		snap.Message = "No Codebuff chats recorded"
		return snap, nil
	}

	populateSnapshot(&snap, entries, p.now())
	snap.Status = core.StatusOK
	snap.Message = buildStatusMessage(snap)
	return snap, nil
}

func populateSnapshot(snap *core.UsageSnapshot, entries []codebuffEntry, now time.Time) {
	type modelTotals struct {
		input      int64
		output     int64
		cacheRead  int64
		cacheWrite int64
		credits    float64
		hasCredits bool
		requests   int64
	}

	perModel := make(map[string]*modelTotals)
	perProvider := make(map[string]string)
	chats := make(map[string]struct{})

	var (
		totalInput      int64
		totalOutput     int64
		totalCacheRead  int64
		totalCacheWrite int64
		totalCredits    float64
		hasAnyCredits   bool
	)

	today := now.UTC().Format("2006-01-02")
	cutoff7d := now.UTC().AddDate(0, 0, -7)
	var chatsToday, chats7d int64
	tokensByDay := make(map[string]float64)
	creditsByDay := make(map[string]float64)
	chatsByDay := make(map[string]float64)
	chatsSeenPerDay := make(map[string]map[string]struct{})

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
		if e.HasCredits {
			bucket.credits += e.Credits
			bucket.hasCredits = true
		}
		if perProvider[e.Model] == "" && e.Provider != "" {
			perProvider[e.Model] = e.Provider
		}

		totalInput += e.Input
		totalOutput += e.Output
		totalCacheRead += e.CacheRead
		totalCacheWrite += e.CacheWrite
		if e.HasCredits {
			totalCredits += e.Credits
			hasAnyCredits = true
		}

		chatKey := e.Channel + "/" + e.Project + "/" + e.ChatID
		if e.ChatID != "" {
			chats[chatKey] = struct{}{}
		}

		if !e.Timestamp.IsZero() {
			day := e.Timestamp.UTC().Format("2006-01-02")
			tokensByDay[day] += float64(e.Input + e.Output)
			if e.HasCredits {
				creditsByDay[day] += e.Credits
			}
			seen, ok := chatsSeenPerDay[day]
			if !ok {
				seen = make(map[string]struct{})
				chatsSeenPerDay[day] = seen
			}
			if e.ChatID != "" {
				if _, dup := seen[chatKey]; !dup {
					seen[chatKey] = struct{}{}
					chatsByDay[day]++
					if day == today {
						chatsToday++
					}
					if !e.Timestamp.Before(cutoff7d) {
						chats7d++
					}
				}
			}
		}
	}

	totalTokens := totalInput + totalOutput

	setUsedMetric(snap, "total_chats", float64(len(chats)), "chats", allTimeWindow)
	setUsedMetric(snap, "chats_today", float64(chatsToday), "chats", "today")
	setUsedMetric(snap, "chats_7d", float64(chats7d), "chats", "7d")
	setUsedMetric(snap, "total_messages", float64(len(entries)), "messages", allTimeWindow)
	setUsedMetric(snap, "total_tokens", float64(totalTokens), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_input_tokens", float64(totalInput), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_output_tokens", float64(totalOutput), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_cache_read", float64(totalCacheRead), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_cache_write", float64(totalCacheWrite), "tokens", allTimeWindow)
	if hasAnyCredits {
		setUsedMetric(snap, "total_credits", totalCredits, "credits", allTimeWindow)
	}

	if len(chatsByDay) > 0 {
		snap.DailySeries["sessions"] = core.SortedTimePoints(chatsByDay)
	}
	if len(tokensByDay) > 0 {
		snap.DailySeries["tokens"] = core.SortedTimePoints(tokensByDay)
	}
	if len(creditsByDay) > 0 {
		snap.DailySeries["credits"] = core.SortedTimePoints(creditsByDay)
	}

	for model, bucket := range perModel {
		rec := core.ModelUsageRecord{
			RawModelID:   model,
			RawSource:    "json",
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
	parts := make([]string, 0, 3)
	if m, ok := snap.Metrics["total_chats"]; ok && m.Used != nil && *m.Used > 0 {
		parts = append(parts, formatCount(*m.Used, "chat"))
	}
	if m, ok := snap.Metrics["total_tokens"]; ok && m.Used != nil && *m.Used > 0 {
		parts = append(parts, shared.FormatTokenCount(int(*m.Used))+" tokens")
	}
	if m, ok := snap.Metrics["total_credits"]; ok && m.Used != nil && *m.Used > 0 {
		parts = append(parts, fmt.Sprintf("%.0f credits", *m.Used))
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
