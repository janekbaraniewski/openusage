package amp

import (
	"path/filepath"
	"testing"
	"time"
)

func TestParseAmpThreadFileBasic(t *testing.T) {
	path := filepath.Join("testdata", "thread_basic.json")
	events, err := parseAmpThreadFile(path)
	if err != nil {
		t.Fatalf("parseAmpThreadFile returned error: %v", err)
	}
	// Two assistant rows with non-empty usage; the user rows and the
	// all-zero usage row should be filtered.
	if len(events) != 2 {
		t.Fatalf("expected 2 assistant events, got %d", len(events))
	}

	first := events[0]
	if first.MessageID != "msg-asst-1" {
		t.Errorf("expected first MessageID 'msg-asst-1', got %q", first.MessageID)
	}
	if first.Model != "sonnet-4.5" {
		t.Errorf("expected first model 'sonnet-4.5', got %q", first.Model)
	}
	if first.Tokens.Input != 1200 || first.Tokens.Output != 350 {
		t.Errorf("unexpected first tokens: %+v", first.Tokens)
	}
	if first.Tokens.CacheRead != 800 || first.Tokens.CacheWrite != 200 {
		t.Errorf("unexpected first cache tokens: %+v", first.Tokens)
	}
	wantTS, _ := time.Parse(time.RFC3339, "2026-05-10T09:00:05Z")
	if !first.Timestamp.Equal(wantTS) {
		t.Errorf("expected timestamp %v, got %v", wantTS, first.Timestamp)
	}
	if first.ThreadID != "thread-basic-001" {
		t.Errorf("expected ThreadID 'thread-basic-001', got %q", first.ThreadID)
	}
	if first.Source != "thread" {
		t.Errorf("expected Source 'thread', got %q", first.Source)
	}
}

func TestParseAmpThreadFileCamelCaseAndNegativeClamp(t *testing.T) {
	path := filepath.Join("testdata", "thread_camelcase.json")
	events, err := parseAmpThreadFile(path)
	if err != nil {
		t.Fatalf("parseAmpThreadFile error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	evt := events[0]
	if evt.MessageID != "msg-cc-1" {
		t.Errorf("camelCase messageId not picked up; got %q", evt.MessageID)
	}
	if evt.Tokens.Input != 500 {
		t.Errorf("expected Input=500, got %d", evt.Tokens.Input)
	}
	if evt.Tokens.Output != 100 {
		t.Errorf("expected Output=100, got %d", evt.Tokens.Output)
	}
	// Negative cache_read clamped to 0.
	if evt.Tokens.CacheRead != 0 {
		t.Errorf("expected negative cache_read clamped to 0, got %d", evt.Tokens.CacheRead)
	}
	if evt.Tokens.CacheWrite != 25 {
		t.Errorf("expected CacheWrite=25 from cacheCreationInputTokens, got %d", evt.Tokens.CacheWrite)
	}
}

func TestParseAmpThreadFileMissing(t *testing.T) {
	if _, err := parseAmpThreadFile(filepath.Join("testdata", "does-not-exist.json")); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestParseAmpThreadFileMalformed(t *testing.T) {
	tmp := t.TempDir()
	bad := filepath.Join(tmp, "bad.json")
	if err := writeFile(bad, `{"id":"oops",`); err != nil {
		t.Fatal(err)
	}
	if _, err := parseAmpThreadFile(bad); err == nil {
		t.Fatal("expected JSON parse error")
	}
}

func TestResolveEventTimestampFallback(t *testing.T) {
	mtime := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	// Empty message and thread strings; fall back to mtime.
	ts := resolveEventTimestamp("", "", mtime)
	if !ts.Equal(mtime) {
		t.Errorf("expected mtime fallback, got %v", ts)
	}
	// Thread timestamp parses; used since message ts is empty.
	ts = resolveEventTimestamp("", "2026-03-04T05:06:07Z", mtime)
	want, _ := time.Parse(time.RFC3339, "2026-03-04T05:06:07Z")
	if !ts.Equal(want) {
		t.Errorf("expected thread fallback %v, got %v", want, ts)
	}
	// Message timestamp parses and wins.
	ts = resolveEventTimestamp("2026-07-08T09:10:11Z", "2026-03-04T05:06:07Z", mtime)
	want, _ = time.Parse(time.RFC3339, "2026-07-08T09:10:11Z")
	if !ts.Equal(want) {
		t.Errorf("expected message ts to win, got %v", ts)
	}
}

// writeFile is a tiny helper used by a couple of tests in this package; it
// avoids importing os here just for the one-liner.
func writeFile(path, content string) error {
	return writeFileImpl(path, content)
}
