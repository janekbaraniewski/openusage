package crush

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// PathHintDBsKey is the AccountConfig path hint used to inject an
// already-resolved list of DB paths. Detectors set this so the provider
// doesn't have to repeat the registry lookup at Fetch time. Values are
// joined with the OS path-list separator.
const PathHintDBsKey = "db_paths"

// PathHintSingleDBKey is a per-account override for a single Crush DB
// path. Useful when a user wants to point openusage at one specific
// project DB.
const PathHintSingleDBKey = "db_path"

// PathHintRegistryKey overrides the Crush project-registry location for
// a single account. Falls through to $XDG_DATA_HOME and the platform
// default when unset.
const PathHintRegistryKey = "registry_path"

// EnvRegistry overrides the registry path across all accounts. Useful
// for sandboxed installs.
const EnvRegistry = "OPENUSAGE_CRUSH_REGISTRY"

// projectDBName is the basename of the per-project Crush SQLite store.
const projectDBName = "crush.db"

// projectDataDirName is the per-project Crush data-directory name,
// used when the registry entry declares a relative data_dir.
const projectDataDirName = ".crush"

// crushRegistry mirrors the on-disk shape of Crush's projects.json.
// Crush writes this file itself; we never produce it.
type crushRegistry struct {
	Projects []crushProject `json:"projects"`
}

type crushProject struct {
	Path    string `json:"path"`
	DataDir string `json:"data_dir"`
}

// defaultRegistryPath returns the canonical location of Crush's project
// registry on this platform. Crush follows XDG on every OS (including
// macOS), so $XDG_DATA_HOME wins when set and ~/.local/share is the
// fallback. Windows uses %LOCALAPPDATA% when XDG isn't set.
func defaultRegistryPath() string {
	if xdg := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); xdg != "" {
		return filepath.Join(xdg, "crush", "projects.json")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	if runtime.GOOS == "windows" {
		if local := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); local != "" {
			return filepath.Join(local, "crush", "projects.json")
		}
	}
	return filepath.Join(home, ".local", "share", "crush", "projects.json")
}

// resolveRegistryPath returns the effective registry path for the
// account, honoring (in order): per-account override, env override,
// platform default.
func resolveRegistryPath(acct core.AccountConfig) string {
	if override := strings.TrimSpace(acct.Path(PathHintRegistryKey, "")); override != "" {
		return override
	}
	if env := strings.TrimSpace(os.Getenv(EnvRegistry)); env != "" {
		return env
	}
	return defaultRegistryPath()
}

// splitPathList splits a path-list-separator string into a deduplicated,
// trimmed slice. Used for the db_paths hint.
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

// resolveDBPaths returns the list of Crush DBs for the account. Order
// of precedence:
//
//  1. Explicit list pre-resolved by the detector via PathHintDBsKey.
//  2. Single explicit override via PathHintSingleDBKey.
//  3. Crush's own project registry (registry_path / OPENUSAGE_CRUSH_REGISTRY
//     / platform default).
//
// Non-existent paths are filtered out so a stale settings.json or a
// stale registry entry doesn't blow up the dashboard.
func resolveDBPaths(acct core.AccountConfig) []string {
	if list := acct.Path(PathHintDBsKey, ""); list != "" {
		return filterExistingFiles(splitPathList(list))
	}
	if single := strings.TrimSpace(acct.Path(PathHintSingleDBKey, "")); single != "" {
		if fileExists(single) {
			return []string{single}
		}
		return nil
	}
	return readRegistryDBs(resolveRegistryPath(acct))
}

// DiscoverDBPaths reads Crush's project registry (using the platform
// default location, or the OPENUSAGE_CRUSH_REGISTRY override) and
// returns every `crush.db` Crush itself has registered. Exported so
// the detect package can seed `db_paths` on the auto-detected account
// without re-reading the registry at Fetch time.
//
// No directory walking is performed: only one JSON file is read, and
// the file paths returned are taken verbatim from Crush's own state.
// This avoids the macOS TCC prompts that would otherwise fire when a
// generic filesystem walk crossed into ~/Pictures, ~/Documents under
// iCloud Drive sync, or any *.photoslibrary bundle.
func DiscoverDBPaths() []string {
	return readRegistryDBs(defaultRegistryPath())
}

// readRegistryDBs parses Crush's projects.json and returns the
// resolved absolute crush.db path for every project the registry
// lists. Projects whose declared DB doesn't exist on disk are skipped.
func readRegistryDBs(registryPath string) []string {
	registryPath = strings.TrimSpace(registryPath)
	if registryPath == "" {
		return nil
	}
	raw, err := os.ReadFile(registryPath)
	if err != nil {
		return nil
	}
	var reg crushRegistry
	if err := json.Unmarshal(raw, &reg); err != nil {
		return nil
	}
	seen := make(map[string]struct{}, len(reg.Projects))
	out := make([]string, 0, len(reg.Projects))
	for _, project := range reg.Projects {
		db := resolveProjectDB(project)
		if db == "" || !fileExists(db) {
			continue
		}
		abs, err := filepath.Abs(db)
		if err != nil {
			abs = db
		}
		if _, dup := seen[abs]; dup {
			continue
		}
		seen[abs] = struct{}{}
		out = append(out, abs)
	}
	return out
}

// resolveProjectDB builds the absolute crush.db path for a single
// registry entry. A relative data_dir is joined onto the project path;
// an absolute one is used verbatim. Empty inputs return "".
func resolveProjectDB(p crushProject) string {
	projectPath := strings.TrimSpace(p.Path)
	dataDir := strings.TrimSpace(p.DataDir)
	if dataDir == "" {
		dataDir = projectDataDirName
	}
	var resolved string
	switch {
	case filepath.IsAbs(dataDir):
		resolved = dataDir
	case projectPath == "":
		return ""
	default:
		resolved = filepath.Join(projectPath, dataDir)
	}
	return filepath.Join(resolved, projectDBName)
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
