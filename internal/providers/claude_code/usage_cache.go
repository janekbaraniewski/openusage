package claude_code

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// The 5-hour usage utilization comes from the claude.ai usage API, which is
// slow to reach (cookie extraction + network round-trip). Surfaces that render
// under a tight time budget — the tmux status bar (~800ms self-kill) and the
// Claude Code statusline — cannot afford that fetch on every render. So any
// process that *does* obtain the value (the daemon's 30s poll, a TUI poll, a
// generous-budget render) writes it to a tiny shared cache file, and the
// budget-limited surfaces read it back instead of fetching live.
//
// The file format is intentionally the same shape the statusline has always
// written (`{"pct":..,"ts":..}`) so the cache is shared, not duplicated: a
// write from any surface warms the fallback for all of them.

// usageCacheEntry is the on-disk shape of the shared 5h-usage cache.
type usageCacheEntry struct {
	Pct float64   `json:"pct"`
	TS  time.Time `json:"ts"`
}

// UsageCachePath returns the shared 5h-usage cache file, or "" when the home
// directory cannot be resolved (in which case caching is silently skipped).
func UsageCachePath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".cache", "openusage", "statusline-5h.json")
}

// WriteFiveHourCache overwrites the shared cache atomically (temp + rename) so
// concurrent readers from parallel render processes can never observe a
// half-written file. Best-effort: any error is swallowed.
func WriteFiveHourCache(pct float64) {
	p := UsageCachePath()
	if p == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	b, err := json.Marshal(usageCacheEntry{Pct: pct, TS: time.Now()})
	if err != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".statusline-5h-*.tmp")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return
	}
	if err := os.Rename(tmpName, p); err != nil {
		_ = os.Remove(tmpName)
	}
}

// ReadFiveHourCache returns the cached 5h usage %, its age, and whether it was
// readable. Callers decide their own staleness tolerance from the age.
func ReadFiveHourCache() (pct float64, age time.Duration, ok bool) {
	p := UsageCachePath()
	if p == "" {
		return 0, 0, false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return 0, 0, false
	}
	var c usageCacheEntry
	if json.Unmarshal(b, &c) != nil || c.TS.IsZero() {
		return 0, 0, false
	}
	return c.Pct, time.Since(c.TS), true
}
