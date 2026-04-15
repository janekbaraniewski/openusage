package telemetry

import (
	"fmt"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/samber/lo"
)

func dedupedUsageCTE(filter usageFilter) (string, []any) {
	if filter.materializedTbl != "" {
		if err := validateMaterializedTable(filter.materializedTbl); err != nil {
			// Defensive: fall through to full CTE rather than interpolating
			// an unvalidated table name into SQL.
			core.Tracef("[dedupedUsageCTE] %v — falling back to full CTE", err)
		} else {
			return fmt.Sprintf(`WITH deduped_usage AS (SELECT * FROM %s) `, filter.materializedTbl), nil
		}
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
	if !filter.Since.IsZero() {
		where += fmt.Sprintf(" AND %soccurred_at >= '%s'", prefix, filter.Since.UTC().Format(time.RFC3339Nano))
	}
	return where, args
}

// todayExpr returns a SQL expression that is true for events occurring on
// the local calendar day. Falls back to UTC date('now') if TodaySince is zero.
func (f usageFilter) todayExpr(col string) string {
	if f.TodaySince.IsZero() {
		return fmt.Sprintf("date(%s) = date('now')", col)
	}
	return fmt.Sprintf("%s >= '%s'", col, f.TodaySince.UTC().Format(time.RFC3339Nano))
}

func normalizeProviderIDs(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	normalized := lo.Map(in, func(s string, _ int) string {
		return strings.ToLower(strings.TrimSpace(s))
	})
	return core.SortedCompactStrings(normalized)
}
