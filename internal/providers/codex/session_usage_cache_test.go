package codex

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

func cacheTestMeta() string {
	return `{"timestamp":"2026-10-01T10:00:00Z","type":"session_meta","payload":{"id":"s1","source":"cli","originator":"codex_cli_rs","model":"gpt-5"}}` + "\n"
}

func cacheTestTokenCount(total int) string {
	return fmt.Sprintf(`{"timestamp":"2026-10-01T10:00:%02dZ","ordinal":%d,"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":%d,"cached_input_tokens":0,"output_tokens":%d,"reasoning_output_tokens":0,"total_tokens":%d}}}}`, total%60, total, total/2, total-total/2, total) + "\n"
}

func cacheTestToolCall(id string) string {
	return fmt.Sprintf(`{"timestamp":"2026-10-01T10:00:30Z","type":"response_item","payload":{"type":"function_call","name":"exec_command","arguments":"{\"cmd\":\"go test ./...\"}","call_id":"%s"}}`, id) + "\n" +
		fmt.Sprintf(`{"timestamp":"2026-10-01T10:00:31Z","type":"response_item","payload":{"type":"function_call_output","call_id":"%s","output":"Process exited with code 0"}}`, id) + "\n"
}

func cacheTestNoise() string {
	return `{"timestamp":"2026-10-01T10:00:32Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"` + strings.Repeat("z", 4096) + `"}]}}` + "\n" +
		`{"timestamp":"2026-10-01T10:00:33Z","type":"compacted","payload":{"message":"x","replacement_history":[{"type":"event_msg"}]}}` + "\n"
}

func breakdownSnapshot(t *testing.T, p *Provider, dir string) core.UsageSnapshot {
	t.Helper()
	snap := core.UsageSnapshot{
		Metrics:     map[string]core.Metric{},
		Raw:         map[string]string{},
		DailySeries: map[string][]core.TimePoint{},
	}
	if err := p.readSessionUsageBreakdowns(dir, &snap); err != nil {
		t.Fatalf("readSessionUsageBreakdowns: %v", err)
	}
	return snap
}

// assertMatchesFullScan checks the incremental provider's result against a
// fresh provider that parses every file from scratch.
func assertMatchesFullScan(t *testing.T, cached *Provider, dir string) core.UsageSnapshot {
	t.Helper()
	got := breakdownSnapshot(t, cached, dir)
	want := breakdownSnapshot(t, New(), dir)
	if !reflect.DeepEqual(got.Metrics, want.Metrics) {
		t.Fatalf("metrics differ from full scan:\n got %v\nwant %v", metricValues(got), metricValues(want))
	}
	if !reflect.DeepEqual(got.Raw, want.Raw) {
		t.Fatalf("raw differs from full scan:\n got %v\nwant %v", got.Raw, want.Raw)
	}
	if !reflect.DeepEqual(got.DailySeries, want.DailySeries) {
		t.Fatalf("daily series differ from full scan:\n got %v\nwant %v", got.DailySeries, want.DailySeries)
	}
	return got
}

func metricValues(s core.UsageSnapshot) map[string]float64 {
	out := make(map[string]float64)
	for k, m := range s.Metrics {
		if m.Used != nil {
			out[k] = *m.Used
		}
	}
	return out
}

func usedMetric(t *testing.T, s core.UsageSnapshot, key string) float64 {
	t.Helper()
	m, ok := s.Metrics[key]
	if !ok || m.Used == nil {
		t.Fatalf("metric %q missing", key)
	}
	return *m.Used
}

var cacheTestClock = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

// writeSession writes content and bumps mtime so every change is visible
// even on filesystems with coarse timestamps.
func writeSession(t *testing.T, path, content string, flag int) {
	t.Helper()
	f, err := os.OpenFile(path, flag|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	cacheTestClock = cacheTestClock.Add(time.Second)
	if err := os.Chtimes(path, cacheTestClock, cacheTestClock); err != nil {
		t.Fatal(err)
	}
}

func sessionCacheEntry(t *testing.T, p *Provider, dir, path string) *sessionFileCacheEntry {
	t.Helper()
	p.sessionFileCacheMu.Lock()
	defer p.sessionFileCacheMu.Unlock()
	entry := p.sessionFileCache[dir][path]
	if entry == nil {
		t.Fatalf("no cache entry for %s", path)
	}
	return entry
}

func newSessionDir(t *testing.T) (dir, path string) {
	t.Helper()
	dir = t.TempDir()
	day := filepath.Join(dir, "2026", "10", "01")
	if err := os.MkdirAll(day, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir, filepath.Join(day, "rollout-2026-10-01T10-00-00-s1.jsonl")
}

func TestSessionBreakdownCacheParsesOnlyAppendedBytes(t *testing.T) {
	dir, path := newSessionDir(t)
	writeSession(t, path, cacheTestMeta()+cacheTestTokenCount(100)+cacheTestNoise(), os.O_TRUNC)

	p := New()
	first := assertMatchesFullScan(t, p, dir)
	if got := usedMetric(t, first, "total_ai_requests"); got != 1 {
		t.Fatalf("total_ai_requests = %v, want 1", got)
	}
	entry := sessionCacheEntry(t, p, dir, path)
	parser, offset := entry.parser, entry.offset

	// Unchanged file: nothing is re-read.
	assertMatchesFullScan(t, p, dir)
	if e := sessionCacheEntry(t, p, dir, path); e != entry || e.offset != offset {
		t.Fatal("unchanged file was re-parsed")
	}

	writeSession(t, path, cacheTestTokenCount(250)+cacheTestToolCall("call_1")+cacheTestTokenCount(400), os.O_APPEND)
	got := assertMatchesFullScan(t, p, dir)
	e := sessionCacheEntry(t, p, dir, path)
	if e.parser != parser {
		t.Fatal("append re-parsed the file from scratch instead of resuming")
	}
	if e.offset <= offset {
		t.Fatalf("offset did not advance: %d -> %d", offset, e.offset)
	}
	if got := usedMetric(t, got, "total_ai_requests"); got != 3 {
		t.Fatalf("total_ai_requests = %v, want 3", got)
	}
	if got := usedMetric(t, got, "tool_calls_total"); got != 1 {
		t.Fatalf("tool_calls_total = %v, want 1", got)
	}
}

func TestSessionBreakdownCacheLeavesPartialLineForNextPoll(t *testing.T) {
	dir, path := newSessionDir(t)
	writeSession(t, path, cacheTestMeta()+cacheTestTokenCount(100), os.O_TRUNC)

	p := New()
	assertMatchesFullScan(t, p, dir)
	complete := sessionCacheEntry(t, p, dir, path).offset

	line := cacheTestTokenCount(300)
	writeSession(t, path, line[:len(line)/2], os.O_APPEND)
	got := assertMatchesFullScan(t, p, dir)
	if off := sessionCacheEntry(t, p, dir, path).offset; off != complete {
		t.Fatalf("offset = %d, want %d (must not pass the incomplete line)", off, complete)
	}
	if got := usedMetric(t, got, "total_ai_requests"); got != 1 {
		t.Fatalf("total_ai_requests = %v, want 1 while the line is partial", got)
	}

	writeSession(t, path, line[len(line)/2:], os.O_APPEND)
	got = assertMatchesFullScan(t, p, dir)
	if got := usedMetric(t, got, "total_ai_requests"); got != 2 {
		t.Fatalf("total_ai_requests = %v, want 2 once the line completes", got)
	}
}

func TestSessionBreakdownCacheReparsesTruncatedOrReplacedFile(t *testing.T) {
	dir, path := newSessionDir(t)
	writeSession(t, path, cacheTestMeta()+cacheTestTokenCount(100)+cacheTestTokenCount(200)+cacheTestTokenCount(300), os.O_TRUNC)

	p := New()
	assertMatchesFullScan(t, p, dir)

	// Shrunk in place.
	writeSession(t, path, cacheTestMeta()+cacheTestTokenCount(50), os.O_TRUNC)
	got := assertMatchesFullScan(t, p, dir)
	if got := usedMetric(t, got, "total_ai_requests"); got != 1 {
		t.Fatalf("after truncate total_ai_requests = %v, want 1", got)
	}

	// Rewritten in place with different bytes but larger: the parsed prefix
	// no longer matches, so resuming at the old offset would be wrong.
	rewritten := strings.Replace(cacheTestMeta(), `"model":"gpt-5"`, `"model":"gpt-6"`, 1) +
		cacheTestTokenCount(70) + cacheTestTokenCount(90) + cacheTestToolCall("call_2")
	writeSession(t, path, rewritten, os.O_TRUNC)
	got = assertMatchesFullScan(t, p, dir)
	if _, ok := got.Metrics["model_gpt_5_total_tokens"]; ok {
		t.Fatal("stale model from the replaced content survived")
	}

	// Replaced by rename (new inode).
	tmp := path + ".tmp"
	writeSession(t, tmp, cacheTestMeta()+cacheTestTokenCount(10)+cacheTestTokenCount(20)+cacheTestTokenCount(30)+cacheTestTokenCount(40), os.O_TRUNC)
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
	got = assertMatchesFullScan(t, p, dir)
	if got := usedMetric(t, got, "total_ai_requests"); got != 4 {
		t.Fatalf("after replace total_ai_requests = %v, want 4", got)
	}

	// Removed files are evicted.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	assertMatchesFullScan(t, p, dir)
	p.sessionFileCacheMu.Lock()
	n := len(p.sessionFileCache[dir])
	p.sessionFileCacheMu.Unlock()
	if n != 0 {
		t.Fatalf("cache holds %d entries after the file was removed", n)
	}
}

func writeZstd(t *testing.T, path, content string) {
	t.Helper()
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	data := enc.EncodeAll([]byte(content), nil)
	_ = enc.Close()
	writeSession(t, path, string(data), os.O_TRUNC)
}

func TestCompressedSessionFilesAreRead(t *testing.T) {
	dir, path := newSessionDir(t)
	content := cacheTestMeta() + cacheTestTokenCount(100) + cacheTestToolCall("call_z") + cacheTestTokenCount(300)
	writeZstd(t, path+".zst", content)

	p := New()
	got := assertMatchesFullScan(t, p, dir)
	if got := usedMetric(t, got, "total_ai_requests"); got != 2 {
		t.Fatalf("total_ai_requests = %v, want 2 from the .jsonl.zst file", got)
	}

	// The latest-session reader and the telemetry collector see it too.
	_, payload, err := findLatestUsableSession(dir)
	if err != nil || payload == nil || payload.Info == nil || payload.Info.TotalTokenUsage.TotalTokens != 300 {
		t.Fatalf("findLatestUsableSession = %+v, %v; want token_count with 300 tokens", payload, err)
	}
	events, err := ParseTelemetrySessionFile(path + ".zst")
	usageEvents := 0
	for _, ev := range events {
		if ev.EventType == shared.TelemetryEventTypeMessageUsage {
			usageEvents++
		}
	}
	if err != nil || usageEvents != 2 {
		t.Fatalf("ParseTelemetrySessionFile = %d usage events, %v; want 2", usageEvents, err)
	}

	// A rewritten compressed file is decoded again in full.
	writeZstd(t, path+".zst", content+cacheTestTokenCount(450))
	got = assertMatchesFullScan(t, p, dir)
	if got := usedMetric(t, got, "total_ai_requests"); got != 3 {
		t.Fatalf("total_ai_requests = %v, want 3 after the .zst changed", got)
	}

	// While the CLI compresses a finished rollout both files exist; the
	// session must be counted once.
	writeSession(t, path, content, os.O_TRUNC)
	got = assertMatchesFullScan(t, p, dir)
	if got := usedMetric(t, got, "total_ai_requests"); got != 2 {
		t.Fatalf("total_ai_requests = %v, want 2 with plain and compressed twins", got)
	}
	if got := usedMetric(t, got, "composer_sessions"); got != 1 {
		t.Fatalf("composer_sessions = %v, want 1", got)
	}
}

func TestSessionLineKind(t *testing.T) {
	tests := []struct {
		line          string
		record, inner string
		ok            bool
	}{
		{`{"timestamp":"2026-10-01T10:00:00Z","type":"event_msg","payload":{"type":"token_count","info":null}}`, "event_msg", "token_count", true},
		{`{"timestamp":"2026-10-01T10:00:00Z","ordinal":42,"type":"response_item","payload":{"type":"reasoning"}}`, "response_item", "reasoning", true},
		{`{"timestamp":"2026-10-01T10:00:00Z","type":"session_meta","payload":{"id":"x"}}`, "session_meta", "", true},
		{`{"timestamp":"2026-10-01T10:00:00Z","type":"compacted","payload":{"message":"\"type\":\"event_msg\""}}`, "compacted", "", true},
		{`{"type":"event_msg","payload":{"type":"token_count"}}`, "", "", false},
	}
	for _, tt := range tests {
		record, inner, ok := sessionLineKind([]byte(tt.line))
		if ok != tt.ok || string(record) != tt.record || string(inner) != tt.inner {
			t.Errorf("sessionLineKind(%s) = %q, %q, %v; want %q, %q, %v", tt.line, record, inner, ok, tt.record, tt.inner, tt.ok)
		}
	}
	// Lines with an unexpected prefix are always decoded.
	if !keepSessionLine([]byte(`{"type":"event_msg","payload":{"type":"agent_message"}}`), keepBreakdownLine) {
		t.Fatal("unrecognised prefix must fall back to a full decode")
	}
	if keepSessionLine([]byte(`{"timestamp":"t","type":"event_msg","payload":{"type":"agent_message"}}`), keepBreakdownLine) {
		t.Fatal("agent_message must be skipped by the breakdown filter")
	}
}
