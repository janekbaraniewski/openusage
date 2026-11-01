package opencode

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

// Timestamps are Unix ms, like OpenCode's. Steps are well beyond the slack
// window so the tests can tell incremental reads from full ones.
const (
	incBase = int64(1771754400000)
	incStep = 10 * opencodeSQLiteWatermarkSlackMS
)

func newIncrementalTestDB(t *testing.T, path string) {
	t.Helper()
	execSQL(t, path,
		`CREATE TABLE session (id TEXT PRIMARY KEY, directory TEXT NOT NULL);`,
		`CREATE TABLE message (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, time_created INTEGER NOT NULL, time_updated INTEGER NOT NULL, data TEXT NOT NULL);`,
		`CREATE TABLE part (id TEXT PRIMARY KEY, message_id TEXT NOT NULL, session_id TEXT NOT NULL, time_created INTEGER NOT NULL, time_updated INTEGER NOT NULL, data TEXT NOT NULL);`,
		`CREATE INDEX part_message_id_id_idx ON part (message_id, id);`,
		`INSERT INTO session (id, directory) VALUES ('sess-1', '/tmp/work');`,
	)
}

func execSQL(t *testing.T, path string, stmts ...string) {
	t.Helper()
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	defer db.Close()
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("exec %q: %v", stmt, err)
		}
	}
}

func insertMessage(id string, ts int64, input int64) string {
	return fmt.Sprintf(`INSERT INTO message (id, session_id, time_created, time_updated, data) VALUES ('%s', 'sess-1', %d, %d, '{"role":"assistant","providerID":"anthropic","modelID":"claude","tokens":{"input":%d,"output":5},"time":{"created":%d,"completed":%d}}');`,
		id, ts, ts, input, ts, ts)
}

func insertStepFinish(id, messageID string, ts int64, input int64) string {
	return fmt.Sprintf(`INSERT INTO part (id, message_id, session_id, time_created, time_updated, data) VALUES ('%s', '%s', 'sess-1', %d, %d, '{"type":"step-finish","reason":"stop","tokens":{"input":%d,"output":7}}');`,
		id, messageID, ts, ts, input)
}

func insertTool(id, messageID, callID string, ts int64) string {
	return fmt.Sprintf(`INSERT INTO part (id, message_id, session_id, time_created, time_updated, data) VALUES ('%s', '%s', 'sess-1', %d, %d, '{"type":"tool","callID":"%s","tool":"read","state":{"status":"completed","input":{"filePath":"/tmp/work/a.go"}}}');`,
		id, messageID, ts, ts, callID)
}

// eventKey mirrors the dedup key of appendDedupTelemetryEvents.
func eventKey(ev shared.TelemetryEvent) string {
	if ev.EventType == shared.TelemetryEventTypeToolUsage {
		return "tool:" + ev.ToolCallID
	}
	return "msg:" + ev.MessageID
}

func eventsByKey(t *testing.T, events []shared.TelemetryEvent) map[string]shared.TelemetryEvent {
	t.Helper()
	out := make(map[string]shared.TelemetryEvent, len(events))
	for _, ev := range events {
		if _, dup := out[eventKey(ev)]; dup {
			t.Fatalf("duplicate event key %q", eventKey(ev))
		}
		out[eventKey(ev)] = ev
	}
	return out
}

func fullRead(t *testing.T, path string) map[string]shared.TelemetryEvent {
	t.Helper()
	events, err := CollectTelemetryFromSQLite(context.Background(), path)
	if err != nil {
		t.Fatalf("full read: %v", err)
	}
	return eventsByKey(t, events)
}

func readSince(t *testing.T, path string, since int64) opencodeSQLiteReadResult {
	t.Helper()
	res, err := collectTelemetryFromSQLiteSince(context.Background(), path, &since)
	if err != nil {
		t.Fatalf("incremental read: %v", err)
	}
	return res
}

// assertSubsetOfFull checks every incremental event equals the full-read
// event with the same key.
func assertSubsetOfFull(t *testing.T, incremental []shared.TelemetryEvent, full map[string]shared.TelemetryEvent) {
	t.Helper()
	for key, ev := range eventsByKey(t, incremental) {
		want, ok := full[key]
		if !ok {
			t.Fatalf("incremental event %q not produced by a full read", key)
		}
		if !reflect.DeepEqual(ev, want) {
			t.Fatalf("incremental event %q differs from full read:\n got %+v\nwant %+v", key, ev, want)
		}
	}
}

// assertMergedEqualsFull applies incremental events over earlier ones and
// checks the result equals a full read of the current DB.
func assertMergedEqualsFull(t *testing.T, earlier, incremental []shared.TelemetryEvent, full map[string]shared.TelemetryEvent) {
	t.Helper()
	merged := eventsByKey(t, earlier)
	for key, ev := range eventsByKey(t, incremental) {
		merged[key] = ev
	}
	if !reflect.DeepEqual(merged, full) {
		t.Fatalf("earlier + incremental events differ from a full read:\n got %+v\nwant %+v", merged, full)
	}
}

func keysOf(m map[string]shared.TelemetryEvent) map[string]bool {
	out := make(map[string]bool, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}

func TestCollectSQLiteIncremental_NewRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.db")
	newIncrementalTestDB(t, path)
	execSQL(t, path,
		insertMessage("msg-old", incBase-incStep, 5),
		insertStepFinish("part-old-step", "msg-old", incBase-incStep, 6),
		insertTool("part-old-tool", "msg-old", "call-old", incBase-incStep),
		insertMessage("msg-1", incBase, 10),
		insertStepFinish("part-1-step", "msg-1", incBase, 11),
		insertTool("part-1-tool", "msg-1", "call-1", incBase),
	)

	first, err := collectTelemetryFromSQLiteSince(context.Background(), path, nil)
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	if first.Incremental || !first.HasWatermark || first.Watermark != incBase {
		t.Fatalf("first read = incremental %v, watermark %v/%d; want full read with watermark %d", first.Incremental, first.HasWatermark, first.Watermark, incBase)
	}
	if len(first.Events) != 4 {
		t.Fatalf("first read events = %d, want 4", len(first.Events))
	}

	ts := incBase + incStep
	execSQL(t, path,
		insertMessage("msg-2", ts, 20),
		insertStepFinish("part-2-step", "msg-2", ts, 21),
		insertTool("part-2-tool", "msg-2", "call-2", ts),
	)

	res := readSince(t, path, first.Watermark)
	if !res.Incremental {
		t.Fatal("second read was not incremental")
	}
	if res.Watermark != ts {
		t.Fatalf("watermark = %d, want %d", res.Watermark, ts)
	}
	// msg-1 sits at the old watermark, inside the slack window, so it is
	// re-read too; msg-old is outside it and must be skipped.
	got := keysOf(eventsByKey(t, res.Events))
	want := map[string]bool{"msg:msg-1": true, "tool:call-1": true, "msg:msg-2": true, "tool:call-2": true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("incremental events = %v, want the new rows plus the slack window %v", got, want)
	}
	full := fullRead(t, path)
	assertSubsetOfFull(t, res.Events, full)
	assertMergedEqualsFull(t, first.Events, res.Events, full)

	// Nothing changed since: only the slack window is re-read.
	idle := readSince(t, path, res.Watermark)
	if !idle.Incremental {
		t.Fatal("idle read was not incremental")
	}
	if got, want := keysOf(eventsByKey(t, idle.Events)), map[string]bool{"msg:msg-2": true, "tool:call-2": true}; !reflect.DeepEqual(got, want) {
		t.Fatalf("idle read events = %v, want only the slack window %v", got, want)
	}

	// A watermark above every row means the file no longer matches it.
	ahead := readSince(t, path, res.Watermark+incStep)
	if ahead.Incremental {
		t.Fatal("read with a watermark ahead of the DB should be a full read")
	}
	if !reflect.DeepEqual(eventsByKey(t, ahead.Events), full) {
		t.Fatal("fallback read differs from a full read")
	}
}

// A part of an existing message gets a newer time_updated (OpenCode rewrites
// parts while streaming). The message's events must be rebuilt from all its
// rows, exactly as a full read would: here the bumped step-finish loses its
// place as the earliest one, so the event switches to the other step.
func TestCollectSQLiteIncremental_UpdatedPart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.db")
	newIncrementalTestDB(t, path)
	execSQL(t, path,
		insertMessage("msg-1", incBase, 10),
		insertStepFinish("part-1-a", "msg-1", incBase, 100),
		insertStepFinish("part-1-b", "msg-1", incBase+1000, 200),
		insertTool("part-1-tool", "msg-1", "call-1", incBase),
		insertMessage("msg-old", incBase-incStep, 30),
		insertStepFinish("part-old", "msg-old", incBase-incStep, 31),
	)

	first, err := collectTelemetryFromSQLiteSince(context.Background(), path, nil)
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	if got := *eventsByKey(t, first.Events)["msg:msg-1"].TokenUsage.InputTokens; got != 100 {
		t.Fatalf("first read msg-1 input tokens = %d, want 100 from part-1-a", got)
	}

	bumped := first.Watermark + incStep
	execSQL(t, path,
		fmt.Sprintf(`UPDATE part SET time_updated = %d, data = json_set(data, '$.tokens.input', 150) WHERE id = 'part-1-a';`, bumped),
	)

	res := readSince(t, path, first.Watermark)
	if !res.Incremental {
		t.Fatal("read after update was not incremental")
	}
	if res.Watermark != bumped {
		t.Fatalf("watermark = %d, want %d", res.Watermark, bumped)
	}
	byKey := eventsByKey(t, res.Events)
	if _, ok := byKey["msg:msg-old"]; ok {
		t.Fatal("incremental read re-emitted an unchanged message")
	}
	msg, ok := byKey["msg:msg-1"]
	if !ok {
		t.Fatal("incremental read did not re-emit the updated message")
	}
	if got := *msg.TokenUsage.InputTokens; got != 200 {
		t.Fatalf("msg-1 input tokens = %d, want 200 from part-1-b (now the earliest step)", got)
	}
	if _, ok := byKey["tool:call-1"]; !ok {
		t.Fatal("incremental read did not rebuild the updated message's tool events")
	}

	full := fullRead(t, path)
	assertSubsetOfFull(t, res.Events, full)
	assertMergedEqualsFull(t, first.Events, res.Events, full)
}

// The slack window re-reads rows stamped just below the watermark, which a
// slower writer may commit after our read.
func TestCollectSQLiteIncremental_SlackWindow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.db")
	newIncrementalTestDB(t, path)
	execSQL(t, path, insertMessage("msg-1", incBase, 10))

	late := incBase - opencodeSQLiteWatermarkSlackMS/2
	execSQL(t, path, insertMessage("msg-late", late, 20))

	res := readSince(t, path, incBase)
	if !res.Incremental {
		t.Fatal("read was not incremental")
	}
	if _, ok := eventsByKey(t, res.Events)["msg:msg-late"]; !ok {
		t.Fatal("row committed late below the watermark was missed")
	}
}

func TestCollectSQLiteIncremental_FallsBackWhenRowsRemoved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.db")
	newIncrementalTestDB(t, path)
	execSQL(t, path,
		insertMessage("msg-1", incBase, 10),
		insertMessage("msg-2", incBase+incStep, 20),
	)
	first, err := collectTelemetryFromSQLiteSince(context.Background(), path, nil)
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	execSQL(t, path, `DELETE FROM message WHERE id = 'msg-2';`)

	res := readSince(t, path, first.Watermark)
	if res.Incremental {
		t.Fatal("read after the newest rows vanished should be a full read")
	}
	if res.Watermark != incBase {
		t.Fatalf("watermark = %d, want it reset to %d", res.Watermark, incBase)
	}
	if !reflect.DeepEqual(eventsByKey(t, res.Events), fullRead(t, path)) {
		t.Fatal("fallback read differs from a full read")
	}
}

func TestCollectSQLiteIncremental_NoWatermarkWithoutTimeUpdated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.db")
	execSQL(t, path,
		`CREATE TABLE message (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, time_created INTEGER NOT NULL, data TEXT NOT NULL);`,
		`CREATE TABLE part (id TEXT PRIMARY KEY, message_id TEXT NOT NULL, time_updated INTEGER NOT NULL);`,
	)
	db, err := openOpenCodeSQLiteReadOnly(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	ok, err := opencodeSQLiteHasWatermarkColumns(context.Background(), db, true)
	if err != nil {
		t.Fatalf("inspect columns: %v", err)
	}
	if ok {
		t.Fatal("schema without message.time_updated reported as watermarkable")
	}

	// A result without a watermark clears any stored one, so the next read
	// is a full read.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	p := New()
	p.storeSQLiteWatermark("k", info, opencodeSQLiteReadResult{HasWatermark: true, Watermark: incBase})
	if p.sqliteWatermarkSince("k", info) == nil {
		t.Fatal("stored watermark not returned")
	}
	p.storeSQLiteWatermark("k", info, opencodeSQLiteReadResult{HasWatermark: false})
	if p.sqliteWatermarkSince("k", info) != nil {
		t.Fatal("watermark kept for a schema without time_updated")
	}
}

// Provider.Collect reads incrementally across cycles and falls back to a
// full read when the DB file is replaced, even if the new file only holds
// rows older than the stored watermark.
func TestProviderCollectSQLiteIncrementalAndReplacedDB(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.db")
	newIncrementalTestDB(t, path)
	execSQL(t, path,
		insertMessage("msg-0", incBase-incStep, 5),
		insertMessage("msg-1", incBase, 10),
	)

	provider := New()
	opts := shared.TelemetryCollectOptions{
		Paths:     map[string]string{"db_path": path},
		PathLists: map[string][]string{"events_dirs": {filepath.Join(dir, "missing")}},
	}
	collect := func() map[string]shared.TelemetryEvent {
		t.Helper()
		events, err := provider.Collect(context.Background(), opts)
		if err != nil {
			t.Fatalf("Collect: %v", err)
		}
		return eventsByKey(t, events)
	}

	if got := keysOf(collect()); !reflect.DeepEqual(got, map[string]bool{"msg:msg-0": true, "msg:msg-1": true}) {
		t.Fatalf("first cycle events = %v", got)
	}

	// msg-1 is inside the slack window of the old watermark; msg-0 is not.
	execSQL(t, path, insertMessage("msg-2", incBase+incStep, 20))
	if got := keysOf(collect()); !reflect.DeepEqual(got, map[string]bool{"msg:msg-1": true, "msg:msg-2": true}) {
		t.Fatalf("second cycle events = %v, want the new message plus the slack window", got)
	}

	// Replace the file: a new inode holding an older row the watermark
	// would otherwise skip.
	replacement := filepath.Join(dir, "replacement.db")
	newIncrementalTestDB(t, replacement)
	execSQL(t, replacement,
		insertMessage("msg-restored", incBase-incStep, 30),
		insertMessage("msg-restored-2", incBase+2*incStep, 40),
	)
	for _, suffix := range []string{"-wal", "-shm"} {
		_ = os.Remove(path + suffix)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatalf("replace db: %v", err)
	}
	got := collect()
	want := map[string]bool{"msg:msg-restored": true, "msg:msg-restored-2": true}
	if !reflect.DeepEqual(keysOf(got), want) {
		t.Fatalf("cycle after replacement events = %v, want a full read %v", keysOf(got), want)
	}
}
