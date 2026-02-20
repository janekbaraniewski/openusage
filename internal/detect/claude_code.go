package detect

import (
	"log"
	"path/filepath"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func detectClaudeCode(result *Result) {
	bin := findBinary("claude")
	if bin == "" {
		return
	}

	home := homeDir()
	configDir := filepath.Join(home, ".claude")
	statsFile := filepath.Join(configDir, "stats-cache.json")
	accountFile := filepath.Join(home, ".claude.json")

	tool := DetectedTool{
		Name:       "Claude Code CLI",
		BinaryPath: bin,
		ConfigDir:  configDir,
		Type:       "cli",
	}
	result.Tools = append(result.Tools, tool)

	log.Printf("[detect] Found Claude Code CLI at %s", bin)

	hasStats := fileExists(statsFile)
	hasAccount := fileExists(accountFile)

	if hasStats || hasAccount {
		log.Printf("[detect] Claude Code data found (stats=%v, account=%v)", hasStats, hasAccount)

		addAccount(result, core.AccountConfig{
			ID:       "claude-code",
			Provider: "claude_code",
			Auth:     "local",
			Binary:   statsFile,
			BaseURL:  accountFile,
		})
	} else {
		log.Printf("[detect] Claude Code found but no stats data at expected locations")
	}
}
