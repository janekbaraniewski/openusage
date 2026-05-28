// Package goose implements a local-data provider that reads usage telemetry
// from the upstream open-source AI agent's SQLite session store.
//
// The provider does not make network calls and does not require any auth. It
// reads the sessions.db file the host tool maintains on disk and surfaces
// per-model token counts, session counts, and (when present) accumulated
// cost. The schema is documented from the upstream project's public source.
package goose

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

// ID is the canonical provider identifier registered in the providers
// registry. Exposed as a const so external packages (detect, telemetry
// links, tests) can reference it without stringly-typed coupling.
const ID = "goose"

// DefaultAccountID is the account ID used by the auto-detector when
// it registers a local install.
const DefaultAccountID = "goose"

// allTimeWindow is the window label we attach to every metric. All numbers
// come from the cumulative session store; we don't compute per-window
// roll-ups beyond "today" and "7d" session counts.
const allTimeWindow = "all-time"

// Provider is a thin wrapper around providerbase.Base. The bulk of the
// work happens in Fetch.
type Provider struct {
	providerbase.Base
	clock core.Clock
}

// New constructs a Goose provider with sensible widget defaults.
func New() *Provider {
	return &Provider{
		Base: providerbase.New(core.ProviderSpec{
			ID: ID,
			Info: core.ProviderInfo{
				Name:         "Goose",
				Capabilities: []string{"local_stats", "session_tracking", "model_tokens"},
				DocURL:       "https://block.github.io/goose/",
			},
			Auth: core.ProviderAuthSpec{
				Type:             core.ProviderAuthTypeLocal,
				DefaultAccountID: DefaultAccountID,
			},
			Setup: core.ProviderSetupSpec{
				Quickstart: []string{
					"Install Goose and start at least one session so sessions.db is created.",
					"openusage auto-detects the database; no configuration required.",
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

// HasChanged reports whether the sessions.db file has been modified since
// the given time. Implementations are advisory; on any error we return
// changed=true so the next Fetch runs.
func (p *Provider) HasChanged(acct core.AccountConfig, since time.Time) (bool, error) {
	dbPath := resolveDBPath(acct)
	if dbPath == "" {
		return false, nil
	}
	return shared.AnyPathModifiedAfter([]string{dbPath}, since), nil
}

// Fetch reads sessions.db (if present) and produces a UsageSnapshot.
//
// Missing-file is not an error: we return an OK snapshot with an empty
// metrics map and a "no data" message so the dashboard shows the provider
// as detected-but-quiet rather than failing.
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
		snap.Message = "Goose sessions.db not found"
		return snap, nil
	}
	snap.Raw["db_path"] = dbPath

	queryResult, err := queryGooseSessions(ctx, dbPath)
	if err != nil {
		// Surface as a diagnostic so the UI doesn't blank out on a
		// transient locking error; preserve OK status if we have nothing
		// stale to report.
		snap.SetDiagnostic("query_error", err.Error())
		snap.Status = core.StatusError
		snap.Message = "Failed to read Goose sessions.db"
		return snap, err
	}
	if queryResult.SkippedRows > 0 {
		snap.SetDiagnostic("skipped_rows", fmt.Sprintf("%d", queryResult.SkippedRows))
	}

	if len(queryResult.Sessions) == 0 {
		snap.Status = core.StatusOK
		snap.Message = "No Goose sessions recorded"
		return snap, nil
	}

	populateSnapshot(&snap, queryResult.Sessions, p.now())
	snap.Status = core.StatusOK
	snap.Message = buildStatusMessage(snap)
	return snap, nil
}

// populateSnapshot aggregates the per-session records into snapshot
// metrics, per-model usage records, and daily series. Kept private and
// pure so it's trivially testable.
func populateSnapshot(snap *core.UsageSnapshot, sessions []gooseSession, now time.Time) {
	type modelTotals struct {
		input     int64
		output    int64
		total     int64
		reasoning int64
		cost      float64
		hasCost   bool
		sessions  int64
	}

	perModel := make(map[string]*modelTotals)
	perProvider := make(map[string]string) // model -> provider hint (first non-empty)

	var (
		totalInput     int64
		totalOutput    int64
		totalTotal     int64
		totalReasoning int64
		totalCost      float64
		hasAnyCost     bool
	)

	today := now.UTC().Format("2006-01-02")
	cutoff7d := now.UTC().AddDate(0, 0, -7)
	var sessionsToday, sessions7d int64
	sessionsByDay := make(map[string]float64)
	tokensByDay := make(map[string]float64)

	for _, s := range sessions {
		bucket, ok := perModel[s.Model]
		if !ok {
			bucket = &modelTotals{}
			perModel[s.Model] = bucket
		}
		bucket.input += s.InputTokens
		bucket.output += s.OutputTokens
		bucket.total += s.TotalTokens
		bucket.reasoning += s.ReasoningTokens
		bucket.sessions++
		if s.HasCost {
			bucket.cost += s.AccumulatedCost
			bucket.hasCost = true
		}
		if perProvider[s.Model] == "" && s.Provider != "" {
			perProvider[s.Model] = s.Provider
		}

		totalInput += s.InputTokens
		totalOutput += s.OutputTokens
		totalTotal += s.TotalTokens
		totalReasoning += s.ReasoningTokens
		if s.HasCost {
			totalCost += s.AccumulatedCost
			hasAnyCost = true
		}

		day := s.CreatedAt.UTC().Format("2006-01-02")
		sessionsByDay[day]++
		tokensByDay[day] += float64(s.TotalTokens)
		if day == today {
			sessionsToday++
		}
		if !s.CreatedAt.Before(cutoff7d) {
			sessions7d++
		}
	}

	setUsedMetric(snap, "total_sessions", float64(len(sessions)), "sessions", allTimeWindow)
	setUsedMetric(snap, "sessions_today", float64(sessionsToday), "sessions", "today")
	setUsedMetric(snap, "sessions_7d", float64(sessions7d), "sessions", "7d")
	setUsedMetric(snap, "total_tokens", float64(totalTotal), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_input_tokens", float64(totalInput), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_output_tokens", float64(totalOutput), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_reasoning_tokens", float64(totalReasoning), "tokens", allTimeWindow)
	if hasAnyCost {
		setUsedMetric(snap, "total_cost_usd", totalCost, "USD", allTimeWindow)
	}

	if len(sessionsByDay) > 0 {
		snap.DailySeries["sessions"] = core.SortedTimePoints(sessionsByDay)
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
			TotalTokens:     core.Float64Ptr(float64(bucket.total)),
			ReasoningTokens: core.Float64Ptr(float64(bucket.reasoning)),
			Requests:        core.Float64Ptr(float64(bucket.sessions)),
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

// buildStatusMessage produces the short human-readable summary shown in the
// dashboard message line.
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
