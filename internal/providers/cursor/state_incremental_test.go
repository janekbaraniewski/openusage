package cursor

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

// newCursorStateDB creates a state.vscdb with Cursor's real schema: plain rowid
// tables whose UNIQUE key replaces on conflict, so a rewrite gets a new rowid.
func newCursorStateDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("open state db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	for _, stmt := range []string{
		`PRAGMA journal_mode=WAL`,
		`CREATE TABLE ItemTable (key TEXT UNIQUE ON CONFLICT REPLACE, value BLOB)`,
		`CREATE TABLE cursorDiskKV (key TEXT UNIQUE ON CONFLICT REPLACE, value BLOB)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	return db
}

func putKV(t *testing.T, db *sql.DB, key, value string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO cursorDiskKV (key, value) VALUES (?, ?)`, key, value); err != nil {
		t.Fatalf("insert %s: %v", key, err)
	}
}

func putComposer(t *testing.T, db *sql.DB, session string, createdAt time.Time, cents float64) {
	t.Helper()
	putKV(t, db, "composerData:"+session, fmt.Sprintf(
		`{"usageData":{"claude-4.5-sonnet":{"costInCents":%g,"amount":1}},"createdAt":%d}`,
		cents, createdAt.UnixMilli()))
}

func putBubble(t *testing.T, db *sql.DB, session, bubble string, inputTokens int) {
	t.Helper()
	putKV(t, db, "bubbleId:"+session+":"+bubble, fmt.Sprintf(
		`{"type":2,"conversationId":%q,"toolFormerData":{"name":"read_file","status":"completed"},"tokenCount":{"inputTokens":%d,"outputTokens":5},"model":"claude-4.5-sonnet"}`,
		session, inputTokens))
}

func putDailyStats(t *testing.T, db *sql.DB, day string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO ItemTable (key, value) VALUES (?, ?)`,
		"aiCodeTracking.dailyStats.v1.5."+day,
		`{"tabSuggestedLines":3,"tabAcceptedLines":2,"composerSuggestedLines":5,"composerAcceptedLines":4}`); err != nil {
		t.Fatalf("insert daily stats: %v", err)
	}
}

// cursorEventKey is the key appendCursorDedupEvents and the store dedup on.
func cursorEventKey(ev shared.TelemetryEvent) string {
	if ev.EventType == shared.TelemetryEventTypeToolUsage {
		return "tool|" + ev.ToolCallID
	}
	return string(ev.EventType) + "|" + ev.MessageID
}

func eventsByKey(events []shared.TelemetryEvent) map[string]shared.TelemetryEvent {
	out := make(map[string]shared.TelemetryEvent, len(events))
	for _, ev := range events {
		out[cursorEventKey(ev)] = ev
	}
	return out
}

func sortedKeys(m map[string]shared.TelemetryEvent) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func stateOnlyOpts(t *testing.T, statePath string) shared.TelemetryCollectOptions {
	return shared.TelemetryCollectOptions{Paths: map[string]string{
		"tracking_db": filepath.Join(t.TempDir(), "missing.db"),
		"state_db":    statePath,
	}}
}

func collectOrFail(t *testing.T, p *Provider, opts shared.TelemetryCollectOptions) []shared.TelemetryEvent {
	t.Helper()
	events, err := p.Collect(context.Background(), opts)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	return events
}

func fullStateRead(t *testing.T, path string) map[string]shared.TelemetryEvent {
	t.Helper()
	events, err := collectStateDBEvents(context.Background(), path)
	if err != nil {
		t.Fatalf("full read: %v", err)
	}
	return eventsByKey(events)
}

// bumpMtime makes sure the PR #450 size/mtime skip cannot hide a write.
func bumpMtime(t *testing.T, path string) {
	t.Helper()
	future := time.Now().Add(time.Duration(time.Now().UnixNano()%1000+1) * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
}

func TestStateDBCollectIsIncrementalAfterAppend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.vscdb")
	db := newCursorStateDB(t, path)
	s1Created := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	putComposer(t, db, "s1", s1Created, 120)
	putBubble(t, db, "s1", "b1", 100)
	putBubble(t, db, "s1", "b2", 200)
	putKV(t, db, "agentKv:noise", `{}`)
	putDailyStats(t, db, "2026-10-01")

	p := New()
	opts := stateOnlyOpts(t, path)

	first := eventsByKey(collectOrFail(t, p, opts))
	if want := fullStateRead(t, path); !reflect.DeepEqual(first, want) {
		t.Fatalf("first collect should be a full read:\n got %v\nwant %v", sortedKeys(first), sortedKeys(want))
	}

	// Activity: a new bubble in the old session (its composerData row stays
	// below the mark), a new session, and an unrelated hot key rewrite.
	putBubble(t, db, "s1", "b3", 300)
	putComposer(t, db, "s2", s1Created.Add(time.Hour), 50)
	putBubble(t, db, "s2", "c1", 400)
	putKV(t, db, "agentKv:noise", `{"n":1}`)
	bumpMtime(t, path)

	second := eventsByKey(collectOrFail(t, p, opts))
	full := fullStateRead(t, path)

	// Only rows written since the mark are read.
	for _, old := range []string{"tool|s1:b1", "tool|s1:b2", "message_usage|cursor-bubble-tokens:s1:b1", "message_usage|cursor-composer:s1:claude_4_5_sonnet"} {
		if _, ok := second[old]; ok {
			t.Errorf("incremental collect re-emitted %s", old)
		}
	}
	for _, added := range []string{"tool|s1:b3", "message_usage|cursor-bubble-tokens:s1:b3", "tool|s2:c1", "message_usage|cursor-composer:s2:claude_4_5_sonnet"} {
		if _, ok := second[added]; !ok {
			t.Errorf("incremental collect missed %s; got %v", added, sortedKeys(second))
		}
	}
	// Every incremental event is identical to the full read's, including the
	// timestamp looked up from s1's older composerData row.
	for key, ev := range second {
		want, ok := full[key]
		if !ok {
			t.Errorf("incremental event %s not in full read", key)
			continue
		}
		if !reflect.DeepEqual(ev, want) {
			t.Errorf("event %s differs from full read:\n got %+v\nwant %+v", key, ev, want)
		}
	}
	if got := second["tool|s1:b3"].OccurredAt; !got.Equal(s1Created) {
		t.Errorf("b3 OccurredAt = %v, want s1 createdAt %v", got, s1Created)
	}
	// Together the two collects cover the full read exactly.
	union := make(map[string]shared.TelemetryEvent)
	for k, v := range first {
		union[k] = v
	}
	for k, v := range second {
		union[k] = v
	}
	if !reflect.DeepEqual(sortedKeys(union), sortedKeys(full)) {
		t.Errorf("first+second keys != full read keys:\n got %v\nwant %v", sortedKeys(union), sortedKeys(full))
	}
}

func TestStateDBCollectFullReadAfterReplace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.vscdb")
	db := newCursorStateDB(t, path)
	putComposer(t, db, "s1", time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC), 120)
	putBubble(t, db, "s1", "b1", 100)

	p := New()
	opts := stateOnlyOpts(t, path)
	collectOrFail(t, p, opts)
	db.Close()

	// Restore from a backup: same rows (so the rowid anchors still match)
	// plus one, written to a new file and renamed over the old one.
	replacement := filepath.Join(dir, "restored.vscdb")
	rdb := newCursorStateDB(t, replacement)
	putComposer(t, rdb, "s1", time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC), 120)
	putBubble(t, rdb, "s1", "b1", 100)
	putBubble(t, rdb, "s1", "b2", 200)
	rdb.Close()
	for _, suffix := range []string{"-wal", "-shm"} {
		_ = os.Remove(path + suffix)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatalf("replace db: %v", err)
	}
	bumpMtime(t, path)

	got := eventsByKey(collectOrFail(t, p, opts))
	want := fullStateRead(t, path)
	if !reflect.DeepEqual(sortedKeys(got), sortedKeys(want)) {
		t.Fatalf("replaced DB must be read in full:\n got %v\nwant %v", sortedKeys(got), sortedKeys(want))
	}
}

func TestStateDBCollectHandlesRowIDReuse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.vscdb")
	db := newCursorStateDB(t, path)
	putComposer(t, db, "s1", time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC), 120)
	putBubble(t, db, "s1", "b1", 100)
	putBubble(t, db, "s1", "b2", 200)

	p := New()
	opts := stateOnlyOpts(t, path)
	collectOrFail(t, p, opts)

	// Deleting the top row lets SQLite hand its rowid to the next insert,
	// which a plain "rowid > max" would skip.
	if _, err := db.Exec(`DELETE FROM cursorDiskKV WHERE key = 'bubbleId:s1:b2'`); err != nil {
		t.Fatal(err)
	}
	putBubble(t, db, "s1", "b3", 300)
	bumpMtime(t, path)

	got := eventsByKey(collectOrFail(t, p, opts))
	if _, ok := got["tool|s1:b3"]; !ok {
		t.Fatalf("bubble written at a reused rowid was missed; got %v", sortedKeys(got))
	}
	if _, ok := got["tool|s1:b1"]; ok {
		t.Errorf("rows below the surviving anchor should not be re-read; got %v", sortedKeys(got))
	}
}

func TestStateDBCollectFullReadAfterShrink(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.vscdb")
	db := newCursorStateDB(t, path)
	putComposer(t, db, "s1", time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC), 120)
	putBubble(t, db, "s1", "b1", 100)

	p := New()
	opts := stateOnlyOpts(t, path)
	collectOrFail(t, p, opts)

	// Pretend the last read saw a bigger file (as before a VACUUM).
	p.stateMarkMu.Lock()
	p.stateMarks[path].Size += 1 << 20
	p.stateMarkMu.Unlock()
	putBubble(t, db, "s1", "b2", 200)
	bumpMtime(t, path)

	got := eventsByKey(collectOrFail(t, p, opts))
	if _, ok := got["tool|s1:b1"]; !ok {
		t.Fatalf("shrunken DB must be read in full; got %v", sortedKeys(got))
	}
}

type memStateStore map[string]string

func (m memStateStore) MetaGet(_ context.Context, key string) (string, bool, error) {
	v, ok := m[key]
	return v, ok, nil
}

func (m memStateStore) MetaSet(_ context.Context, key, value string) error {
	m[key] = value
	return nil
}

func TestStateDBMarkSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.vscdb")
	db := newCursorStateDB(t, path)
	putComposer(t, db, "s1", time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC), 120)
	putBubble(t, db, "s1", "b1", 100)

	store := memStateStore{}
	opts := stateOnlyOpts(t, path)
	before := New()
	before.SetTelemetryStateStore(store)
	collectOrFail(t, before, opts)
	if len(store) != 0 {
		t.Fatalf("mark persisted before the batch could be ingested: %v", store)
	}
	collectOrFail(t, before, opts) // next cycle persists the previous mark
	if raw := store[stateDBMarkMetaPrefix+path]; !strings.Contains(raw, "bubbleId:s1:b1") {
		t.Fatalf("persisted mark = %q, want anchors", raw)
	}

	putBubble(t, db, "s1", "b2", 200)
	after := New() // daemon restart
	after.SetTelemetryStateStore(store)
	got := eventsByKey(collectOrFail(t, after, opts))
	if _, ok := got["tool|s1:b1"]; ok {
		t.Errorf("restart re-read rows before the persisted mark; got %v", sortedKeys(got))
	}
	if _, ok := got["tool|s1:b2"]; !ok {
		t.Errorf("restart missed new row; got %v", sortedKeys(got))
	}
}

func TestStateDBPeriodicFullRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.vscdb")
	db := newCursorStateDB(t, path)
	putComposer(t, db, "s1", time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC), 120)
	putBubble(t, db, "s1", "b1", 100)

	p := New()
	opts := stateOnlyOpts(t, path)
	collectOrFail(t, p, opts)
	p.stateMarkMu.Lock()
	p.stateMarks[path].FullReadAt = time.Now().Add(-stateDBFullReadInterval - time.Minute)
	p.stateMarkMu.Unlock()
	putBubble(t, db, "s1", "b2", 200)
	bumpMtime(t, path)

	got := eventsByKey(collectOrFail(t, p, opts))
	if _, ok := got["tool|s1:b1"]; !ok {
		t.Fatalf("stale mark must trigger a full read; got %v", sortedKeys(got))
	}
}

func TestCursorSQLiteURIEscapesPath(t *testing.T) {
	got := cursorSQLiteURI("/tmp/a b/c?d#e%f.db", "mode=ro")
	if want := "file:/tmp/a b/c%3fd%23e%25f.db?mode=ro"; got != want {
		t.Fatalf("uri = %q, want %q", got, want)
	}
}

func TestOpenCursorDBReadOnlyNeverWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.vscdb")
	db := newCursorStateDB(t, path)
	putKV(t, db, "k", "v")

	ro, err := openCursorDBReadOnly(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer ro.Close()
	if _, err := ro.Exec(`INSERT INTO cursorDiskKV (key, value) VALUES ('x', 'y')`); err == nil {
		t.Fatal("write through the read-only handle succeeded")
	}
}
