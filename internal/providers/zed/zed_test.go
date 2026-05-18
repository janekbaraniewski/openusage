package zed

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/klauspost/compress/zstd"

	"github.com/janekbaraniewski/openusage/internal/core"
)

type fixedClock struct{ t time.Time }

func (f fixedClock) Now() time.Time { return f.t }

// makeTempThreadsDB creates an in-temp-dir SQLite db with the `threads`
// schema we expect from Zed.
func makeTempThreadsDB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "threads.db")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ddl := `CREATE TABLE threads (
		id TEXT PRIMARY KEY,
		updated_at TEXT,
		created_at TEXT,
		folder_paths TEXT,
		folder_paths_order TEXT,
		data_type TEXT,
		data BLOB
	)`
	if _, err := db.Exec(ddl); err != nil {
		t.Fatalf("ddl: %v", err)
	}
	return dbPath
}

func insertThreadRow(
	t *testing.T,
	dbPath, id, updatedAt, createdAt, folderPaths, folderOrder, dataType string,
	data []byte,
) {
	t.Helper()
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open insert: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(
		`INSERT INTO threads (id, updated_at, created_at, folder_paths, folder_paths_order, data_type, data) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, updatedAt, createdAt, folderPaths, folderOrder, dataType, data,
	); err != nil {
		t.Fatalf("insert: %v", err)
	}
}

func TestProvider_BasicMetadata(t *testing.T) {
	p := New()
	if p.ID() != ID {
		t.Errorf("ID = %q, want %q", p.ID(), ID)
	}
	if p.Spec().Auth.Type != core.ProviderAuthTypeLocal {
		t.Errorf("auth type = %v, want local", p.Spec().Auth.Type)
	}
	if p.DashboardWidget().IsZero() {
		t.Error("DashboardWidget is zero")
	}
}

func TestProvider_Fetch_MissingDB(t *testing.T) {
	p := New()
	p.clock = fixedClock{t: time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)}
	acct := core.AccountConfig{ID: "zed", Provider: "zed", Auth: "local"}
	acct.SetPath("db_path", filepath.Join(t.TempDir(), "missing.db"))

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if snap.Status != core.StatusUnknown {
		t.Errorf("status = %v want UNKNOWN", snap.Status)
	}
}

// happyPayload builds the inflated thread JSON for a zed.dev row.
func happyPayload(t *testing.T, model string, input, output, cacheRead, reasoning int64) []byte {
	t.Helper()
	payload := map[string]any{
		"model": map[string]any{
			"provider": "zed.dev",
			"name":     model,
		},
		"request_token_usage": []map[string]any{
			{
				"token_usage": map[string]any{
					"input_tokens":            input,
					"output_tokens":           output,
					"cache_read_input_tokens": cacheRead,
					"reasoning_tokens":        reasoning,
				},
			},
		},
		"message_count": 3,
		"created_at":    "2026-05-19T08:00:00Z",
	}
	out, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return out
}

func TestProvider_Fetch_HappyPath_JSON(t *testing.T) {
	dbPath := makeTempThreadsDB(t)
	body := happyPayload(t, "claude-sonnet-4", 1000, 500, 200, 100)

	insertThreadRow(t, dbPath,
		"thread-aaaa", "2026-05-19T08:00:00Z", "2026-05-19T08:00:00Z",
		"/Users/janek/code/project\n/Users/janek/code/other", "0",
		"json", body,
	)

	// Insert a non-zed.dev row that must be filtered out.
	nonZed, _ := json.Marshal(map[string]any{
		"model": map[string]any{"provider": "ollama", "name": "llama3"},
		"request_token_usage": []map[string]any{
			{"token_usage": map[string]any{"input_tokens": 99, "output_tokens": 99}},
		},
		"created_at": "2026-05-19T07:00:00Z",
	})
	insertThreadRow(t, dbPath, "thread-bbbb", "2026-05-19T07:00:00Z", "2026-05-19T07:00:00Z",
		"", "", "json", nonZed)

	p := New()
	p.clock = fixedClock{t: time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)}
	acct := core.AccountConfig{ID: "zed", Provider: "zed", Auth: "local"}
	acct.SetPath("db_path", dbPath)

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Fatalf("status = %v want OK; msg=%q", snap.Status, snap.Message)
	}

	expect := func(key string, want float64) {
		t.Helper()
		m, ok := snap.Metrics[key]
		if !ok {
			t.Errorf("missing metric %s", key)
			return
		}
		if m.Used == nil || *m.Used != want {
			got := -1.0
			if m.Used != nil {
				got = *m.Used
			}
			t.Errorf("metric %s = %v, want %v", key, got, want)
		}
	}
	// Only the zed.dev row contributes.
	expect("total_threads", 1)
	expect("threads_today", 1)
	expect("total_input_tokens", 1000)
	expect("total_output_tokens", 500)
	expect("total_cache_read", 200)
	expect("total_reasoning_tokens", 100)

	if len(snap.ModelUsage) != 1 {
		t.Fatalf("len(ModelUsage) = %d, want 1", len(snap.ModelUsage))
	}
	if got := snap.ModelUsage[0].RawModelID; got != "claude-sonnet-4" {
		t.Errorf("model = %q, want claude-sonnet-4", got)
	}
	if snap.ModelUsage[0].Dimensions["upstream_provider"] != "zed.dev" {
		t.Errorf("upstream_provider = %q, want zed.dev", snap.ModelUsage[0].Dimensions["upstream_provider"])
	}
}

func TestProvider_Fetch_HappyPath_Zstd(t *testing.T) {
	dbPath := makeTempThreadsDB(t)
	plain := happyPayload(t, "claude-opus-4", 2000, 800, 0, 0)

	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatalf("zstd writer: %v", err)
	}
	encAll := encoder.EncodeAll(plain, nil)
	_ = encoder.Close()

	insertThreadRow(t, dbPath,
		"thread-cccc", "2026-05-19T09:00:00Z", "2026-05-19T09:00:00Z",
		"/repo/x", "0", "zstd", encAll,
	)

	p := New()
	p.clock = fixedClock{t: time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)}
	acct := core.AccountConfig{ID: "zed", Provider: "zed", Auth: "local"}
	acct.SetPath("db_path", dbPath)

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Fatalf("status = %v want OK; msg=%q", snap.Status, snap.Message)
	}
	if m, ok := snap.Metrics["total_input_tokens"]; !ok || m.Used == nil || *m.Used != 2000 {
		t.Errorf("total_input_tokens = %+v, want 2000", m)
	}
	if m, ok := snap.Metrics["total_output_tokens"]; !ok || m.Used == nil || *m.Used != 800 {
		t.Errorf("total_output_tokens = %+v, want 800", m)
	}
}

func TestPickWorkspace(t *testing.T) {
	cases := []struct {
		name  string
		paths string
		order string
		want  string
	}{
		{"empty", "", "", ""},
		{"single line, no order", "/foo/bar", "", "/foo/bar"},
		{"order picks second", "/a\n/b\n/c", "1,0,2", "/b"},
		{"out-of-range falls through to first", "/a\n/b", "99", "/a"},
		{"unparseable order falls through to first", "/a\n/b", "xxx", "/a"},
		{"trims blanks", "  /a  \n\n/b  ", "0", "/a"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := pickWorkspace(tc.paths, tc.order)
			if got != tc.want {
				t.Errorf("pickWorkspace(%q,%q) = %q, want %q", tc.paths, tc.order, got, tc.want)
			}
		})
	}
}
