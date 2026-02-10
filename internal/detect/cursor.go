package detect

import (
	"database/sql"
	"fmt"
	"log"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"

	"github.com/janekbaraniewski/agentusage/internal/core"
)

// detectCursor looks for the Cursor IDE installation and extracts:
//   - Auth access token from state.vscdb for API calls to DashboardService
//   - Local tracking DB paths as fallback
//
// Cursor stores usage data in two places:
//  1. Remote: api2.cursor.sh/aiserver.v1.DashboardService/* (requires auth token)
//     - GetCurrentPeriodUsage: billing cycle, plan spend, spend limits
//     - GetPlanInfo: plan name, price, included amount
//     - GetAggregatedUsageEvents: per-model token & cost breakdown
//     - GetHardLimit: usage-based billing status
//     - /auth/full_stripe_profile: membership type, team info
//  2. Local: SQLite databases
//     - ~/.cursor/ai-tracking/ai-code-tracking.db — per-request hashes
//     - ~/Library/Application Support/Cursor/User/globalStorage/state.vscdb
//     — daily stats, auth tokens
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
		ExtraData: make(map[string]string),
	}

	if hasTracking {
		acct.ExtraData["tracking_db"] = trackingDB
		// Legacy field for backward compat
		acct.Binary = trackingDB
	}
	if hasState {
		acct.ExtraData["state_db"] = stateDB
		// Legacy field for backward compat
		acct.BaseURL = stateDB
	}

	// Try to extract access token from state.vscdb for API access
	if hasState {
		token, email, membership := extractCursorAuth(stateDB)
		if token != "" {
			acct.Auth = "token"
			acct.Token = token
			log.Printf("[detect] Extracted Cursor auth token for API access")
		}
		if email != "" {
			acct.ExtraData["email"] = email
			log.Printf("[detect] Cursor account: %s", email)
		}
		if membership != "" {
			acct.ExtraData["membership"] = membership
			log.Printf("[detect] Cursor membership: %s", membership)
		}
	}

	addAccount(result, acct)
}

// extractCursorAuth reads the access token, email, and membership type from state.vscdb.
func extractCursorAuth(stateDBPath string) (token, email, membership string) {
	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?mode=ro&_journal_mode=WAL", stateDBPath))
	if err != nil {
		log.Printf("[detect] Cannot open state.vscdb: %v", err)
		return "", "", ""
	}
	defer db.Close()

	// Extract access token
	err = db.QueryRow(
		`SELECT value FROM ItemTable WHERE key = 'cursorAuth/accessToken'`).Scan(&token)
	if err != nil {
		log.Printf("[detect] No Cursor access token found: %v", err)
		token = ""
	}

	// Extract cached email
	err = db.QueryRow(
		`SELECT value FROM ItemTable WHERE key = 'cursorAuth/cachedEmail'`).Scan(&email)
	if err != nil {
		email = ""
	}

	// Extract membership type
	err = db.QueryRow(
		`SELECT value FROM ItemTable WHERE key = 'cursorAuth/stripeMembershipType'`).Scan(&membership)
	if err != nil {
		membership = ""
	}

	return token, email, membership
}
