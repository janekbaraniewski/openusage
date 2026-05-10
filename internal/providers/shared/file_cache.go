package shared

import (
	"os"
	"time"
)

// FileSignature captures the (mtime, size) pair we use to detect whether a
// JSONL file has changed since we last parsed it. Providers that maintain
// per-file caches of parsed records (claude_code's jsonlCache and
// telemetryCache, codex's telemetryCache) all want the same invalidation
// rule: re-parse if mtime moved, re-parse if the file shrank, incremental-
// parse the suffix if it only grew.
//
// Use Stat to fetch a fresh signature. Compare with Equal to decide whether
// the cache is still valid for re-use as-is. Use Grew to decide whether an
// append-only incremental parse is sufficient.
type FileSignature struct {
	ModTime time.Time
	Size    int64
}

// StatSignature returns the current signature for path. Returns (zero, err)
// on I/O failure; callers typically treat that as "not cached, must read".
func StatSignature(path string) (FileSignature, error) {
	info, err := os.Stat(path)
	if err != nil {
		return FileSignature{}, err
	}
	return FileSignature{ModTime: info.ModTime(), Size: info.Size()}, nil
}

// Equal reports whether the file is byte-for-byte identical to when the
// cache was populated.
func (a FileSignature) Equal(b FileSignature) bool {
	return a.Size == b.Size && a.ModTime.Equal(b.ModTime)
}

// Grew reports whether the file is the same modtime-or-newer and is at
// least as large as the cached signature — the "append-only growth" case
// that JSONL caches can satisfy with an incremental seek-and-parse rather
// than a full re-read.
func (a FileSignature) Grew(b FileSignature) bool {
	return b.Size >= a.Size && (a.ModTime.Equal(b.ModTime) || b.ModTime.After(a.ModTime))
}
