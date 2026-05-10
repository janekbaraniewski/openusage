package gemini_cli

import "github.com/janekbaraniewski/openusage/internal/core"

func testGeminiCLIAccount(id, configDir string) core.AccountConfig {
	acct := core.AccountConfig{
		ID:        id,
		Provider:  "gemini_cli",
		RuntimeHints: map[string]string{"config_dir": configDir},
	}
	acct.SetHint("config_dir", configDir)
	return acct
}
