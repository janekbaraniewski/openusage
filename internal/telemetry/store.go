package telemetry

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// telemetryLog is the package-level structured logger used for telemetry's
// emitted events. Component=telemetry; level/event are the daemon-style
// pair so dashboards / log greps can consume both daemon and telemetry
// output uniformly. Keep this as the convention for new log lines added
// in this package.
var telemetryLog = core.NewLogger("telemetry")

type Store struct {
	db  *sql.DB
	now func() time.Time
}

// openAndConfigureDB opens a SQLite database at the given path and applies
// connection pragmas. Caller is responsible for closing on error.
func openAndConfigureDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("telemetry: opening DB: %w", err)
	}
	if err := configureSQLiteConnection(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("telemetry: configure sqlite: %w", err)
	}
	return db, nil
}

func OpenStore(path string) (*Store, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("telemetry: creating DB dir: %w", err)
	}

	// Remove the shared-memory file before opening the database.
	// After an unclean shutdown (SIGKILL, OOM, crash), the -shm file
	// retains stale WAL frame indexes and lock counters from the dead
	// process. If a new process opens the DB and trusts the stale -shm,
	// it can misread WAL frames, causing duplicate page references and
	// B-tree corruption. Removing the -shm forces SQLite to rebuild the
	// WAL index from the checksummed WAL file, which is crash-safe.
	// If another process holds the DB open, the file is still
	// referenced via its inode and that process is unaffected.
	_ = os.Remove(path + "-shm")

	db, err := openAndConfigureDB(path)
	if err != nil {
		return nil, err
	}

	// Quick integrity check before proceeding. If the database is corrupt
	// (e.g. from a previous unclean shutdown that the -shm removal didn't
	// fully recover), back it up and start fresh rather than serving bad data.
	if corrupt, detail := quickIntegrityCheck(db); corrupt {
		_ = db.Close()
		backupPath := path + ".corrupt." + time.Now().Format("20060102T150405")
		log.Printf("telemetry: database corrupt (%s), backing up to %s and starting fresh", detail, backupPath)
		if err := os.Rename(path, backupPath); err != nil {
			return nil, fmt.Errorf("telemetry: backup corrupt DB: %w", err)
		}
		_ = os.Remove(path + "-wal")
		_ = os.Remove(path + "-shm")
		db, err = openAndConfigureDB(path)
		if err != nil {
			return nil, fmt.Errorf("telemetry: opening fresh DB after corruption: %w", err)
		}
	}

	// Reclaim disk from past corruption rotations. Each corruption event renames
	// the DB to "<path>.corrupt.<ts>" and starts fresh; these snapshots are
	// corrupt by definition and never read again, so they only waste disk (they
	// have grown to multiple GB in the field). Keep the most recent one for
	// forensics and delete the rest.
	pruneCorruptBackups(path, 1)

	store := NewStore(db)
	if err := store.Init(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

// pruneCorruptBackups removes old "<dbPath>.corrupt.*" snapshots, keeping the
// newest `keep` by modification time. Best-effort: errors are logged but never
// fatal. Companion -wal/-shm files for each removed snapshot are removed too.
func pruneCorruptBackups(dbPath string, keep int) {
	if keep < 0 {
		keep = 0
	}
	matches, err := filepath.Glob(dbPath + ".corrupt.*")
	if err != nil || len(matches) == 0 {
		return
	}
	// Only consider the base snapshot files, not their -wal/-shm sidecars.
	bases := make([]string, 0, len(matches))
	for _, m := range matches {
		if strings.HasSuffix(m, "-wal") || strings.HasSuffix(m, "-shm") {
			continue
		}
		bases = append(bases, m)
	}
	if len(bases) <= keep {
		return
	}
	// Sort newest-first by modtime so the first `keep` are retained.
	type backup struct {
		path string
		mod  time.Time
	}
	infos := make([]backup, 0, len(bases))
	for _, b := range bases {
		fi, statErr := os.Stat(b)
		if statErr != nil {
			continue
		}
		infos = append(infos, backup{path: b, mod: fi.ModTime()})
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].mod.After(infos[j].mod) })

	var freed int64
	removed := 0
	for _, b := range infos[min(keep, len(infos)):] {
		if fi, statErr := os.Stat(b.path); statErr == nil {
			freed += fi.Size()
		}
		if rmErr := os.Remove(b.path); rmErr != nil {
			log.Printf("telemetry: could not remove stale corrupt backup %s: %v", b.path, rmErr)
			continue
		}
		_ = os.Remove(b.path + "-wal")
		_ = os.Remove(b.path + "-shm")
		removed++
	}
	if removed > 0 {
		log.Printf("telemetry: removed %d stale corrupt DB backup(s), reclaimed %d MB", removed, freed/(1024*1024))
	}
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

// DB returns the underlying database handle for operations that need direct
// access (e.g. WAL checkpointing).
func (s *Store) DB() *sql.DB {
	if s == nil {
		return nil
	}
	return s.db
}

// Vacuum reclaims disk space from deleted rows. Should be called after large
// batch deletions (e.g. retention pruning). This can be slow on large databases.
func (s *Store) Vacuum(ctx context.Context) error {
	if s == nil || s.db == nil {
		return nil
	}
	_, err := s.db.ExecContext(ctx, "VACUUM")
	return err
}

// Analyze updates SQLite's query planner statistics for all tables and indexes.
func (s *Store) Analyze(ctx context.Context) error {
	if s == nil || s.db == nil {
		return nil
	}
	_, err := s.db.ExecContext(ctx, "ANALYZE")
	return err
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
		`CREATE INDEX IF NOT EXISTS idx_usage_events_raw_event_id ON usage_events(raw_event_id);`,
		`CREATE INDEX IF NOT EXISTS idx_usage_events_provider_window ON usage_events(provider_id, account_id, occurred_at);`,
		`CREATE INDEX IF NOT EXISTS idx_usage_events_provider_account_type_occurred ON usage_events(provider_id, account_id, event_type, occurred_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_usage_events_type_provider ON usage_events(event_type, provider_id);`,
		`CREATE INDEX IF NOT EXISTS idx_usage_raw_events_source_system ON usage_raw_events(source_system);`,
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
		// balance_observations is a compact, durable numeric time-series of each
		// credit/balance metric we observe on every poll. Unlike the limit_snapshot
		// raw payloads (wiped to '{}' after 1h), these rows persist for the full
		// retention horizon so windowed spend can be computed as a delta over any
		// window. One row is a handful of floats, so the series stays tiny.
		`CREATE TABLE IF NOT EXISTS balance_observations (
			provider_id TEXT NOT NULL,
			account_id TEXT NOT NULL,
			metric_key TEXT NOT NULL,
			observed_at TEXT NOT NULL,
			used REAL,
			limit_val REAL,
			remaining REAL,
			unit TEXT,
			semantics TEXT NOT NULL,
			PRIMARY KEY (provider_id, account_id, metric_key, observed_at)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_balance_obs_lookup ON balance_observations(provider_id, account_id, metric_key, observed_at);`,
		// Daily downsample of usage_events: per-day aggregates kept long-term so
		// raw per-event rows past the hot window can be pruned without losing the
		// shape of history. See docs/TELEMETRY_TIERED_RETENTION_DESIGN.md.
		// Dimensions are limited to column-backed fields on usage_events. project
		// is workspace_id (well-populated on historical rows). language/interface
		// are intentionally absent: they are derived from source_payload, which is
		// blanked ~1h after ingest, so they are already unavailable for old events
		// regardless of downsampling. Capturing them would require denormalizing
		// at ingest (separate change).
		`CREATE TABLE IF NOT EXISTS usage_rollup_daily (
			day TEXT NOT NULL,
			provider_id TEXT NOT NULL,
			account_id TEXT NOT NULL DEFAULT '',
			model_canonical TEXT NOT NULL DEFAULT '',
			tool_name TEXT NOT NULL DEFAULT '',
			project TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT '',
			input_tokens INTEGER NOT NULL DEFAULT 0,
			output_tokens INTEGER NOT NULL DEFAULT 0,
			reasoning_tokens INTEGER NOT NULL DEFAULT 0,
			cache_read_tokens INTEGER NOT NULL DEFAULT 0,
			cache_write_tokens INTEGER NOT NULL DEFAULT 0,
			total_tokens INTEGER NOT NULL DEFAULT 0,
			cost_usd REAL NOT NULL DEFAULT 0,
			requests INTEGER NOT NULL DEFAULT 0,
			event_count INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (day, provider_id, account_id, model_canonical, tool_name, project, status)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_rollup_daily_window ON usage_rollup_daily(provider_id, account_id, day);`,
		// Key/value store for daemon-internal state (e.g. the rollup watermark).
		`CREATE TABLE IF NOT EXISTS daemon_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);`,
	}

	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("telemetry: init schema: %w", err)
		}
	}
	return nil
}

// RunMigrations runs one-shot data repair migrations. Called at daemon startup.
func (s *Store) RunMigrations(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS _migrations (name TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}

	repairs := []struct {
		name string
		sql  string
	}{
		{
			name: "repair_codex_provider_id",
			sql: `UPDATE usage_events
				SET provider_id = 'codex'
				WHERE LOWER(TRIM(provider_id)) = 'openai'
				  AND LOWER(TRIM(agent_name)) = 'codex'
				  AND raw_event_id IN (
					SELECT raw_event_id FROM usage_raw_events WHERE LOWER(TRIM(source_system)) = 'codex'
				  )`,
		},
		{
			name: "repair_codex_account_id",
			sql: `UPDATE usage_events
				SET account_id = 'codex-cli'
				WHERE LOWER(TRIM(provider_id)) = 'codex'
				  AND LOWER(TRIM(account_id)) = 'codex'
				  AND LOWER(TRIM(agent_name)) = 'codex'
				  AND raw_event_id IN (
					SELECT raw_event_id FROM usage_raw_events WHERE LOWER(TRIM(source_system)) = 'codex'
				  )`,
		},
		{
			name: "repair_cursor_provider_id",
			sql: `UPDATE usage_events
				SET provider_id = 'cursor'
				WHERE LOWER(TRIM(provider_id)) != 'cursor'
				  AND raw_event_id IN (
					SELECT raw_event_id FROM usage_raw_events WHERE LOWER(TRIM(source_system)) = 'cursor'
				  )`,
		},
		{
			name: "cleanup_zero_timestamp_events",
			sql: `DELETE FROM usage_events
				WHERE event_id IN (
					SELECT e.event_id
					FROM usage_events e
					JOIN usage_raw_events r ON r.raw_event_id = e.raw_event_id
					WHERE LOWER(TRIM(r.source_system)) IN ('cursor', 'ollama')
					  AND (e.session_id IS NULL OR TRIM(e.session_id) = '')
					  AND ABS(julianday(e.occurred_at) - julianday(r.ingested_at)) < 0.00002
				)`,
		},
	}

	for _, r := range repairs {
		var exists int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM _migrations WHERE name = ?`, r.name).Scan(&exists); err != nil {
			return fmt.Errorf("check migration %s: %w", r.name, err)
		}
		if exists > 0 {
			continue
		}
		telemetryLog.Infof("migration_run", "name=%q", r.name)
		start := time.Now()
		if _, err := s.db.ExecContext(ctx, r.sql); err != nil {
			return fmt.Errorf("run migration %s: %w", r.name, err)
		}
		if _, err := s.db.ExecContext(ctx, `INSERT INTO _migrations (name, applied_at) VALUES (?, datetime('now'))`, r.name); err != nil {
			return fmt.Errorf("record migration %s: %w", r.name, err)
		}
		telemetryLog.Infof("migration_complete", "name=%q duration_ms=%d", r.name, time.Since(start).Milliseconds())
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

	existing, found, err := findEventByDedupKey(ctx, tx, dedupKey)
	if err != nil {
		return IngestResult{}, fmt.Errorf("telemetry: lookup dedup key: %w", err)
	}
	if found {
		if enrichErr := enrichEventByDedupKey(ctx, tx, dedupKey, norm); enrichErr != nil {
			return IngestResult{}, fmt.Errorf("telemetry: enrich dedup event: %w", enrichErr)
		}
		if commitErr := tx.Commit(); commitErr != nil {
			return IngestResult{}, fmt.Errorf("telemetry: commit dedup tx: %w", commitErr)
		}
		return IngestResult{
			Status:     "accepted",
			Deduped:    true,
			EventID:    existing.EventID,
			RawEventID: existing.RawEventID,
		}, nil
	}

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
			existing, found, lookupErr := findEventByDedupKey(ctx, tx, dedupKey)
			if lookupErr != nil {
				return IngestResult{}, fmt.Errorf("telemetry: lookup dedup event: %w", lookupErr)
			}
			if !found {
				return IngestResult{}, fmt.Errorf("telemetry: dedup event disappeared for key %q", dedupKey)
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
				EventID:    existing.EventID,
				RawEventID: existing.RawEventID,
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

type storedDedupEventRef struct {
	EventID    string
	RawEventID string
}

func findEventByDedupKey(ctx context.Context, tx *sql.Tx, dedupKey string) (storedDedupEventRef, bool, error) {
	var ref storedDedupEventRef
	err := tx.QueryRowContext(
		ctx,
		`SELECT event_id, raw_event_id FROM usage_events WHERE dedup_key = ? LIMIT 1`,
		dedupKey,
	).Scan(&ref.EventID, &ref.RawEventID)
	if err != nil {
		if err == sql.ErrNoRows {
			return storedDedupEventRef{}, false, nil
		}
		return storedDedupEventRef{}, false, err
	}
	return ref, true, nil
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
	toolName := chooseToolName(current.ToolName, norm.ToolName, override)

	inputTokens := chooseInt64(current.InputTokens, norm.InputTokens, override)
	outputTokens := chooseInt64(current.OutputTokens, norm.OutputTokens, override)
	reasoningTokens := chooseInt64(current.Reasoning, norm.ReasoningTokens, override)
	cacheReadTokens := chooseInt64(current.CacheRead, norm.CacheReadTokens, override)
	cacheWriteTokens := chooseInt64(current.CacheWrite, norm.CacheWriteTokens, override)
	totalTokens := chooseInt64(current.TotalTokens, norm.TotalTokens, override)
	costUSD := chooseFloat64(current.CostUSD, norm.CostUSD, override)
	requests := chooseInt64(current.Requests, norm.Requests, override)
	status := chooseStatus(current.Status, norm.Status, override)

	// A re-ingested event usually merges to exactly what is already stored:
	// local-file sources re-import their recent history every collection
	// cycle, and a hook event often arrives again from the session file. The
	// UPDATE below rewrites twenty columns, dirtying the row's page and every
	// index covering them, and all of it lands in the WAL. Issuing it for a
	// merge that changes nothing turned steady-state ingest into sustained
	// write amplification (#318), so compare first and skip the no-op.
	if !canonicalEventDiffers(current, canonicalEventFields{
		providerID:       providerID,
		accountID:        accountID,
		workspaceID:      workspaceID,
		sessionID:        sessionID,
		turnID:           turnID,
		messageID:        messageID,
		toolCallID:       toolCallID,
		modelRaw:         modelRaw,
		modelCanonical:   modelCanonical,
		modelLineage:     modelLineage,
		toolName:         toolName,
		inputTokens:      inputTokens,
		outputTokens:     outputTokens,
		reasoningTokens:  reasoningTokens,
		cacheReadTokens:  cacheReadTokens,
		cacheWriteTokens: cacheWriteTokens,
		totalTokens:      totalTokens,
		costUSD:          costUSD,
		requests:         requests,
		status:           status,
	}) {
		return nil
	}

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

// canonicalEventFields is the merged result enrichEventByDedupKey is about to
// write, in the same nullable-or-zero form the UPDATE binds.
type canonicalEventFields struct {
	providerID       string
	accountID        string
	workspaceID      string
	sessionID        string
	turnID           string
	messageID        string
	toolCallID       string
	modelRaw         string
	modelCanonical   string
	modelLineage     string
	toolName         string
	inputTokens      *int64
	outputTokens     *int64
	reasoningTokens  *int64
	cacheReadTokens  *int64
	cacheWriteTokens *int64
	totalTokens      *int64
	costUSD          *float64
	requests         *int64
	status           EventStatus
}

// canonicalEventDiffers reports whether writing merged would change the stored
// row. Each comparison mirrors the binder the UPDATE uses for that column, so
// a value the binder would store as NULL compares equal to a NULL column:
// nullable() nulls a blank string, and nullableInt64/nullableFloat64 null a nil
// pointer. Getting that mapping wrong would either skip a real update or never
// skip at all, so the binders are the reference, not the Go zero value.
func canonicalEventDiffers(current storedCanonicalEvent, merged canonicalEventFields) bool {
	strFields := []struct {
		stored sql.NullString
		next   string
	}{
		{current.ProviderID, merged.providerID},
		{current.AccountID, merged.accountID},
		{current.WorkspaceID, merged.workspaceID},
		{current.SessionID, merged.sessionID},
		{current.TurnID, merged.turnID},
		{current.MessageID, merged.messageID},
		{current.ToolCallID, merged.toolCallID},
		{current.ModelRaw, merged.modelRaw},
		{current.ModelCanonical, merged.modelCanonical},
		{current.ModelLineageID, merged.modelLineage},
		{current.ToolName, merged.toolName},
	}
	for _, f := range strFields {
		if nullStringDiffers(f.stored, f.next) {
			return true
		}
	}

	intFields := []struct {
		stored sql.NullInt64
		next   *int64
	}{
		{current.InputTokens, merged.inputTokens},
		{current.OutputTokens, merged.outputTokens},
		{current.Reasoning, merged.reasoningTokens},
		{current.CacheRead, merged.cacheReadTokens},
		{current.CacheWrite, merged.cacheWriteTokens},
		{current.TotalTokens, merged.totalTokens},
		{current.Requests, merged.requests},
	}
	for _, f := range intFields {
		if nullInt64Differs(f.stored, f.next) {
			return true
		}
	}

	if nullFloat64Differs(current.CostUSD, merged.costUSD) {
		return true
	}

	// status is bound as a plain string, never NULL.
	return current.Status != string(merged.status)
}

// nullStringDiffers compares a stored column against what nullable(next) would
// write: NULL for a blank string, otherwise next verbatim.
func nullStringDiffers(stored sql.NullString, next string) bool {
	if strings.TrimSpace(next) == "" {
		return stored.Valid
	}
	return !stored.Valid || stored.String != next
}

// nullInt64Differs compares a stored column against what nullableInt64(next)
// would write: NULL for a nil pointer, otherwise *next.
func nullInt64Differs(stored sql.NullInt64, next *int64) bool {
	if next == nil {
		return stored.Valid
	}
	return !stored.Valid || stored.Int64 != *next
}

// nullFloat64Differs compares a stored column against what
// nullableFloat64(next) would write: NULL for a nil pointer, otherwise *next.
func nullFloat64Differs(stored sql.NullFloat64, next *float64) bool {
	if next == nil {
		return stored.Valid
	}
	return !stored.Valid || stored.Float64 != *next
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

func chooseToolName(current sql.NullString, incoming string, override bool) string {
	currentName := strings.ToLower(strings.TrimSpace(current.String))
	incomingName := strings.ToLower(strings.TrimSpace(incoming))

	if !current.Valid || currentName == "" {
		return incomingName
	}
	if override && incomingName != "" {
		return incomingName
	}
	if incomingName == "" {
		return currentName
	}
	if currentName == "unknown" && incomingName != "unknown" {
		return incomingName
	}
	// When parsers improve MCP normalization over time, prefer canonical
	// mcp__server__function labels so existing deduped rows self-heal.
	if isCanonicalMCPToolName(incomingName) && !isCanonicalMCPToolName(currentName) {
		return incomingName
	}
	return currentName
}

func isCanonicalMCPToolName(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if !strings.HasPrefix(normalized, "mcp__") {
		return false
	}
	rest := strings.TrimPrefix(normalized, "mcp__")
	parts := strings.SplitN(rest, "__", 2)
	if len(parts) != 2 {
		return false
	}
	return strings.TrimSpace(parts[0]) != "" && strings.TrimSpace(parts[1]) != ""
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

// pruneEventsBatch is the number of rows PruneOldEvents deletes per statement.
// Bounded so each DELETE holds the write lock briefly and the loop always makes
// forward progress even under poll/checkpoint contention — a single unbounded
// DELETE of a large backlog can lose its write-lock race and time out, which is
// how retention silently stalled in the field (months of data accumulating
// despite a correct 30-day policy).
const pruneEventsBatch = 5000

// PruneOldEvents deletes usage_events older than retentionDays (the hot window)
// in bounded batches — but only days that have already been rolled up into
// usage_rollup_daily, so per-event detail is never discarded before its
// aggregate exists. rolledThroughDay is the rollup watermark (YYYY-MM-DD); an
// empty watermark means the rollup has not run, so nothing is pruned.
//
// Returns the number of rows deleted and whether the backlog was fully drained
// (complete=false means it stopped early — context cancelled or a batch error
// after partial progress — so the caller should reschedule soon).
func (s *Store) PruneOldEvents(ctx context.Context, retentionDays int, rolledThroughDay string) (deleted int64, complete bool, err error) {
	if s == nil || s.db == nil || retentionDays <= 0 {
		return 0, true, nil
	}
	if strings.TrimSpace(rolledThroughDay) == "" {
		// Rollup hasn't established a watermark yet — refuse to delete un-rolled
		// detail. Not an error; the next pass (after a rollup) will prune.
		return 0, false, nil
	}
	cutoff := s.retentionCutoff(time.Duration(retentionDays) * 24 * time.Hour)
	for {
		if ctx.Err() != nil {
			return deleted, false, nil
		}
		// mattn/go-sqlite3 isn't built with the DELETE...LIMIT extension, so
		// scope the delete via a subselect on the indexed occurred_at column.
		// The date() guard is the safety gate: only delete days at or before the
		// rollup watermark.
		result, execErr := s.db.ExecContext(ctx, `
			DELETE FROM usage_events
			WHERE event_id IN (
				SELECT event_id FROM usage_events
				WHERE occurred_at < ?
				  AND date(occurred_at) <= ?
				ORDER BY occurred_at ASC
				LIMIT ?
			)
		`, cutoff, rolledThroughDay, pruneEventsBatch)
		if execErr != nil {
			if deleted > 0 {
				return deleted, false, nil
			}
			return 0, false, fmt.Errorf("telemetry: prune old events: %w", execErr)
		}
		n, _ := result.RowsAffected()
		deleted += n
		if n < pruneEventsBatch {
			return deleted, true, nil
		}
	}
}

func (s *Store) PruneOrphanRawEvents(ctx context.Context, limit int) (int64, error) {
	if s == nil || s.db == nil || limit <= 0 {
		return 0, nil
	}
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM usage_raw_events
		WHERE raw_event_id IN (
			SELECT r.raw_event_id
			FROM usage_raw_events r
			WHERE NOT EXISTS (
				SELECT 1 FROM usage_events e WHERE e.raw_event_id = r.raw_event_id
			)
			ORDER BY r.ingested_at ASC
			LIMIT ?
		)
	`, limit)
	if err != nil {
		return 0, fmt.Errorf("telemetry: prune orphan raw events: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("telemetry: prune orphan raw events rows affected: %w", err)
	}
	return n, nil
}

// retentionCutoff renders a horizon as an RFC3339Nano string comparable against
// the timestamp columns.
//
// The columns store time.RFC3339Nano ("2026-08-31T08:57:12.211861Z"), so they
// must not be compared against datetime('now', ...), which renders a space
// separator ("2026-08-31 07:57:34"). Those forms only sort consistently when
// the dates differ: on the same day the 'T' (0x54) outranks the ' ' (0x20), so
// every row ingested earlier today compares as newer than the horizon and
// escapes pruning. The horizon effectively became "previous calendar days"
// instead of "older than N hours".
//
// Rendering the bound in Go keeps both sides in one format and, unlike wrapping
// the column in datetime(), leaves the index on that column usable.
func (s *Store) retentionCutoff(d time.Duration) string {
	return s.now().UTC().Add(-d).Format(time.RFC3339Nano)
}

// PruneRawEventPayloads clears source_payload from old raw events to reclaim
// disk space. For usage events all useful data has already been extracted into
// usage_events columns, so the payload is redundant detail. Keeps payloads for
// events newer than retentionHours.
//
// limit_snapshot is the exception: its payload IS the read model's source of
// truth. hydrateRootsFromLimitSnapshots reads the newest one back per
// provider/account to rebuild a provider's metrics, and nothing in
// usage_events can substitute for it. Blanking it left the dashboard
// hydrating from an empty envelope, which reads as a confident UNKNOWN with no
// metrics and survives a daemon restart because the damage is in the database
// (#293).
//
// Only the newest limit_snapshot per (provider_id, account_id) is protected.
// The historical ones are never read back and are the bulk of the volume, so
// they still get reclaimed.
func (s *Store) PruneRawEventPayloads(ctx context.Context, retentionHours int, limit int) (int64, error) {
	if s == nil || s.db == nil || retentionHours < 0 || limit <= 0 {
		return 0, nil
	}
	cutoff := s.retentionCutoff(time.Duration(retentionHours) * time.Hour)
	res, err := s.db.ExecContext(ctx, `
		UPDATE usage_raw_events
		SET source_payload = '{}'
		WHERE raw_event_id IN (
			SELECT raw_event_id
			FROM usage_raw_events
			WHERE ingested_at < ?
			  AND source_payload != '{}'
			  AND raw_event_id NOT IN (
				SELECT e.raw_event_id
				FROM usage_events e
				JOIN (
					SELECT
						COALESCE(provider_id, '') AS provider_id,
						COALESCE(account_id, '') AS account_id,
						MAX(occurred_at) AS occurred_at
					FROM usage_events
					WHERE event_type = 'limit_snapshot'
					GROUP BY 1, 2
				) latest
				  ON COALESCE(e.provider_id, '') = latest.provider_id
				 AND COALESCE(e.account_id, '') = latest.account_id
				 AND e.occurred_at = latest.occurred_at
				WHERE e.event_type = 'limit_snapshot'
			  )
			ORDER BY ingested_at ASC
			LIMIT ?
		)
	`, cutoff, limit)
	if err != nil {
		return 0, fmt.Errorf("telemetry: prune raw event payloads: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("telemetry: prune raw event payloads rows affected: %w", err)
	}
	return n, nil
}

// newUUID generates a random UUID v4 string.
func newUUID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	buf[6] = (buf[6] & 0x0f) | 0x40 // version 4
	buf[8] = (buf[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16]), nil
}
