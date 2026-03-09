package cursor

import (
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func normalizeLegacyPaths(acct *core.AccountConfig) {
	if acct == nil {
		return
	}
	if strings.TrimSpace(acct.Binary) != "" {
		acct.SetPath("tracking_db", acct.Binary)
	}
	if strings.TrimSpace(acct.BaseURL) != "" {
		acct.SetPath("state_db", acct.BaseURL)
	}
}
