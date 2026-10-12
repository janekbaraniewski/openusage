package detect

import (
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// detectDsh registers a local dsh (DeepSeek Harness) account when its
// sessions directory exists: $DSH_HOME/sessions, or ~/.dsh/sessions when
// DSH_HOME is unset. The binary alone is not a signal, since `dsh` is also
// the name of unrelated tools.
func detectDsh(result *Result) {
	sessionsDir := defaultDshSessionsDir()
	if sessionsDir == "" || !dirExists(sessionsDir) {
		return
	}
	log.Printf("[detect] dsh sessions dir at %s", sessionsDir)

	bin := findBinary("dsh")
	if bin != "" {
		log.Printf("[detect] Found dsh at %s", bin)
		result.Tools = append(result.Tools, DetectedTool{
			Name:       "dsh",
			BinaryPath: bin,
			ConfigDir:  filepath.Dir(sessionsDir),
			Type:       "cli",
		})
	}

	acct := core.AccountConfig{
		ID:           "dsh",
		Provider:     "dsh",
		Auth:         "local",
		Binary:       bin,
		RuntimeHints: make(map[string]string),
	}
	acct.SetPath("sessions_dir", sessionsDir)
	acct.SetHint("sessions_dir", sessionsDir)
	acct.SetHint("data_dir", filepath.Dir(sessionsDir))

	addAccount(result, acct)
}

func defaultDshSessionsDir() string {
	if override := strings.TrimSpace(os.Getenv("DSH_HOME")); override != "" {
		return filepath.Join(override, "sessions")
	}
	home := homeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".dsh", "sessions")
}
