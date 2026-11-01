package shared

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setMtime(t *testing.T, path string, m time.Time) {
	t.Helper()
	if err := os.Chtimes(path, m, m); err != nil {
		t.Fatal(err)
	}
}

// TestAnyPathModifiedAfter_NestedAppend: appending to a file two levels
// below the watched directory moves no directory mtime but must count.
func TestAnyPathModifiedAfter_NestedAppend(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "project", "session")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	live := filepath.Join(nested, "live.jsonl")
	if err := os.WriteFile(live, []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	for _, p := range []string{live, nested, filepath.Dir(nested), root} {
		setMtime(t, p, old)
	}
	since := time.Now().Add(-time.Minute)

	if AnyPathModifiedAfter([]string{root}, since) {
		t.Fatal("untouched tree reported as modified")
	}

	// Append: only the file's mtime moves (directories keep theirs).
	appendFile(t, live, "b\n")
	setMtime(t, live, time.Now())
	if !AnyPathModifiedAfter([]string{root}, since) {
		t.Fatal("append to a nested file not detected")
	}

	// A new file in a nested directory changes that directory, which
	// triggers a re-walk that picks up the new file.
	since = time.Now().Add(time.Second)
	if AnyPathModifiedAfter([]string{root}, since) {
		t.Fatal("nothing changed after since")
	}
	newFile := filepath.Join(nested, "new.jsonl")
	if err := os.WriteFile(newFile, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	later := since.Add(time.Minute)
	setMtime(t, newFile, later)
	setMtime(t, nested, later)
	setMtime(t, filepath.Dir(nested), old)
	setMtime(t, root, old)
	if !AnyPathModifiedAfter([]string{root}, since) {
		t.Fatal("new nested file not detected")
	}
}
