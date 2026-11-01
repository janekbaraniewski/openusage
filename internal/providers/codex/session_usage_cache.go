package codex

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/janekbaraniewski/openusage/internal/pricing"
)

// Codex rollouts are append-only JSONL and a long history runs to gigabytes,
// so re-parsing every file on each poll costs seconds of CPU (issue #379).
// Each file's parsed aggregate is cached with the byte offset it was parsed
// up to; a poll skips files whose size and mtime are unchanged and parses
// only the bytes appended to the rest.

// sessionFingerprintBytes is how many bytes at the file start and just before
// the resume offset are kept to detect a file rewritten in place.
const sessionFingerprintBytes = 64

// priceGeneration reports the pricing resolver's table generation. Cached
// per-file costs are re-priced from their recorded deltas when it changes.
var priceGeneration = func() uint64 {
	return pricing.DefaultResolver().Generation()
}

type sessionFileCacheEntry struct {
	info    os.FileInfo
	size    int64
	modTime time.Time
	// offset is the end of the last complete line parsed; a partial final
	// line is left for the next poll.
	offset int64
	line   int
	head   []byte
	tail   []byte
	parser *sessionFileParser
}

// refreshSessionFiles brings the per-file cache for sessionsDir up to date
// and returns the parsers in path order. Files that disappeared are evicted.
// The caller must hold sessionFileCacheMu until it is done with the parsers.
func (p *Provider) refreshSessionFiles(sessionsDir string) ([]*sessionFileParser, error) {
	paths, err := collectSessionFiles(sessionsDir)
	if err != nil {
		return nil, fmt.Errorf("collect codex session files: %w", err)
	}

	if p.sessionFileCache == nil {
		p.sessionFileCache = make(map[string]map[string]*sessionFileCacheEntry)
	}
	prev := p.sessionFileCache[sessionsDir]
	next := make(map[string]*sessionFileCacheEntry, len(paths))
	parsers := make([]*sessionFileParser, 0, len(paths))
	for _, path := range paths {
		info, statErr := os.Stat(path)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				continue // removed between the walk and the stat
			}
			p.sessionFileCache[sessionsDir] = next
			return nil, sessionFileReadError(path, statErr)
		}
		entry, err := refreshSessionFile(prev[path], path, info, dayFromSessionPath(path, sessionsDir))
		if err != nil {
			// Drop the entry: a failed walk may have applied part of the
			// appended bytes, so the next poll re-parses it from scratch.
			p.sessionFileCache[sessionsDir] = next
			return nil, sessionFileReadError(path, err)
		}
		next[path] = entry
		parsers = append(parsers, entry.parser)
	}
	p.sessionFileCache[sessionsDir] = next

	gen := priceGeneration()
	for _, parser := range parsers {
		if parser.priceGen != gen {
			parser.reprice(gen)
		}
	}
	return parsers, nil
}

func sessionFileReadError(path string, err error) error {
	return fmt.Errorf("read codex session file %s: %w", path, err)
}

// refreshSessionFile returns an entry reflecting the file's current
// contents, reusing cached when the file only grew by appends.
func refreshSessionFile(cached *sessionFileCacheEntry, path string, info os.FileInfo, defaultDay string) (*sessionFileCacheEntry, error) {
	if cached != nil && os.SameFile(cached.info, info) &&
		cached.size == info.Size() && cached.modTime.Equal(info.ModTime()) {
		return cached, nil
	}

	entry := cached
	if entry == nil || !entry.canResume(path, info) {
		entry = &sessionFileCacheEntry{parser: newSessionFileParser(defaultDay)}
		entry.parser.priceGen = priceGeneration()
	}

	offset, line, err := walkSessionLines(path, entry.offset, -1, entry.line, keepBreakdownLine, func(record sessionLine) error {
		entry.parser.apply(record)
		return nil
	})
	if err != nil {
		return nil, err
	}
	var head, tail []byte
	if !isCompressedSessionFile(path) {
		if head, tail, err = readSessionFingerprint(path, offset); err != nil {
			return nil, err
		}
	}
	entry.info = info
	entry.size = info.Size()
	entry.modTime = info.ModTime()
	entry.offset = offset
	entry.line = line
	entry.head = head
	entry.tail = tail
	return entry, nil
}

// canResume reports whether the file still holds the bytes parsed so far, so
// parsing can continue at the cached offset. A file that shrank, was replaced
// by another inode, or was rewritten in place must be parsed again in full.
func (e *sessionFileCacheEntry) canResume(path string, info os.FileInfo) bool {
	// Compressed rollouts cannot be resumed at a byte offset; a changed
	// .jsonl.zst is decoded again in full.
	if isCompressedSessionFile(path) || !os.SameFile(e.info, info) || info.Size() < e.offset {
		return false
	}
	head, tail, err := readSessionFingerprint(path, e.offset)
	if err != nil {
		return false
	}
	return bytes.Equal(head, e.head) && bytes.Equal(tail, e.tail)
}

// readSessionFingerprint returns the first bytes of the file and the bytes
// just before offset.
func readSessionFingerprint(path string, offset int64) (head, tail []byte, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	head = make([]byte, min(offset, sessionFingerprintBytes))
	if _, err := f.ReadAt(head, 0); err != nil && err != io.EOF {
		return nil, nil, err
	}
	tailStart := max(offset-sessionFingerprintBytes, 0)
	tail = make([]byte, offset-tailStart)
	if _, err := f.ReadAt(tail, tailStart); err != nil && err != io.EOF {
		return nil, nil, err
	}
	return head, tail, nil
}
