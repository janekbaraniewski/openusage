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
	acct.RuntimeHints = extra
	for _, key := range []string{"tracking_db", "state_db"} {
		if value := extra[key]; value != "" {
			acct.SetHint(key, value)
		}
	}
	return acct
}

// testCursorAccountWithBase mirrors testCursorAccount but also sets BaseURL,
// used by tests that point Fetch at an httptest server. Replaces the
// pre-const-conversion idiom of mutating package-level cursorAPIBase.
func testCursorAccountWithBase(id, token, baseURL string, extra map[string]string) core.AccountConfig {
	acct := testCursorAccount(id, token, extra)
	acct.BaseURL = baseURL
	return acct
}
