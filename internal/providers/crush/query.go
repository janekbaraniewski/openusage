package crush

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// crushSession is the in-memory representation of one root session
// (one row from the `sessions` table where parent_session_id IS NULL).
//
// Tokens / cost reflect the session aggregate as Crush tracks it:
// `prompt_tokens`, `completion_tokens`, and `cost` are documented columns
// on the sessions table. Schema reference:
// github.com/charmbracelet/crush internal/db/migrations/20250424200609_initial.sql
type crushSession struct {
	ID               string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	MessageCount     int64
	PromptTokens     int64
	CompletionTokens int64
	Cost             float64
	HasCost          bool
	// Model and Provider are derived from the most recent assistant
	// message in the session. Empty when Crush hasn't recorded an
	// assistant message yet (e.g. session created but no LLM call made).
	Model    string
	Provider string
}

// hasMessagesProviderColumn reports whether the `messages.provider`
// column is present. The column was added by upstream migration
// 20250627000000_add_provider_to_messages.sql, so older DBs won't have
// it and probing avoids `no such column` errors.
func hasMessagesProviderColumn(ctx context.Context, db *sql.DB) (bool, error) {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(messages)`)
	if err != nil {
		return false, fmt.Errorf("crush: inspecting messages schema: %w", err)
	}
	defer rows.Close()

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
			return false, fmt.Errorf("crush: scanning messages schema: %w", err)
		}
		if strings.EqualFold(strings.TrimSpace(name), "provider") {
			return true, rows.Err()
		}
	}
	return false, rows.Err()
}

// querySessions reads all root sessions from a single Crush DB and
// enriches each with the most-recent assistant model/provider hint.
//
// Implementation notes:
//   - We filter to root sessions (`parent_session_id IS NULL`). Child
//     sessions exist (Crush forks for sub-agents), but per the recon
//     notes the parent row already carries the rolled-up token/cost
//     totals, so a sum over roots avoids double-counting.
//   - We skip sessions that have no messages AND no cost — those are
//     placeholder rows Crush creates eagerly on UI navigation.
//   - Model is sourced from the latest assistant message rather than
//     iterating every message: it's a tile attribution hint, not a
//     summable metric. If multiple models were used in one session, the
//     last one wins; the per-message breakdown is out of scope for v1.
func querySessions(ctx context.Context, dbPath string) ([]crushSession, error) {
	db, err := openReadOnly(dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	if err := pingContext(ctx, db); err != nil {
		return nil, err
	}

	hasProvider, err := hasMessagesProviderColumn(ctx, db)
	if err != nil {
		return nil, err
	}

	rows, err := db.QueryContext(ctx, `
		SELECT id, message_count, prompt_tokens, completion_tokens,
		       cost, created_at, updated_at
		FROM sessions
		WHERE parent_session_id IS NULL
		  AND (message_count > 0 OR cost > 0)
	`)
	if err != nil {
		return nil, fmt.Errorf("crush: querying sessions: %w", err)
	}
	defer rows.Close()

	var sessions []crushSession
	for rows.Next() {
		var (
			id           string
			msgCount     sql.NullInt64
			promptTokens sql.NullInt64
			complTokens  sql.NullInt64
			cost         sql.NullFloat64
			createdMS    sql.NullInt64
			updatedMS    sql.NullInt64
		)
		if err := rows.Scan(&id, &msgCount, &promptTokens, &complTokens, &cost, &createdMS, &updatedMS); err != nil {
			return nil, fmt.Errorf("crush: scanning session row: %w", err)
		}

		s := crushSession{
			ID:               strings.TrimSpace(id),
			MessageCount:     nonNegativeInt64(msgCount),
			PromptTokens:     nonNegativeInt64(promptTokens),
			CompletionTokens: nonNegativeInt64(complTokens),
			CreatedAt:        millisToTime(createdMS),
			UpdatedAt:        millisToTime(updatedMS),
		}
		if cost.Valid && cost.Float64 > 0 {
			s.Cost = cost.Float64
			s.HasCost = true
		}
		sessions = append(sessions, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("crush: iterating session rows: %w", err)
	}

	// Annotate each session with model/provider from its latest
	// assistant message. One query per session keeps the implementation
	// simple; sessions are typically << 1000 per project so the
	// round-trip cost is negligible.
	for i := range sessions {
		model, provider, err := latestAssistantModel(ctx, db, sessions[i].ID, hasProvider)
		if err != nil {
			return nil, err
		}
		sessions[i].Model = model
		sessions[i].Provider = provider
	}
	return sessions, nil
}

// latestAssistantModel returns the (model, provider) tuple for the most
// recent assistant message in the session. Returns empty strings (no
// error) when no assistant message exists.
func latestAssistantModel(ctx context.Context, db *sql.DB, sessionID string, hasProvider bool) (string, string, error) {
	cols := "model, NULL AS provider"
	if hasProvider {
		cols = "model, provider"
	}
	query := fmt.Sprintf(`
		SELECT %s FROM messages
		WHERE session_id = ? AND role = 'assistant'
		ORDER BY created_at DESC
		LIMIT 1
	`, cols)

	var model, provider sql.NullString
	err := db.QueryRowContext(ctx, query, sessionID).Scan(&model, &provider)
	if err == sql.ErrNoRows {
		return "", "", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("crush: querying messages.model for session %s: %w", sessionID, err)
	}
	return strings.TrimSpace(model.String), strings.TrimSpace(provider.String), nil
}

// millisToTime converts a nullable Unix-milliseconds column into a UTC
// time.Time. NULL or zero values return the zero time; callers must
// check before formatting day buckets.
func millisToTime(v sql.NullInt64) time.Time {
	if !v.Valid || v.Int64 <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(v.Int64).UTC()
}

func nonNegativeInt64(v sql.NullInt64) int64 {
	if !v.Valid || v.Int64 < 0 {
		return 0
	}
	return v.Int64
}
