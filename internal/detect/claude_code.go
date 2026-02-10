package detect

import (
	"log"
	"path/filepath"

	"github.com/janekbaraniewski/agentusage/internal/core"
)

// detectClaudeCode looks for the Claude Code CLI and its local stats data.
//
// Claude Code stores rich usage data locally:
//   - ~/.claude.json — account info (oauthAccount, hasAvailableSubscription, etc.)
//   - ~/.claude/settings.json — user preferences (model, plugins)
//   - ~/.claude/stats-cache.json — usage statistics:
//   - dailyActivity: messageCount, sessionCount, toolCallCount per day
//   - dailyModelTokens: tokensByModel per day
//   - modelUsage: per-model inputTokens, outputTokens, cacheReadInputTokens,
//     cacheCreationInputTokens, costUSD
//   - totalSessions, totalMessages
//   - ~/.claude/history.jsonl — conversation history
//
// Claude Code does NOT expose a public rate-limit API, but the stats-cache
// provides comprehensive local usage tracking.
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
			// Binary stores the stats-cache.json path
			Binary: statsFile,
			// BaseURL stores the account .claude.json path
			BaseURL: accountFile,
		})
	} else {
		log.Printf("[detect] Claude Code found but no stats data at expected locations")
	}
}
