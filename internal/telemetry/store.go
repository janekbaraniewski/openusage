package telemetry

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Store struct {
	db  *sql.DB
	now func() time.Time
}

func OpenStore(path string) (*Store, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("telemetry: creating DB dir: %w", err)
	}

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("telemetry: opening DB: %w", err)
	}

	store := NewStore(db)
	if err := store.Init(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db, now: time.Now}
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Init(ctx context.Context) error {
	stmts := []string{
		`PRAGMA foreign_keys = ON;`,
		`CREATE TABLE IF NOT EXISTS usage_raw_events (
			raw_event_id TEXT PRIMARY KEY,
			ingested_at TEXT NOT NULL,
			source_system TEXT NOT NULL,
			source_channel TEXT NOT NULL,
			source_schema_version TEXT NOT NULL,
			source_payload TEXT NOT NULL,
			source_payload_hash TEXT NOT NULL,
			workspace_id TEXT,
			agent_session_id TEXT
		);`,
		`CREATE INDEX IF NOT EXISTS idx_usage_raw_events_ingested_at ON usage_raw_events(ingested_at);`,
		`CREATE INDEX IF NOT EXISTS idx_usage_raw_events_source ON usage_raw_events(source_system, source_channel);`,
		`CREATE TABLE IF NOT EXISTS usage_events (
			event_id TEXT PRIMARY KEY,
			occurred_at TEXT NOT NULL,
			provider_id TEXT,
			agent_name TEXT NOT NULL,
			account_id TEXT,
			workspace_id TEXT,
			session_id TEXT,
			turn_id TEXT,
			message_id TEXT,
			tool_call_id TEXT,
			event_type TEXT NOT NULL,
			model_raw TEXT,
			model_canonical TEXT,
			model_lineage_id TEXT,
			input_tokens INTEGER,
			output_tokens INTEGER,
			reasoning_tokens INTEGER,
			cache_read_tokens INTEGER,
			cache_write_tokens INTEGER,
			total_tokens INTEGER,
			cost_usd REAL,
			requests INTEGER,
			tool_name TEXT,
			status TEXT NOT NULL,
			dedup_key TEXT NOT NULL UNIQUE,
			raw_event_id TEXT NOT NULL,
			normalization_version TEXT NOT NULL,
			FOREIGN KEY(raw_event_id) REFERENCES usage_raw_events(raw_event_id)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_usage_events_occurred_at ON usage_events(occurred_at);`,
		`CREATE INDEX IF NOT EXISTS idx_usage_events_provider_window ON usage_events(provider_id, account_id, occurred_at);`,
		`CREATE TABLE IF NOT EXISTS usage_reconciliation_windows (
			recon_id TEXT PRIMARY KEY,
			provider_id TEXT NOT NULL,
			account_id TEXT,
			window_start TEXT NOT NULL,
			window_end TEXT NOT NULL,
			authoritative_cost_usd REAL,
			authoritative_tokens INTEGER,
			authoritative_requests INTEGER,
			event_sum_cost_usd REAL,
			event_sum_tokens INTEGER,
			event_sum_requests INTEGER,
			delta_cost_usd REAL,
			delta_tokens INTEGER,
			delta_requests INTEGER,
			resolution TEXT NOT NULL,
			created_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_usage_recon_provider_window ON usage_reconciliation_windows(provider_id, account_id, window_start, window_end);`,
	}

	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("telemetry: init schema: %w", err)
		}
	}
	return nil
}

func (s *Store) Ingest(ctx context.Context, req IngestRequest) (IngestResult, error) {
	norm := normalizeRequest(req, s.now().UTC())
	payloadBytes, err := marshalPayload(norm.Payload)
	if err != nil {
		return IngestResult{}, fmt.Errorf("telemetry: marshal payload: %w", err)
	}

	rawEventID, err := newUUID()
	if err != nil {
		return IngestResult{}, fmt.Errorf("telemetry: create raw event id: %w", err)
	}
	eventID, err := newUUID()
	if err != nil {
		return IngestResult{}, fmt.Errorf("telemetry: create event id: %w", err)
	}
	now := s.now().UTC()
	dedupKey := BuildDedupKey(norm)
	payloadHash := sha256.Sum256(payloadBytes)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return IngestResult{}, fmt.Errorf("telemetry: begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO usage_raw_events (
			raw_event_id, ingested_at, source_system, source_channel, source_schema_version,
			source_payload, source_payload_hash, workspace_id, agent_session_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		rawEventID,
		now.Format(time.RFC3339Nano),
		string(norm.SourceSystem),
		string(norm.SourceChannel),
		norm.SourceSchemaVersion,
		string(payloadBytes),
		hex.EncodeToString(payloadHash[:]),
		nullable(norm.WorkspaceID),
		nullable(norm.SessionID),
	); err != nil {
		return IngestResult{}, fmt.Errorf("telemetry: insert raw event: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO usage_events (
			event_id, occurred_at, provider_id, agent_name, account_id, workspace_id, session_id,
			turn_id, message_id, tool_call_id, event_type, model_raw, model_canonical,
			model_lineage_id, input_tokens, output_tokens, reasoning_tokens, cache_read_tokens,
			cache_write_tokens, total_tokens, cost_usd, requests, tool_name, status, dedup_key,
			raw_event_id, normalization_version
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		eventID,
		norm.OccurredAt.Format(time.RFC3339Nano),
		nullable(norm.ProviderID),
		norm.AgentName,
		nullable(norm.AccountID),
		nullable(norm.WorkspaceID),
		nullable(norm.SessionID),
		nullable(norm.TurnID),
		nullable(norm.MessageID),
		nullable(norm.ToolCallID),
		string(norm.EventType),
		nullable(norm.ModelRaw),
		nullable(norm.ModelCanonical),
		nullable(norm.ModelLineageID),
		nullableInt64(norm.InputTokens),
		nullableInt64(norm.OutputTokens),
		nullableInt64(norm.ReasoningTokens),
		nullableInt64(norm.CacheReadTokens),
		nullableInt64(norm.CacheWriteTokens),
		nullableInt64(norm.TotalTokens),
		nullableFloat64(norm.CostUSD),
		nullableInt64(norm.Requests),
		nullable(norm.ToolName),
		string(norm.Status),
		dedupKey,
		rawEventID,
		norm.NormalizationVersion,
	)
	if err != nil {
		if isUniqueConstraintError(err, "usage_events.dedup_key") {
			existingID, lookupErr := findEventIDByDedupKey(ctx, tx, dedupKey)
			if lookupErr != nil {
				return IngestResult{}, fmt.Errorf("telemetry: lookup dedup event id: %w", lookupErr)
			}
			if enrichErr := enrichEventByDedupKey(ctx, tx, dedupKey, norm); enrichErr != nil {
				return IngestResult{}, fmt.Errorf("telemetry: enrich dedup event: %w", enrichErr)
			}
			if commitErr := tx.Commit(); commitErr != nil {
				return IngestResult{}, fmt.Errorf("telemetry: commit dedup tx: %w", commitErr)
			}
			return IngestResult{
				Status:     "accepted",
				Deduped:    true,
				EventID:    existingID,
				RawEventID: rawEventID,
			}, nil
		}
		return IngestResult{}, fmt.Errorf("telemetry: insert canonical event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return IngestResult{}, fmt.Errorf("telemetry: commit tx: %w", err)
	}

	return IngestResult{
		Status:     "accepted",
		Deduped:    false,
		EventID:    eventID,
		RawEventID: rawEventID,
	}, nil
}

func findEventIDByDedupKey(ctx context.Context, tx *sql.Tx, dedupKey string) (string, error) {
	var eventID string
	if err := tx.QueryRowContext(ctx, `SELECT event_id FROM usage_events WHERE dedup_key = ?`, dedupKey).Scan(&eventID); err != nil {
		return "", err
	}
	return eventID, nil
}

// enrichEventByDedupKey fills missing canonical fields from a duplicate event.
func enrichEventByDedupKey(ctx context.Context, tx *sql.Tx, dedupKey string, norm IngestRequest) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE usage_events
		SET
			provider_id = COALESCE(provider_id, ?),
			account_id = COALESCE(account_id, ?),
			workspace_id = COALESCE(workspace_id, ?),
			session_id = COALESCE(session_id, ?),
			turn_id = COALESCE(turn_id, ?),
			message_id = COALESCE(message_id, ?),
			tool_call_id = COALESCE(tool_call_id, ?),
			model_raw = COALESCE(model_raw, ?),
			model_canonical = COALESCE(model_canonical, ?),
			model_lineage_id = COALESCE(model_lineage_id, ?),
			input_tokens = COALESCE(input_tokens, ?),
			output_tokens = COALESCE(output_tokens, ?),
			reasoning_tokens = COALESCE(reasoning_tokens, ?),
			cache_read_tokens = COALESCE(cache_read_tokens, ?),
			cache_write_tokens = COALESCE(cache_write_tokens, ?),
			total_tokens = COALESCE(total_tokens, ?),
			cost_usd = COALESCE(cost_usd, ?),
			requests = COALESCE(requests, ?),
			tool_name = COALESCE(tool_name, ?),
			status = CASE
				WHEN status IN ('unknown', '') AND ? <> '' THEN ?
				ELSE status
			END
		WHERE dedup_key = ?
	`,
		nullable(norm.ProviderID),
		nullable(norm.AccountID),
		nullable(norm.WorkspaceID),
		nullable(norm.SessionID),
		nullable(norm.TurnID),
		nullable(norm.MessageID),
		nullable(norm.ToolCallID),
		nullable(norm.ModelRaw),
		nullable(norm.ModelCanonical),
		nullable(norm.ModelLineageID),
		nullableInt64(norm.InputTokens),
		nullableInt64(norm.OutputTokens),
		nullableInt64(norm.ReasoningTokens),
		nullableInt64(norm.CacheReadTokens),
		nullableInt64(norm.CacheWriteTokens),
		nullableInt64(norm.TotalTokens),
		nullableFloat64(norm.CostUSD),
		nullableInt64(norm.Requests),
		nullable(norm.ToolName),
		string(norm.Status),
		string(norm.Status),
		dedupKey,
	)
	return err
}

func isUniqueConstraintError(err error, target string) bool {
	if err == nil {
		return false
	}
	errText := err.Error()
	return strings.Contains(errText, "UNIQUE constraint failed") && strings.Contains(errText, target)
}

func nullable(v string) interface{} {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}

func nullableInt64(v *int64) interface{} {
	if v == nil {
		return nil
	}
	return *v
}

func nullableFloat64(v *float64) interface{} {
	if v == nil {
		return nil
	}
	return *v
}

func newUUID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}

	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80

	encoded := hex.EncodeToString(buf)
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		encoded[0:8],
		encoded[8:12],
		encoded[12:16],
		encoded[16:20],
		encoded[20:32],
	), nil
}
