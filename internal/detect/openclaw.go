package detect

import (
	"log"
	"path/filepath"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// detectOpenClaw registers a local OpenClaw account when any of the known
// data directories exist, or when an `openclaw` binary is on PATH.
func detectOpenClaw(result *Result) {
	bin := findBinary("openclaw")
	configDir, agentsDir := firstOpenClawLocations()

	if bin == "" && configDir == "" {
		return
	}

	if bin != "" {
		log.Printf("[detect] Found OpenClaw at %s", bin)
		result.Tools = append(result.Tools, DetectedTool{
			Name:       "OpenClaw",
			BinaryPath: bin,
			ConfigDir:  configDir,
			Type:       "cli",
		})
	}

	acct := core.AccountConfig{
		ID:           "openclaw",
		Provider:     "openclaw",
		Auth:         "local",
		Binary:       bin,
		RuntimeHints: make(map[string]string),
	}
	if agentsDir != "" {
		acct.SetPath("agents_dir", agentsDir)
		acct.SetHint("agents_dir", agentsDir)
		log.Printf("[detect] OpenClaw agents dir at %s", agentsDir)
	}
	if configDir != "" {
		acct.SetHint("data_dir", configDir)
	}

	addAccount(result, acct)
}

// firstOpenClawLocations returns (configDir, agentsDir) for the first
// matching install location, checking the canonical path first followed by
// legacy aliases.
func firstOpenClawLocations() (string, string) {
	home := homeDir()
	if home == "" {
		return "", ""
	}
	candidates := []string{".openclaw", ".clawdbot", ".moltbot", ".moldbot"}
	for _, name := range candidates {
		configDir := filepath.Join(home, name)
		if !dirExists(configDir) {
			continue
		}
		agentsDir := filepath.Join(configDir, "agents")
		if !dirExists(agentsDir) {
			agentsDir = ""
		}
		return configDir, agentsDir
	}
	return "", ""
}
