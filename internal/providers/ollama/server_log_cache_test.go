package ollama

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func ginLine(ts time.Time, status int, path string) string {
	return fmt.Sprintf("[GIN] %s | %d |  1.5s |  127.0.0.1 | POST     %q\n", ts.Format("2006/01/02 - 15:04:05"), status, path)
}

func appendLog(t *testing.T, path, s string, mtime time.Time) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

// TestServerLogCache_MatchesFullParse appends to server.log, rotates it and
// leaves partial lines, comparing the cached events with a cold cache.
func TestServerLogCache_MatchesFullParse(t *testing.T) {
	dir := t.TempDir()
	server := filepath.Join(dir, "server.log")
	rotated := filepath.Join(dir, "server-1.log")
	ts := time.Now().Add(-time.Hour).Truncate(time.Second)
	mtime := time.Now()
	tick := func() time.Time { mtime = mtime.Add(time.Second); return mtime }

	appendLog(t, rotated, ginLine(ts, 200, "/api/chat")+"noise\n", tick())
	appendLog(t, server, ginLine(ts, 200, "/api/generate")+ginLine(ts, 200, "/api/tags"), tick())

	var warm serverLogCache
	check := func(step string) {
		t.Helper()
		paths := []string{rotated, server}
		got, err := warm.events(paths)
		if err != nil {
			t.Fatal(err)
		}
		var cold serverLogCache
		want, err := cold.events(paths)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: warm %v != cold %v", step, got, want)
		}
	}
	check("initial")
	check("unchanged")

	line := ginLine(ts.Add(time.Minute), 500, "/v1/chat/completions")
	appendLog(t, server, line[:20], tick())
	check("partial line")
	check("partial line unchanged")
	appendLog(t, server, line[20:], tick())
	check("partial line completed")
	appendLog(t, server, ginLine(ts.Add(2*time.Minute), 200, "/api/chat")[:60], tick())
	check("unterminated but parseable line")
	appendLog(t, server, "\n", tick())
	check("newline")

	// Rotate: server.log becomes server-1.log, a fresh server.log starts.
	if err := os.Rename(server, rotated); err != nil {
		t.Fatal(err)
	}
	appendLog(t, server, ginLine(ts.Add(3*time.Minute), 404, "/api/embed"), tick())
	check("rotation")

	// Truncate in place.
	if err := os.WriteFile(server, []byte(ginLine(ts, 200, "/api/chat")), 0o644); err != nil {
		t.Fatal(err)
	}
	check("truncate")
}

func TestHasChanged_SeesServerLogAppends(t *testing.T) {
	dir := t.TempDir()
	logs := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logs, 0o755); err != nil {
		t.Fatal(err)
	}
	server := filepath.Join(logs, "server.log")
	old := time.Now().Add(-time.Hour)
	appendLog(t, server, ginLine(old, 200, "/api/chat"), old)
	if err := os.Chtimes(logs, old, old); err != nil {
		t.Fatal(err)
	}
	acct := core.AccountConfig{ID: "t", Provider: "ollama", RuntimeHints: map[string]string{"logs_dir": logs, "db_path": filepath.Join(dir, "none.db"), "server_config": filepath.Join(dir, "none.json")}}
	since := time.Now().Add(-time.Minute)
	p := New()
	if changed, _ := p.HasChanged(acct, since); changed {
		t.Fatal("unchanged logs reported as changed")
	}
	appendLog(t, server, ginLine(time.Now(), 200, "/api/chat"), time.Now())
	if changed, _ := p.HasChanged(acct, since); !changed {
		t.Fatal("append to server.log not detected")
	}
}
