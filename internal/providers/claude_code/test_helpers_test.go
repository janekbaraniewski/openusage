package claude_code

import "github.com/janekbaraniewski/openusage/internal/core"

func testClaudeAccount(id, statsPath, accountPath string) core.AccountConfig {
	return core.AccountConfig{
		ID:      id,
		Binary:  statsPath,
		BaseURL: accountPath,
	}
}

func testClaudeAccountWithDir(id, statsPath, accountPath, claudeDir string) core.AccountConfig {
	acct := testClaudeAccount(id, statsPath, accountPath)
	acct.ExtraData = map[string]string{"claude_dir": claudeDir}
	acct.SetHint("claude_dir", claudeDir)
	return acct
}
