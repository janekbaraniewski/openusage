package zed

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"

	_ "github.com/mattn/go-sqlite3"
)

// maxDecompressedBytes caps the inflated thread payload at 32 MB. Anything
// larger almost certainly indicates a corrupted or hostile record and would
// only burn memory.
const maxDecompressedBytes int64 = 32 << 20

// zedThread is the flattened representation we surface upstream after parsing
// a row of Zed's threads table.
type zedThread struct {
	ThreadID     string
	Model        string
	Provider     string
	Timestamp    time.Time
	Input        int64
	Output       int64
	CacheRead    int64
	CacheWrite   int64
	Reasoning    int64
	MessageCount int64
	Workspace    string
}

// threadColumns summarises which optional columns are present in the
// `threads` table. Older Zed schemas omitted `created_at` and the folder
// columns, so we probe before SELECTing.
type threadColumns struct {
	HasCreatedAt   bool
	HasFolderPaths bool
	HasFolderOrder bool
	HasUpdatedAt   bool
	HasDataType    bool
	HasData        bool
}

// detectColumns inspects the threads table via PRAGMA table_info.
func detectColumns(ctx context.Context, db *sql.DB) (threadColumns, error) {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(threads)`)
	if err != nil {
		return threadColumns{}, fmt.Errorf("zed: reading threads schema: %w", err)
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
			return threadColumns{}, fmt.Errorf("zed: scanning threads schema: %w", err)
		}
		present[strings.ToLower(strings.TrimSpace(name))] = true
	}
	if err := rows.Err(); err != nil {
		return threadColumns{}, fmt.Errorf("zed: iterating threads schema: %w", err)
	}

	return threadColumns{
		HasCreatedAt:   present["created_at"],
		HasFolderPaths: present["folder_paths"],
		HasFolderOrder: present["folder_paths_order"],
		HasUpdatedAt:   present["updated_at"],
		HasDataType:    present["data_type"],
		HasData:        present["data"],
	}, nil
}

// openReadOnly opens threads.db using SQLite's read-only, immutable URI so we
// never compete for the lock with the live Zed process.
func openReadOnly(dbPath string) (*sql.DB, error) {
	if dbPath == "" {
		return nil, fmt.Errorf("zed: empty db path")
	}
	encoded := (&url.URL{Path: dbPath}).EscapedPath()
	dsn := fmt.Sprintf("file:%s?mode=ro&immutable=1", encoded)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("zed: opening threads db: %w", err)
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

// queryZedThreads returns every thread row in the database that targets the
// hosted "zed.dev" model provider. Rows from local/self-hosted providers
// (Ollama, vLLM, ...) are intentionally skipped; they have no billing
// implication for the user.
func queryZedThreads(ctx context.Context, dbPath string) ([]zedThread, error) {
	db, err := openReadOnly(dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("zed: pinging threads db: %w", err)
	}

	cols, err := detectColumns(ctx, db)
	if err != nil {
		return nil, err
	}
	if !cols.HasData || !cols.HasDataType {
		return nil, nil
	}

	// Build the SELECT list dynamically based on what columns are present.
	// We always select id + updated_at + data_type + data; everything else
	// is NULL-coalesced when absent.
	selectParts := []string{"id"}
	if cols.HasUpdatedAt {
		selectParts = append(selectParts, "updated_at")
	} else {
		selectParts = append(selectParts, "NULL AS updated_at")
	}
	if cols.HasCreatedAt {
		selectParts = append(selectParts, "created_at")
	} else {
		selectParts = append(selectParts, "NULL AS created_at")
	}
	if cols.HasFolderPaths {
		selectParts = append(selectParts, "folder_paths")
	} else {
		selectParts = append(selectParts, "NULL AS folder_paths")
	}
	if cols.HasFolderOrder {
		selectParts = append(selectParts, "folder_paths_order")
	} else {
		selectParts = append(selectParts, "NULL AS folder_paths_order")
	}
	selectParts = append(selectParts, "data_type", "data")

	query := fmt.Sprintf(`SELECT %s FROM threads`, strings.Join(selectParts, ", "))

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("zed: querying threads: %w", err)
	}
	defer rows.Close()

	var out []zedThread
	for rows.Next() {
		var (
			id          string
			updatedAt   sql.NullString
			createdAt   sql.NullString
			folderPaths sql.NullString
			folderOrder sql.NullString
			dataType    sql.NullString
			data        []byte
		)
		if err := rows.Scan(&id, &updatedAt, &createdAt, &folderPaths, &folderOrder, &dataType, &data); err != nil {
			return nil, fmt.Errorf("zed: scanning thread row: %w", err)
		}

		payload, ok := decodeThreadData(data, dataType.String)
		if !ok {
			continue
		}

		thread, ok := parseThreadPayload(payload)
		if !ok {
			continue
		}

		thread.ThreadID = strings.TrimSpace(id)
		thread.Timestamp = pickThreadTimestamp(thread.Timestamp, createdAt.String, updatedAt.String)
		thread.Workspace = pickWorkspace(folderPaths.String, folderOrder.String)
		out = append(out, thread)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("zed: iterating thread rows: %w", err)
	}

	return out, nil
}

// decodeThreadData returns the JSON bytes for a thread row, decompressing
// when data_type indicates a zstd payload. Returns (nil, false) when the
// data is unusable.
func decodeThreadData(data []byte, dataType string) ([]byte, bool) {
	if len(data) == 0 {
		return nil, false
	}
	switch strings.ToLower(strings.TrimSpace(dataType)) {
	case "", "json":
		return data, true
	case "zstd":
		inflated, err := decompressZstd(data)
		if err != nil {
			return nil, false
		}
		return inflated, true
	default:
		// Unknown data_type. Be conservative.
		return nil, false
	}
}

// decompressZstd decompresses up to maxDecompressedBytes from compressed.
// Refuses to allocate beyond the cap; truncated output is treated as a
// failure so we don't surface partial JSON.
func decompressZstd(compressed []byte) ([]byte, error) {
	reader, err := zstd.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, fmt.Errorf("zed: zstd reader: %w", err)
	}
	defer reader.Close()

	limited := io.LimitReader(reader, maxDecompressedBytes+1)
	out, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("zed: zstd decode: %w", err)
	}
	if int64(len(out)) > maxDecompressedBytes {
		return nil, fmt.Errorf("zed: zstd payload exceeds %d bytes", maxDecompressedBytes)
	}
	return out, nil
}

// zedThreadPayload mirrors the fields we extract from the inflated thread
// JSON. Unknown fields are ignored.
type zedThreadPayload struct {
	Model             *zedModel              `json:"model,omitempty"`
	RequestTokenUsage []zedRequestTokenUsage `json:"request_token_usage,omitempty"`
	CumulativeUsage   *zedTokenBreakdown     `json:"cumulative_token_usage,omitempty"`
	Messages          json.RawMessage        `json:"messages,omitempty"`
	UpdatedAt         string                 `json:"updated_at,omitempty"`
	CreatedAt         string                 `json:"created_at,omitempty"`
	MessageCount      int64                  `json:"message_count,omitempty"`
}

type zedModel struct {
	Provider string `json:"provider,omitempty"`
	Name     string `json:"name,omitempty"`
	ID       string `json:"id,omitempty"`
}

type zedRequestTokenUsage struct {
	Tokens zedTokenBreakdown `json:"token_usage"`
}

type zedTokenBreakdown struct {
	InputTokens          int64 `json:"input_tokens,omitempty"`
	OutputTokens         int64 `json:"output_tokens,omitempty"`
	CacheReadInputTokens int64 `json:"cache_read_input_tokens,omitempty"`
	CacheCreationTokens  int64 `json:"cache_creation_input_tokens,omitempty"`
	ReasoningTokens      int64 `json:"reasoning_tokens,omitempty"`
}

// parseThreadPayload parses an inflated thread JSON body, filters by the
// "zed.dev" provider, and aggregates token usage. The returned bool is false
// when the row should be skipped (wrong provider, missing model, or zero
// tokens).
func parseThreadPayload(payload []byte) (zedThread, bool) {
	var raw zedThreadPayload
	if err := json.Unmarshal(payload, &raw); err != nil {
		return zedThread{}, false
	}
	if raw.Model == nil {
		return zedThread{}, false
	}
	if !strings.EqualFold(strings.TrimSpace(raw.Model.Provider), "zed.dev") {
		return zedThread{}, false
	}

	model := strings.TrimSpace(raw.Model.Name)
	if model == "" {
		model = strings.TrimSpace(raw.Model.ID)
	}
	if model == "" {
		return zedThread{}, false
	}

	var totals zedTokenBreakdown
	if len(raw.RequestTokenUsage) > 0 {
		for _, entry := range raw.RequestTokenUsage {
			totals.InputTokens += entry.Tokens.InputTokens
			totals.OutputTokens += entry.Tokens.OutputTokens
			totals.CacheReadInputTokens += entry.Tokens.CacheReadInputTokens
			totals.CacheCreationTokens += entry.Tokens.CacheCreationTokens
			totals.ReasoningTokens += entry.Tokens.ReasoningTokens
		}
	} else if raw.CumulativeUsage != nil {
		totals = *raw.CumulativeUsage
	}

	if totals.InputTokens == 0 && totals.OutputTokens == 0 &&
		totals.CacheReadInputTokens == 0 && totals.CacheCreationTokens == 0 &&
		totals.ReasoningTokens == 0 {
		return zedThread{}, false
	}

	// MessageCount: prefer the embedded counter; otherwise count the
	// messages array (best-effort).
	messageCount := raw.MessageCount
	if messageCount == 0 && len(raw.Messages) > 0 {
		var msgs []json.RawMessage
		if json.Unmarshal(raw.Messages, &msgs) == nil {
			messageCount = int64(len(msgs))
		}
	}

	var ts time.Time
	if raw.CreatedAt != "" {
		if parsed, err := time.Parse(time.RFC3339, raw.CreatedAt); err == nil {
			ts = parsed.UTC()
		}
	}
	if ts.IsZero() && raw.UpdatedAt != "" {
		if parsed, err := time.Parse(time.RFC3339, raw.UpdatedAt); err == nil {
			ts = parsed.UTC()
		}
	}

	return zedThread{
		Model:        model,
		Provider:     "zed.dev",
		Input:        totals.InputTokens,
		Output:       totals.OutputTokens,
		CacheRead:    totals.CacheReadInputTokens,
		CacheWrite:   totals.CacheCreationTokens,
		Reasoning:    totals.ReasoningTokens,
		MessageCount: messageCount,
		Timestamp:    ts,
	}, true
}

// pickThreadTimestamp resolves the timestamp using a payload-first,
// column-fallback policy. Returns the first parseable RFC3339 string.
func pickThreadTimestamp(payloadTS time.Time, createdAt, updatedAt string) time.Time {
	if !payloadTS.IsZero() {
		return payloadTS
	}
	for _, candidate := range []string{createdAt, updatedAt} {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if parsed, err := time.Parse(time.RFC3339, candidate); err == nil {
			return parsed.UTC()
		}
		// Best-effort: Unix seconds stored as TEXT.
		if secs, err := strconv.ParseInt(candidate, 10, 64); err == nil && secs > 0 {
			return time.Unix(secs, 0).UTC()
		}
	}
	return time.Time{}
}

// pickWorkspace returns the priority workspace path for a thread.
// folder_paths is newline-delimited; folder_paths_order is a comma-separated
// list of indices into that slice. We pick the path indexed by the first
// entry of folder_paths_order, falling back to the first folder when order is
// missing.
func pickWorkspace(folderPaths, folderOrder string) string {
	folderPaths = strings.TrimSpace(folderPaths)
	if folderPaths == "" {
		return ""
	}
	paths := strings.Split(folderPaths, "\n")
	cleaned := make([]string, 0, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p != "" {
			cleaned = append(cleaned, p)
		}
	}
	if len(cleaned) == 0 {
		return ""
	}

	order := strings.TrimSpace(folderOrder)
	if order != "" {
		first := strings.SplitN(order, ",", 2)[0]
		first = strings.TrimSpace(first)
		if idx, err := strconv.Atoi(first); err == nil && idx >= 0 && idx < len(cleaned) {
			return cleaned[idx]
		}
	}
	return cleaned[0]
}
