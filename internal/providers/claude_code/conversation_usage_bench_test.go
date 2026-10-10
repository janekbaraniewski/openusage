package claude_code

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeBenchHistory writes files x perFile assistant records spread over the
// last ~60 days, each with one tool call, mimicking a long-lived install.
// Returns the projects dir and the path of the most recent ("live") file.
func writeBenchHistory(b *testing.B, root string, files, perFile int) (string, string) {
	b.Helper()
	projects := filepath.Join(root, "projects")
	start := goldenNow.Add(-60 * 24 * time.Hour)
	step := 60 * 24 * time.Hour / time.Duration(files*perFile)
	models := []string{"claude-opus-4-6", "claude-sonnet-4-5", "claude-haiku-4-5"}
	var last string
	for f := 0; f < files; f++ {
		lines := make([]fxLine, 0, perFile)
		for i := 0; i < perFile; i++ {
			n := f*perFile + i
			lines = append(lines, fxLine{
				ts:    start.Add(time.Duration(n) * step),
				sess:  fmt.Sprintf("s%d", f),
				req:   fmt.Sprintf("r%d-%d", f, i),
				msg:   fmt.Sprintf("m%d-%d", f, i),
				cwd:   fmt.Sprintf("/work/repo-%d", f%7),
				model: models[n%3],
				in:    100 + n%50, out: 20 + n%7, cr: 1000, cc: 50,
				tools: []string{toolUse(fmt.Sprintf("t%d-%d", f, i), "Edit",
					fmt.Sprintf(`{"file_path":"pkg/f%d.go","old_string":"a","new_string":"a\nb"}`, n%40))},
			})
		}
		last = filepath.Join(projects, fmt.Sprintf("repo-%d", f%7), fmt.Sprintf("s%d.jsonl", f))
		writeFixtureFile(b, last, lines)
	}
	return projects, last
}

// BenchmarkReadConversationJSONL measures one poll over a 100k-record
// history once the per-file parse cache is warm.
//
//   - unchanged: no file changed since the previous poll.
//   - live_append: one session file grew by a record, the common case while
//     a Claude Code session is active.
func BenchmarkReadConversationJSONL(b *testing.B) {
	for _, mode := range []string{"unchanged", "live_append"} {
		b.Run(mode, func(b *testing.B) {
			projects, live := writeBenchHistory(b, b.TempDir(), 200, 500)
			p := newGoldenProvider()
			warm := newConversationSnapshot(p)
			if err := p.readConversationJSONL(projects, "", &warm); err != nil {
				b.Fatal(err)
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if mode == "live_append" {
					b.StopTimer()
					f, err := os.OpenFile(live, os.O_APPEND|os.O_WRONLY, 0)
					if err != nil {
						b.Fatal(err)
					}
					line := fxLine{ts: goldenNow.Add(-time.Minute), sess: "live", req: fmt.Sprintf("live-%d", i),
						msg: fmt.Sprintf("live-%d", i), cwd: "/work/live", model: "claude-opus-4-6", in: 10, out: 1}
					_, _ = f.WriteString(line.json() + "\n")
					_ = f.Close()
					b.StartTimer()
				}
				snap := newConversationSnapshot(p)
				if err := p.readConversationJSONL(projects, "", &snap); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
