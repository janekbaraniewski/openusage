package copilot

import "github.com/janekbaraniewski/openusage/internal/core"

func testCopilotAccount(binary, configDir, copilotBinary string) core.AccountConfig {
	acct := core.AccountConfig{
		ID:       "copilot",
		Provider: "copilot",
		Auth:     "cli",
		Binary:   binary,
	}
	if configDir == "" && copilotBinary == "" {
		return acct
	}
	acct.ExtraData = map[string]string{}
	if configDir != "" {
		acct.ExtraData["config_dir"] = configDir
		acct.SetHint("config_dir", configDir)
	}
	if copilotBinary != "" {
		acct.ExtraData["copilot_binary"] = copilotBinary
		acct.SetHint("copilot_binary", copilotBinary)
	}
	return acct
}
