package muse_code

import (
	"strconv"
	"testing"
	"time"
)

func TestRecordEntry_ParsesBuckets(t *testing.T) {
	ts := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	entry, ok := museRecordEntry(testRecord(t, ts, "muse-spark-1.3"))
	if !ok {
		t.Fatal("recordEntry rejected a valid model_completed record")
	}
	if entry.Model != "muse-spark-1.3" {
		t.Errorf("model = %q", entry.Model)
	}
	if entry.Input != 2000 || entry.CacheRead != 8000 {
		t.Errorf("input/cacheRead = %d/%d, want 2000/8000", entry.Input, entry.CacheRead)
	}
	if entry.Output != 500 || entry.Reasoning != 100 {
		t.Errorf("output/reasoning = %d/%d, want 500/100", entry.Output, entry.Reasoning)
	}
	if entry.TotalTokens != 10600 {
		t.Errorf("total = %d, want 10600", entry.TotalTokens)
	}
	if !entry.Timestamp.Equal(ts) {
		t.Errorf("timestamp = %v, want %v", entry.Timestamp, ts)
	}
}

func TestRecordEntry_IgnoresNonUsagePayloads(t *testing.T) {
	ts := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	if _, ok := museRecordEntry([]byte(`{"payload_type":"runtime.session","payload":{"kind":"run","event":{"kind":"resource_usage_sampled"}}}`)); ok {
		t.Error("resource sample accepted")
	}
	if _, ok := museRecordEntry([]byte("not json")); ok {
		t.Error("invalid JSON accepted")
	}
	if _, ok := museRecordEntry(zeroUsageRecord(t, ts)); ok {
		t.Error("zero-usage completion accepted")
	}
	if _, ok := museRecordEntry(testRecord(t, ts, "  ")); ok {
		t.Error("blank model accepted")
	}
}

func zeroUsageRecord(t *testing.T, ts time.Time) []byte {
	t.Helper()
	return []byte(`{"schema_version":1,"recorded_at":` + strconv.FormatInt(ts.UnixMicro(), 10) +
		`,"payload_type":"runtime.session","payload":{"kind":"run",` +
		`"event":{"kind":"model_completed","model":"muse-spark-1.3",` +
		`"usage":{"input_tokens":0,"output_tokens":0,"reasoning_tokens":0}}}}`)
}

func TestParseMuseRecords_Wrapper(t *testing.T) {
	ts := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	entries := parseMuseRecords(wrappedRecord(t, testRecord(t, ts, "muse-spark-1.3")))
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if entries[0].Model != "muse-spark-1.3" {
		t.Errorf("model = %q", entries[0].Model)
	}
}

func TestReadMuseSessionFile_SkipsQuietLines(t *testing.T) {
	ts := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	root := t.TempDir()
	rec := testRecord(t, ts, "muse-spark-1.3")
	quiet := []byte(`{"payload_type":"runtime.session","payload":{"kind":"security_mode"}}`)
	writeSession(t, root, "2026/09/07/s1/session.jsonl", rec, quiet, rec)
	entries, err := readMuseSessionFile(root + "/2026/09/07/s1/session.jsonl")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("entries = %d, want 2", len(entries))
	}
	for _, e := range entries {
		if e.SessionID != "s1" {
			t.Errorf("session id = %q, want s1", e.SessionID)
		}
	}
}
