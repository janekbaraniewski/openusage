package claude_code

import (
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func normalizeLegacyPaths(acct *core.AccountConfig) {
	if acct == nil {
		return
	}
	if strings.TrimSpace(acct.Binary) != "" {
		acct.SetPath("stats_cache", acct.Binary)
	}
	if strings.TrimSpace(acct.BaseURL) != "" {
		acct.SetPath("account_config", acct.BaseURL)
	}
}
