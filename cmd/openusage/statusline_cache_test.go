package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stubStatuslineRender swaps the slow render path for a counter so tests can
// tell a cache hit (no render) from a miss.
func stubStatuslineRender(t *testing.T) *int {
	t.Helper()
	calls := 0
	orig := statuslineRender
	statuslineRender = func(in statuslineInput, _ statuslineOptions) string {
		calls++
		return fmt.Sprintf("render#%d %s", calls, in.SessionID)
	}
	t.Cleanup(func() { statuslineRender = orig })
	return &calls
}

func statuslineStdin(t *testing.T, sessionID, transcript string) string {
	t.Helper()
	return fmt.Sprintf(`{"session_id":%q,"transcript_path":%q,"model":{"display_name":"Opus"},"cost":{"total_duration_ms":%d}}`,
		sessionID, transcript, time.Now().UnixNano())
}

func runStatuslineOnce(t *testing.T, opts statuslineOptions, stdin string) string {
	t.Helper()
	var out bytes.Buffer
	if err := runStatusline(opts, strings.NewReader(stdin), &out); err != nil {
		t.Fatalf("runStatusline: %v", err)
	}
	return out.String()
}

func setupStatuslineCacheTest(t *testing.T) (transcript string, calls *int) {
	t.Helper()
	home := isolateHome(t)
	transcript = filepath.Join(home, "session.jsonl")
	if err := os.WriteFile(transcript, []byte(`{"type":"user"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return transcript, stubStatuslineRender(t)
}

func TestStatuslineCache_HitServesIdenticalBytes(t *testing.T) {
	transcript, calls := setupStatuslineCacheTest(t)
	opts := defaultStatuslineOptions()

	first := runStatuslineOnce(t, opts, statuslineStdin(t, "sess-1", transcript))
	// The stdin duration field changes between calls; it must not bust the cache.
	second := runStatuslineOnce(t, opts, statuslineStdin(t, "sess-1", transcript))

	if *calls != 1 {
		t.Fatalf("expected 1 render (second call cached), got %d", *calls)
	}
	if first != second {
		t.Fatalf("cached output differs from fresh render:\nfresh:  %q\ncached: %q", first, second)
	}
}

func TestStatuslineCache_MissOnTranscriptChange(t *testing.T) {
	transcript, calls := setupStatuslineCacheTest(t)
	opts := defaultStatuslineOptions()

	runStatuslineOnce(t, opts, statuslineStdin(t, "sess-1", transcript))
	f, err := os.OpenFile(transcript, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(`{"type":"assistant"}` + "\n")
	_ = f.Close()

	out := runStatuslineOnce(t, opts, statuslineStdin(t, "sess-1", transcript))
	if *calls != 2 {
		t.Fatalf("transcript change should force a re-render, got %d renders", *calls)
	}
	if out != "render#2 sess-1\n" {
		t.Fatalf("unexpected output %q", out)
	}
}

func TestStatuslineCache_MissOnExpiry(t *testing.T) {
	transcript, calls := setupStatuslineCacheTest(t)
	opts := defaultStatuslineOptions()

	runStatuslineOnce(t, opts, statuslineStdin(t, "sess-1", transcript))
	path := statuslineCachePath("sess-1")
	old := time.Now().Add(-statuslineCacheTTL - time.Second)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	runStatuslineOnce(t, opts, statuslineStdin(t, "sess-1", transcript))
	if *calls != 2 {
		t.Fatalf("expired cache should force a re-render, got %d renders", *calls)
	}
}

func TestStatuslineCache_MissOnOptionsOrSession(t *testing.T) {
	transcript, calls := setupStatuslineCacheTest(t)
	opts := defaultStatuslineOptions()

	runStatuslineOnce(t, opts, statuslineStdin(t, "sess-1", transcript))

	other := opts
	other.segments = []string{"today"}
	runStatuslineOnce(t, other, statuslineStdin(t, "sess-1", transcript))
	if *calls != 2 {
		t.Fatalf("different segments must not reuse the cache, got %d renders", *calls)
	}

	out := runStatuslineOnce(t, opts, statuslineStdin(t, "sess-2", transcript))
	if *calls != 3 || out != "render#3 sess-2\n" {
		t.Fatalf("a different session must not reuse the cache: renders=%d out=%q", *calls, out)
	}
}

func TestStatuslineCache_CorruptFileFallsBackToRender(t *testing.T) {
	transcript, calls := setupStatuslineCacheTest(t)
	path := statuslineCachePath("sess-1")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("garbage-without-newline"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := runStatuslineOnce(t, defaultStatuslineOptions(), statuslineStdin(t, "sess-1", transcript))
	if *calls != 1 || out != "render#1 sess-1\n" {
		t.Fatalf("corrupt cache should fall back to a render: renders=%d out=%q", *calls, out)
	}
}

func TestPruneStatuslineCache(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, statuslineCachePrefix+"old")
	fresh := filepath.Join(dir, statuslineCachePrefix+"new")
	other := filepath.Join(dir, "tmux-laststatus")
	for _, p := range []string{stale, fresh, other} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-statuslinePruneAge - time.Hour)
	for _, p := range []string{stale, other} {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	pruneStatuslineCache(dir, time.Now())
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("stale statusline cache should be pruned")
	}
	for _, p := range []string{fresh, other} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s should be kept: %v", p, err)
		}
	}
}
