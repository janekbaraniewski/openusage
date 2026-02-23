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
	if err := configureSQLiteConnection(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("telemetry: configure sqlite: %w", err)
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
		`CREATE INDEX IF NOT EXISTS idx_usage_events_provider_account_type_occurred ON usage_events(provider_id, account_id, event_type, occurred_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_usage_events_type_provider ON usage_events(event_type, provider_id);`,
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

type storedCanonicalEvent struct {
	EventID        string
	SourceChannel  string
	ProviderID     sql.NullString
	AccountID      sql.NullString
	WorkspaceID    sql.NullString
	SessionID      sql.NullString
	TurnID         sql.NullString
	MessageID      sql.NullString
	ToolCallID     sql.NullString
	ModelRaw       sql.NullString
	ModelCanonical sql.NullString
	ModelLineageID sql.NullString
	InputTokens    sql.NullInt64
	OutputTokens   sql.NullInt64
	Reasoning      sql.NullInt64
	CacheRead      sql.NullInt64
	CacheWrite     sql.NullInt64
	TotalTokens    sql.NullInt64
	CostUSD        sql.NullFloat64
	Requests       sql.NullInt64
	ToolName       sql.NullString
	Status         string
}

func loadCanonicalEventByDedupKey(ctx context.Context, tx *sql.Tx, dedupKey string) (storedCanonicalEvent, error) {
	var row storedCanonicalEvent
	err := tx.QueryRowContext(ctx, `
		SELECT
			e.event_id,
			e.provider_id,
			e.account_id,
			e.workspace_id,
			e.session_id,
			e.turn_id,
			e.message_id,
			e.tool_call_id,
			e.model_raw,
			e.model_canonical,
			e.model_lineage_id,
			e.input_tokens,
			e.output_tokens,
			e.reasoning_tokens,
			e.cache_read_tokens,
			e.cache_write_tokens,
			e.total_tokens,
			e.cost_usd,
			e.requests,
			e.tool_name,
			e.status,
			COALESCE(r.source_channel, '')
		FROM usage_events e
		JOIN usage_raw_events r ON r.raw_event_id = e.raw_event_id
		WHERE e.dedup_key = ?
		LIMIT 1
	`, dedupKey).Scan(
		&row.EventID,
		&row.ProviderID,
		&row.AccountID,
		&row.WorkspaceID,
		&row.SessionID,
		&row.TurnID,
		&row.MessageID,
		&row.ToolCallID,
		&row.ModelRaw,
		&row.ModelCanonical,
		&row.ModelLineageID,
		&row.InputTokens,
		&row.OutputTokens,
		&row.Reasoning,
		&row.CacheRead,
		&row.CacheWrite,
		&row.TotalTokens,
		&row.CostUSD,
		&row.Requests,
		&row.ToolName,
		&row.Status,
		&row.SourceChannel,
	)
	return row, err
}

// enrichEventByDedupKey merges duplicate canonical fields with source priority.
// Hook payloads take precedence over file/sqlite events when both provide values.
func enrichEventByDedupKey(ctx context.Context, tx *sql.Tx, dedupKey string, norm IngestRequest) error {
	current, err := loadCanonicalEventByDedupKey(ctx, tx, dedupKey)
	if err != nil {
		return err
	}

	override := sourceChannelPriority(norm.SourceChannel) > sourceChannelPriority(SourceChannel(current.SourceChannel))

	providerID := chooseString(current.ProviderID, norm.ProviderID, override)
	accountID := chooseString(current.AccountID, norm.AccountID, override)
	workspaceID := chooseString(current.WorkspaceID, norm.WorkspaceID, override)
	sessionID := chooseString(current.SessionID, norm.SessionID, override)
	turnID := chooseString(current.TurnID, norm.TurnID, override)
	messageID := chooseString(current.MessageID, norm.MessageID, override)
	toolCallID := chooseString(current.ToolCallID, norm.ToolCallID, override)
	modelRaw := chooseString(current.ModelRaw, norm.ModelRaw, override)
	modelCanonical := chooseString(current.ModelCanonical, norm.ModelCanonical, override)
	modelLineage := chooseString(current.ModelLineageID, norm.ModelLineageID, override)
	toolName := chooseString(current.ToolName, norm.ToolName, override)

	inputTokens := chooseInt64(current.InputTokens, norm.InputTokens, override)
	outputTokens := chooseInt64(current.OutputTokens, norm.OutputTokens, override)
	reasoningTokens := chooseInt64(current.Reasoning, norm.ReasoningTokens, override)
	cacheReadTokens := chooseInt64(current.CacheRead, norm.CacheReadTokens, override)
	cacheWriteTokens := chooseInt64(current.CacheWrite, norm.CacheWriteTokens, override)
	totalTokens := chooseInt64(current.TotalTokens, norm.TotalTokens, override)
	costUSD := chooseFloat64(current.CostUSD, norm.CostUSD, override)
	requests := chooseInt64(current.Requests, norm.Requests, override)
	status := chooseStatus(current.Status, norm.Status, override)

	_, err = tx.ExecContext(ctx, `
		UPDATE usage_events
		SET
			provider_id = ?,
			account_id = ?,
			workspace_id = ?,
			session_id = ?,
			turn_id = ?,
			message_id = ?,
			tool_call_id = ?,
			model_raw = ?,
			model_canonical = ?,
			model_lineage_id = ?,
			input_tokens = ?,
			output_tokens = ?,
			reasoning_tokens = ?,
			cache_read_tokens = ?,
			cache_write_tokens = ?,
			total_tokens = ?,
			cost_usd = ?,
			requests = ?,
			tool_name = ?,
			status = ?
		WHERE event_id = ?
	`,
		nullable(providerID),
		nullable(accountID),
		nullable(workspaceID),
		nullable(sessionID),
		nullable(turnID),
		nullable(messageID),
		nullable(toolCallID),
		nullable(modelRaw),
		nullable(modelCanonical),
		nullable(modelLineage),
		nullableInt64(inputTokens),
		nullableInt64(outputTokens),
		nullableInt64(reasoningTokens),
		nullableInt64(cacheReadTokens),
		nullableInt64(cacheWriteTokens),
		nullableInt64(totalTokens),
		nullableFloat64(costUSD),
		nullableInt64(requests),
		nullable(toolName),
		string(status),
		current.EventID,
	)
	return err
}

func sourceChannelPriority(channel SourceChannel) int {
	switch channel {
	case SourceChannelHook:
		return 4
	case SourceChannelSSE:
		return 3
	case SourceChannelSQLite, SourceChannelJSONL:
		return 2
	case SourceChannelAPI:
		return 1
	default:
		return 0
	}
}

func chooseString(current sql.NullString, incoming string, override bool) string {
	trimmedIncoming := strings.TrimSpace(incoming)
	if !current.Valid || strings.TrimSpace(current.String) == "" {
		return trimmedIncoming
	}
	if override && trimmedIncoming != "" {
		return trimmedIncoming
	}
	return strings.TrimSpace(current.String)
}

func chooseInt64(current sql.NullInt64, incoming *int64, override bool) *int64 {
	if !current.Valid {
		if incoming == nil {
			return nil
		}
		v := *incoming
		return &v
	}
	if override && incoming != nil {
		v := *incoming
		return &v
	}
	v := current.Int64
	return &v
}

func chooseFloat64(current sql.NullFloat64, incoming *float64, override bool) *float64 {
	if !current.Valid {
		if incoming == nil {
			return nil
		}
		v := *incoming
		return &v
	}
	if override && incoming != nil {
		v := *incoming
		return &v
	}
	v := current.Float64
	return &v
}

func chooseStatus(current string, incoming EventStatus, override bool) EventStatus {
	currentStatus := EventStatus(strings.TrimSpace(current))
	incomingStatus := EventStatus(strings.TrimSpace(string(incoming)))

	if currentStatus == "" || currentStatus == EventStatusUnknown {
		if incomingStatus != "" {
			return incomingStatus
		}
		return EventStatusUnknown
	}

	if override && incomingStatus != "" && incomingStatus != EventStatusUnknown {
		return incomingStatus
	}

	return currentStatus
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
