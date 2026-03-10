package cursor

import "github.com/janekbaraniewski/openusage/internal/core"

func testCursorAccount(id, token string, extra map[string]string) core.AccountConfig {
	acct := core.AccountConfig{
		ID:       id,
		Provider: "cursor",
		Token:    token,
	}
	if len(extra) == 0 {
		return acct
	}
	acct.ExtraData = extra
	for _, key := range []string{"tracking_db", "state_db"} {
		if value := extra[key]; value != "" {
			acct.SetHint(key, value)
		}
	}
	return acct
}
