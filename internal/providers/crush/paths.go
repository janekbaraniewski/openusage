package crush

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// PathHintRootsKey is the AccountConfig path hint used to override the
// list of project roots to walk for `.crush/crush.db` files. Values are
// colon-separated (matching $PATH-style env vars).
const PathHintRootsKey = "search_roots"

// PathHintDBsKey is the AccountConfig path hint used to inject an
// already-resolved list of DB paths. Detectors set this so the provider
// doesn't have to repeat the walk at Fetch time. Values are colon
// separated absolute paths.
const PathHintDBsKey = "db_paths"

// PathHintSingleDBKey is a per-account override for a single Crush DB
// path. Useful when a user wants to point openusage at one specific
// project DB and skip the auto-walk.
const PathHintSingleDBKey = "db_path"

// EnvSearchRoots is the env var users may set to override the default
// list of search roots without editing settings.json. Colon-separated.
const EnvSearchRoots = "OPENUSAGE_CRUSH_ROOTS"

// defaultMaxDepth caps how deep we descend under each search root looking
// for `.crush/crush.db`. The vast majority of project layouts have the
// DB within 3-4 levels of $HOME (e.g. `~/code/<org>/<repo>/.crush/crush.db`).
const defaultMaxDepth = 4

// projectDBName is the basename of the per-project Crush SQLite store.
// See upstream `internal/db/connect.go` (the binary calls
// `filepath.Join(dataDir, "crush.db")`) and `internal/config/config.go`
// where `defaultDataDirectory = ".crush"`.
const projectDBName = "crush.db"

// projectDataDirName is the per-project Crush data-directory name. Same
// upstream reference as projectDBName.
const projectDataDirName = ".crush"

// defaultSearchRoots returns the candidate directories we walk when no
// per-account override is set. We bias towards "directories developers
// commonly check out project trees into". Missing directories are
// silently skipped at walk time.
//
// $HOME and $HOME/Documents are intentionally excluded. Walking either
// crosses into macOS-protected locations (~/Pictures/Photos Library,
// and ~/Documents/~/Desktop under iCloud Drive sync) which triggers
// TCC permission prompts on every launch. Users who keep crush
// projects in $HOME or ~/Documents should set OPENUSAGE_CRUSH_ROOTS or
// the per-account `search_roots` override.
func defaultSearchRoots() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	return []string{
		filepath.Join(home, "code"),
		filepath.Join(home, "src"),
		filepath.Join(home, "workspace"),
		filepath.Join(home, "dev"),
		filepath.Join(home, "Projects"),
		filepath.Join(home, "projects"),
		filepath.Join(home, "Workspace"),
	}
}

// resolveSearchRoots returns the effective list of project-tree roots to
// scan, in priority order: explicit per-account hint, env override,
// then defaults. Results are deduplicated and stripped of empties.
func resolveSearchRoots(acct core.AccountConfig) []string {
	if override := acct.Path(PathHintRootsKey, ""); override != "" {
		return splitPathList(override)
	}
	if env := strings.TrimSpace(os.Getenv(EnvSearchRoots)); env != "" {
		return splitPathList(env)
	}
	return defaultSearchRoots()
}

// splitPathList splits a colon/path-list separator string into a
// deduplicated, trimmed slice. We accept ':' on Unix and ';' on Windows
// — Go's `os.PathListSeparator` switches accordingly so we use it here.
func splitPathList(value string) []string {
	parts := strings.Split(value, string(os.PathListSeparator))
	seen := make(map[string]struct{}, len(parts))
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

// resolveDBPaths returns the list of discovered Crush DBs for the
// account. Order of precedence:
//
//  1. An explicit list pre-resolved by the detector via PathHintDBsKey.
//  2. A single explicit override via PathHintSingleDBKey.
//  3. Walking each search root with discoverDBs.
//
// The result is deduplicated by absolute path. Non-existent paths from
// (1) and (2) are filtered out so a stale settings.json doesn't blow up
// the dashboard.
func resolveDBPaths(acct core.AccountConfig) []string {
	if list := acct.Path(PathHintDBsKey, ""); list != "" {
		paths := splitPathList(list)
		return filterExistingFiles(paths)
	}
	if single := strings.TrimSpace(acct.Path(PathHintSingleDBKey, "")); single != "" {
		if fileExists(single) {
			return []string{single}
		}
		return nil
	}
	roots := resolveSearchRoots(acct)
	return discoverDBs(roots, defaultMaxDepth)
}

// DiscoverDBPaths walks the default search roots (or the override in
// $OPENUSAGE_CRUSH_ROOTS) and returns every `.crush/crush.db` it finds.
// Exported so the detect package can seed `db_paths` on the auto-detected
// account without duplicating the walker.
func DiscoverDBPaths() []string {
	if env := strings.TrimSpace(os.Getenv(EnvSearchRoots)); env != "" {
		return discoverDBs(splitPathList(env), defaultMaxDepth)
	}
	return discoverDBs(defaultSearchRoots(), defaultMaxDepth)
}

// discoverDBs walks each root with WalkDir bounded to maxDepth, looking
// for files at `<dir>/.crush/crush.db`. Directories we know never hold
// project trees (node_modules, .git, etc.) are skipped to keep walks
// cheap on $HOME-rooted scans.
//
// The walk is best-effort: a permission-denied or vanishing directory
// is silently skipped (fs.SkipDir) rather than aborting the whole scan.
func discoverDBs(roots []string, maxDepth int) []string {
	seen := make(map[string]struct{})
	var out []string

	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			continue
		}
		rootDepth := strings.Count(filepath.Clean(root), string(filepath.Separator))

		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				// Permission or vanished — skip this subtree but keep walking.
				if d != nil && d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if d == nil || !d.IsDir() {
				return nil
			}

			depth := strings.Count(filepath.Clean(path), string(filepath.Separator)) - rootDepth
			if depth > maxDepth {
				return fs.SkipDir
			}

			name := d.Name()
			if depth > 0 && isSkippableDirName(name) {
				return fs.SkipDir
			}

			if name == projectDataDirName {
				candidate := filepath.Join(path, projectDBName)
				if fileExists(candidate) {
					abs, err := filepath.Abs(candidate)
					if err != nil {
						abs = candidate
					}
					if _, dup := seen[abs]; !dup {
						seen[abs] = struct{}{}
						out = append(out, abs)
					}
				}
				// `.crush` is always a leaf for our purposes; do not
				// descend into it.
				return fs.SkipDir
			}
			return nil
		})
	}
	return out
}

// isSkippableDirName returns true for directory basenames we never want
// to descend into when scanning for Crush DBs.
//
// The macOS user-data directories (Library, Pictures, Movies, Music,
// Desktop, Public, Applications, .Trash) are listed because descending
// into them is what triggers TCC permission prompts (Photo Library,
// iCloud Drive). They are guaranteed never to hold a project clone, so
// skipping them is purely a safety net for users who override
// `search_roots` with $HOME.
//
// .photoslibrary directories are skipped because they are Photos.app
// bundle directories; descending into them on macOS pops a Photo
// Library access prompt regardless of where they live.
func isSkippableDirName(name string) bool {
	switch name {
	case ".git",
		"node_modules",
		".venv",
		"venv",
		"__pycache__",
		".cache",
		"vendor",
		".direnv",
		".terraform",
		"target",
		"build",
		"dist",
		".idea",
		".vscode",
		"Library",
		"Pictures",
		"Movies",
		"Music",
		"Desktop",
		"Public",
		"Applications",
		".Trash":
		return true
	}
	if strings.HasSuffix(name, ".photoslibrary") {
		return true
	}
	return false
}

func filterExistingFiles(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if fileExists(p) {
			out = append(out, p)
		}
	}
	return out
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
