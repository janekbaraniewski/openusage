package cursor

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestTrackingMaxRowID(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tracking.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	db.Exec(`CREATE TABLE ai_code_hashes (hash TEXT PRIMARY KEY, source TEXT, createdAt INTEGER, model TEXT)`)

	maxID, err := trackingMaxRowID(context.Background(), db)
	if err != nil {
		t.Fatalf("trackingMaxRowID on empty table: %v", err)
	}
	if maxID != 0 {
		t.Fatalf("expected 0 for empty table, got %d", maxID)
	}

	db.Exec(`INSERT INTO ai_code_hashes VALUES ('h1', 'composer', ?, 'claude')`, time.Now().UnixMilli())
	db.Exec(`INSERT INTO ai_code_hashes VALUES ('h2', 'tab', ?, 'gpt-4o')`, time.Now().UnixMilli())

	maxID, err = trackingMaxRowID(context.Background(), db)
	if err != nil {
		t.Fatalf("trackingMaxRowID with 2 rows: %v", err)
	}
	if maxID < 2 {
		t.Fatalf("expected maxID >= 2, got %d", maxID)
	}
}

func TestLoadTrackingRecordsIncremental(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tracking.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	db.Exec(`CREATE TABLE ai_code_hashes (hash TEXT PRIMARY KEY, source TEXT, createdAt INTEGER, model TEXT)`)
	db.Exec(`INSERT INTO ai_code_hashes VALUES ('h1', 'composer', ?, 'claude')`, time.Now().UnixMilli())
	db.Exec(`INSERT INTO ai_code_hashes VALUES ('h2', 'tab', ?, 'gpt-4o')`, time.Now().UnixMilli())

	// Full load (afterRowID=0).
	all, err := loadTrackingRecordsIncremental(context.Background(), db, core.SystemClock{}, 0)
	if err != nil {
		t.Fatalf("full load: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 records, got %d", len(all))
	}

	maxBefore := all[len(all)-1].RowID

	// Insert more rows.
	db.Exec(`INSERT INTO ai_code_hashes VALUES ('h3', 'cli', ?, 'gemini')`, time.Now().UnixMilli())

	// Incremental load.
	incremental, err := loadTrackingRecordsIncremental(context.Background(), db, core.SystemClock{}, maxBefore)
	if err != nil {
		t.Fatalf("incremental load: %v", err)
	}
	if len(incremental) != 1 {
		t.Fatalf("expected 1 incremental record, got %d", len(incremental))
	}
	if incremental[0].Source != "cli" {
		t.Fatalf("expected source 'cli', got %q", incremental[0].Source)
	}

	// No new rows.
	empty, err := loadTrackingRecordsIncremental(context.Background(), db, core.SystemClock{}, incremental[0].RowID)
	if err != nil {
		t.Fatalf("no-new-rows load: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected 0 records when no new rows, got %d", len(empty))
	}
}

func TestLoadTrackingRecordsCached(t *testing.T) {
	dbPath := createCursorTrackingDBForTest(t, []cursorTrackingRow{
		{Hash: "h1", Source: "composer", Model: "claude", CreatedAt: time.Now().UnixMilli()},
		{Hash: "h2", Source: "tab", Model: "gpt-4o", CreatedAt: time.Now().UnixMilli()},
	})

	p := New()

	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?mode=ro", dbPath))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	// First call: full load.
	records1, err := p.loadTrackingRecordsCached(context.Background(), db)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	if len(records1) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records1))
	}
	if p.trackingMaxRowID == 0 {
		t.Fatal("expected trackingMaxRowID to be set after first load")
	}

	// Second call: no new data, should return cached records.
	records2, err := p.loadTrackingRecordsCached(context.Background(), db)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	if len(records2) != 2 {
		t.Fatalf("expected 2 cached records, got %d", len(records2))
	}
}

func TestLoadTrackingRecordsCached_Incremental(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tracking.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	db.Exec(`CREATE TABLE ai_code_hashes (hash TEXT PRIMARY KEY, source TEXT, createdAt INTEGER, model TEXT)`)
	db.Exec(`INSERT INTO ai_code_hashes VALUES ('h1', 'composer', ?, 'claude')`, time.Now().UnixMilli())
	db.Close()

	p := New()

	// Open read-only for cache method.
	roDB, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?mode=ro", dbPath))
	if err != nil {
		t.Fatalf("open ro: %v", err)
	}
	defer roDB.Close()

	// First load.
	records1, err := p.loadTrackingRecordsCached(context.Background(), roDB)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	if len(records1) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records1))
	}

	// Insert more data via a writable connection.
	rwDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open rw: %v", err)
	}
	rwDB.Exec(`INSERT INTO ai_code_hashes VALUES ('h2', 'tab', ?, 'gpt-4o')`, time.Now().UnixMilli())
	rwDB.Exec(`INSERT INTO ai_code_hashes VALUES ('h3', 'cli', ?, 'gemini')`, time.Now().UnixMilli())
	rwDB.Close()

	// Second load should pick up new records incrementally.
	records2, err := p.loadTrackingRecordsCached(context.Background(), roDB)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	if len(records2) != 3 {
		t.Fatalf("expected 3 records after incremental load, got %d", len(records2))
	}

	// Verify the original record is preserved.
	if records2[0].Source != "composer" {
		t.Fatalf("first record should be original, got source=%q", records2[0].Source)
	}
}

func TestLoadComposerSessionKeys(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "state.vscdb")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	db.Exec(`CREATE TABLE cursorDiskKV (key TEXT PRIMARY KEY, value TEXT)`)

	session1 := fmt.Sprintf(`{"usageData":{"claude":{"costInCents":100,"amount":5}},"createdAt":%d}`, time.Now().UnixMilli())
	session2 := `{"usageData":{},"createdAt":1000}` // empty usage — should be excluded
	db.Exec(`INSERT INTO cursorDiskKV (key, value) VALUES ('composerData:s1', ?)`, session1)
	db.Exec(`INSERT INTO cursorDiskKV (key, value) VALUES ('composerData:s2', ?)`, session2)
	db.Exec(`INSERT INTO cursorDiskKV (key, value) VALUES ('otherKey:x1', '{"foo":"bar"}')`)

	keys, err := loadComposerSessionKeys(context.Background(), db)
	if err != nil {
		t.Fatalf("loadComposerSessionKeys: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 key (only non-empty usage), got %d: %v", len(keys), keys)
	}
	if keys[0] != "composerData:s1" {
		t.Fatalf("expected 'composerData:s1', got %q", keys[0])
	}
}

func TestLoadComposerSessionRecordsByKeys(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "state.vscdb")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	db.Exec(`CREATE TABLE cursorDiskKV (key TEXT PRIMARY KEY, value TEXT)`)

	usage1 := map[string]composerModelUsage{"claude": {CostInCents: 100, Amount: 5}}
	usage2 := map[string]composerModelUsage{"gpt-4o": {CostInCents: 200, Amount: 10}}
	u1JSON, _ := json.Marshal(usage1)
	u2JSON, _ := json.Marshal(usage2)

	session1 := fmt.Sprintf(`{"usageData":%s,"createdAt":%d,"unifiedMode":"agent"}`, string(u1JSON), time.Now().UnixMilli())
	session2 := fmt.Sprintf(`{"usageData":%s,"createdAt":%d,"unifiedMode":"chat"}`, string(u2JSON), time.Now().UnixMilli())
	db.Exec(`INSERT INTO cursorDiskKV (key, value) VALUES ('composerData:s1', ?)`, session1)
	db.Exec(`INSERT INTO cursorDiskKV (key, value) VALUES ('composerData:s2', ?)`, session2)

	// Load only s2.
	records, err := loadComposerSessionRecordsByKeys(context.Background(), db, []string{"composerData:s2"})
	if err != nil {
		t.Fatalf("loadComposerSessionRecordsByKeys: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].SessionID != "s2" {
		t.Fatalf("expected session s2, got %q", records[0].SessionID)
	}
	if records[0].Mode != "chat" {
		t.Fatalf("expected mode 'chat', got %q", records[0].Mode)
	}

	// Load empty set.
	empty, err := loadComposerSessionRecordsByKeys(context.Background(), db, nil)
	if err != nil {
		t.Fatalf("empty keys: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected 0 records for empty keys, got %d", len(empty))
	}
}

func TestLoadBubbleKeys(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "state.vscdb")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	db.Exec(`CREATE TABLE cursorDiskKV (key TEXT PRIMARY KEY, value TEXT)`)

	bubble1 := `{"type":2,"toolFormerData":{"name":"read_file","status":"completed"},"conversationId":"s1","tokenCount":{"inputTokens":10,"outputTokens":5},"model":"claude"}`
	bubble2 := `{"type":1,"text":"some text"}` // type != 2, should be excluded
	db.Exec(`INSERT INTO cursorDiskKV (key, value) VALUES ('bubbleId:b1', ?)`, bubble1)
	db.Exec(`INSERT INTO cursorDiskKV (key, value) VALUES ('bubbleId:b2', ?)`, bubble2)

	keys, err := loadBubbleKeys(context.Background(), db)
	if err != nil {
		t.Fatalf("loadBubbleKeys: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d: %v", len(keys), keys)
	}
	if keys[0] != "bubbleId:b1" {
		t.Fatalf("expected 'bubbleId:b1', got %q", keys[0])
	}
}

func TestLoadBubbleRecordsByKeys(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "state.vscdb")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	db.Exec(`CREATE TABLE cursorDiskKV (key TEXT PRIMARY KEY, value TEXT)`)

	bubble1 := `{"type":2,"toolFormerData":{"name":"read_file","status":"completed"},"conversationId":"s1","tokenCount":{"inputTokens":10,"outputTokens":5},"model":"claude"}`
	bubble2 := `{"type":2,"toolFormerData":{"name":"write_file","status":"error"},"conversationId":"s1","tokenCount":{"inputTokens":20,"outputTokens":8},"model":"gpt-4o"}`
	db.Exec(`INSERT INTO cursorDiskKV (key, value) VALUES ('bubbleId:b1', ?)`, bubble1)
	db.Exec(`INSERT INTO cursorDiskKV (key, value) VALUES ('bubbleId:b2', ?)`, bubble2)

	// Load only b2.
	records, err := loadBubbleRecordsByKeys(context.Background(), db, []string{"bubbleId:b2"})
	if err != nil {
		t.Fatalf("loadBubbleRecordsByKeys: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].BubbleID != "b2" {
		t.Fatalf("expected bubble b2, got %q", records[0].BubbleID)
	}
	if records[0].ToolName != "write_file" {
		t.Fatalf("expected tool 'write_file', got %q", records[0].ToolName)
	}
}

func TestLoadComposerRecordsCached(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "state.vscdb")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	db.Exec(`CREATE TABLE cursorDiskKV (key TEXT PRIMARY KEY, value TEXT)`)

	usage := map[string]composerModelUsage{"claude": {CostInCents: 100, Amount: 5}}
	uJSON, _ := json.Marshal(usage)
	session := fmt.Sprintf(`{"usageData":%s,"createdAt":%d,"unifiedMode":"agent"}`, string(uJSON), time.Now().UnixMilli())
	db.Exec(`INSERT INTO cursorDiskKV (key, value) VALUES ('composerData:s1', ?)`, session)
	db.Close()

	p := New()

	roDB, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?mode=ro", dbPath))
	if err != nil {
		t.Fatalf("open ro: %v", err)
	}
	defer roDB.Close()

	// First load: full scan.
	records1, err := p.loadComposerRecordsCached(context.Background(), roDB)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	if len(records1) != 1 {
		t.Fatalf("expected 1, got %d", len(records1))
	}

	// Second load: cache hit, no new keys.
	records2, err := p.loadComposerRecordsCached(context.Background(), roDB)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	if len(records2) != 1 {
		t.Fatalf("expected 1 cached, got %d", len(records2))
	}

	// Add a new session via writable connection.
	rwDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open rw: %v", err)
	}
	session2 := fmt.Sprintf(`{"usageData":%s,"createdAt":%d,"unifiedMode":"chat"}`, string(uJSON), time.Now().UnixMilli())
	rwDB.Exec(`INSERT INTO cursorDiskKV (key, value) VALUES ('composerData:s2', ?)`, session2)
	rwDB.Close()

	// Third load: incremental, picks up new session.
	records3, err := p.loadComposerRecordsCached(context.Background(), roDB)
	if err != nil {
		t.Fatalf("third load: %v", err)
	}
	if len(records3) != 2 {
		t.Fatalf("expected 2 after incremental, got %d", len(records3))
	}
}

func TestLoadBubbleRecordsCached(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "state.vscdb")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	db.Exec(`CREATE TABLE cursorDiskKV (key TEXT PRIMARY KEY, value TEXT)`)
	bubble := `{"type":2,"toolFormerData":{"name":"read_file","status":"completed"},"conversationId":"s1","tokenCount":{"inputTokens":10,"outputTokens":5},"model":"claude"}`
	db.Exec(`INSERT INTO cursorDiskKV (key, value) VALUES ('bubbleId:b1', ?)`, bubble)
	db.Close()

	p := New()

	roDB, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?mode=ro", dbPath))
	if err != nil {
		t.Fatalf("open ro: %v", err)
	}
	defer roDB.Close()

	// First load.
	records1, err := p.loadBubbleRecordsCached(context.Background(), roDB)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	if len(records1) != 1 {
		t.Fatalf("expected 1, got %d", len(records1))
	}

	// Cache hit.
	records2, err := p.loadBubbleRecordsCached(context.Background(), roDB)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	if len(records2) != 1 {
		t.Fatalf("expected 1 cached, got %d", len(records2))
	}

	// Add new bubble.
	rwDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open rw: %v", err)
	}
	bubble2 := `{"type":2,"toolFormerData":{"name":"write_file","status":"completed"},"conversationId":"s1","tokenCount":{"inputTokens":20,"outputTokens":8},"model":"gpt-4o"}`
	rwDB.Exec(`INSERT INTO cursorDiskKV (key, value) VALUES ('bubbleId:b2', ?)`, bubble2)
	rwDB.Close()

	// Incremental load.
	records3, err := p.loadBubbleRecordsCached(context.Background(), roDB)
	if err != nil {
		t.Fatalf("third load: %v", err)
	}
	if len(records3) != 2 {
		t.Fatalf("expected 2 after incremental, got %d", len(records3))
	}
}

func TestScoredCommitsCaching(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tracking.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	db.Exec(`CREATE TABLE ai_code_hashes (hash TEXT PRIMARY KEY, source TEXT, createdAt INTEGER, model TEXT)`)
	db.Exec(`INSERT INTO ai_code_hashes VALUES ('h1', 'composer', ?, 'claude')`, time.Now().UnixMilli())

	db.Exec(`CREATE TABLE scored_commits (
		commitHash TEXT, branchName TEXT, scoredAt INTEGER,
		linesAdded INTEGER, linesDeleted INTEGER,
		tabLinesAdded INTEGER, tabLinesDeleted INTEGER,
		composerLinesAdded INTEGER, composerLinesDeleted INTEGER,
		humanLinesAdded INTEGER, humanLinesDeleted INTEGER,
		blankLinesAdded INTEGER, blankLinesDeleted INTEGER,
		commitMessage TEXT, commitDate TEXT,
		v1AiPercentage TEXT, v2AiPercentage TEXT,
		PRIMARY KEY (commitHash, branchName))`)
	db.Exec(`INSERT INTO scored_commits VALUES ('abc', 'main', ?, 100, 10, 20, 5, 60, 3, 20, 2, 0, 0, 'test', '2026-02-23', '50.0', '80.0')`, time.Now().UnixMilli())
	db.Close()

	p := New()

	roDB, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?mode=ro", dbPath))
	if err != nil {
		t.Fatalf("open ro: %v", err)
	}
	defer roDB.Close()

	// First load.
	agg1, err := p.loadScoredCommitsCached(context.Background(), roDB)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	if agg1 == nil || agg1.TotalCommits != 1 {
		t.Fatalf("expected 1 commit, got %+v", agg1)
	}
	if p.scoredCommitsCount != 1 {
		t.Fatalf("expected cache count=1, got %d", p.scoredCommitsCount)
	}

	// Second load: same count, should use cache.
	agg2, err := p.loadScoredCommitsCached(context.Background(), roDB)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	if agg2 != agg1 {
		t.Fatal("expected pointer equality for cached aggregate")
	}

	// Add another commit.
	rwDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open rw: %v", err)
	}
	rwDB.Exec(`INSERT INTO scored_commits VALUES ('def', 'main', ?, 200, 20, 40, 10, 120, 6, 40, 4, 0, 0, 'test2', '2026-02-24', '30.0', '60.0')`, time.Now().UnixMilli())
	rwDB.Close()

	// Third load: count changed, should re-aggregate.
	agg3, err := p.loadScoredCommitsCached(context.Background(), roDB)
	if err != nil {
		t.Fatalf("third load: %v", err)
	}
	if agg3 == nil || agg3.TotalCommits != 2 {
		t.Fatalf("expected 2 commits after re-aggregate, got %+v", agg3)
	}
	if agg3 == agg1 {
		t.Fatal("expected new aggregate after count change")
	}
}

// TestFetchProducesIdenticalOutput_CachedVsFresh verifies that calling Fetch
// twice on the same data produces identical metrics (the cached path must
// produce the same snapshot as the full-scan path).
func TestFetchProducesIdenticalOutput_CachedVsFresh(t *testing.T) {
	now := time.Now()
	trackingDBPath := createCursorTrackingDBForTest(t, []cursorTrackingRow{
		{Hash: "h1", Source: "composer", Model: "claude", CreatedAt: now.Add(-2 * time.Hour).UnixMilli()},
		{Hash: "h2", Source: "tab", Model: "gpt-4o", CreatedAt: now.Add(-1 * time.Hour).UnixMilli()},
	})

	stateDBPath := filepath.Join(t.TempDir(), "state.vscdb")
	sdb, err := sql.Open("sqlite3", stateDBPath)
	if err != nil {
		t.Fatalf("open state db: %v", err)
	}
	sdb.Exec(`CREATE TABLE IF NOT EXISTS ItemTable (key TEXT PRIMARY KEY, value TEXT)`)
	sdb.Exec(`CREATE TABLE IF NOT EXISTS cursorDiskKV (key TEXT PRIMARY KEY, value TEXT)`)
	usage := map[string]composerModelUsage{"claude": {CostInCents: 500, Amount: 10}}
	uJSON, _ := json.Marshal(usage)
	session := fmt.Sprintf(`{"usageData":%s,"createdAt":%d,"unifiedMode":"agent"}`, string(uJSON), now.Add(-1*time.Hour).UnixMilli())
	sdb.Exec(`INSERT INTO cursorDiskKV (key, value) VALUES ('composerData:s1', ?)`, session)
	bubble := `{"type":2,"toolFormerData":{"name":"read_file","status":"completed"},"conversationId":"s1","tokenCount":{"inputTokens":10,"outputTokens":5},"model":"claude"}`
	sdb.Exec(`INSERT INTO cursorDiskKV (key, value) VALUES ('bubbleId:b1', ?)`, bubble)
	sdb.Close()

	p := New()
	acct := core.AccountConfig{
		ID:       "test-identical",
		Provider: "cursor",
		RuntimeHints: map[string]string{
			"tracking_db": trackingDBPath,
			"state_db":    stateDBPath,
		},
	}

	snap1, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("first Fetch: %v", err)
	}

	snap2, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("second Fetch: %v", err)
	}

	// Compare key metrics that should be identical.
	for _, key := range []string{
		"total_ai_requests", "composer_cost", "composer_sessions",
		"composer_requests", "tool_calls_total",
	} {
		m1, ok1 := snap1.Metrics[key]
		m2, ok2 := snap2.Metrics[key]
		if ok1 != ok2 {
			t.Errorf("metric %q presence differs: first=%v, second=%v", key, ok1, ok2)
			continue
		}
		if !ok1 {
			continue
		}
		if (m1.Used == nil) != (m2.Used == nil) {
			t.Errorf("metric %q Used nil mismatch", key)
			continue
		}
		if m1.Used != nil && *m1.Used != *m2.Used {
			t.Errorf("metric %q: first=%.2f, second=%.2f", key, *m1.Used, *m2.Used)
		}
	}
}
