package claude_code

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func appendRaw(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestReadConversationJSONL_IncrementalEdgeCases drives a warm provider
// through the cases an append-only reader has to get right and checks every
// poll against a cold provider that parses everything from scratch.
func TestReadConversationJSONL_IncrementalEdgeCases(t *testing.T) {
	primary, alt := writeGoldenHistory(t, t.TempDir())
	warm := newGoldenProvider()
	now := goldenNow
	live := filepath.Join(primary, "repo-a", "sess-a.jsonl")
	fetchConversationDump(t, warm, primary, alt)

	// One streamed assistant message: every line repeats message id and
	// request id, output grows, and each line carries the next content block.
	streamed := func(out int, tools ...string) fxLine {
		return fxLine{ts: now.Add(-50 * time.Second), sess: "sess-a", req: "s1", msg: "m-s1", cwd: "/work/repo-a",
			model: "claude-opus-4-6", in: 100, out: out, cr: 1000, cc: 10, tools: tools}
	}
	toolA := toolUse("t-s1a", "Edit", `{"file_path":"a.go","old_string":"a","new_string":"a\nb"}`)
	toolB := toolUse("t-s1b", "Write", `{"file_path":"b.py","content":"x\ny\nz"}`)
	next := fxLine{ts: now.Add(-40 * time.Second), sess: "sess-a", req: "s2", msg: "m-s2", cwd: "/work/repo-a",
		model: "claude-sonnet-4-5", in: 5, out: 6}

	steps := []struct {
		name string
		// wantExtend: the poll must take the incremental merge path.
		wantExtend bool
		mutate     func()
	}{
		{"first streamed line", true, func() { appendFixtureLines(t, live, streamed(3, toolA)) }},
		{"streamed line straddling the previous poll", true, func() { appendFixtureLines(t, live, streamed(40, toolB)) }},
		{"final streamed line repeats a block", true, func() { appendFixtureLines(t, live, streamed(55, toolB)) }},
		{"partial line", true, func() { appendRaw(t, live, next.json()[:30]) }},
		{"partial line completed", true, func() { appendRaw(t, live, next.json()[30:]+"\n") }},
		{"final line without newline", false, func() {
			l := next
			l.req, l.msg, l.ts = "s3", "m-s3", now.Add(-30*time.Second)
			appendRaw(t, live, l.json())
		}},
		{"newline after it", false, func() { appendRaw(t, live, "\n") }},
		{"out-of-order append", false, func() {
			l := next
			l.req, l.msg, l.ts = "s4", "m-s4", now.Add(-48*time.Hour)
			appendFixtureLines(t, live, l)
		}},
		{"truncate and rewrite", false, func() {
			writeFixtureFile(t, live, []fxLine{streamed(7, toolA)})
		}},
		{"replace by rename", false, func() {
			tmp := live + ".tmp"
			writeFixtureFile(t, tmp, []fxLine{streamed(7, toolA), next})
			if err := os.Rename(tmp, live); err != nil {
				t.Fatal(err)
			}
		}},
		{"append after replace", true, func() {
			l := next
			l.req, l.msg, l.ts = "s5", "m-s5", now.Add(-10*time.Second)
			appendFixtureLines(t, live, l)
		}},
	}
	for _, step := range steps {
		before := warm.convAgg.extended
		step.mutate()
		got := fetchConversationDump(t, warm, primary, alt)
		want := fetchConversationDump(t, newGoldenProvider(), primary, alt)
		if got != want {
			t.Fatalf("step %q: incremental diverged from full parse\n%s", step.name, lineDiff(want, got))
		}
		if step.wantExtend && warm.convAgg.extended != before+1 {
			t.Errorf("step %q: expected the incremental merge path", step.name)
		}
	}
}

// TestConversationFile_MatchesFullParse checks the per-file records the
// incremental reader keeps against parseConversationRecords after an append
// that splits a streamed message.
func TestConversationFile_MatchesFullParse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	base := fxLine{ts: goldenNow, sess: "s", req: "r1", msg: "m1", model: "claude-opus-4-6", in: 1, out: 1,
		tools: []string{toolUse("t1", "Edit", `{"file_path":"a.go"}`)}}
	writeFixtureFile(t, path, []fxLine{base})
	var f conversationFile
	f.update(path, nil)

	more := base
	more.out = 9
	more.tools = []string{toolUse("t2", "Bash", `{"command":"ls"}`)}
	earlier := fxLine{ts: goldenNow.Add(-time.Second), sess: "s", req: "r0", msg: "m0", model: "claude-opus-4-6", in: 2, out: 2}
	appendFixtureLines(t, path, more, earlier)
	f.update(path, nil)

	want := parseConversationRecords(path)
	sortRecords(want)
	got := f.entry.records
	if len(got) != len(want) {
		t.Fatalf("got %d records, want %d", len(got), len(want))
	}
	for i := range want {
		g, w := got[i], want[i]
		if !g.timestamp.Equal(w.timestamp) || g.lineNumber != w.lineNumber || *g.usage != *w.usage || len(g.content) != len(w.content) {
			t.Fatalf("record %d: got %+v / %+v, want %+v / %+v", i, g, *g.usage, w, *w.usage)
		}
	}
}

func sortRecords(rs []conversationRecord) {
	for i := 1; i < len(rs); i++ {
		for j := i; j > 0 && recordLess(&rs[j], &rs[j-1]); j-- {
			rs[j], rs[j-1] = rs[j-1], rs[j]
		}
	}
}
