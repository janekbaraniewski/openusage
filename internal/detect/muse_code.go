package detect

import (
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// detectMuseCode registers a local Muse Code account when the `muse` CLI
// binary is on PATH, the sessions directory exists, or the auth file written
// by `muse login` exists.
func detectMuseCode(result *Result) {
	bin := findBinary("muse")
	sessionsDir := defaultMuseSessionsDir()
	authFile := defaultMuseAuthFile()
	hasSessions := sessionsDir != "" && dirExists(sessionsDir)
	hasAuth := authFile != "" && fileExists(authFile)

	if bin == "" && !hasSessions && !hasAuth {
		return
	}

	if bin != "" {
		log.Printf("[detect] Found Muse Code at %s", bin)
		result.Tools = append(result.Tools, DetectedTool{
			Name:       "Muse Code",
			BinaryPath: bin,
			ConfigDir:  defaultMuseConfigDir(),
			Type:       "cli",
		})
	}

	acct := core.AccountConfig{
		ID:           "muse-code",
		Provider:     "muse_code",
		Auth:         "local",
		Binary:       bin,
		RuntimeHints: make(map[string]string),
	}
	if hasSessions {
		acct.SetHint("sessions_dir", sessionsDir)
		log.Printf("[detect] Muse Code sessions dir at %s", sessionsDir)
	}
	if dir := defaultMuseConfigDir(); dir != "" {
		acct.SetHint("config_dir", dir)
	}
	addAccount(result, acct)
}

func defaultMuseSessionsDir() string {
	if xdg := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); xdg != "" {
		return filepath.Join(xdg, "muse", "sessions")
	}
	home := homeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".local", "share", "muse", "sessions")
}

func defaultMuseConfigDir() string {
	if override := strings.TrimSpace(os.Getenv("MUSE_CONFIG_DIR")); override != "" {
		return override
	}
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "muse")
	}
	home := homeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".config", "muse")
}

func defaultMuseAuthFile() string {
	if override := strings.TrimSpace(os.Getenv("MUSE_AUTH_PATH")); override != "" {
		return override
	}
	if dir := defaultMuseConfigDir(); dir != "" {
		return filepath.Join(dir, "auth.json")
	}
	return ""
}
