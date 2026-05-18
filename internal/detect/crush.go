package detect

import (
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// detectCrush registers a local Crush account when either the CLI
// binary is on PATH or at least one project-level `.crush/crush.db`
// exists in one of the default search roots.
//
// Crush stores usage data per-project, not in a global data dir
// (see github.com/charmbracelet/crush internal/config/config.go,
// `defaultDataDirectory = ".crush"`), so we mirror the provider's
// walker here to seed `db_paths` on the auto-detected account. That
// lets the provider skip the walk on subsequent ticks.
func detectCrush(result *Result) {
	bin := findBinary("crush")
	dbPaths := discoverCrushDBs()

	if bin == "" && len(dbPaths) == 0 {
		return
	}

	if bin != "" {
		log.Printf("[detect] Found Crush at %s", bin)
		result.Tools = append(result.Tools, DetectedTool{
			Name:       "Crush",
			BinaryPath: bin,
			ConfigDir:  defaultCrushConfigDir(),
			Type:       "cli",
		})
	}

	acct := core.AccountConfig{
		ID:           "crush",
		Provider:     "crush",
		Auth:         "local",
		Binary:       bin,
		RuntimeHints: make(map[string]string),
	}
	if len(dbPaths) > 0 {
		joined := strings.Join(dbPaths, string(os.PathListSeparator))
		acct.SetPath("db_paths", joined)
		acct.SetHint("db_paths", joined)
		log.Printf("[detect] Crush: discovered %d project DB(s)", len(dbPaths))
	}
	if dir := defaultCrushConfigDir(); dir != "" {
		acct.SetHint("config_dir", dir)
	}

	addAccount(result, acct)
}

// discoverCrushDBs walks the default search roots looking for
// `.crush/crush.db` files. Mirrors the provider's walker but lives in
// the detect package so we don't import providers from upstream
// detection code (layering rule).
func discoverCrushDBs() []string {
	home := homeDir()
	if home == "" {
		return nil
	}

	if env := strings.TrimSpace(os.Getenv("OPENUSAGE_CRUSH_ROOTS")); env != "" {
		return walkCrushRoots(splitPathListString(env), 4)
	}

	roots := []string{
		home,
		filepath.Join(home, "code"),
		filepath.Join(home, "src"),
		filepath.Join(home, "workspace"),
		filepath.Join(home, "dev"),
		filepath.Join(home, "Projects"),
		filepath.Join(home, "projects"),
		filepath.Join(home, "Workspace"),
		filepath.Join(home, "Documents"),
	}
	return walkCrushRoots(roots, 4)
}

// walkCrushRoots is the WalkDir implementation shared between
// discoverCrushDBs and any future test entry-point.
func walkCrushRoots(roots []string, maxDepth int) []string {
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
			if depth > 0 && isCrushSkippableDirName(name) {
				return fs.SkipDir
			}
			if name == ".crush" {
				candidate := filepath.Join(path, "crush.db")
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
				return fs.SkipDir
			}
			return nil
		})
	}
	return out
}

// isCrushSkippableDirName lists directory basenames we never descend
// into when scanning for project DBs. Identical policy to the provider
// package's `isSkippableDirName`; duplicated to avoid importing the
// provider from the detect layer.
func isCrushSkippableDirName(name string) bool {
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
		".vscode":
		return true
	}
	return false
}

func splitPathListString(value string) []string {
	parts := strings.Split(value, string(os.PathListSeparator))
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
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

// defaultCrushConfigDir returns the global Crush config dir for
// surfacing in the detected-tool record. Note: this directory holds
// only OAuth tokens and recent-model preferences (`crush.json`); usage
// data lives per-project per recon notes section 7.4.
func defaultCrushConfigDir() string {
	home := homeDir()
	if home == "" {
		return ""
	}
	// Crush uses XDG even on macOS (no `~/Library/Application Support`
	// branch). See recon section 7.4.
	base := strings.TrimSpace(os.Getenv("XDG_DATA_HOME"))
	if base == "" {
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "crush")
}
