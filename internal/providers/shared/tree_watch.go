package shared

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// A directory's mtime only moves when an entry is created, removed or
// renamed in it, not when a file inside (or deeper) is appended to. Session
// logs grow by appends, so checking the directory alone misses new usage.
//
// treeWatch tracks the newest mtime under a directory without walking the
// tree on every check. A full walk records every subdirectory's mtime and the
// most recently modified files ("hot" files). Later checks stat only those:
// a changed subdirectory means files were added or removed and triggers a new
// walk; a hot file's mtime catches appends to live sessions. A file that was
// not hot at the last walk is caught by the periodic re-walk.
const (
	treeHotFiles      = 64
	treeRewalkEvery   = 10 * time.Minute
	treeMaxWalkNodes  = 50000
	treeWatchMaxRoots = 64
)

type treeState struct {
	dirs map[string]time.Time
	hot  map[string]time.Time
	// coldNewest is the newest mtime among files that are not hot, as of
	// the last walk; newest adds the hot files' current mtimes. Only file
	// mtimes count: directories are watched to notice added or removed
	// files, and the root's own mtime is checked by the caller.
	coldNewest time.Time
	newest     time.Time
	walkedAt   time.Time
	tooBig     bool // tree exceeded treeMaxWalkNodes; fall back to the root's mtime
}

var treeWatch = struct {
	mu    sync.Mutex
	roots map[string]*treeState
	now   func() time.Time
}{roots: make(map[string]*treeState), now: time.Now}

// treeModifiedAfter reports whether anything under dir (the directory, its
// subdirectories, or files in them) was modified after since.
func treeModifiedAfter(dir string, since time.Time) bool {
	treeWatch.mu.Lock()
	defer treeWatch.mu.Unlock()
	st := treeWatch.roots[dir]
	if st == nil || treeWatch.now().Sub(st.walkedAt) > treeRewalkEvery || !st.refresh() {
		st = walkTree(dir)
		if len(treeWatch.roots) >= treeWatchMaxRoots {
			clear(treeWatch.roots)
		}
		treeWatch.roots[dir] = st
	}
	return st.newest.After(since)
}

// refresh re-stats the recorded directories and hot files. It returns false
// when the directory structure changed and the tree must be walked again.
func (st *treeState) refresh() bool {
	for path, mtime := range st.dirs {
		info, err := os.Stat(path)
		if err != nil || !info.ModTime().Equal(mtime) {
			return false
		}
	}
	if st.tooBig {
		return true
	}
	newest := st.coldNewest
	for path := range st.hot {
		info, err := os.Stat(path)
		if err != nil {
			return false
		}
		m := info.ModTime()
		st.hot[path] = m
		if m.After(newest) {
			newest = m
		}
	}
	st.newest = newest
	return true
}

func walkTree(root string) *treeState {
	st := &treeState{dirs: make(map[string]time.Time), hot: make(map[string]time.Time), walkedAt: treeWatch.now()}
	type file struct {
		path  string
		mtime time.Time
	}
	var files []file
	nodes := 0
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		nodes++
		if nodes > treeMaxWalkNodes {
			st.tooBig = true
			return filepath.SkipAll
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if d.IsDir() {
			st.dirs[path] = info.ModTime()
		} else {
			files = append(files, file{path, info.ModTime()})
		}
		return nil
	})
	if st.tooBig {
		// Too large to track: watch only the root, as before.
		st.dirs = make(map[string]time.Time)
		if info, err := os.Stat(root); err == nil {
			st.dirs[root] = info.ModTime()
		}
		return st
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mtime.After(files[j].mtime) })
	for i, f := range files {
		if i < treeHotFiles {
			st.hot[f.path] = f.mtime
			if f.mtime.After(st.newest) {
				st.newest = f.mtime
			}
		} else if f.mtime.After(st.coldNewest) {
			st.coldNewest = f.mtime
		}
	}
	if st.coldNewest.After(st.newest) {
		st.newest = st.coldNewest
	}
	return st
}
