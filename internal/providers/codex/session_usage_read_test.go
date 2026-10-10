package codex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFindLastTokenCountDoesNotScanLargeHistoricalPrefix(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout-large-prefix.jsonl")
	largeHistoricalLine := `{"type":"response_item","payload":{"type":"function_call_output","output":"` + strings.Repeat("x", maxScannerBufferSize) + `"}}` + "\n"
	latest := `{"timestamp":"2026-07-17T10:00:02Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":15,"output_tokens":10,"total_tokens":25}}}}` + "\n"
	if err := os.WriteFile(path, []byte(largeHistoricalLine+latest), 0o644); err != nil {
		t.Fatalf("write session: %v", err)
	}

	payload, err := findLastTokenCount(path)
	if err != nil {
		t.Fatalf("findLastTokenCount() error: %v", err)
	}
	if payload == nil || payload.Info == nil {
		t.Fatal("findLastTokenCount() returned no token payload")
	}
	if got := payload.Info.TotalTokenUsage.TotalTokens; got != 25 {
		t.Fatalf("total_tokens = %d, want 25", got)
	}
}

func TestSessionUsageBreakdownsCanBeDisabled(t *testing.T) {
	t.Setenv("OPENUSAGE_CODEX_SKIP_SESSION_BREAKDOWNS", "true")
	if codexSessionUsageBreakdownsEnabled() {
		t.Fatal("session usage breakdowns enabled with skip flag set")
	}

	t.Setenv("OPENUSAGE_CODEX_SKIP_SESSION_BREAKDOWNS", "false")
	if !codexSessionUsageBreakdownsEnabled() {
		t.Fatal("session usage breakdowns disabled without skip flag")
	}
}

func TestSessionFileTime(t *testing.T) {
	want, _ := time.Parse("2006-01-02T15-04-05", "2026-09-15T23-59-18")
	cases := map[string]struct {
		path string
		ok   bool
		when time.Time
	}{
		"dated rollout": {
			path: filepath.Join("2026", "09", "15", "rollout-2026-09-15T23-59-18-01a0a895-6536-77f3-9eb3-def8496598ab.jsonl"),
			ok:   true, when: want,
		},
		"foreign drop": {path: "notes.jsonl"},
		"short stamp":  {path: "rollout-2026-09-15.jsonl"},
		"bad stamp":    {path: "rollout-not-a-date-01a0a895.jsonl"},
		"wrong ext":    {path: "rollout-2026-09-15T23-59-18-01a0a895.txt"},
		"unprefixed":   {path: "2026-09-15T23-59-18-01a0a895.jsonl"},
	}
	for name, tc := range cases {
		got, ok := sessionFileTime(tc.path)
		if ok != tc.ok || (ok && !got.Equal(tc.when)) {
			t.Errorf("%s: sessionFileTime(%q) = (%v, %v), want (%v, %v)",
				name, tc.path, got, ok, tc.when, tc.ok)
		}
	}
}

// Codex rewrites old rollout files during compaction/archival, bumping their
// mtime past the live session's. Latest-file selection must follow the
// filename timestamp, not mtime — otherwise a stale file's dead rate limits
// win and the provider degrades to cache_hit_ratio-only output.
func TestFindLatestSessionFile_PrefersFilenameTimeOverMtime(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "rollout-2026-08-14T00-54-32-019ffed6-1a52-70e1-9815-55047135977f.jsonl")
	live := filepath.Join(dir, "rollout-2026-09-15T23-59-18-01a0a895-6536-77f3-9eb3-def8496598ab.jsonl")
	foreign := filepath.Join(dir, "scratch.jsonl")
	for _, p := range []string{stale, live, foreign} {
		if err := os.WriteFile(p, []byte("{}\n"), 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}
	now := time.Now()
	// Invert the truth: stale file has the newest mtime (the rewrite case),
	// live file is older, foreign newest of all but unparseable.
	if err := os.Chtimes(stale, now, now); err != nil {
		t.Fatalf("chtimes stale: %v", err)
	}
	if err := os.Chtimes(live, now.Add(-time.Hour), now.Add(-time.Hour)); err != nil {
		t.Fatalf("chtimes live: %v", err)
	}
	if err := os.Chtimes(foreign, now.Add(time.Hour), now.Add(time.Hour)); err != nil {
		t.Fatalf("chtimes foreign: %v", err)
	}

	got, err := findLatestSessionFile(dir)
	if err != nil {
		t.Fatalf("findLatestSessionFile() error: %v", err)
	}
	if got != live {
		t.Fatalf("findLatestSessionFile() = %q, want live file %q", got, live)
	}
}
