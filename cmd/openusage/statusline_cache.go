package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/version"
)

// statuslineCacheTTL bounds how long a rendered statusline is reprinted
// without re-reading the logs. Claude Code invokes the statusline on every
// message (several times a minute while chatting); the line only shows
// minute-resolution block time and cents, so a 10s reuse window is invisible
// while cutting most of the per-invocation work.
const statuslineCacheTTL = 10 * time.Second

// statuslinePruneAge removes per-session cache files for sessions that have
// not rendered for a day, so the cache directory does not grow forever.
const statuslinePruneAge = 24 * time.Hour

const statuslineCachePrefix = "statusline-render-"

// statuslineCacheKey fingerprints every input the rendered line depends on:
// the decoded stdin fields we read, the transcript's mtime and size (a new
// message changes both), the render options, and the binary version. Fields
// of the stdin payload we ignore (durations, line counts) are deliberately
// excluded, since they change on every call without affecting the output.
func statuslineCacheKey(in statuslineInput, opts statuslineOptions) string {
	h := sha256.New()
	if b, err := json.Marshal(in); err == nil {
		h.Write(b)
	}
	h.Write([]byte{0})
	if p := strings.TrimSpace(in.TranscriptPath); p != "" {
		if info, err := os.Stat(p); err == nil {
			fmt.Fprintf(h, "transcript:%d:%d", info.ModTime().UnixNano(), info.Size())
		} else {
			h.Write([]byte("transcript:missing"))
		}
	}
	h.Write([]byte{0})
	fmt.Fprintf(h, "opts:%t|%s|%t|%g|%g|%s", opts.offline, opts.mode, opts.color,
		opts.contextMedium, opts.contextHigh, strings.Join(opts.segments, ","))
	h.Write([]byte{0})
	h.Write([]byte(version.Version))
	return hex.EncodeToString(h.Sum(nil))
}

// statuslineCachePath returns the per-session cache file, or "" when the home
// directory cannot be resolved. One file per session keeps parallel Claude
// Code sessions from evicting each other's entry.
func statuslineCachePath(sessionID string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(sessionID))
	return filepath.Join(home, ".cache", "openusage", statuslineCachePrefix+hex.EncodeToString(sum[:8]))
}

// readStatuslineCache returns the cached line when the file holds the same
// key and was written less than statuslineCacheTTL ago.
func readStatuslineCache(sessionID, key string, now time.Time) (string, bool) {
	path := statuslineCachePath(sessionID)
	if path == "" {
		return "", false
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", false
	}
	if age := now.Sub(info.ModTime()); age < 0 || age >= statuslineCacheTTL {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	gotKey, line, ok := strings.Cut(string(data), "\n")
	if !ok || gotKey != key {
		return "", false
	}
	return line, true
}

// writeStatuslineCache stores the rendered line atomically and prunes
// long-idle session entries. Best-effort: errors are ignored.
func writeStatuslineCache(sessionID, key, line string) {
	path := statuslineCachePath(sessionID)
	if path == "" {
		return
	}
	if err := writeFileAtomic(path, []byte(key+"\n"+line)); err != nil {
		return
	}
	pruneStatuslineCache(filepath.Dir(path), time.Now())
}

func pruneStatuslineCache(dir string, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), statuslineCachePrefix) {
			continue
		}
		info, err := e.Info()
		if err != nil || now.Sub(info.ModTime()) < statuslinePruneAge {
			continue
		}
		_ = os.Remove(filepath.Join(dir, e.Name()))
	}
}
