// Package crush implements a local-data provider for the Charmbracelet
// Crush CLI agent. Crush stores per-project usage data in
// `<project>/.crush/crush.db` — one SQLite database per project root.
// This provider walks a configurable set of search roots to discover
// every project DB, then aggregates session-level token and cost data
// across them.
//
// Schema reference: github.com/charmbracelet/crush
//
//	internal/db/migrations/20250424200609_initial.sql (sessions, messages)
//	internal/db/migrations/20250627000000_add_provider_to_messages.sql
//	internal/db/connect.go                            (DB filename)
//	internal/config/config.go                         (".crush" data dir)
package crush

import (
	"context"
	"fmt"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

// ID is the canonical provider identifier registered in
// providers.AllProviders. Exposed as a const so external packages
// (detect, telemetry links, tests) can reference it without
// stringly-typed coupling.
const ID = "crush"

// DefaultAccountID is the account ID used by the auto-detector when it
// registers a local Crush install.
const DefaultAccountID = "crush"

// allTimeWindow is the window label we attach to every cumulative
// metric. Crush stores rolling per-session totals, so all top-level
// metrics are all-time; the only windowed metrics are the
// today/7d session-count derivations we compute below.
const allTimeWindow = "all-time"

// Provider is a thin wrapper around providerbase.Base.
type Provider struct {
	providerbase.Base
	clock core.Clock
}

// New constructs a Crush provider with sensible widget defaults.
func New() *Provider {
	return &Provider{
		Base: providerbase.New(core.ProviderSpec{
			ID: ID,
			Info: core.ProviderInfo{
				Name:         "Crush",
				Capabilities: []string{"local_stats", "session_tracking", "model_tokens", "per_project"},
				DocURL:       "https://github.com/charmbracelet/crush",
			},
			Auth: core.ProviderAuthSpec{
				Type:             core.ProviderAuthTypeLocal,
				DefaultAccountID: DefaultAccountID,
			},
			Setup: core.ProviderSetupSpec{
				Quickstart: []string{
					"Install Crush and run it at least once in a project directory to create `.crush/crush.db`.",
					"openusage walks your home directory and common code roots to find project DBs.",
					"Override the search roots with $OPENUSAGE_CRUSH_ROOTS (colon-separated paths).",
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

// HasChanged reports whether any of the resolved Crush DB files has
// been modified since the given time. Implementations are advisory; on
// any error we return changed=true so the next Fetch runs.
func (p *Provider) HasChanged(acct core.AccountConfig, since time.Time) (bool, error) {
	paths := resolveDBPaths(acct)
	if len(paths) == 0 {
		return false, nil
	}
	return shared.AnyPathModifiedAfter(paths, since), nil
}

// Fetch discovers every Crush DB visible to the account and aggregates
// session-level totals into a single UsageSnapshot.
//
// Missing-data is not an error: when no `.crush/crush.db` is found
// anywhere on disk we return an OK-ish StatusUnknown snapshot with a
// friendly message so the dashboard shows the provider as
// detected-but-quiet rather than failing.
func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
	if strings.TrimSpace(acct.Provider) == "" {
		acct.Provider = p.ID()
	}

	snap := core.NewUsageSnapshot(p.ID(), acct.ID)
	snap.Timestamp = p.now()
	snap.DailySeries = make(map[string][]core.TimePoint)

	dbPaths := resolveDBPaths(acct)
	if len(dbPaths) == 0 {
		snap.Status = core.StatusUnknown
		snap.Message = "No Crush project databases found"
		return snap, nil
	}
	for i, p := range dbPaths {
		snap.Raw[fmt.Sprintf("db_paths.%d", i)] = p
	}
	snap.Raw["db_count"] = fmt.Sprintf("%d", len(dbPaths))

	allSessions := make([]crushSession, 0, 64)
	var queryErrs []string
	for _, path := range dbPaths {
		sessions, err := querySessions(ctx, path)
		if err != nil {
			// One bad DB shouldn't blank the whole tile. Note the error,
			// continue with the rest.
			queryErrs = append(queryErrs, fmt.Sprintf("%s: %s", path, err.Error()))
			continue
		}
		allSessions = append(allSessions, sessions...)
	}

	if len(queryErrs) > 0 {
		snap.SetDiagnostic("query_errors", strings.Join(queryErrs, "; "))
	}

	if len(allSessions) == 0 && len(queryErrs) == len(dbPaths) {
		// Every DB errored out — surface as an error state.
		snap.Status = core.StatusError
		snap.Message = "Failed to read any Crush database"
		return snap, fmt.Errorf("crush: all %d databases failed to read", len(dbPaths))
	}

	populateSnapshot(&snap, allSessions, len(dbPaths), p.now())
	snap.Status = core.StatusOK
	snap.Message = buildStatusMessage(snap)
	return snap, nil
}

// populateSnapshot rolls the per-session records up into snapshot
// metrics, per-model usage records, and per-day series. Kept private
// and pure so it can be exercised without going through Fetch.
func populateSnapshot(snap *core.UsageSnapshot, sessions []crushSession, dbCount int, now time.Time) {
	type modelTotals struct {
		input    int64
		output   int64
		total    int64
		cost     float64
		hasCost  bool
		sessions int64
	}
	perModel := make(map[string]*modelTotals)
	perProvider := make(map[string]string) // model -> upstream provider hint

	var (
		totalInput    int64
		totalOutput   int64
		totalSessions int64
		totalCost     float64
		hasAnyCost    bool
	)

	today := now.UTC().Format("2006-01-02")
	cutoff7d := now.UTC().AddDate(0, 0, -7)
	var sessionsToday, sessions7d int64
	sessionsByDay := make(map[string]float64)
	tokensByDay := make(map[string]float64)
	costByDay := make(map[string]float64)

	for _, s := range sessions {
		modelKey := s.Model
		if modelKey == "" {
			modelKey = "unknown"
		}
		bucket, ok := perModel[modelKey]
		if !ok {
			bucket = &modelTotals{}
			perModel[modelKey] = bucket
		}
		bucket.input += s.PromptTokens
		bucket.output += s.CompletionTokens
		bucket.total += s.PromptTokens + s.CompletionTokens
		bucket.sessions++
		if s.HasCost {
			bucket.cost += s.Cost
			bucket.hasCost = true
		}
		if perProvider[modelKey] == "" && s.Provider != "" {
			perProvider[modelKey] = s.Provider
		}

		totalInput += s.PromptTokens
		totalOutput += s.CompletionTokens
		totalSessions++
		if s.HasCost {
			totalCost += s.Cost
			hasAnyCost = true
		}

		// Use CreatedAt for day attribution (matches how the user
		// thinks about "today's sessions"); fall back to UpdatedAt if
		// CreatedAt is missing.
		anchor := s.CreatedAt
		if anchor.IsZero() {
			anchor = s.UpdatedAt
		}
		if !anchor.IsZero() {
			day := anchor.UTC().Format("2006-01-02")
			sessionsByDay[day]++
			tokensByDay[day] += float64(s.PromptTokens + s.CompletionTokens)
			if s.HasCost {
				costByDay[day] += s.Cost
			}
			if day == today {
				sessionsToday++
			}
			if !anchor.Before(cutoff7d) {
				sessions7d++
			}
		}
	}

	setUsedMetric(snap, "total_sessions", float64(totalSessions), "sessions", allTimeWindow)
	setUsedMetric(snap, "sessions_today", float64(sessionsToday), "sessions", "today")
	setUsedMetric(snap, "sessions_7d", float64(sessions7d), "sessions", "7d")
	setUsedMetric(snap, "total_tokens", float64(totalInput+totalOutput), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_input_tokens", float64(totalInput), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_output_tokens", float64(totalOutput), "tokens", allTimeWindow)
	if hasAnyCost {
		setUsedMetric(snap, "total_cost_usd", totalCost, "USD", allTimeWindow)
	}
	if dbCount > 0 {
		setUsedMetric(snap, "total_projects", float64(dbCount), "projects", allTimeWindow)
	}

	if len(sessionsByDay) > 0 {
		snap.DailySeries["sessions"] = core.SortedTimePoints(sessionsByDay)
	}
	if len(tokensByDay) > 0 {
		snap.DailySeries["tokens"] = core.SortedTimePoints(tokensByDay)
	}
	if len(costByDay) > 0 {
		snap.DailySeries["cost_usd"] = core.SortedTimePoints(costByDay)
	}

	for model, bucket := range perModel {
		rec := core.ModelUsageRecord{
			RawModelID:   model,
			RawSource:    "sqlite",
			Window:       allTimeWindow,
			InputTokens:  core.Float64Ptr(float64(bucket.input)),
			OutputTokens: core.Float64Ptr(float64(bucket.output)),
			TotalTokens:  core.Float64Ptr(float64(bucket.total)),
			Requests:     core.Float64Ptr(float64(bucket.sessions)),
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

// buildStatusMessage returns the short human-readable summary shown in
// the dashboard tile.
func buildStatusMessage(snap core.UsageSnapshot) string {
	parts := make([]string, 0, 4)
	if m, ok := snap.Metrics["total_projects"]; ok && m.Used != nil && *m.Used > 0 {
		parts = append(parts, formatCount(*m.Used, "project"))
	}
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
