package telemetry

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"

	_ "github.com/mattn/go-sqlite3"
)

type telemetryModelAgg struct {
	Model        string
	InputTokens  float64
	OutputTokens float64
	CachedTokens float64
	Reasoning    float64
	TotalTokens  float64
	CostUSD      float64
	Requests     float64
	Requests1d   float64
}

type telemetrySourceAgg struct {
	Source     string
	Requests   float64
	Requests1d float64
	Tokens     float64
	Input      float64
	Output     float64
	Cached     float64
	Reasoning  float64
	Sessions   float64
}

type telemetryProjectAgg struct {
	Project    string
	Requests   float64
	Requests1d float64
}

type telemetryToolAgg struct {
	Tool           string
	Calls          float64
	Calls1d        float64
	CallsOK        float64
	CallsOK1d      float64
	CallsError     float64
	CallsError1d   float64
	CallsAborted   float64
	CallsAborted1d float64
}

type telemetryMCPFunctionAgg struct {
	Function string
	Calls    float64
	Calls1d  float64
}

type telemetryMCPServerAgg struct {
	Server    string
	Calls     float64
	Calls1d   float64
	Functions []telemetryMCPFunctionAgg
}

type telemetryLanguageAgg struct {
	Language string
	Requests float64
}

type telemetryProviderAgg struct {
	Provider string
	CostUSD  float64
	Requests float64
	Input    float64
	Output   float64
}

type telemetryDayPoint struct {
	Day      string
	CostUSD  float64
	Requests float64
	Tokens   float64
}

type telemetryActivityAgg struct {
	Messages     float64
	Sessions     float64
	ToolCalls    float64
	InputTokens  float64
	OutputTokens float64
	CachedTokens float64
	ReasonTokens float64
	TotalTokens  float64
	TotalCost    float64
}

type telemetryCodeStatsAgg struct {
	FilesChanged float64
	LinesAdded   float64
	LinesRemoved float64
}

type telemetryUsageAgg struct {
	LastOccurred string
	EventCount   int64
	Scope        string
	AccountID    string
	Models       []telemetryModelAgg
	Providers    []telemetryProviderAgg
	Sources      []telemetrySourceAgg
	Projects     []telemetryProjectAgg
	Tools        []telemetryToolAgg
	MCPServers   []telemetryMCPServerAgg
	Languages    []telemetryLanguageAgg
	Activity     telemetryActivityAgg
	CodeStats    telemetryCodeStatsAgg
	Daily        []telemetryDayPoint
	ModelDaily   map[string][]core.TimePoint
	SourceDaily  map[string][]core.TimePoint
	ProjectDaily map[string][]core.TimePoint
	ClientDaily  map[string][]core.TimePoint
	ClientTokens map[string][]core.TimePoint
}

type usageFilter struct {
	ProviderIDs     []string
	AccountID       string
	Since           time.Time
	TodaySince      time.Time
	materializedTbl string
}

func applyCanonicalUsageViewWithDB(
	ctx context.Context,
	db *sql.DB,
	snaps map[string]core.UsageSnapshot,
	providerLinks map[string]string,
	since time.Time, todaySince time.Time, timeWindow core.TimeWindow,
) (map[string]core.UsageSnapshot, error) {
	if db == nil {
		return snaps, nil
	}

	out := make(map[string]core.UsageSnapshot, len(snaps))
	cache := make(map[string]*telemetryUsageAgg)

	activeStart := time.Now()
	telemetryActiveProviders := queryTelemetryActiveProviders(ctx, db)
	core.Tracef("[usage_view_perf] queryTelemetryActiveProviders: %dms", time.Since(activeStart).Milliseconds())

	for accountID, snap := range snaps {
		s := snap
		providerID := strings.TrimSpace(s.ProviderID)
		if providerID == "" {
			out[accountID] = s
			continue
		}
		accountScope := strings.TrimSpace(s.AccountID)
		if accountScope == "" {
			accountScope = strings.TrimSpace(accountID)
		}
		sourceProviders := telemetrySourceProvidersForTarget(providerID, providerLinks)
		if len(sourceProviders) == 0 {
			out[accountID] = s
			continue
		}

		cacheKey := strings.Join(sourceProviders, ",") + "|" + accountScope
		agg, ok := cache[cacheKey]
		if !ok {
			loaded, loadErr := loadUsageViewForProviderWithSources(ctx, db, sourceProviders, accountScope, since, todaySince)
			if loadErr != nil {
				return snaps, loadErr
			}
			cache[cacheKey] = loaded
			agg = loaded
		}
		if agg == nil || agg.EventCount == 0 {
			// Check if telemetry is active for this provider (has ANY events, just not in this window).
			hasTelemetry := false
			for _, sp := range sourceProviders {
				if telemetryActiveProviders[sp] {
					hasTelemetry = true
					break
				}
			}
			if hasTelemetry && agg != nil {
				// Telemetry is active but no events in this time window.
				// Strip stale all-time metrics so TUI shows "no data" placeholders.
				windowLabel := core.TimeWindowAll
				if !since.IsZero() && timeWindow != "" {
					windowLabel = timeWindow
				}
				applyUsageViewToSnapshot(&s, agg, windowLabel)
				out[accountID] = s
			} else {
				out[accountID] = s
			}
			continue
		}

		windowLabel := core.TimeWindowAll
		if !since.IsZero() && timeWindow != "" {
			windowLabel = timeWindow
		}
		applyUsageViewToSnapshot(&s, agg, windowLabel)
		out[accountID] = s
	}

	return out, nil
}

// queryTelemetryActiveProviders returns the set of provider IDs that have at least
// one telemetry event in the database, regardless of time window. This is used to
// distinguish providers that have a telemetry adapter (but may have no events in the
// current time window) from providers that have no telemetry at all.
func queryTelemetryActiveProviders(ctx context.Context, db *sql.DB) map[string]bool {
	out := make(map[string]bool)
	// Use raw provider_id (no LOWER/TRIM in SQL) so SQLite can resolve
	// the DISTINCT directly from idx_usage_events_type_provider index
	// without scanning every matching row.
	rows, err := db.QueryContext(ctx, `
		SELECT DISTINCT provider_id
		FROM usage_events
		WHERE event_type IN ('message_usage', 'tool_usage')
		  AND provider_id IS NOT NULL AND provider_id != ''
	`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var pid string
		if rows.Scan(&pid) == nil {
			pid = strings.ToLower(strings.TrimSpace(pid))
			if pid != "" {
				out[pid] = true
			}
		}
	}
	return out
}

func loadUsageViewForProviderWithSources(ctx context.Context, db *sql.DB, providerIDs []string, accountID string, since time.Time, todaySince time.Time) (*telemetryUsageAgg, error) {
	providerIDs = normalizeProviderIDs(providerIDs)
	if len(providerIDs) == 0 {
		return &telemetryUsageAgg{}, nil
	}
	accountID = strings.TrimSpace(accountID)

	if accountID != "" {
		scoped, err := loadUsageViewForFilter(ctx, db, usageFilter{
			ProviderIDs:     providerIDs,
			AccountID:       accountID,
			Since:      since,
			TodaySince: todaySince,
		})
		if err != nil {
			return nil, err
		}
		if scoped == nil {
			scoped = &telemetryUsageAgg{}
		}
		// If account-scoped query found events, use it.
		if scoped.EventCount > 0 {
			scoped.Scope = "account"
			scoped.AccountID = accountID
			return scoped, nil
		}
		// Fall through to provider-scoped query if no account-scoped events found.
	}

	fallback, err := loadUsageViewForFilter(ctx, db, usageFilter{
		ProviderIDs: providerIDs,
		Since:       since,
		TodaySince:  todaySince,
	})
	if err != nil {
		return nil, err
	}
	if fallback == nil {
		fallback = &telemetryUsageAgg{}
	}
	fallback.Scope = "provider"
	return fallback, nil
}

func loadUsageViewForFilter(ctx context.Context, db *sql.DB, filter usageFilter) (*telemetryUsageAgg, error) {
	filterStart := time.Now()
	agg := newTelemetryUsageAgg()

	matFilter, cleanup, err := materializeUsageFilter(ctx, db, filter)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	// Count from the materialized table.
	if err := validateMaterializedTable(matFilter.materializedTbl); err != nil {
		return nil, fmt.Errorf("loadUsageViewForFilter: %w", err)
	}
	countStart := time.Now()
	countQuery := fmt.Sprintf(`
		SELECT COALESCE(MAX(occurred_at), ''), COUNT(*)
		FROM %s
		WHERE event_type IN ('message_usage', 'tool_usage')
	`, matFilter.materializedTbl)
	if err := db.QueryRowContext(ctx, countQuery).Scan(&agg.LastOccurred, &agg.EventCount); err != nil {
		return nil, fmt.Errorf("canonical usage count query: %w", err)
	}
	core.Tracef("[usage_view_perf] countQuery: %dms (events=%d, providers=%v, since=%s)",
		time.Since(countStart).Milliseconds(), agg.EventCount, filter.ProviderIDs, filter.Since.Format(time.RFC3339))
	if agg.EventCount == 0 {
		return agg, nil
	}
	if err := loadMaterializedUsageAgg(ctx, db, matFilter, agg); err != nil {
		return nil, err
	}
	core.Tracef("[usage_view_perf] loadUsageViewForFilter TOTAL: %dms (providers=%v)", time.Since(filterStart).Milliseconds(), filter.ProviderIDs)
	return agg, nil
}

// parseMCPToolName extracts server and function from an MCP tool name.
