package goose

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// gooseSession is the in-memory representation of one row of the sessions
// table. Token totals are normalised to non-negative int64s; the caller is
// responsible for deriving reasoning tokens and filtering all-zero rows.
type gooseSession struct {
	ID              string
	Model           string
	Provider        string
	CreatedAt       time.Time
	InputTokens     int64
	OutputTokens    int64
	TotalTokens     int64
	ReasoningTokens int64
	AccumulatedCost float64
	HasCost         bool
}

// columnPresence summarises which optional columns are present in the
// sessions table. The upstream schema has evolved over time (migrations 1
// through 9+) so we probe before SELECTing rather than failing fast on a
// missing column.
type columnPresence struct {
	HasAccumulatedTotal  bool
	HasAccumulatedInput  bool
	HasAccumulatedOutput bool
	HasAccumulatedCost   bool
	HasTotalTokens       bool
	HasInputTokens       bool
	HasOutputTokens      bool
	HasProviderName      bool
	HasModelConfigJSON   bool
}

// detectColumns inspects the sessions table via PRAGMA table_info and
// reports which optional columns exist.
func detectColumns(ctx context.Context, db *sql.DB) (columnPresence, error) {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(sessions)`)
	if err != nil {
		return columnPresence{}, fmt.Errorf("goose: reading sessions schema: %w", err)
	}
	defer rows.Close()

	present := make(map[string]bool)
	for rows.Next() {
		var (
			cid       int
			name      string
			ctype     string
			notnull   int
			dfltValue sql.NullString
			pk        int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return columnPresence{}, fmt.Errorf("goose: scanning sessions schema: %w", err)
		}
		present[strings.ToLower(strings.TrimSpace(name))] = true
	}
	if err := rows.Err(); err != nil {
		return columnPresence{}, fmt.Errorf("goose: iterating sessions schema: %w", err)
	}

	return columnPresence{
		HasAccumulatedTotal:  present["accumulated_total_tokens"],
		HasAccumulatedInput:  present["accumulated_input_tokens"],
		HasAccumulatedOutput: present["accumulated_output_tokens"],
		HasAccumulatedCost:   present["accumulated_cost"],
		HasTotalTokens:       present["total_tokens"],
		HasInputTokens:       present["input_tokens"],
		HasOutputTokens:      present["output_tokens"],
		HasProviderName:      present["provider_name"],
		HasModelConfigJSON:   present["model_config_json"],
	}, nil
}

// gooseQueryResult bundles the parsed sessions with diagnostic counters
// (e.g. rows skipped due to an unparseable timestamp) so the caller can
// surface them on the snapshot.
type gooseQueryResult struct {
	Sessions    []gooseSession
	SkippedRows int
}

// queryGooseSessions returns all non-empty sessions in the database. A
// session is considered "empty" when every available token column is zero
// or NULL, and it's filtered out.
//
// The query is tolerant of older schemas — accumulated_* columns are
// preferred when present but the function falls back to plain *_tokens
// columns and finally to a degenerate "no tokens at all" select that still
// returns session IDs (useful for showing session counts).
func queryGooseSessions(ctx context.Context, dbPath string) (gooseQueryResult, error) {
	var result gooseQueryResult
	db, err := openReadOnly(dbPath)
	if err != nil {
		return result, err
	}
	defer db.Close()

	if err := pingContext(ctx, db); err != nil {
		return result, err
	}

	cols, err := detectColumns(ctx, db)
	if err != nil {
		return result, err
	}
	if !cols.HasModelConfigJSON {
		// Without model_config_json we can't recover model name; nothing
		// useful to surface. Treat as empty (graceful) rather than error.
		return result, nil
	}

	selectParts := []string{
		"id",
		"model_config_json",
		"created_at",
	}
	if cols.HasProviderName {
		selectParts = append(selectParts, "provider_name")
	} else {
		selectParts = append(selectParts, "NULL AS provider_name")
	}
	selectParts = append(selectParts,
		columnOrNull("accumulated_input_tokens", cols.HasAccumulatedInput),
		columnOrNull("accumulated_output_tokens", cols.HasAccumulatedOutput),
		columnOrNull("accumulated_total_tokens", cols.HasAccumulatedTotal),
		columnOrNull("input_tokens", cols.HasInputTokens),
		columnOrNull("output_tokens", cols.HasOutputTokens),
		columnOrNull("total_tokens", cols.HasTotalTokens),
		columnOrNull("accumulated_cost", cols.HasAccumulatedCost),
	)

	query := fmt.Sprintf(
		`SELECT %s FROM sessions WHERE model_config_json IS NOT NULL AND model_config_json != ''`,
		strings.Join(selectParts, ", "),
	)

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return result, fmt.Errorf("goose: querying sessions: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id          string
			modelCfgRaw string
			createdAt   sql.NullString
			providerRaw sql.NullString
			accInput    sql.NullInt64
			accOutput   sql.NullInt64
			accTotal    sql.NullInt64
			rawInput    sql.NullInt64
			rawOutput   sql.NullInt64
			rawTotal    sql.NullInt64
			accCost     sql.NullFloat64
		)
		if err := rows.Scan(
			&id,
			&modelCfgRaw,
			&createdAt,
			&providerRaw,
			&accInput,
			&accOutput,
			&accTotal,
			&rawInput,
			&rawOutput,
			&rawTotal,
			&accCost,
		); err != nil {
			return result, fmt.Errorf("goose: scanning session row: %w", err)
		}

		model := strings.TrimSpace(extractModelName(modelCfgRaw))
		if model == "" {
			continue
		}

		ts, ok := parseTimestamp(createdAt.String)
		if !ok {
			result.SkippedRows++
			continue
		}

		input := nonNegativeInt64Preferred(accInput, rawInput)
		output := nonNegativeInt64Preferred(accOutput, rawOutput)
		total := nonNegativeInt64Preferred(accTotal, rawTotal)
		if total == 0 && (input+output) > 0 {
			total = input + output
		}

		if input == 0 && output == 0 && total == 0 {
			continue
		}

		reasoning := total - input - output
		if reasoning < 0 {
			reasoning = 0
		}

		sess := gooseSession{
			ID:              strings.TrimSpace(id),
			Model:           model,
			Provider:        strings.TrimSpace(providerRaw.String),
			CreatedAt:       ts,
			InputTokens:     input,
			OutputTokens:    output,
			TotalTokens:     total,
			ReasoningTokens: reasoning,
		}
		if accCost.Valid && accCost.Float64 > 0 {
			sess.AccumulatedCost = accCost.Float64
			sess.HasCost = true
		}
		result.Sessions = append(result.Sessions, sess)
	}
	if err := rows.Err(); err != nil {
		return result, fmt.Errorf("goose: iterating session rows: %w", err)
	}

	return result, nil
}

// columnOrNull returns either the literal column name (when present) or a
// NULL literal aliased to the column name so the row scan order stays
// fixed across schema variants.
func columnOrNull(name string, present bool) string {
	if present {
		return name
	}
	return "NULL AS " + name
}

// nonNegativeInt64Preferred returns the value of the first NullInt64 that is
// non-NULL and >= 0, falling back to the second. Returns 0 when both are
// NULL or negative.
func nonNegativeInt64Preferred(preferred, fallback sql.NullInt64) int64 {
	if preferred.Valid && preferred.Int64 >= 0 {
		return preferred.Int64
	}
	if fallback.Valid && fallback.Int64 >= 0 {
		return fallback.Int64
	}
	return 0
}

// extractModelName pulls the "model_name" field out of model_config_json.
// We tolerate a few historical key variants seen in older schemas.
func extractModelName(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var probe map[string]any
	if err := json.Unmarshal([]byte(raw), &probe); err != nil {
		return ""
	}
	for _, key := range []string{"model_name", "model", "name"} {
		if v, ok := probe[key]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

// parseTimestamp accepts the three timestamp serialisations that have
// appeared in upstream releases:
//
//   - RFC3339           ("2025-05-18T10:30:00Z" / with offset)
//   - SQLite datetime   ("2025-05-18 10:30:00")
//   - date-only         ("2025-05-18", interpreted as 00:00:00 UTC)
//
// Returns (time, true) on success, (zero, false) when none match.
func parseTimestamp(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}
