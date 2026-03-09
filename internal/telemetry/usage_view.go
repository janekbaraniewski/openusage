package telemetry

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/samber/lo"

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
	TimeWindowHours int    // 0 = no filter
	materializedTbl string // if set, queries read from this temp table instead of rebuilding the CTE
}

func clientDimensionExpr() string {
	return `COALESCE(
		NULLIF(TRIM(
			COALESCE(
				json_extract(source_payload, '$.client'),
				json_extract(source_payload, '$.payload.client'),
				json_extract(source_payload, '$._normalized.client'),
				json_extract(source_payload, '$.cursor_source'),
				json_extract(source_payload, '$.source.client'),
				''
			)
		), ''),
		CASE
			WHEN LOWER(TRIM(source_system)) = 'codex' THEN 'CLI'
			ELSE NULL
		END,
		COALESCE(NULLIF(TRIM(source_system), ''), NULLIF(TRIM(workspace_id), ''), 'unknown')
	)`
}

func applyCanonicalUsageViewWithDB(
	ctx context.Context,
	db *sql.DB,
	snaps map[string]core.UsageSnapshot,
	providerLinks map[string]string,
	timeWindowHours int,
	timeWindow core.TimeWindow,
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
			loaded, loadErr := loadUsageViewForProviderWithSources(ctx, db, sourceProviders, accountScope, timeWindowHours)
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
				if timeWindowHours > 0 && timeWindow != "" {
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
		if timeWindowHours > 0 && timeWindow != "" {
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

func loadUsageViewForProviderWithSources(ctx context.Context, db *sql.DB, providerIDs []string, accountID string, timeWindowHours int) (*telemetryUsageAgg, error) {
	providerIDs = normalizeProviderIDs(providerIDs)
	if len(providerIDs) == 0 {
		return &telemetryUsageAgg{}, nil
	}
	accountID = strings.TrimSpace(accountID)

	if accountID != "" {
		scoped, err := loadUsageViewForFilter(ctx, db, usageFilter{
			ProviderIDs:     providerIDs,
			AccountID:       accountID,
			TimeWindowHours: timeWindowHours,
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
		ProviderIDs:     providerIDs,
		TimeWindowHours: timeWindowHours,
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
	agg := &telemetryUsageAgg{
		ModelDaily:   make(map[string][]core.TimePoint),
		SourceDaily:  make(map[string][]core.TimePoint),
		ProjectDaily: make(map[string][]core.TimePoint),
		ClientDaily:  make(map[string][]core.TimePoint),
		ClientTokens: make(map[string][]core.TimePoint),
	}

	// Materialize the deduped CTE into a temp table so subsequent queries
	// read from a flat table instead of rebuilding the 3-level CTE each time.
	usageCTE, whereArgs := dedupedUsageCTE(filter)
	tempTable := "_deduped_tmp"

	matStart := time.Now()
	_, _ = db.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", tempTable))
	materializeSQL := fmt.Sprintf("CREATE TEMP TABLE %s AS %s SELECT * FROM deduped_usage", tempTable, usageCTE)
	if _, err := db.ExecContext(ctx, materializeSQL, whereArgs...); err != nil {
		return nil, fmt.Errorf("materialize deduped usage: %w", err)
	}
	core.Tracef("[usage_view_perf] materialize temp table: %dms (providers=%v, windowHours=%d)",
		time.Since(matStart).Milliseconds(), filter.ProviderIDs, filter.TimeWindowHours)
	defer func() {
		_, _ = db.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", tempTable))
	}()

	// Create indexes on the temp table for the aggregation queries.
	// Compound (event_type, status) covers the most common WHERE pattern.
	_, _ = db.ExecContext(ctx, fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_deduped_event_status ON %s(event_type, status)", tempTable))
	_, _ = db.ExecContext(ctx, fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_deduped_occurred ON %s(occurred_at)", tempTable))

	// Count from the materialized table.
	countStart := time.Now()
	countQuery := fmt.Sprintf(`
		SELECT COALESCE(MAX(occurred_at), ''), COUNT(*)
		FROM %s
		WHERE event_type IN ('message_usage', 'tool_usage')
	`, tempTable)
	if err := db.QueryRowContext(ctx, countQuery).Scan(&agg.LastOccurred, &agg.EventCount); err != nil {
		return nil, fmt.Errorf("canonical usage count query: %w", err)
	}
	core.Tracef("[usage_view_perf] countQuery: %dms (events=%d, providers=%v, windowHours=%d)",
		time.Since(countStart).Milliseconds(), agg.EventCount, filter.ProviderIDs, filter.TimeWindowHours)
	if agg.EventCount == 0 {
		return agg, nil
	}

	// All subsequent queries use the materialized temp table.
	matFilter := filter
	matFilter.materializedTbl = tempTable

	trace := func(label string) func() {
		start := time.Now()
		return func() { core.Tracef("[usage_view_perf]   %s: %dms", label, time.Since(start).Milliseconds()) }
	}

	done := trace("queryModelAgg")
	models, err := queryModelAgg(ctx, db, matFilter)
	done()
	if err != nil {
		return nil, err
	}
	done = trace("querySourceAgg")
	sources, err := querySourceAgg(ctx, db, matFilter)
	done()
	if err != nil {
		return nil, err
	}
	done = trace("queryProjectAgg")
	projects, err := queryProjectAgg(ctx, db, matFilter)
	done()
	if err != nil {
		return nil, err
	}
	done = trace("queryToolAgg")
	tools, err := queryToolAgg(ctx, db, matFilter)
	done()
	if err != nil {
		return nil, err
	}
	done = trace("queryProviderAgg")
	providers, err := queryProviderAgg(ctx, db, matFilter)
	done()
	if err != nil {
		return nil, err
	}
	done = trace("queryLanguageAgg")
	languages, err := queryLanguageAgg(ctx, db, matFilter)
	done()
	if err != nil {
		return nil, err
	}
	done = trace("queryActivityAgg")
	activity, err := queryActivityAgg(ctx, db, matFilter)
	done()
	if err != nil {
		return nil, err
	}
	done = trace("queryCodeStatsAgg")
	codeStats, err := queryCodeStatsAgg(ctx, db, matFilter)
	done()
	if err != nil {
		return nil, err
	}
	done = trace("queryDailyTotals")
	daily, err := queryDailyTotals(ctx, db, matFilter)
	done()
	if err != nil {
		return nil, err
	}
	done = trace("queryDailyByDimension(model)")
	modelDaily, err := queryDailyByDimension(ctx, db, matFilter, "model")
	done()
	if err != nil {
		return nil, err
	}
	done = trace("queryDailyByDimension(source)")
	sourceDaily, err := queryDailyByDimension(ctx, db, matFilter, "source")
	done()
	if err != nil {
		return nil, err
	}
	done = trace("queryDailyByDimension(project)")
	projectDaily, err := queryDailyByDimension(ctx, db, matFilter, "project")
	done()
	if err != nil {
		return nil, err
	}
	done = trace("queryDailyByDimension(client)")
	clientDaily, err := queryDailyByDimension(ctx, db, matFilter, "client")
	done()
	if err != nil {
		return nil, err
	}
	done = trace("queryDailyClientTokens")
	clientTokens, err := queryDailyClientTokens(ctx, db, matFilter)
	done()
	if err != nil {
		return nil, err
	}

	agg.Models = models
	agg.Providers = providers
	agg.Sources = sources
	agg.Projects = projects
	agg.Tools = tools
	agg.MCPServers = buildMCPAgg(tools)
	agg.Languages = languages
	agg.Activity = activity
	agg.CodeStats = codeStats
	agg.Daily = daily
	agg.ModelDaily = modelDaily
	agg.SourceDaily = sourceDaily
	agg.ProjectDaily = projectDaily
	agg.ClientDaily = clientDaily
	agg.ClientTokens = clientTokens
	core.Tracef("[usage_view_perf] loadUsageViewForFilter TOTAL: %dms (providers=%v)", time.Since(filterStart).Milliseconds(), filter.ProviderIDs)
	return agg, nil
}

func queryModelAgg(ctx context.Context, db *sql.DB, filter usageFilter) ([]telemetryModelAgg, error) {
	usageCTE, whereArgs := dedupedUsageCTE(filter)
	query := usageCTE + `
		SELECT
			COALESCE(NULLIF(TRIM(COALESCE(model_canonical, model_raw)), ''), 'unknown') AS model_key,
			SUM(COALESCE(input_tokens, 0)) AS input_tokens,
			SUM(COALESCE(output_tokens, 0)) AS output_tokens,
			SUM(COALESCE(cache_read_tokens, 0) + COALESCE(cache_write_tokens, 0)) AS cached_tokens,
			SUM(COALESCE(reasoning_tokens, 0)) AS reasoning_tokens,
			SUM(COALESCE(total_tokens,
				COALESCE(input_tokens, 0) +
				COALESCE(output_tokens, 0) +
				COALESCE(reasoning_tokens, 0) +
				COALESCE(cache_read_tokens, 0) +
				COALESCE(cache_write_tokens, 0))) AS total_tokens,
			SUM(COALESCE(cost_usd, 0)) AS cost_usd,
			SUM(COALESCE(requests, 1)) AS requests,
			SUM(CASE WHEN date(occurred_at) = date('now') THEN COALESCE(requests, 1) ELSE 0 END) AS requests_today
		FROM deduped_usage
		WHERE 1=1
		  AND event_type = 'message_usage'
		  AND status != 'error'
		GROUP BY model_key
		ORDER BY total_tokens DESC, requests DESC
		LIMIT 500
	`
	rows, err := db.QueryContext(ctx, query, whereArgs...)
	if err != nil {
		return nil, fmt.Errorf("canonical usage model query: %w", err)
	}
	defer rows.Close()

	var out []telemetryModelAgg
	for rows.Next() {
		var row telemetryModelAgg
		if err := rows.Scan(
			&row.Model,
			&row.InputTokens,
			&row.OutputTokens,
			&row.CachedTokens,
			&row.Reasoning,
			&row.TotalTokens,
			&row.CostUSD,
			&row.Requests,
			&row.Requests1d,
		); err != nil {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

func querySourceAgg(ctx context.Context, db *sql.DB, filter usageFilter) ([]telemetrySourceAgg, error) {
	usageCTE, whereArgs := dedupedUsageCTE(filter)
	query := usageCTE + `
		SELECT
			` + clientDimensionExpr() + ` AS source_name,
			SUM(COALESCE(requests, 1)) AS requests,
			SUM(CASE WHEN date(occurred_at) = date('now') THEN COALESCE(requests, 1) ELSE 0 END) AS requests_today,
			SUM(COALESCE(total_tokens,
				COALESCE(input_tokens, 0) +
				COALESCE(output_tokens, 0) +
				COALESCE(reasoning_tokens, 0) +
				COALESCE(cache_read_tokens, 0) +
				COALESCE(cache_write_tokens, 0))) AS total_tokens,
			SUM(COALESCE(input_tokens, 0)) AS input_tokens,
			SUM(COALESCE(output_tokens, 0)) AS output_tokens,
			SUM(COALESCE(cache_read_tokens, 0) + COALESCE(cache_write_tokens, 0)) AS cached_tokens,
			SUM(COALESCE(reasoning_tokens, 0)) AS reasoning_tokens,
			COUNT(DISTINCT COALESCE(NULLIF(TRIM(session_id), ''), 'unknown')) AS sessions
		FROM deduped_usage
		WHERE 1=1
		  AND event_type = 'message_usage'
		  AND status != 'error'
		GROUP BY source_name
		ORDER BY requests DESC
		LIMIT 500
	`
	rows, err := db.QueryContext(ctx, query, whereArgs...)
	if err != nil {
		return nil, fmt.Errorf("canonical usage source query: %w", err)
	}
	defer rows.Close()

	var out []telemetrySourceAgg
	for rows.Next() {
		var row telemetrySourceAgg
		if err := rows.Scan(
			&row.Source,
			&row.Requests,
			&row.Requests1d,
			&row.Tokens,
			&row.Input,
			&row.Output,
			&row.Cached,
			&row.Reasoning,
			&row.Sessions,
		); err != nil {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

func queryProjectAgg(ctx context.Context, db *sql.DB, filter usageFilter) ([]telemetryProjectAgg, error) {
	usageCTE, whereArgs := dedupedUsageCTE(filter)
	query := usageCTE + `
		SELECT
			COALESCE(NULLIF(TRIM(workspace_id), ''), '') AS project_name,
			SUM(COALESCE(requests, 1)) AS requests,
			SUM(CASE WHEN date(occurred_at) = date('now') THEN COALESCE(requests, 1) ELSE 0 END) AS requests_today
		FROM deduped_usage
		WHERE 1=1
		  AND event_type = 'message_usage'
		  AND status != 'error'
		  AND NULLIF(TRIM(workspace_id), '') IS NOT NULL
		GROUP BY project_name
		ORDER BY requests DESC
		LIMIT 500
	`
	rows, err := db.QueryContext(ctx, query, whereArgs...)
	if err != nil {
		return nil, fmt.Errorf("canonical usage project query: %w", err)
	}
	defer rows.Close()

	var out []telemetryProjectAgg
	for rows.Next() {
		var row telemetryProjectAgg
		if err := rows.Scan(&row.Project, &row.Requests, &row.Requests1d); err != nil {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

func queryToolAgg(ctx context.Context, db *sql.DB, filter usageFilter) ([]telemetryToolAgg, error) {
	usageCTE, whereArgs := dedupedUsageCTE(filter)
	query := usageCTE + `
		SELECT
			COALESCE(NULLIF(TRIM(LOWER(tool_name)), ''), 'unknown') AS tool_name,
			SUM(COALESCE(requests, 1)) AS calls,
			SUM(CASE WHEN date(occurred_at) = date('now') THEN COALESCE(requests, 1) ELSE 0 END) AS calls_today,
			SUM(CASE WHEN status = 'ok' THEN COALESCE(requests, 1) ELSE 0 END) AS calls_ok,
			SUM(CASE WHEN date(occurred_at) = date('now') AND status = 'ok' THEN COALESCE(requests, 1) ELSE 0 END) AS calls_ok_today,
			SUM(CASE WHEN status = 'error' THEN COALESCE(requests, 1) ELSE 0 END) AS calls_error,
			SUM(CASE WHEN date(occurred_at) = date('now') AND status = 'error' THEN COALESCE(requests, 1) ELSE 0 END) AS calls_error_today,
			SUM(CASE WHEN status = 'aborted' THEN COALESCE(requests, 1) ELSE 0 END) AS calls_aborted,
			SUM(CASE WHEN date(occurred_at) = date('now') AND status = 'aborted' THEN COALESCE(requests, 1) ELSE 0 END) AS calls_aborted_today
		FROM deduped_usage
		WHERE 1=1
		  AND event_type = 'tool_usage'
		GROUP BY tool_name
		ORDER BY calls DESC
		LIMIT 500
	`
	rows, err := db.QueryContext(ctx, query, whereArgs...)
	if err != nil {
		return nil, fmt.Errorf("canonical usage tool query: %w", err)
	}
	defer rows.Close()

	var out []telemetryToolAgg
	for rows.Next() {
		var row telemetryToolAgg
		if err := rows.Scan(
			&row.Tool,
			&row.Calls,
			&row.Calls1d,
			&row.CallsOK,
			&row.CallsOK1d,
			&row.CallsError,
			&row.CallsError1d,
			&row.CallsAborted,
			&row.CallsAborted1d,
		); err != nil {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

func queryLanguageAgg(ctx context.Context, db *sql.DB, filter usageFilter) ([]telemetryLanguageAgg, error) {
	usageCTE, whereArgs := dedupedUsageCTE(filter)
	// Query file paths from usage events. Language is inferred in Go
	// from the file extension since SQLite lacks convenient path functions.
	//
	// File paths live in different locations depending on the source:
	//   - JSONL collector:  $.file or $.payload.file
	//   - Hook events:      $.tool_input.file_path (Read/Edit/Write)
	//                       $.tool_input.path (Grep/Glob)
	//   - Hook response:    $.tool_response.file.filePath (Read response)
	//   - Cursor tracking:  $.file or $.file_extension (message_usage events)
	query := usageCTE + `
		SELECT
			COALESCE(
				NULLIF(TRIM(json_extract(source_payload, '$.file')), ''),
				NULLIF(TRIM(json_extract(source_payload, '$.payload.file')), ''),
				NULLIF(TRIM(json_extract(source_payload, '$.tool_input.file_path')), ''),
				NULLIF(TRIM(json_extract(source_payload, '$.tool_input.path')), ''),
				NULLIF(TRIM(json_extract(source_payload, '$.tool_response.file.filePath')), ''),
				NULLIF(TRIM(json_extract(source_payload, '$.file_extension')), ''),
				''
			) AS file_path,
			COALESCE(requests, 1) AS requests
		FROM deduped_usage
		WHERE event_type IN ('tool_usage', 'message_usage')
		  AND status != 'error'
	`
	rows, err := db.QueryContext(ctx, query, whereArgs...)
	if err != nil {
		return nil, fmt.Errorf("canonical usage language query: %w", err)
	}
	defer rows.Close()

	langCounts := make(map[string]float64)
	for rows.Next() {
		var filePath string
		var requests float64
		if err := rows.Scan(&filePath, &requests); err != nil {
			continue
		}
		lang := inferLanguageFromFilePath(filePath)
		if lang != "" {
			langCounts[lang] += requests
		}
	}

	out := make([]telemetryLanguageAgg, 0, len(langCounts))
	for lang, count := range langCounts {
		out = append(out, telemetryLanguageAgg{Language: lang, Requests: count})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Requests > out[j].Requests
	})
	return out, nil
}

// inferLanguageFromFilePath maps a file path, file extension, or bare
// extension string to a programming language name.
func inferLanguageFromFilePath(path string) string {
	p := strings.TrimSpace(path)
	if p == "" {
		return ""
	}
	// Check base name for special files.
	base := p
	if idx := strings.LastIndex(p, "/"); idx >= 0 {
		base = p[idx+1:]
	}
	if idx := strings.LastIndex(base, "\\"); idx >= 0 {
		base = base[idx+1:]
	}
	switch strings.ToLower(base) {
	case "dockerfile":
		return "docker"
	case "makefile":
		return "make"
	}
	// Check file extension.
	idx := strings.LastIndex(p, ".")
	if idx < 0 {
		// Handle bare extension without dot (e.g., "go", "py" from file_extension fields).
		if lang := extToLanguage("." + strings.ToLower(p)); lang != "" {
			return lang
		}
		return ""
	}
	ext := strings.ToLower(p[idx:])
	return extToLanguage(ext)
}

// extToLanguage maps a dotted file extension to a language name.
func extToLanguage(ext string) string {
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx":
		return "javascript"
	case ".tf", ".tfvars", ".hcl":
		return "terraform"
	case ".sh", ".bash", ".zsh", ".fish":
		return "shell"
	case ".md", ".mdx":
		return "markdown"
	case ".json":
		return "json"
	case ".yml", ".yaml":
		return "yaml"
	case ".sql":
		return "sql"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".c", ".h":
		return "c"
	case ".cc", ".cpp", ".cxx", ".hpp":
		return "cpp"
	case ".rb":
		return "ruby"
	case ".php":
		return "php"
	case ".swift":
		return "swift"
	case ".kt", ".kts":
		return "kotlin"
	case ".cs":
		return "csharp"
	case ".vue":
		return "vue"
	case ".svelte":
		return "svelte"
	case ".toml":
		return "toml"
	case ".xml":
		return "xml"
	case ".css", ".scss", ".less":
		return "css"
	case ".html", ".htm":
		return "html"
	case ".dart":
		return "dart"
	case ".zig":
		return "zig"
	case ".lua":
		return "lua"
	case ".r":
		return "r"
	case ".proto":
		return "protobuf"
	case ".ex", ".exs":
		return "elixir"
	case ".graphql", ".gql":
		return "graphql"
	}
	return ""
}

func queryProviderAgg(ctx context.Context, db *sql.DB, filter usageFilter) ([]telemetryProviderAgg, error) {
	usageCTE, whereArgs := dedupedUsageCTE(filter)
	// Provider resolution order:
	// 1) hook-enriched upstream provider from source payload (if present),
	// 2) fallback to provider_id.
	//
	// Provider hosting names must come from real payload fields, not inferred
	// model-id heuristics.
	query := usageCTE + `
		SELECT
			COALESCE(
				NULLIF(TRIM(
					COALESCE(
						json_extract(source_payload, '$._normalized.upstream_provider'),
						json_extract(source_payload, '$.upstream_provider'),
						json_extract(source_payload, '$.payload._normalized.upstream_provider'),
						json_extract(source_payload, '$.payload.upstream_provider'),
						''
					)
				), ''),
				COALESCE(NULLIF(TRIM(provider_id), ''), 'unknown')
			) AS provider_name,
			SUM(COALESCE(cost_usd, 0)) AS cost_usd,
			SUM(COALESCE(requests, 1)) AS requests,
			SUM(COALESCE(input_tokens, 0)) AS input_tokens,
			SUM(COALESCE(output_tokens, 0)) AS output_tokens
		FROM deduped_usage
		WHERE 1=1
		  AND event_type = 'message_usage'
		  AND status != 'error'
		GROUP BY provider_name
		ORDER BY cost_usd DESC, requests DESC
		LIMIT 200
	`
	rows, err := db.QueryContext(ctx, query, whereArgs...)
	if err != nil {
		return nil, fmt.Errorf("canonical usage provider query: %w", err)
	}
	defer rows.Close()

	var out []telemetryProviderAgg
	for rows.Next() {
		var row telemetryProviderAgg
		if err := rows.Scan(&row.Provider, &row.CostUSD, &row.Requests, &row.Input, &row.Output); err != nil {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

func queryActivityAgg(ctx context.Context, db *sql.DB, filter usageFilter) (telemetryActivityAgg, error) {
	usageCTE, whereArgs := dedupedUsageCTE(filter)
	query := usageCTE + `
		SELECT
			COUNT(DISTINCT CASE WHEN event_type = 'message_usage' AND status != 'error' THEN
				COALESCE(NULLIF(TRIM(message_id), ''), COALESCE(NULLIF(TRIM(turn_id), ''), dedup_key))
			END) AS messages,
			COUNT(DISTINCT CASE WHEN event_type = 'message_usage' AND status != 'error' THEN
				NULLIF(TRIM(session_id), '')
			END) AS sessions,
			SUM(CASE WHEN event_type = 'tool_usage' THEN COALESCE(requests, 1) ELSE 0 END) AS tool_calls,
			SUM(CASE WHEN event_type = 'message_usage' AND status != 'error' THEN COALESCE(input_tokens, 0) ELSE 0 END) AS input_tokens,
			SUM(CASE WHEN event_type = 'message_usage' AND status != 'error' THEN COALESCE(output_tokens, 0) ELSE 0 END) AS output_tokens,
			SUM(CASE WHEN event_type = 'message_usage' AND status != 'error' THEN COALESCE(cache_read_tokens, 0) ELSE 0 END) AS cached_tokens,
			SUM(CASE WHEN event_type = 'message_usage' AND status != 'error' THEN COALESCE(reasoning_tokens, 0) ELSE 0 END) AS reasoning_tokens,
			SUM(CASE WHEN event_type = 'message_usage' AND status != 'error' THEN COALESCE(total_tokens, 0) ELSE 0 END) AS total_tokens,
			SUM(CASE WHEN event_type = 'message_usage' AND status != 'error' THEN COALESCE(cost_usd, 0) ELSE 0 END) AS total_cost
		FROM deduped_usage
		WHERE 1=1
	`
	var out telemetryActivityAgg
	err := db.QueryRowContext(ctx, query, whereArgs...).Scan(
		&out.Messages, &out.Sessions, &out.ToolCalls,
		&out.InputTokens, &out.OutputTokens, &out.CachedTokens,
		&out.ReasonTokens, &out.TotalTokens, &out.TotalCost,
	)
	if err != nil {
		return out, fmt.Errorf("canonical usage activity query: %w", err)
	}
	return out, nil
}

func queryCodeStatsAgg(ctx context.Context, db *sql.DB, filter usageFilter) (telemetryCodeStatsAgg, error) {
	usageCTE, whereArgs := dedupedUsageCTE(filter)
	// Count distinct file paths from tool_usage events to estimate files changed.
	// Only count mutating tools (edit, write, create, delete, rename, move).
	// Also sum lines_added/lines_removed from message_usage event payloads
	// (e.g. Cursor composer sessions store these).
	query := usageCTE + `
		SELECT
			COUNT(DISTINCT CASE
				WHEN event_type = 'tool_usage'
				  AND (LOWER(tool_name) LIKE '%edit%'
				  OR LOWER(tool_name) LIKE '%write%'
				  OR LOWER(tool_name) LIKE '%create%'
				  OR LOWER(tool_name) LIKE '%delete%'
				  OR LOWER(tool_name) LIKE '%rename%'
				  OR LOWER(tool_name) LIKE '%move%')
				THEN NULLIF(TRIM(COALESCE(
					json_extract(source_payload, '$.file'),
					json_extract(source_payload, '$.payload.file'),
					json_extract(source_payload, '$.tool_input.file_path'),
					json_extract(source_payload, '$.tool_input.path'),
					''
				)), '')
			END) AS files_changed,
			SUM(COALESCE(CAST(json_extract(source_payload, '$.lines_added') AS REAL), 0)) AS lines_added,
			SUM(COALESCE(CAST(json_extract(source_payload, '$.lines_removed') AS REAL), 0)) AS lines_removed
		FROM deduped_usage
		WHERE event_type IN ('tool_usage', 'message_usage')
		  AND status != 'error'
	`
	var out telemetryCodeStatsAgg
	err := db.QueryRowContext(ctx, query, whereArgs...).Scan(&out.FilesChanged, &out.LinesAdded, &out.LinesRemoved)
	if err != nil {
		return out, fmt.Errorf("canonical usage code stats query: %w", err)
	}
	return out, nil
}

func queryDailyTotals(ctx context.Context, db *sql.DB, filter usageFilter) ([]telemetryDayPoint, error) {
	usageCTE, whereArgs := dedupedUsageCTE(filter)
	dailyTimeFilter := ""
	if filter.TimeWindowHours <= 0 {
		dailyTimeFilter = "\n\t\t\t  AND occurred_at >= datetime('now', '-30 day')"
	}
	query := usageCTE + fmt.Sprintf(`
		SELECT
			date(occurred_at) AS day,
			SUM(COALESCE(cost_usd, 0)) AS cost_usd,
			SUM(COALESCE(requests, 1)) AS requests,
			SUM(COALESCE(total_tokens,
				COALESCE(input_tokens, 0) +
				COALESCE(output_tokens, 0) +
				COALESCE(reasoning_tokens, 0) +
				COALESCE(cache_read_tokens, 0) +
				COALESCE(cache_write_tokens, 0))) AS tokens
		FROM deduped_usage
		WHERE 1=1
		  AND event_type = 'message_usage'
		  AND status != 'error'%s
		GROUP BY day
		ORDER BY day ASC
	`, dailyTimeFilter)
	rows, err := db.QueryContext(ctx, query, whereArgs...)
	if err != nil {
		return nil, fmt.Errorf("canonical usage daily query: %w", err)
	}
	defer rows.Close()

	var out []telemetryDayPoint
	for rows.Next() {
		var row telemetryDayPoint
		if err := rows.Scan(&row.Day, &row.CostUSD, &row.Requests, &row.Tokens); err != nil {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

func queryDailyByDimension(ctx context.Context, db *sql.DB, filter usageFilter, dimension string) (map[string][]core.TimePoint, error) {
	usageCTE, whereArgs := dedupedUsageCTE(filter)
	dailyTimeFilter := ""
	if filter.TimeWindowHours <= 0 {
		dailyTimeFilter = "\n\t\t\t  AND occurred_at >= datetime('now', '-30 day')"
	}
	var query string

	switch dimension {
	case "model":
		query = usageCTE + fmt.Sprintf(`
			SELECT date(occurred_at) AS day,
			       COALESCE(NULLIF(TRIM(COALESCE(model_canonical, model_raw)), ''), 'unknown') AS dim_key,
			       SUM(COALESCE(requests, 1)) AS value
			FROM deduped_usage
			WHERE 1=1
			  AND event_type = 'message_usage'
			  AND status != 'error'%s
			GROUP BY day, dim_key
		`, dailyTimeFilter)
	case "source":
		query = usageCTE + fmt.Sprintf(`
			SELECT date(occurred_at) AS day,
			       COALESCE(NULLIF(TRIM(workspace_id), ''), COALESCE(NULLIF(TRIM(source_system), ''), 'unknown')) AS dim_key,
			       SUM(COALESCE(requests, 1)) AS value
			FROM deduped_usage
			WHERE 1=1
			  AND event_type = 'message_usage'
			  AND status != 'error'%s
			GROUP BY day, dim_key
		`, dailyTimeFilter)
	case "project":
		query = usageCTE + fmt.Sprintf(`
			SELECT date(occurred_at) AS day,
			       COALESCE(NULLIF(TRIM(workspace_id), ''), '') AS dim_key,
			       SUM(COALESCE(requests, 1)) AS value
			FROM deduped_usage
			WHERE 1=1
			  AND event_type = 'message_usage'
			  AND status != 'error'
			  AND NULLIF(TRIM(workspace_id), '') IS NOT NULL%s
			GROUP BY day, dim_key
		`, dailyTimeFilter)
	case "client":
		query = usageCTE + fmt.Sprintf(`
			SELECT date(occurred_at) AS day,
			       %s AS dim_key,
			       SUM(COALESCE(requests, 1)) AS value
			FROM deduped_usage
			WHERE 1=1
			  AND event_type = 'message_usage'
			  AND status != 'error'%s
			GROUP BY day, dim_key
		`, clientDimensionExpr(), dailyTimeFilter)
	default:
		return map[string][]core.TimePoint{}, nil
	}

	rows, err := db.QueryContext(ctx, query, whereArgs...)
	if err != nil {
		return nil, fmt.Errorf("canonical usage daily dimension query (%s): %w", dimension, err)
	}
	defer rows.Close()

	byDim := make(map[string]map[string]float64)
	for rows.Next() {
		var day, key string
		var value float64
		if err := rows.Scan(&day, &key, &value); err != nil {
			continue
		}
		key = sanitizeMetricID(key)
		if key == "" {
			key = "unknown"
		}
		if dimension == "project" && key == "unknown" {
			continue
		}
		if byDim[key] == nil {
			byDim[key] = make(map[string]float64)
		}
		byDim[key][day] += value
	}

	out := make(map[string][]core.TimePoint, len(byDim))
	for key, dayMap := range byDim {
		out[key] = sortedSeriesFromByDay(dayMap)
	}
	return out, nil
}

func queryDailyClientTokens(ctx context.Context, db *sql.DB, filter usageFilter) (map[string][]core.TimePoint, error) {
	usageCTE, whereArgs := dedupedUsageCTE(filter)
	dailyTimeFilter := ""
	if filter.TimeWindowHours <= 0 {
		dailyTimeFilter = "\n\t\t\t  AND occurred_at >= datetime('now', '-30 day')"
	}
	query := usageCTE + fmt.Sprintf(`
		SELECT
			date(occurred_at) AS day,
			%s AS source_name,
			SUM(COALESCE(total_tokens,
				COALESCE(input_tokens, 0) +
				COALESCE(output_tokens, 0) +
				COALESCE(reasoning_tokens, 0) +
				COALESCE(cache_read_tokens, 0) +
				COALESCE(cache_write_tokens, 0))) AS tokens
		FROM deduped_usage
		WHERE 1=1
		  AND event_type = 'message_usage'
		  AND status != 'error'%s
		GROUP BY day, source_name
	`, clientDimensionExpr(), dailyTimeFilter)
	rows, err := db.QueryContext(ctx, query, whereArgs...)
	if err != nil {
		return nil, fmt.Errorf("canonical usage daily client token query: %w", err)
	}
	defer rows.Close()

	byClient := make(map[string]map[string]float64)
	for rows.Next() {
		var day, client string
		var value float64
		if err := rows.Scan(&day, &client, &value); err != nil {
			continue
		}
		client = sanitizeMetricID(client)
		if client == "" {
			client = "unknown"
		}
		if byClient[client] == nil {
			byClient[client] = make(map[string]float64)
		}
		byClient[client][day] += value
	}

	out := make(map[string][]core.TimePoint, len(byClient))
	for key, dayMap := range byClient {
		out[key] = sortedSeriesFromByDay(dayMap)
	}
	return out, nil
}

func dedupedUsageCTE(filter usageFilter) (string, []any) {
	// If a materialized temp table exists, just alias it — no CTE rebuild needed.
	if filter.materializedTbl != "" {
		return fmt.Sprintf(`WITH deduped_usage AS (SELECT * FROM %s) `, filter.materializedTbl), nil
	}
	where, args := usageWhereClause("e", filter)
	cte := fmt.Sprintf(`
		WITH scoped_usage AS (
			SELECT
				e.*,
				COALESCE(r.source_system, '') AS source_system,
				COALESCE(r.source_channel, '') AS source_channel,
				COALESCE(r.source_payload, '{}') AS source_payload
			FROM usage_events e
			JOIN usage_raw_events r ON r.raw_event_id = e.raw_event_id
			WHERE %s
			  AND e.event_type IN ('message_usage', 'tool_usage')
		),
		ranked_usage AS (
			SELECT
				scoped_usage.*,
					CASE
						WHEN COALESCE(NULLIF(TRIM(tool_call_id), ''), '') != '' THEN 'tool:' || LOWER(TRIM(tool_call_id))
						WHEN LOWER(TRIM(event_type)) = 'message_usage'
							AND LOWER(TRIM(source_system)) = 'codex'
							AND COALESCE(NULLIF(TRIM(turn_id), ''), '') != ''
						THEN 'message_turn:' || LOWER(TRIM(turn_id))
						WHEN COALESCE(NULLIF(TRIM(message_id), ''), '') != '' THEN 'message:' || LOWER(TRIM(message_id))
						WHEN COALESCE(NULLIF(TRIM(turn_id), ''), '') != '' THEN 'turn:' || LOWER(TRIM(turn_id))
						ELSE 'fallback:' || dedup_key
					END AS logical_event_id,
				CASE COALESCE(NULLIF(TRIM(source_channel), ''), '')
					WHEN 'hook' THEN 4
					WHEN 'sse' THEN 3
					WHEN 'sqlite' THEN 2
					WHEN 'jsonl' THEN 2
					WHEN 'api' THEN 1
					ELSE 0
				END AS source_priority,
				(
					CASE WHEN COALESCE(total_tokens, 0) > 0 THEN 4 ELSE 0 END +
					CASE WHEN COALESCE(cost_usd, 0) > 0 THEN 2 ELSE 0 END +
					CASE WHEN COALESCE(NULLIF(TRIM(COALESCE(model_canonical, model_raw)), ''), '') != '' THEN 1 ELSE 0 END +
					CASE
						WHEN COALESCE(NULLIF(TRIM(provider_id), ''), '') != ''
							AND LOWER(TRIM(provider_id)) NOT IN ('unknown', 'opencode')
						THEN 1
						ELSE 0
					END
				) AS quality_score
			FROM scoped_usage
		),
		deduped_usage AS (
			SELECT *
			FROM (
				SELECT
					ranked_usage.*,
					ROW_NUMBER() OVER (
						PARTITION BY
							LOWER(TRIM(source_system)),
							LOWER(TRIM(event_type)),
							LOWER(TRIM(COALESCE(session_id, ''))),
							logical_event_id
						ORDER BY source_priority DESC, quality_score DESC, occurred_at DESC, event_id DESC
					) AS rn
				FROM ranked_usage
			)
			WHERE rn = 1
		)
		`, where)
	return cte, args
}

func usageWhereClause(alias string, filter usageFilter) (string, []any) {
	prefix := ""
	if strings.TrimSpace(alias) != "" {
		prefix = strings.TrimSpace(alias) + "."
	}
	providerIDs := normalizeProviderIDs(filter.ProviderIDs)
	if len(providerIDs) == 0 {
		return prefix + "provider_id = ''", nil
	}
	where := ""
	args := make([]any, 0, len(providerIDs)+1)
	if len(providerIDs) == 1 {
		where = prefix + "provider_id = ?"
		args = append(args, providerIDs[0])
	} else {
		placeholders := make([]string, 0, len(providerIDs))
		for _, providerID := range providerIDs {
			placeholders = append(placeholders, "?")
			args = append(args, providerID)
		}
		where = prefix + "provider_id IN (" + strings.Join(placeholders, ",") + ")"
	}
	if strings.TrimSpace(filter.AccountID) != "" {
		where += " AND " + prefix + "account_id = ?"
		args = append(args, strings.TrimSpace(filter.AccountID))
	}
	if filter.TimeWindowHours > 0 {
		where += fmt.Sprintf(" AND %soccurred_at >= datetime('now', '-%d hour')", prefix, filter.TimeWindowHours)
	}
	return where, args
}

func normalizeProviderIDs(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	normalized := lo.Map(in, func(s string, _ int) string {
		return strings.ToLower(strings.TrimSpace(s))
	})
	result := lo.Uniq(lo.Compact(normalized))
	sort.Strings(result)
	return result
}

// parseMCPToolName extracts server and function from an MCP tool name.
