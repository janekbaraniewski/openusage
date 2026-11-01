package dsh

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

// sessionCache keeps the parsed form of every session log, keyed by path and
// invalidated by (mtime, size). Session logs are compressed, so a changed
// file is re-read whole; an unchanged one costs a single stat per poll.
type sessionCache struct {
	mu      sync.Mutex
	entries map[string]cachedSession
}

type cachedSession struct {
	signature shared.FileSignature
	session   *sessionData
}

// loadedSession pairs a parsed session with the file state it was read at.
type loadedSession struct {
	path      string
	signature shared.FileSignature
	session   *sessionData
}

// load returns every session under dir in path order, re-reading only the
// logs whose signature moved and forgetting the ones that are gone. A single
// unreadable directory or file never aborts the walk.
func (c *sessionCache) load(ctx context.Context, dir string) ([]loadedSession, error) {
	signatures := make(map[string]shared.FileSignature)
	walkErr := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != sessionFileName {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		info, statErr := d.Info()
		if statErr != nil {
			return nil
		}
		signatures[path] = shared.FileSignature{ModTime: info.ModTime(), Size: info.Size()}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}

	paths := make([]string, 0, len(signatures))
	for path := range signatures {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]cachedSession)
	}
	for path := range c.entries {
		if _, ok := signatures[path]; !ok {
			delete(c.entries, path)
		}
	}

	out := make([]loadedSession, 0, len(paths))
	for _, path := range paths {
		signature := signatures[path]
		entry, ok := c.entries[path]
		if !ok || !entry.signature.Equal(signature) {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			session, err := readSessionFile(path)
			if err != nil {
				continue
			}
			entry = cachedSession{signature: signature, session: session}
			c.entries[path] = entry
		}
		if entry.session == nil {
			continue
		}
		out = append(out, loadedSession{path: path, signature: entry.signature, session: entry.session})
	}
	return out, nil
}
