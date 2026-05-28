package goose

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// makeTempDB constructs a fresh SQLite database file with a schema
// resembling the upstream sessions table. Callers pick which optional
// columns to include via opts so tests can exercise older/newer
// migrations on the same harness.
type schemaOpts struct {
	// Drop accumulated_* columns to simulate an older schema.
	dropAccumulated bool
	// Drop plain *_tokens columns to simulate the newest schema.
	dropPlainTokens bool
	// Drop model_config_json (degenerate; query must early-return empty).
	dropModelConfigJSON bool
	// Drop provider_name (older migration).
	dropProviderName bool
	// Drop accumulated_cost.
	dropAccumulatedCost bool
}

func makeTempDB(t *testing.T, opts schemaOpts) string {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sessions.db")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open temp db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	cols := []string{
		"id TEXT PRIMARY KEY",
		"created_at TEXT",
	}
	if !opts.dropModelConfigJSON {
		cols = append(cols, "model_config_json TEXT")
	}
	if !opts.dropProviderName {
		cols = append(cols, "provider_name TEXT")
	}
	if !opts.dropAccumulated {
		cols = append(cols,
			"accumulated_input_tokens INTEGER",
			"accumulated_output_tokens INTEGER",
			"accumulated_total_tokens INTEGER",
		)
	}
	if !opts.dropPlainTokens {
		cols = append(cols,
			"input_tokens INTEGER",
			"output_tokens INTEGER",
			"total_tokens INTEGER",
		)
	}
	if !opts.dropAccumulatedCost {
		cols = append(cols, "accumulated_cost REAL")
	}

	ddl := fmt.Sprintf("CREATE TABLE sessions (%s)", joinComma(cols))
	if _, err := db.Exec(ddl); err != nil {
		t.Fatalf("create sessions table: %v\nDDL: %s", err, ddl)
	}
	return dbPath
}

func joinComma(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out
}

// insertRow inserts a row using the supplied named-value map. Unknown
// columns are ignored silently (the test harness mirrors the schema-skew
// reality), so callers can keep one row template across multiple schema
// variants.
type rowValues struct {
	ID             string
	CreatedAt      string
	ModelConfigCol string // already-serialised JSON for model_config_json
	ProviderName   string
	AccInput       any
	AccOutput      any
	AccTotal       any
	Input          any
	Output         any
	Total          any
	AccCost        any
}

func insertRow(t *testing.T, dbPath string, opts schemaOpts, r rowValues) {
	t.Helper()
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open for insert: %v", err)
	}
	defer db.Close()

	var (
		cols []string
		ph   []string
		vals []any
	)
	add := func(name string, v any) {
		cols = append(cols, name)
		ph = append(ph, "?")
		vals = append(vals, v)
	}
	add("id", r.ID)
	add("created_at", r.CreatedAt)
	if !opts.dropModelConfigJSON {
		add("model_config_json", r.ModelConfigCol)
	}
	if !opts.dropProviderName {
		add("provider_name", r.ProviderName)
	}
	if !opts.dropAccumulated {
		add("accumulated_input_tokens", r.AccInput)
		add("accumulated_output_tokens", r.AccOutput)
		add("accumulated_total_tokens", r.AccTotal)
	}
	if !opts.dropPlainTokens {
		add("input_tokens", r.Input)
		add("output_tokens", r.Output)
		add("total_tokens", r.Total)
	}
	if !opts.dropAccumulatedCost {
		add("accumulated_cost", r.AccCost)
	}

	stmt := fmt.Sprintf("INSERT INTO sessions (%s) VALUES (%s)", joinComma(cols), joinComma(ph))
	if _, err := db.Exec(stmt, vals...); err != nil {
		t.Fatalf("insert row: %v\nstmt: %s", err, stmt)
	}
}

func TestQueryGooseSessions_AccumulatedSchema(t *testing.T) {
	opts := schemaOpts{}
	dbPath := makeTempDB(t, opts)

	// One healthy session with accumulated tokens populated.
	insertRow(t, dbPath, opts, rowValues{
		ID:             "20250518_1",
		CreatedAt:      "2025-05-18T10:30:00Z",
		ModelConfigCol: `{"model_name": "claude-opus-4-7"}`,
		ProviderName:   "anthropic",
		AccInput:       1000,
		AccOutput:      500,
		AccTotal:       1700, // 200 derived as reasoning
		Input:          50,   // stale per-turn; should be ignored in favour of accumulated
		Output:         25,
		Total:          75,
		AccCost:        0.04,
	})

	result, err := queryGooseSessions(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("queryGooseSessions: %v", err)
	}
	sessions := result.Sessions
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
	s := sessions[0]
	if s.Model != "claude-opus-4-7" {
		t.Errorf("model = %q, want claude-opus-4-7", s.Model)
	}
	if s.Provider != "anthropic" {
		t.Errorf("provider = %q, want anthropic", s.Provider)
	}
	if s.InputTokens != 1000 || s.OutputTokens != 500 || s.TotalTokens != 1700 {
		t.Errorf("tokens = (in=%d out=%d total=%d), want (1000, 500, 1700)",
			s.InputTokens, s.OutputTokens, s.TotalTokens)
	}
	if s.ReasoningTokens != 200 {
		t.Errorf("reasoning = %d, want 200", s.ReasoningTokens)
	}
	if !s.HasCost || s.AccumulatedCost != 0.04 {
		t.Errorf("cost: HasCost=%v value=%v, want HasCost=true value=0.04", s.HasCost, s.AccumulatedCost)
	}
	if s.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}
}

func TestQueryGooseSessions_PlainTokensFallback(t *testing.T) {
	// Schema with no accumulated_* columns; query must fall back to plain
	// columns.
	opts := schemaOpts{dropAccumulated: true, dropAccumulatedCost: true}
	dbPath := makeTempDB(t, opts)

	insertRow(t, dbPath, opts, rowValues{
		ID:             "20250518_2",
		CreatedAt:      "2025-05-18 10:30:00",
		ModelConfigCol: `{"model_name": "gpt-4o"}`,
		ProviderName:   "openai",
		Input:          100,
		Output:         50,
		Total:          150,
	})

	result, err := queryGooseSessions(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("queryGooseSessions: %v", err)
	}
	sessions := result.Sessions
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
	s := sessions[0]
	if s.InputTokens != 100 || s.OutputTokens != 50 || s.TotalTokens != 150 {
		t.Errorf("tokens = (in=%d out=%d total=%d), want (100, 50, 150)",
			s.InputTokens, s.OutputTokens, s.TotalTokens)
	}
	if s.ReasoningTokens != 0 {
		t.Errorf("reasoning = %d, want 0", s.ReasoningTokens)
	}
	if s.HasCost {
		t.Error("HasCost should be false when accumulated_cost column is missing")
	}
}

func TestQueryGooseSessions_TimestampVariants(t *testing.T) {
	opts := schemaOpts{}
	dbPath := makeTempDB(t, opts)

	tests := []struct {
		id        string
		ts        string
		wantUTC   string // expected "2006-01-02 15:04:05Z" representation
		wantValid bool
	}{
		{"rfc3339", "2025-05-18T10:30:00Z", "2025-05-18 10:30:00", true},
		{"rfc3339_off", "2025-05-18T10:30:00+02:00", "2025-05-18 08:30:00", true},
		{"sqlite_dt", "2025-05-18 10:30:00", "2025-05-18 10:30:00", true},
		{"date_only", "2025-05-18", "2025-05-18 00:00:00", true},
		{"garbage", "definitely not a date", "", false},
	}

	for i, tc := range tests {
		insertRow(t, dbPath, opts, rowValues{
			ID:             fmt.Sprintf("ts_%d", i),
			CreatedAt:      tc.ts,
			ModelConfigCol: `{"model_name": "test-model"}`,
			ProviderName:   "test",
			AccInput:       10,
			AccOutput:      5,
			AccTotal:       15,
		})
	}

	result, err := queryGooseSessions(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("queryGooseSessions: %v", err)
	}
	sessions := result.Sessions

	wantValidCount := 0
	wantSkipped := 0
	for _, tc := range tests {
		if tc.wantValid {
			wantValidCount++
		} else {
			wantSkipped++
		}
	}
	if len(sessions) != wantValidCount {
		t.Fatalf("got %d sessions, want %d valid", len(sessions), wantValidCount)
	}
	if result.SkippedRows != wantSkipped {
		t.Errorf("SkippedRows = %d, want %d", result.SkippedRows, wantSkipped)
	}
	// Verify each timestamp round-trips correctly.
	byID := make(map[string]gooseSession)
	for _, s := range sessions {
		byID[s.ID] = s
	}
	for i, tc := range tests {
		if !tc.wantValid {
			continue
		}
		id := fmt.Sprintf("ts_%d", i)
		s, ok := byID[id]
		if !ok {
			t.Errorf("missing session %s for ts=%q", id, tc.ts)
			continue
		}
		if got := s.CreatedAt.UTC().Format("2006-01-02 15:04:05"); got != tc.wantUTC {
			t.Errorf("ts=%q parsed to %q, want %q", tc.ts, got, tc.wantUTC)
		}
	}
}

func TestQueryGooseSessions_FiltersZeroTokenRows(t *testing.T) {
	opts := schemaOpts{}
	dbPath := makeTempDB(t, opts)

	// All-zero row: must be filtered out.
	insertRow(t, dbPath, opts, rowValues{
		ID:             "zero",
		CreatedAt:      "2025-05-18T10:30:00Z",
		ModelConfigCol: `{"model_name": "noop"}`,
		ProviderName:   "test",
		AccInput:       0,
		AccOutput:      0,
		AccTotal:       0,
		Input:          0,
		Output:         0,
		Total:          0,
	})
	// Row with empty model_config_json: must be filtered out via WHERE.
	insertRow(t, dbPath, opts, rowValues{
		ID:             "noconfig",
		CreatedAt:      "2025-05-18T10:30:00Z",
		ModelConfigCol: "",
		ProviderName:   "test",
		AccInput:       100,
		AccOutput:      50,
		AccTotal:       150,
	})
	// Row with NULL-equivalent empty model_name JSON: must be filtered out.
	insertRow(t, dbPath, opts, rowValues{
		ID:             "blankmodel",
		CreatedAt:      "2025-05-18T10:30:00Z",
		ModelConfigCol: `{"model_name": ""}`,
		ProviderName:   "test",
		AccInput:       100,
		AccOutput:      50,
		AccTotal:       150,
	})
	// Healthy row: must survive.
	insertRow(t, dbPath, opts, rowValues{
		ID:             "good",
		CreatedAt:      "2025-05-18T10:30:00Z",
		ModelConfigCol: `{"model_name": "good-model"}`,
		ProviderName:   "test",
		AccInput:       100,
		AccOutput:      50,
		AccTotal:       150,
	})

	result, err := queryGooseSessions(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("queryGooseSessions: %v", err)
	}
	sessions := result.Sessions
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1 (only the healthy row)", len(sessions))
	}
	if sessions[0].ID != "good" {
		t.Errorf("survivor.ID = %q, want %q", sessions[0].ID, "good")
	}
}

func TestQueryGooseSessions_MissingDB(t *testing.T) {
	// queryGooseSessions on a non-existent path returns an error; the
	// provider's Fetch wraps this with resolveDBPath (which yields "" for
	// missing files) so the caller never reaches this path in practice.
	// Verified separately in TestProviderFetch_MissingDB.
	dir := t.TempDir()
	_, err := queryGooseSessions(context.Background(), filepath.Join(dir, "does-not-exist.db"))
	if err == nil {
		t.Fatal("expected error for missing db, got nil")
	}
}

func TestQueryGooseSessions_EmptyDB(t *testing.T) {
	opts := schemaOpts{}
	dbPath := makeTempDB(t, opts)

	result, err := queryGooseSessions(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("queryGooseSessions on empty db: %v", err)
	}
	if len(result.Sessions) != 0 {
		t.Fatalf("got %d sessions on empty db, want 0", len(result.Sessions))
	}
}

func TestQueryGooseSessions_MissingModelConfigColumn(t *testing.T) {
	// Without model_config_json there's no way to recover model names; the
	// query short-circuits to (nil, nil).
	opts := schemaOpts{dropModelConfigJSON: true}
	dbPath := makeTempDB(t, opts)

	// Even with a token-bearing row, no sessions should come back.
	insertRow(t, dbPath, opts, rowValues{
		ID:        "orphan",
		CreatedAt: "2025-05-18T10:30:00Z",
		AccInput:  100,
		AccOutput: 50,
		AccTotal:  150,
	})

	result, err := queryGooseSessions(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("queryGooseSessions: %v", err)
	}
	if len(result.Sessions) != 0 {
		t.Fatalf("got %d sessions, want 0 when model_config_json missing", len(result.Sessions))
	}
}

// TestQueryGooseSessions_ConcurrentWrite simulates the host AI tool writing
// to sessions.db while our reader is open. Read-only + immutable mode means
// the reader sees a stable snapshot and does not race with the writer or
// hold a shared lock.
func TestQueryGooseSessions_ConcurrentWrite(t *testing.T) {
	opts := schemaOpts{}
	dbPath := makeTempDB(t, opts)

	insertRow(t, dbPath, opts, rowValues{
		ID:             "initial",
		CreatedAt:      "2025-05-18T10:30:00Z",
		ModelConfigCol: `{"model_name": "test-model"}`,
		ProviderName:   "test",
		AccInput:       100,
		AccOutput:      50,
		AccTotal:       150,
	})

	// Open a writer in a separate goroutine and bang on it while we query.
	writer, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open writer: %v", err)
	}
	defer writer.Close()
	if _, err := writer.Exec("PRAGMA busy_timeout = 1000"); err != nil {
		t.Fatalf("busy_timeout: %v", err)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		i := 0
		for {
			select {
			case <-stop:
				return
			default:
			}
			i++
			_, _ = writer.Exec(
				`INSERT INTO sessions (id, created_at, model_config_json, provider_name,
					accumulated_input_tokens, accumulated_output_tokens, accumulated_total_tokens)
					VALUES (?, ?, ?, ?, ?, ?, ?)`,
				fmt.Sprintf("concurrent_%d", i),
				time.Now().UTC().Format(time.RFC3339),
				`{"model_name": "test-model"}`,
				"test",
				1, 1, 2,
			)
		}
	}()

	// Multiple reads should not error or hang.
	for i := 0; i < 10; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, err := queryGooseSessions(ctx, dbPath)
		cancel()
		if err != nil {
			close(stop)
			wg.Wait()
			t.Fatalf("read %d failed under concurrent write: %v", i, err)
		}
	}
	close(stop)
	wg.Wait()
}

// TestQueryGooseSessions_OlderSchemaNoProviderName covers a schema before
// the provider_name column was added.
func TestQueryGooseSessions_OlderSchemaNoProviderName(t *testing.T) {
	opts := schemaOpts{dropProviderName: true}
	dbPath := makeTempDB(t, opts)

	insertRow(t, dbPath, opts, rowValues{
		ID:             "older_1",
		CreatedAt:      "2025-05-18T10:30:00Z",
		ModelConfigCol: `{"model_name": "claude-3-opus"}`,
		AccInput:       1000,
		AccOutput:      500,
		AccTotal:       1500,
	})

	result, err := queryGooseSessions(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("queryGooseSessions: %v", err)
	}
	sessions := result.Sessions
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
	if sessions[0].Provider != "" {
		t.Errorf("provider = %q, want empty", sessions[0].Provider)
	}
}

func TestExtractModelName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`{"model_name": "claude-opus-4-7"}`, "claude-opus-4-7"},
		{`{"model_name": "  spaced  "}`, "spaced"},
		{`{"model": "gpt-4o"}`, "gpt-4o"},
		{`{"name": "fallback-key"}`, "fallback-key"},
		{`{"model_name": ""}`, ""},
		{`not json at all`, ""},
		{``, ""},
		{`{"unrelated": "value"}`, ""},
	}
	for _, tc := range cases {
		if got := extractModelName(tc.in); got != tc.want {
			t.Errorf("extractModelName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseTimestamp(t *testing.T) {
	cases := []struct {
		in     string
		wantOK bool
	}{
		{"2025-05-18T10:30:00Z", true},
		{"2025-05-18T10:30:00.123456Z", true},
		{"2025-05-18T10:30:00+02:00", true},
		{"2025-05-18 10:30:00", true},
		{"2025-05-18", true},
		{"", false},
		{"not a timestamp", false},
		{"2025/05/18", false},
	}
	for _, tc := range cases {
		_, ok := parseTimestamp(tc.in)
		if ok != tc.wantOK {
			t.Errorf("parseTimestamp(%q) ok=%v, want %v", tc.in, ok, tc.wantOK)
		}
	}
}

// Helper to ensure no file handle leaks: opening the same DB readonly many
// times must succeed.
func TestOpenReadOnly_ManyOpens(t *testing.T) {
	opts := schemaOpts{}
	dbPath := makeTempDB(t, opts)

	for i := 0; i < 64; i++ {
		db, err := openReadOnly(dbPath)
		if err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
		if err := db.Close(); err != nil {
			t.Fatalf("close %d: %v", i, err)
		}
	}
}

func TestOpenReadOnly_EmptyPath(t *testing.T) {
	if _, err := openReadOnly(""); err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestOpenReadOnly_ReadsExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.db")
	// Create an empty sqlite file by opening then closing rw.
	rw, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := rw.Exec("CREATE TABLE foo (id INTEGER)"); err != nil {
		t.Fatalf("ddl: %v", err)
	}
	rw.Close()

	db, err := openReadOnly(path)
	if err != nil {
		t.Fatalf("openReadOnly: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("ping: %v", err)
	}
	// Confirm we really opened it read-only.
	if _, err := db.Exec("INSERT INTO foo (id) VALUES (1)"); err == nil {
		t.Error("expected write to fail on read-only handle")
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}
}
