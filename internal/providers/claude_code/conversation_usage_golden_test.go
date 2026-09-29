package claude_code

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

var updateGolden = flag.Bool("update-golden", false, "rewrite testdata/conversation_usage.golden")

// goldenNow pins the clock so the today / 7d / 5h-block windows are stable.
var goldenNow = time.Date(2026, 3, 15, 14, 20, 0, 0, time.UTC)

// fxLine describes one assistant JSONL entry in the golden fixture.
type fxLine struct {
	ts                  time.Time
	sess, req, msg, cwd string
	model               string
	in, out, cr, cc     int
	reasoning           int
	c5m, c1h            int
	webSearch, webFetch int
	tier, geo           string
	tools               []string // raw JSON content items
	noUsage             bool
}

func (l fxLine) json() string {
	var b strings.Builder
	b.WriteString(`{"type":"assistant"`)
	fmt.Fprintf(&b, `,"sessionId":%q`, l.sess)
	if l.req != "" {
		fmt.Fprintf(&b, `,"requestId":%q`, l.req)
	}
	fmt.Fprintf(&b, `,"timestamp":%q`, l.ts.UTC().Format(time.RFC3339Nano))
	if l.cwd != "" {
		fmt.Fprintf(&b, `,"cwd":%q`, l.cwd)
	}
	b.WriteString(`,"message":{`)
	if l.msg != "" {
		fmt.Fprintf(&b, `"id":%q,`, l.msg)
	}
	fmt.Fprintf(&b, `"model":%q,"role":"assistant","content":[%s]`, l.model, strings.Join(l.tools, ","))
	if !l.noUsage {
		fmt.Fprintf(&b, `,"usage":{"input_tokens":%d,"output_tokens":%d,"cache_read_input_tokens":%d,"cache_creation_input_tokens":%d,"reasoning_tokens":%d`,
			l.in, l.out, l.cr, l.cc, l.reasoning)
		if l.tier != "" {
			fmt.Fprintf(&b, `,"service_tier":%q`, l.tier)
		}
		if l.geo != "" {
			fmt.Fprintf(&b, `,"inference_geo":%q`, l.geo)
		}
		if l.c5m > 0 || l.c1h > 0 {
			fmt.Fprintf(&b, `,"cache_creation":{"ephemeral_5m_input_tokens":%d,"ephemeral_1h_input_tokens":%d}`, l.c5m, l.c1h)
		}
		if l.webSearch > 0 || l.webFetch > 0 {
			fmt.Fprintf(&b, `,"server_tool_use":{"web_search_requests":%d,"web_fetch_requests":%d}`, l.webSearch, l.webFetch)
		}
		b.WriteString(`}`)
	}
	b.WriteString(`}}`)
	return b.String()
}

func toolUse(id, name, input string) string {
	if input == "" {
		input = "{}"
	}
	return fmt.Sprintf(`{"type":"tool_use","id":%q,"name":%q,"input":%s}`, id, name, input)
}

func writeFixtureFile(t testing.TB, path string, lines []fxLine) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l.json())
		b.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// writeGoldenHistory lays out a multi-file history exercising within-file
// streaming merges, within-file and cross-file (incl. cross-directory)
// duplicates, keyless records, tool-only records, day/week/block boundaries
// and a subagent transcript. Returns (projectsDir, altProjectsDir).
func writeGoldenHistory(t testing.TB, root string) (string, string) {
	t.Helper()
	now := goldenNow
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	primary := filepath.Join(root, "projects")
	alt := filepath.Join(root, "alt-projects")

	const cwdA = "/work/repo-a"
	a1 := fxLine{ts: now.Add(-10*24*time.Hour - 5*time.Hour), sess: "sess-a", req: "a1", msg: "m-a1", cwd: cwdA, model: "claude-opus-4-6",
		in: 1000, out: 200, cc: 500, c5m: 300, c1h: 200,
		tools: []string{toolUse("t-a1", "Edit", `{"file_path":"cmd/main.go","old_string":"x","new_string":"x\ny"}`)}}
	a2 := fxLine{ts: now.Add(-10*24*time.Hour - 4*time.Hour), sess: "sess-a", req: "a2", msg: "m-a2", cwd: cwdA, model: "claude-sonnet-4-5",
		in: 300, out: 50, cr: 2000, tier: "standard", geo: "us",
		tools: []string{toolUse("t-a2", "Bash", `{"command":"git commit -m \"one\""}`)}}
	// Streaming partial of a2: same message+request, later and larger output.
	a2s := a2
	a2s.ts = a2.ts.Add(time.Second)
	a2s.out = 60
	a2s.tools = nil
	a3 := fxLine{ts: now.Add(-7 * 24 * time.Hour), sess: "sess-a", req: "a3", msg: "m-a3", cwd: cwdA, model: "claude-haiku-4-5", in: 100, out: 10}
	a4 := fxLine{ts: now.Add(-7*24*time.Hour - time.Second), sess: "sess-a", req: "a4", msg: "m-a4", cwd: cwdA, model: "claude-haiku-4-5", in: 100, out: 10}
	a5 := fxLine{ts: today.Add(-time.Second), sess: "sess-a", req: "a5", msg: "m-a5", cwd: cwdA, model: "claude-sonnet-4-5",
		in: 400, out: 40, webSearch: 2, webFetch: 1,
		tools: []string{toolUse("t-a5", "Write", `{"path":"docs/notes.md","content":"alpha\nbeta"}`)}}
	a6 := fxLine{ts: today, sess: "sess-a", req: "a6", msg: "m-a6", cwd: cwdA, model: "claude-sonnet-4-5", in: 500, out: 50, cc: 100, c1h: 100}
	a7 := fxLine{ts: today.Add(70 * time.Minute), sess: "sess-a", req: "a7", msg: "m-a7", cwd: cwdA, model: "claude-sonnet-4-5", in: 50, out: 5}
	// a5 opens a 23:00-04:00 block; exactly at its end stays in that block.
	a8 := fxLine{ts: today.Add(4 * time.Hour), sess: "sess-a", req: "a8", msg: "m-a8", cwd: cwdA, model: "claude-sonnet-4-5", in: 60, out: 6}
	// After the block end: opens the 06:00 block.
	a9 := fxLine{ts: today.Add(6*time.Hour + 20*time.Minute), sess: "sess-a", req: "a9", msg: "m-a9", cwd: cwdA, model: "claude-sonnet-4-5", in: 70, out: 7}
	// now-3h opens the current 11:00-16:00 block.
	a10 := fxLine{ts: now.Add(-3 * time.Hour), sess: "sess-a", req: "a10", msg: "m-a10", cwd: cwdA, model: "claude-opus-4-6",
		in: 1200, out: 300, cr: 8000,
		tools: []string{
			toolUse("t-a10-read", "Read", `{"file_path":"internal/app.py"}`),
			toolUse("t-a10-c1", "Bash", `{"command":"git commit -m \"one\""}`),
			toolUse("t-a10-c2", "Bash", `{"command":"git commit -m \"two\""}`),
		}}
	// Same request, different message id, later: usage and tool deduped.
	a10dup := a10
	a10dup.msg = "m-a10-b"
	a10dup.ts = now.Add(-170 * time.Minute)
	a10dup.tools = []string{toolUse("t-a10-read", "Read", `{"file_path":"internal/app.py"}`)}
	aKeyless := fxLine{ts: now.Add(-2 * time.Hour), sess: "sess-a", cwd: cwdA, model: "claude-sonnet-4-5", in: 70, out: 7}
	aToolOnly := fxLine{ts: now.Add(-90 * time.Minute), sess: "sess-a", req: "a-tool", msg: "m-a-tool", cwd: cwdA, model: "claude-sonnet-4-5",
		noUsage: true, tools: []string{toolUse("t-grep", "Grep", `{"pattern":"TODO"}`)}}
	a11 := fxLine{ts: now.Add(-10 * time.Minute), sess: "sess-a", req: "a11", msg: "m-a11", cwd: cwdA, model: "claude-opus-4-6",
		in: 800, out: 80, cr: 5000, reasoning: 30,
		tools: []string{toolUse("t-a11", "Read", `{"file_path":"README.md"}`)}}
	// Deliberately not in timestamp order on disk.
	writeFixtureFile(t, filepath.Join(primary, "repo-a", "sess-a.jsonl"), []fxLine{
		a1, a2, a2s, a3, a4, a5, a6, a7, a8, a9, a11, a10, a10dup, aKeyless, aToolOnly,
	})

	// Resumed session: identical copies of a1/a2/keyless (same timestamps),
	// an earlier copy of a11 with different tokens (must win), and new work.
	b1 := a11
	b1.sess = "sess-b"
	b1.ts = now.Add(-20 * time.Minute)
	b1.in, b1.out, b1.cr = 900, 90, 0
	b2 := fxLine{ts: now.Add(-5 * time.Minute), sess: "sess-b", req: "b2", msg: "m-b2", cwd: cwdA, model: "claude-sonnet-4-5",
		in: 200, out: 20, tools: []string{toolUse("t-b2", "Edit", `{"file_path":"cmd/main.go","old_string":"a","new_string":"b"}`)}}
	writeFixtureFile(t, filepath.Join(primary, "repo-a", "sess-b.jsonl"), []fxLine{a1, a2, aKeyless, b1, b2})

	// No cwd: project label falls back to the directory name. Carries a later
	// copy of a4 with different tokens (a4 must win, keeping it outside 7d).
	c1 := fxLine{ts: now.Add(-48 * time.Hour), sess: "sess-c", req: "c1", msg: "m-c1", model: "", in: 10, out: 1}
	c2 := fxLine{ts: now.Add(-30 * time.Hour), sess: "sess-c", req: "c2", msg: "m-c2", model: "claude-sonnet-4-5",
		in: 20, out: 2, tier: "priority", geo: "eu"}
	c4 := a4
	c4.sess = "sess-c"
	c4.cwd = ""
	c4.ts = now.Add(-6 * 24 * time.Hour)
	c4.in, c4.out = 9999, 999
	writeFixtureFile(t, filepath.Join(primary, "repo-b", "sess-c.jsonl"), []fxLine{c1, c2, c4})

	// Subagent transcript with a meta sidecar.
	subDir := filepath.Join(primary, "repo-b", "sess-c", "subagents")
	d1 := fxLine{ts: now.Add(-time.Hour), sess: "sess-c", req: "d1", msg: "m-d1", cwd: "/work/repo-b", model: "claude-haiku-4-5", in: 30, out: 3}
	d2 := fxLine{ts: now.Add(-40 * time.Minute), sess: "sess-c", req: "d2", msg: "m-d2", cwd: "/work/repo-b", model: "claude-haiku-4-5", in: 40, out: 4,
		tools: []string{toolUse("t-d2", "Glob", `{"pattern":"**/*.ts"}`)}}
	writeFixtureFile(t, filepath.Join(subDir, "agent-x1.jsonl"), []fxLine{d1, d2})
	if err := os.WriteFile(filepath.Join(subDir, "agent-x1.meta.json"), []byte(`{"agentType":"code-reviewer"}`), 0o644); err != nil {
		t.Fatalf("write meta: %v", err)
	}

	// Alternate projects dir: identical copy of b2 plus unique work.
	e1 := fxLine{ts: now.Add(-15 * time.Minute), sess: "sess-e", req: "e1", msg: "m-e1", cwd: "/work/repo-e", model: "claude-opus-4-6", in: 150, out: 15}
	e2 := fxLine{ts: now.Add(-4 * 24 * time.Hour), sess: "sess-e", req: "e2", msg: "m-e2", cwd: "/work/repo-e", model: "claude-opus-4-6", in: 250, out: 25}
	writeFixtureFile(t, filepath.Join(alt, "repo-a", "sess-e.jsonl"), []fxLine{b2, e1, e2})

	return primary, alt
}

func newConversationSnapshot(p *Provider) core.UsageSnapshot {
	return core.UsageSnapshot{
		ProviderID:  p.ID(),
		AccountID:   "golden",
		Timestamp:   goldenNow,
		Status:      core.StatusOK,
		Metrics:     make(map[string]core.Metric),
		Raw:         make(map[string]string),
		Resets:      make(map[string]time.Time),
		DailySeries: make(map[string][]core.TimePoint),
	}
}

func fetchConversationDump(t testing.TB, p *Provider, primary, alt string) string {
	t.Helper()
	snap := newConversationSnapshot(p)
	if err := p.readConversationJSONL(primary, alt, &snap); err != nil {
		t.Fatalf("readConversationJSONL: %v", err)
	}
	return dumpConversationSnapshot(snap)
}

func fmtGoldenFloat(v *float64) string {
	if v == nil {
		return "-"
	}
	// 8 decimals: stable against float summation-order noise, far below
	// anything the UI renders.
	return strconv.FormatFloat(*v, 'f', 8, 64)
}

func dumpConversationSnapshot(snap core.UsageSnapshot) string {
	var b strings.Builder
	for _, k := range core.SortedStringKeys(snap.Metrics) {
		m := snap.Metrics[k]
		fmt.Fprintf(&b, "metric %s used=%s limit=%s remaining=%s unit=%q window=%q\n",
			k, fmtGoldenFloat(m.Used), fmtGoldenFloat(m.Limit), fmtGoldenFloat(m.Remaining), m.Unit, m.Window)
	}
	for _, k := range core.SortedStringKeys(snap.Raw) {
		fmt.Fprintf(&b, "raw %s=%q\n", k, snap.Raw[k])
	}
	for _, k := range core.SortedStringKeys(snap.Resets) {
		fmt.Fprintf(&b, "reset %s=%s\n", k, snap.Resets[k].UTC().Format(time.RFC3339))
	}
	series := make([]string, 0, len(snap.DailySeries))
	for k := range snap.DailySeries {
		series = append(series, k)
	}
	sort.Strings(series)
	for _, k := range series {
		for _, pt := range snap.DailySeries[k] {
			v := pt.Value
			fmt.Fprintf(&b, "series %s %s=%s\n", k, pt.Date, fmtGoldenFloat(&v))
		}
	}
	return b.String()
}

func newGoldenProvider() *Provider {
	p := New()
	p.nowFn = func() time.Time { return goldenNow }
	return p
}

// TestReadConversationJSONL_Golden pins every metric/raw/series value the
// JSONL aggregator produces over a multi-file fixture history. Regenerate
// with: go test ./internal/providers/claude_code/ -run Golden -update-golden
func TestReadConversationJSONL_Golden(t *testing.T) {
	primary, alt := writeGoldenHistory(t, t.TempDir())
	got := fetchConversationDump(t, newGoldenProvider(), primary, alt)

	goldenPath := filepath.Join("testdata", "conversation_usage.golden")
	if *updateGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run with -update-golden to create): %v", err)
	}
	if got != string(want) {
		t.Fatalf("golden mismatch\n%s", lineDiff(string(want), got))
	}
}

// TestReadConversationJSONL_GoldenStableAcrossFetches: a warm-cache Fetch
// must produce byte-identical output to the cold one.
func TestReadConversationJSONL_GoldenStableAcrossFetches(t *testing.T) {
	primary, alt := writeGoldenHistory(t, t.TempDir())
	p := newGoldenProvider()
	first := fetchConversationDump(t, p, primary, alt)
	for i := 0; i < 3; i++ {
		if again := fetchConversationDump(t, p, primary, alt); again != first {
			t.Fatalf("fetch %d differs from first\n%s", i+2, lineDiff(first, again))
		}
	}
}

func lineDiff(want, got string) string {
	wl := strings.Split(want, "\n")
	gl := strings.Split(got, "\n")
	wset := make(map[string]bool, len(wl))
	for _, l := range wl {
		wset[l] = true
	}
	gset := make(map[string]bool, len(gl))
	for _, l := range gl {
		gset[l] = true
	}
	var b strings.Builder
	for _, l := range wl {
		if !gset[l] {
			b.WriteString("- " + l + "\n")
		}
	}
	for _, l := range gl {
		if !wset[l] {
			b.WriteString("+ " + l + "\n")
		}
	}
	return b.String()
}
