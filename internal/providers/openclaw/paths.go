package openclaw

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// PathHintAgentsDirKey overrides the resolved agents directory location.
const PathHintAgentsDirKey = "agents_dir"

// defaultAgentsDir returns the canonical agents directory:
// $HOME/.openclaw/agents
func defaultAgentsDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".openclaw", "agents")
}

// legacyAgentsDirs returns historical aliases that may still exist on the
// workstation.
func legacyAgentsDirs() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	return []string{
		filepath.Join(home, ".clawdbot", "agents"),
		filepath.Join(home, ".moltbot", "agents"),
		filepath.Join(home, ".moldbot", "agents"),
	}
}

// resolveAgentsDirs returns every agents directory we should walk for this
// account. An explicit override wins outright; otherwise we return the
// de-duped union of existing default + legacy locations.
func resolveAgentsDirs(acct core.AccountConfig) []string {
	if override := strings.TrimSpace(acct.Path(PathHintAgentsDirKey, "")); override != "" {
		if dirExists(override) {
			return []string{override}
		}
		return nil
	}

	seen := make(map[string]struct{})
	var out []string
	add := func(p string) {
		if p == "" || !dirExists(p) {
			return
		}
		if _, dup := seen[p]; dup {
			return
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}

	add(defaultAgentsDir())
	for _, p := range legacyAgentsDirs() {
		add(p)
	}
	return out
}

func dirExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
