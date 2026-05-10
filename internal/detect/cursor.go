package detect

import (
	"database/sql"
	"fmt"
	"log"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func detectCursor(result *Result) {
	bin := findBinary("cursor")
	if bin == "" {
		return
	}

	home := homeDir()
	configDir := filepath.Join(home, ".cursor")
	appSupport := cursorAppSupportDir()

	tool := DetectedTool{
		Name:       "Cursor IDE",
		BinaryPath: bin,
		ConfigDir:  configDir,
		Type:       "ide",
	}
	result.Tools = append(result.Tools, tool)

	log.Printf("[detect] Found Cursor IDE at %s", bin)

	trackingDB := filepath.Join(configDir, "ai-tracking", "ai-code-tracking.db")
	stateDB := filepath.Join(appSupport, "User", "globalStorage", "state.vscdb")

	hasTracking := fileExists(trackingDB)
	hasState := fileExists(stateDB)

	if !hasTracking && !hasState {
		log.Printf("[detect] Cursor found but no tracking data at expected locations")
		return
	}

	log.Printf("[detect] Cursor tracking data found (tracking_db=%v, state_db=%v)", hasTracking, hasState)

	acct := core.AccountConfig{
		ID:        "cursor-ide",
		Provider:  "cursor",
		Auth:      "local",
		RuntimeHints: make(map[string]string),
	}

	if hasTracking {
		acct.SetPath("tracking_db", trackingDB)
		acct.SetHint("tracking_db", trackingDB)
		acct.RuntimeHints["tracking_db"] = trackingDB
	}
	if hasState {
		acct.SetPath("state_db", stateDB)
		acct.SetHint("state_db", stateDB)
		acct.RuntimeHints["state_db"] = stateDB
	}

	if hasState {
		token, email, membership := extractCursorAuth(stateDB)
		if token != "" {
			acct.Auth = "token"
			acct.Token = token
			log.Printf("[detect] Extracted Cursor auth token for API access")
		}
		if email != "" {
			acct.RuntimeHints["email"] = email
			log.Printf("[detect] Cursor account: %s", email)
		}
		if membership != "" {
			acct.RuntimeHints["membership"] = membership
			log.Printf("[detect] Cursor membership: %s", membership)
		}
	}

	addAccount(result, acct)
}

func extractCursorAuth(stateDBPath string) (token, email, membership string) {
	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?mode=ro", stateDBPath))
	if err != nil {
		log.Printf("[detect] Cannot open state.vscdb: %v", err)
		return "", "", ""
	}
	defer db.Close()

	err = db.QueryRow(
		`SELECT value FROM ItemTable WHERE key = 'cursorAuth/accessToken'`).Scan(&token)
	if err != nil {
		log.Printf("[detect] No Cursor access token found: %v", err)
		token = ""
	}

	err = db.QueryRow(
		`SELECT value FROM ItemTable WHERE key = 'cursorAuth/cachedEmail'`).Scan(&email)
	if err != nil {
		email = ""
	}

	err = db.QueryRow(
		`SELECT value FROM ItemTable WHERE key = 'cursorAuth/stripeMembershipType'`).Scan(&membership)
	if err != nil {
		membership = ""
	}

	return token, email, membership
}
