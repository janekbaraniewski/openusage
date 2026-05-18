package detect

import (
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// detectZed registers a local Zed account when threads.db is present at the
// OS-appropriate location, or when the `zed` (or platform-specific `Zed.app`
// on macOS) binary is on PATH. Either signal alone is enough so a freshly
// installed editor still surfaces before its first agent thread runs.
func detectZed(result *Result) {
	bin := findBinary("zed")
	dbPath := zedDefaultThreadsDB()
	hasDB := dbPath != "" && fileExists(dbPath)

	if bin == "" && !hasDB {
		return
	}

	configDir := zedDefaultDataDir()

	if bin != "" {
		log.Printf("[detect] Found Zed at %s", bin)
		result.Tools = append(result.Tools, DetectedTool{
			Name:       "Zed",
			BinaryPath: bin,
			ConfigDir:  configDir,
			Type:       "ide",
		})
	}

	acct := core.AccountConfig{
		ID:           "zed",
		Provider:     "zed",
		Auth:         "local",
		Binary:       bin,
		RuntimeHints: make(map[string]string),
	}
	if hasDB {
		acct.SetPath("db_path", dbPath)
		acct.SetHint("db_path", dbPath)
		log.Printf("[detect] Zed threads.db at %s", dbPath)
	}
	if configDir != "" {
		acct.SetHint("data_dir", configDir)
	}

	addAccount(result, acct)
}

// zedDefaultThreadsDB returns the canonical path to Zed's threads.db on this
// platform. Mirrors internal/providers/zed/paths.go so the detector and the
// provider agree without an import cycle.
func zedDefaultThreadsDB() string {
	home := homeDir()
	switch runtime.GOOS {
	case "darwin":
		if home == "" {
			return ""
		}
		return filepath.Join(home, "Library", "Application Support", "Zed", "threads", "threads.db")
	case "windows":
		if local := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); local != "" {
			return filepath.Join(local, "Zed", "threads", "threads.db")
		}
		if home != "" {
			return filepath.Join(home, "AppData", "Local", "Zed", "threads", "threads.db")
		}
		return ""
	default:
		if xdg := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); xdg != "" {
			return filepath.Join(xdg, "zed", "threads", "threads.db")
		}
		if home != "" {
			return filepath.Join(home, ".local", "share", "zed", "threads", "threads.db")
		}
		return ""
	}
}

// zedDefaultDataDir returns the parent data directory for Zed (one level
// above threads/threads.db), useful as a generic config-dir hint.
func zedDefaultDataDir() string {
	db := zedDefaultThreadsDB()
	if db == "" {
		return ""
	}
	// .../<data dir>/threads/threads.db → strip last two segments.
	return filepath.Dir(filepath.Dir(db))
}
