package shared

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type readLine struct {
	no   int
	text string
}

func readAll(t *testing.T, r *IncrementalReader, path string) ([]readLine, ReadResult) {
	t.Helper()
	var got []readLine
	res, err := r.Read(path, nil, func(line []byte, no int) {
		got = append(got, readLine{no, string(line)})
	})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	return got, res
}

func appendFile(t *testing.T, path, s string) {
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
	// Make sure the mtime moves even on coarse-grained filesystems.
	bump(t, path)
}

var bumpClock = time.Now()

func bump(t *testing.T, path string) {
	t.Helper()
	bumpClock = bumpClock.Add(time.Second)
	if err := os.Chtimes(path, bumpClock, bumpClock); err != nil {
		t.Fatal(err)
	}
}

func TestIncrementalReader_AppendsAndPartialLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.jsonl")
	appendFile(t, path, "one\r\ntwo\n\nthr")
	var r IncrementalReader

	got, res := readAll(t, &r, path)
	want := []readLine{{1, "one"}, {2, "two"}, {3, ""}}
	if !reflect.DeepEqual(got, want) || !res.Reset || !res.Changed {
		t.Fatalf("first read = %v %+v", got, res)
	}
	if string(res.Tail) != "thr" || res.TailLine != 4 {
		t.Fatalf("tail = %q line %d", res.Tail, res.TailLine)
	}
	if r.Offset() != int64(len("one\r\ntwo\n\n")) {
		t.Fatalf("offset = %d", r.Offset())
	}

	// Unchanged file: nothing delivered.
	got, res = readAll(t, &r, path)
	if len(got) != 0 || res.Changed {
		t.Fatalf("unchanged read = %v %+v", got, res)
	}

	// The partial line completes; it is delivered whole with its number.
	appendFile(t, path, "ee\nfour\n")
	got, res = readAll(t, &r, path)
	want = []readLine{{4, "three"}, {5, "four"}}
	if !reflect.DeepEqual(got, want) || res.Reset || res.Tail != nil {
		t.Fatalf("append read = %v %+v", got, res)
	}
}

func TestIncrementalReader_TruncateReplaceRewrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.jsonl")
	appendFile(t, path, "aaaa\nbbbb\n")
	var r IncrementalReader
	readAll(t, &r, path)

	// Truncated and rewritten shorter.
	if err := os.WriteFile(path, []byte("cc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bump(t, path)
	got, res := readAll(t, &r, path)
	if !res.Reset || !reflect.DeepEqual(got, []readLine{{1, "cc"}}) {
		t.Fatalf("after truncate = %v %+v", got, res)
	}

	// Rewritten in place to a larger size: the guard bytes differ.
	if err := os.WriteFile(path, []byte("dd\nee\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bump(t, path)
	got, res = readAll(t, &r, path)
	if !res.Reset || !reflect.DeepEqual(got, []readLine{{1, "dd"}, {2, "ee"}}) {
		t.Fatalf("after rewrite = %v %+v", got, res)
	}

	// Replaced by a different file (rename over), same prefix and larger.
	tmp := filepath.Join(dir, "new.jsonl")
	if err := os.WriteFile(tmp, []byte("dd\nee\nff\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
	bump(t, path)
	got, res = readAll(t, &r, path)
	if !res.Reset || len(got) != 3 {
		t.Fatalf("after replace = %v %+v", got, res)
	}

	// Plain append after all that is incremental again.
	appendFile(t, path, "gg\n")
	got, res = readAll(t, &r, path)
	if res.Reset || !reflect.DeepEqual(got, []readLine{{4, "gg"}}) {
		t.Fatalf("after append = %v %+v", got, res)
	}
}

func TestIncrementalReader_LongLinesAreSkippedButCounted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.jsonl")
	long := strings.Repeat("x", 600*1024) // spans several bufio refills
	appendFile(t, path, "a\n"+long+"\nb\n"+long)
	r := IncrementalReader{MaxLineBytes: 512 * 1024}
	got, res := readAll(t, &r, path)
	if !reflect.DeepEqual(got, []readLine{{1, "a"}, {3, "b"}}) || res.Tail != nil {
		t.Fatalf("got %v tail %d bytes", got, len(res.Tail))
	}
	if want := int64(2 + len(long) + 1 + 2); r.Offset() != want {
		t.Fatalf("offset = %d, want %d", r.Offset(), want)
	}

	// A long line that fits the limit is delivered intact.
	r = IncrementalReader{}
	got, _ = readAll(t, &r, path)
	if len(got) != 3 || got[1].text != long {
		t.Fatalf("default limit: %d lines", len(got))
	}
}

// TestIncrementalReader_MatchesFullRead appends random-sized chunks (often
// splitting lines) and checks the union of incremental reads equals one read
// of the final file.
func TestIncrementalReader_MatchesFullRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.jsonl")
	var content strings.Builder
	for i := 0; i < 200; i++ {
		content.WriteString(strings.Repeat(string(rune('a'+i%26)), i%37))
		content.WriteString("\n")
	}
	all := content.String()
	var r IncrementalReader
	var inc []readLine
	for pos, step := 0, 1; pos < len(all); step = step*7%97 + 1 {
		end := min(pos+step, len(all))
		appendFile(t, path, all[pos:end])
		pos = end
		got, _ := readAll(t, &r, path)
		inc = append(inc, got...)
	}
	var fresh IncrementalReader
	full, _ := readAll(t, &fresh, path)
	if !reflect.DeepEqual(inc, full) {
		t.Fatalf("incremental (%d lines) != full (%d lines)", len(inc), len(full))
	}
}
