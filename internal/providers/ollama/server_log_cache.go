package ollama

import (
	"sync"

	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

// maxLogLine bounds one server log line; longer lines are skipped.
const maxLogLine = 1024 * 1024

// serverLogCache keeps the inference events parsed from each server log so a
// poll only parses bytes appended since the previous one. Rotated logs
// (server-N.log) never change and are not re-read; server.log is read from
// where the last poll stopped. A rotation (rename) shows up as a different
// file behind the same path and is re-read from the start.
//
// Events, not window totals, are cached: the 5h / 1d / today windows move with
// the clock, so totals are recomputed from the events on every poll.
type serverLogCache struct {
	mu    sync.Mutex
	files map[string]*serverLogFile
}

type serverLogFile struct {
	reader shared.IncrementalReader
	events []ginLogEvent // inference requests from complete lines
	// tail is the event on a last line without a newline (server.log
	// mid-write). It counts now, as in a full parse, and is re-read once the
	// line is complete.
	tail []ginLogEvent
}

// events returns the inference events of every file, in file order and line
// order within a file, exactly as a full parse of the files would.
func (c *serverLogCache) events(paths []string) ([]ginLogEvent, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.files == nil {
		c.files = make(map[string]*serverLogFile)
	}
	live := make(map[string]bool, len(paths))
	var out []ginLogEvent
	for _, path := range paths {
		live[path] = true
		f := c.files[path]
		if f == nil {
			f = &serverLogFile{reader: shared.IncrementalReader{MaxLineBytes: maxLogLine}}
			c.files[path] = f
		}
		var fresh []ginLogEvent
		res, err := f.reader.Read(path, nil, func(line []byte, _ int) {
			if ev, ok := parseGINLogLine(string(line)); ok && isInferencePath(ev.Path) {
				fresh = append(fresh, ev)
			}
		})
		if err != nil {
			delete(c.files, path)
			return nil, err
		}
		if res.Changed {
			if res.Reset {
				f.events = nil
			}
			f.events = append(f.events, fresh...)
			f.tail = nil
			if ev, ok := parseGINLogLine(string(res.Tail)); ok && isInferencePath(ev.Path) {
				f.tail = []ginLogEvent{ev}
			}
		}
		out = append(out, f.events...)
		out = append(out, f.tail...)
	}
	for path := range c.files {
		if !live[path] {
			delete(c.files, path)
		}
	}
	return out, nil
}
