package detect

import (
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/crush"
)

// detectCrush registers a local Crush account when either the CLI
// binary is on PATH or Crush's own projects.json registry lists at
// least one project with a present crush.db.
func detectCrush(result *Result) {
	bin := findBinary("crush")
	dbPaths := crush.DiscoverDBPaths()

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
		acct.SetPath(crush.PathHintDBsKey, joined)
		acct.SetHint(crush.PathHintDBsKey, joined)
		log.Printf("[detect] Crush: discovered %d project DB(s)", len(dbPaths))
	}
	if dir := defaultCrushConfigDir(); dir != "" {
		acct.SetHint("config_dir", dir)
	}

	addAccount(result, acct)
}

// defaultCrushConfigDir returns the global Crush config dir for
// surfacing in the detected-tool record. Crush stores only OAuth tokens
// and recent-model preferences here; usage data lives per-project.
func defaultCrushConfigDir() string {
	home := homeDir()
	if home == "" {
		return ""
	}
	base := strings.TrimSpace(os.Getenv("XDG_DATA_HOME"))
	if base == "" {
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "crush")
}
