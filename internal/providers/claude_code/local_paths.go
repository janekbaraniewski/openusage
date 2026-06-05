package claude_code

import (
	"os"
	"path/filepath"
)

// LocalSourcePaths returns the file system locations the provider reads on
// each Fetch. Used by internal/tmux active-tool detection to gauge whether
// Claude Code has had recent activity. The path resolution mirrors
// Provider.Fetch (see claude_code.go) for the no-override case; a runtime
// override via acct.Hint("claude_dir", ...) is not consulted here since
// active-tool detection runs without an account context.
func (p *Provider) LocalSourcePaths() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	claudeDir := filepath.Join(home, ".claude")
	return []string{
		filepath.Join(claudeDir, "projects"),
		filepath.Join(home, ".config", "claude", "projects"),
		filepath.Join(claudeDir, "stats-cache.json"),
		filepath.Join(home, ".claude.json"),
		filepath.Join(claudeDir, "settings.json"),
	}
}
