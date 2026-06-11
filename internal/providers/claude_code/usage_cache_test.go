package claude_code

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFiveHourCacheRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if _, _, ok := ReadFiveHourCache(); ok {
		t.Fatalf("expected no cache before any write")
	}

	WriteFiveHourCache(42.5)

	pct, age, ok := ReadFiveHourCache()
	if !ok {
		t.Fatalf("expected cache to be readable after write")
	}
	if pct != 42.5 {
		t.Errorf("pct = %v, want 42.5", pct)
	}
	if age < 0 || age > time.Minute {
		t.Errorf("age = %v, want a small non-negative duration", age)
	}

	// The on-disk file must live at the shared statusline path so the bar and
	// the statusline read the same cache.
	want := filepath.Join(home, ".cache", "openusage", "statusline-5h.json")
	if got := UsageCachePath(); got != want {
		t.Errorf("UsageCachePath() = %q, want %q", got, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Errorf("cache file not at shared path: %v", err)
	}
}

func TestReadFiveHourCacheRejectsCorrupt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	p := UsageCachePath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := ReadFiveHourCache(); ok {
		t.Errorf("expected corrupt cache to be rejected")
	}
}
