// cursor-probe: Exhaustive reverse-engineering tool for Cursor IDE data sources.
// Discovers API endpoints, probes local databases, and decodes JWT tokens.
//
// Usage: go run ./cmd/cursor-probe
package main

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const (
	cursorAPIBase = "https://api2.cursor.sh"

	colorReset  = "\033[0m"
	colorBold   = "\033[1m"
	colorDim    = "\033[2m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorCyan   = "\033[36m"
	colorWhite  = "\033[37m"

	barFull  = "█"
	barEmpty = "░"
	barMid   = "▓"
)

// ────────────────────── helpers ──────────────────────

func banner(title string) {
	w := 72
	pad := (w - len(title) - 4) / 2
	if pad < 0 {
		pad = 0
	}
	line := strings.Repeat("─", w)
	fmt.Printf("\n%s%s%s\n", colorCyan, line, colorReset)
	fmt.Printf("%s%s%s %s%s%s %s%s\n",
		colorCyan, strings.Repeat(" ", pad), colorBold+colorWhite, title,
		colorReset+colorCyan, strings.Repeat(" ", pad), "", colorReset)
	fmt.Printf("%s%s%s\n", colorCyan, line, colorReset)
}

func section(icon, title string) {
	fmt.Printf("\n  %s %s%s%s\n", icon, colorBold+colorWhite, title, colorReset)
	fmt.Printf("  %s%s%s\n", colorDim, strings.Repeat("─", 50), colorReset)
}

func kvLine(key, value string) {
	dots := 40 - len(key)
	if dots < 2 {
		dots = 2
	}
	fmt.Printf("    %s%s%s %s%s%s %s%s%s\n",
		colorDim, key, colorReset,
		colorDim, strings.Repeat("·", dots), colorReset,
		colorWhite, value, colorReset)
}

func okLine(msg string) {
	fmt.Printf("    %s✓%s %s\n", colorGreen, colorReset, msg)
}

func failLine(msg string) {
	fmt.Printf("    %s✗%s %s%s%s\n", colorRed, colorReset, colorDim, msg, colorReset)
}

func warnLine(msg string) {
	fmt.Printf("    %s⚠%s %s%s%s\n", colorYellow, colorReset, colorDim, msg, colorReset)
}

func infoLine(msg string) {
	fmt.Printf("    %s→%s %s\n", colorBlue, colorReset, msg)
}

func miniBar(used, total float64, width int) string {
	if total <= 0 {
		return strings.Repeat(barEmpty, width)
	}
	pct := used / total
	if pct > 1 {
		pct = 1
	}
	filled := int(pct * float64(width))
	empty := width - filled
	color := colorGreen
	if pct > 0.7 {
		color = colorYellow
	}
	if pct > 0.85 {
		color = colorRed
	}
	return fmt.Sprintf("%s%s%s%s%s%s",
		color, strings.Repeat(barFull, filled), colorReset,
		colorDim, strings.Repeat(barEmpty, empty), colorReset)
}

func prettyJSON(data interface{}) string {
	b, err := json.MarshalIndent(data, "      ", "  ")
	if err != nil {
		return fmt.Sprintf("%v", data)
	}
	return string(b)
}

func formatDollars(cents float64) string {
	return fmt.Sprintf("$%.2f", cents/100.0)
}

func formatTokens(s string) string {
	s = strings.ReplaceAll(s, ",", "")
	s = strings.ReplaceAll(s, "_", "")
	return s
}

// ────────────────────── paths ──────────────────────

func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}

func cursorAppSupportDir() string {
	home := homeDir()
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Cursor")
	case "linux":
		return filepath.Join(home, ".config", "Cursor")
	case "windows":
		if ad := os.Getenv("APPDATA"); ad != "" {
			return filepath.Join(ad, "Cursor")
		}
		return filepath.Join(home, "AppData", "Roaming", "Cursor")
	}
	return ""
}

func trackingDBPath() string {
	return filepath.Join(homeDir(), ".cursor", "ai-tracking", "ai-code-tracking.db")
}

func stateDBPath() string {
	return filepath.Join(cursorAppSupportDir(), "User", "globalStorage", "state.vscdb")
}

// ────────────────────── API calls ──────────────────────

func callDashboardAPI(token, method string) (map[string]interface{}, int, error) {
	url := fmt.Sprintf("%s/aiserver.v1.DashboardService/%s", cursorAPIBase, method)
	req, err := http.NewRequest("POST", url, bytes.NewReader([]byte("{}")))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, resp.StatusCode, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncStr(string(body), 200))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, resp.StatusCode, err
	}
	return result, resp.StatusCode, nil
}

func callRESTAPI(token, path string) (map[string]interface{}, int, error) {
	url := cursorAPIBase + path
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, resp.StatusCode, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncStr(string(body), 200))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		// Try array response
		var arr []interface{}
		if err2 := json.Unmarshal(body, &arr); err2 == nil {
			return map[string]interface{}{"_array": arr}, resp.StatusCode, nil
		}
		// Return raw string
		return map[string]interface{}{"_raw": string(body)}, resp.StatusCode, nil
	}
	return result, resp.StatusCode, nil
}

func truncStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// ────────────────────── JWT decode ──────────────────────

func decodeJWT(token string) map[string]interface{} {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return nil
	}

	payload := parts[1]
	// Add padding if needed
	switch len(payload) % 4 {
	case 2:
		payload += "=="
	case 3:
		payload += "="
	}

	data, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		// Try standard encoding
		data, err = base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return nil
		}
	}

	var claims map[string]interface{}
	if err := json.Unmarshal(data, &claims); err != nil {
		return nil
	}
	return claims
}

// ────────────────────── token extraction ──────────────────────

func extractToken() (token, email, membership string) {
	dbPath := stateDBPath()
	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?mode=ro", dbPath))
	if err != nil {
		return "", "", ""
	}
	defer db.Close()

	_ = db.QueryRow(`SELECT value FROM ItemTable WHERE key = 'cursorAuth/accessToken'`).Scan(&token)
	_ = db.QueryRow(`SELECT value FROM ItemTable WHERE key = 'cursorAuth/cachedEmail'`).Scan(&email)
	_ = db.QueryRow(`SELECT value FROM ItemTable WHERE key = 'cursorAuth/stripeMembershipType'`).Scan(&membership)
	return
}

// ────────────────────── main ──────────────────────

func main() {
	fmt.Printf("\n%s%s╔══════════════════════════════════════════════════════════════════════════╗%s\n", colorBold, colorCyan, colorReset)
	fmt.Printf("%s%s║                    CURSOR IDE — DEEP PROBE REPORT                       ║%s\n", colorBold, colorCyan, colorReset)
	fmt.Printf("%s%s╚══════════════════════════════════════════════════════════════════════════╝%s\n", colorBold, colorCyan, colorReset)
	fmt.Printf("  %s%s · %s%s\n", colorDim, time.Now().Format("2006-01-02 15:04:05"), runtime.GOOS, colorReset)

	// ══════════════════════ 1. AUTHENTICATION ══════════════════════
	banner("1. AUTHENTICATION & TOKEN")
	token, email, membership := extractToken()
	if token == "" {
		fmt.Printf("\n  %s%sFATAL: No Cursor auth token found. Is Cursor installed and logged in?%s\n", colorBold, colorRed, colorReset)
		os.Exit(1)
	}

	section("🔑", "Credentials")
	kvLine("Email", email)
	kvLine("Membership", membership)
	kvLine("Token length", fmt.Sprintf("%d chars", len(token)))
	kvLine("Token prefix", token[:min(20, len(token))]+"...")

	section("🪪", "JWT Claims")
	claims := decodeJWT(token)
	if claims != nil {
		keys := make([]string, 0, len(claims))
		for k := range claims {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := claims[k]
			switch val := v.(type) {
			case float64:
				if k == "iat" || k == "exp" || k == "auth_time" || k == "nbf" {
					t := time.Unix(int64(val), 0)
					kvLine(k, fmt.Sprintf("%.0f (%s)", val, t.Format("2006-01-02 15:04:05")))
				} else {
					kvLine(k, fmt.Sprintf("%.0f", val))
				}
			case string:
				if len(val) > 80 {
					val = val[:80] + "..."
				}
				kvLine(k, val)
			case bool:
				kvLine(k, fmt.Sprintf("%v", val))
			case map[string]interface{}:
				kvLine(k, prettyJSON(val))
			default:
				kvLine(k, fmt.Sprintf("%v", v))
			}
		}
	} else {
		warnLine("Could not decode JWT claims")
	}

	// ══════════════════════ 2. KNOWN API ENDPOINTS ══════════════════════
	banner("2. KNOWN API ENDPOINTS (currently used)")

	knownDashboard := []struct {
		method string
		desc   string
	}{
		{"GetCurrentPeriodUsage", "Billing cycle, plan/team spend, percentages"},
		{"GetPlanInfo", "Plan name, price, included credits"},
		{"GetAggregatedUsageEvents", "Per-model token counts + costs"},
		{"GetHardLimit", "Usage-based billing flag"},
		{"GetUsageLimitPolicyStatus", "Spend limit permissions"},
	}

	for _, ep := range knownDashboard {
		section("📡", fmt.Sprintf("DashboardService/%s", ep.method))
		infoLine(ep.desc)
		result, code, err := callDashboardAPI(token, ep.method)
		if err != nil {
			failLine(fmt.Sprintf("HTTP %d — %s", code, err))
			continue
		}
		okLine(fmt.Sprintf("HTTP %d — %d fields", code, len(result)))
		printResponseFields(result, "")
	}

	section("📡", "REST: /auth/full_stripe_profile")
	infoLine("Membership, team info, payment ID")
	result, code, err := callRESTAPI(token, "/auth/full_stripe_profile")
	if err != nil {
		failLine(fmt.Sprintf("HTTP %d — %s", code, err))
	} else {
		okLine(fmt.Sprintf("HTTP %d — %d fields", code, len(result)))
		printResponseFields(result, "")
	}

	// ══════════════════════ 3. PROBE UNKNOWN ENDPOINTS ══════════════════════
	banner("3. PROBING UNKNOWN API ENDPOINTS")

	unknownDashboard := []struct {
		method string
		desc   string
	}{
		// Team/Org
		{"GetTeamInfo", "Team details, size, seats"},
		{"GetTeamMembers", "Team member list + usage"},
		{"GetTeamUsage", "Aggregated team usage"},
		{"GetTeamSettings", "Team configuration"},
		{"GetOrganizationInfo", "Org-level info"},
		{"GetOrganizationMembers", "Org members"},
		// Billing
		{"GetBillingHistory", "Past invoices/charges"},
		{"GetInvoices", "Invoice details"},
		{"GetPaymentMethods", "Saved cards"},
		{"GetCreditsHistory", "Credit/promo tracking"},
		{"GetSubscription", "Subscription details"},
		{"GetSubscriptionInfo", "Subscription info"},
		// Usage
		{"GetDailyUsage", "Per-day breakdown"},
		{"GetHourlyUsage", "Hourly breakdown"},
		{"GetUsageHistory", "Historical usage"},
		{"GetUsageBreakdown", "Usage by dimension"},
		{"GetUsageByModel", "Per-model history"},
		{"GetUsageBySource", "Per-client-type history"},
		{"GetUsageSummary", "Usage summary"},
		// Models
		{"GetAvailableModels", "Models user can access"},
		{"GetModelAvailability", "Model availability"},
		{"GetModelPricing", "Per-model pricing"},
		{"GetModelLimits", "Rate limits per model"},
		{"GetModels", "Model list"},
		{"ListModels", "Model list (alt)"},
		// Settings/Features
		{"GetSettings", "User settings"},
		{"GetUserSettings", "User settings (alt)"},
		{"GetPreferences", "User preferences"},
		{"GetFeatureFlags", "Feature flags"},
		{"GetFeatures", "Feature access"},
		// Notifications
		{"GetNotifications", "Alerts/warnings"},
		{"GetAlerts", "Alert list"},
		{"GetAnnouncements", "Announcements"},
		// Misc
		{"GetDashboard", "Dashboard overview"},
		{"GetStatus", "Service status"},
		{"GetHealth", "Health check"},
		{"Ping", "Service ping"},
		{"GetAccountInfo", "Account details"},
		{"GetUserInfo", "User info"},
		{"GetProfile", "User profile"},
		{"GetSpendLimit", "Spend limit details"},
		{"GetRateLimits", "Rate limit info"},
		{"GetApiKeys", "API key list"},
		{"GetSessions", "Active sessions"},
		{"GetUsageEvents", "Raw usage events"},
		{"GetCostBreakdown", "Cost analysis"},
		{"GetTokenUsage", "Token usage stats"},
		{"GetComposerSessions", "Composer session history"},
	}

	discovered := 0
	for _, ep := range unknownDashboard {
		result, code, err := callDashboardAPI(token, ep.method)
		if err != nil {
			if code == 404 || code == 0 {
				fmt.Printf("    %s·%s %-35s %s%d%s\n", colorDim, colorReset,
					ep.method, colorDim, code, colorReset)
			} else {
				fmt.Printf("    %s⚡%s %-35s %s%s%d%s — %s\n", colorYellow, colorReset,
					ep.method, colorYellow, "HTTP ", code, colorReset, truncStr(err.Error(), 60))
			}
			continue
		}
		discovered++
		fmt.Printf("\n    %s★ DISCOVERED: %s%s\n", colorGreen+colorBold, ep.method, colorReset)
		fmt.Printf("      %s%s%s\n", colorDim, ep.desc, colorReset)
		okLine(fmt.Sprintf("HTTP %d — %d fields", code, len(result)))
		printResponseFields(result, "")
	}

	// Probe REST endpoints
	unknownREST := []string{
		"/auth/user",
		"/auth/me",
		"/auth/profile",
		"/auth/verify",
		"/auth/account",
		"/auth/team",
		"/auth/team/members",
		"/auth/subscription",
		"/auth/billing",
		"/auth/invoices",
		"/auth/credits",
		"/auth/usage",
		"/auth/settings",
		"/auth/features",
		"/auth/notifications",
		"/api/user",
		"/api/me",
		"/api/team",
		"/api/billing",
		"/api/usage",
		"/api/models",
		"/api/settings",
		"/api/health",
		"/api/v1/user",
		"/api/v1/team",
		"/api/v1/billing",
		"/api/v1/usage",
		"/user",
		"/team",
		"/billing",
		"/usage",
		"/health",
		"/status",
	}

	fmt.Printf("\n  %s REST endpoints:%s\n", colorBold, colorReset)
	for _, path := range unknownREST {
		result, code, err := callRESTAPI(token, path)
		if err != nil {
			if code == 404 || code == 0 {
				fmt.Printf("    %s·%s %-35s %s%d%s\n", colorDim, colorReset,
					path, colorDim, code, colorReset)
			} else {
				fmt.Printf("    %s⚡%s %-35s %s%s%d%s — %s\n", colorYellow, colorReset,
					path, colorYellow, "HTTP ", code, colorReset, truncStr(err.Error(), 60))
			}
			continue
		}
		discovered++
		fmt.Printf("\n    %s★ DISCOVERED: %s%s\n", colorGreen+colorBold, path, colorReset)
		okLine(fmt.Sprintf("HTTP %d — %d fields", code, len(result)))
		printResponseFields(result, "")
	}

	fmt.Printf("\n  %s%d new endpoints discovered%s\n", colorBold+colorGreen, discovered, colorReset)

	// ══════════════════════ 4. LOCAL DATABASES ══════════════════════
	banner("4. LOCAL DATABASES")
	probeTrackingDB()
	probeStateDB()

	// ══════════════════════ 5. FILESYSTEM ══════════════════════
	banner("5. FILESYSTEM SCAN")
	probeCursorFiles()

	// Done
	fmt.Printf("\n%s%s══════════════════════════════════════════════════════════════════════════%s\n", colorBold, colorCyan, colorReset)
	fmt.Printf("  %s%sPROBE COMPLETE%s\n", colorBold, colorGreen, colorReset)
	fmt.Printf("%s%s══════════════════════════════════════════════════════════════════════════%s\n\n", colorBold, colorCyan, colorReset)
}

func printResponseFields(data map[string]interface{}, indent string) {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		v := data[k]
		prefix := indent + "    "
		switch val := v.(type) {
		case map[string]interface{}:
			fmt.Printf("%s%s%s:%s\n", prefix, colorCyan, k, colorReset)
			printResponseFields(val, indent+"  ")
		case []interface{}:
			fmt.Printf("%s%s%s:%s [%d items]", prefix, colorCyan, k, colorReset, len(val))
			if len(val) > 0 {
				if m, ok := val[0].(map[string]interface{}); ok {
					fmt.Println()
					printResponseFields(m, indent+"  ")
				} else {
					fmt.Printf(" → %v\n", truncStr(fmt.Sprintf("%v", val), 80))
				}
			} else {
				fmt.Println()
			}
		case string:
			if len(val) > 100 {
				val = val[:100] + "..."
			}
			fmt.Printf("%s%s%s%s → %s\"%s\"%s\n", prefix, colorDim, k, colorReset, colorGreen, val, colorReset)
		case float64:
			if val == float64(int64(val)) {
				fmt.Printf("%s%s%s%s → %s%.0f%s\n", prefix, colorDim, k, colorReset, colorYellow, val, colorReset)
			} else {
				fmt.Printf("%s%s%s%s → %s%.4f%s\n", prefix, colorDim, k, colorReset, colorYellow, val, colorReset)
			}
		case bool:
			color := colorGreen
			if !val {
				color = colorRed
			}
			fmt.Printf("%s%s%s%s → %s%v%s\n", prefix, colorDim, k, colorReset, color, val, colorReset)
		default:
			fmt.Printf("%s%s%s%s → %v\n", prefix, colorDim, k, colorReset, v)
		}
	}
}

// ────────────────────── tracking DB probe ──────────────────────

func probeTrackingDB() {
	section("💾", "Tracking DB: ai-code-tracking.db")
	dbPath := trackingDBPath()
	kvLine("Path", dbPath)

	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?mode=ro", dbPath))
	if err != nil {
		failLine(fmt.Sprintf("Cannot open: %v", err))
		return
	}
	defer db.Close()

	// List all tables
	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type='table' ORDER BY name")
	if err != nil {
		failLine(fmt.Sprintf("Cannot list tables: %v", err))
		return
	}
	var tables []string
	for rows.Next() {
		var name string
		rows.Scan(&name)
		tables = append(tables, name)
	}
	rows.Close()
	kvLine("Tables", strings.Join(tables, ", "))

	// For each table, show schema and row count
	for _, table := range tables {
		fmt.Printf("\n    %s📋 %s%s\n", colorCyan, table, colorReset)

		// Row count
		var count int
		db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM [%s]", table)).Scan(&count)
		fmt.Printf("      Rows: %s%d%s\n", colorYellow, count, colorReset)

		// Schema
		schemaRows, err := db.Query(fmt.Sprintf("PRAGMA table_info([%s])", table))
		if err != nil {
			continue
		}
		var cols []string
		for schemaRows.Next() {
			var cid int
			var name, dtype string
			var notnull, pk int
			var dflt sql.NullString
			schemaRows.Scan(&cid, &name, &dtype, &notnull, &dflt, &pk)
			marker := ""
			if pk > 0 {
				marker = " PK"
			}
			cols = append(cols, fmt.Sprintf("%s(%s%s)", name, dtype, marker))
		}
		schemaRows.Close()
		fmt.Printf("      Columns: %s%s%s\n", colorDim, strings.Join(cols, ", "), colorReset)

		// Sample data
		if count > 0 {
			sampleRows, err := db.Query(fmt.Sprintf("SELECT * FROM [%s] LIMIT 1", table))
			if err != nil {
				continue
			}
			colNames, _ := sampleRows.Columns()
			values := make([]interface{}, len(colNames))
			valuePtrs := make([]interface{}, len(colNames))
			for i := range values {
				valuePtrs[i] = &values[i]
			}
			if sampleRows.Next() {
				sampleRows.Scan(valuePtrs...)
				fmt.Printf("      %sSample row:%s\n", colorDim, colorReset)
				for i, col := range colNames {
					val := fmt.Sprintf("%v", values[i])
					if len(val) > 80 {
						val = val[:80] + "..."
					}
					fmt.Printf("        %s%s%s = %s\n", colorDim, col, colorReset, val)
				}
			}
			sampleRows.Close()
		}

		// Source breakdown for ai_code_hashes
		if table == "ai_code_hashes" {
			fmt.Printf("\n      %sSource breakdown:%s\n", colorBold, colorReset)
			srcRows, err := db.Query("SELECT COALESCE(source, '(null)'), COUNT(*) FROM ai_code_hashes GROUP BY source ORDER BY COUNT(*) DESC")
			if err == nil {
				var totalReqs int
				type srcEntry struct {
					name  string
					count int
				}
				var entries []srcEntry
				for srcRows.Next() {
					var source string
					var cnt int
					srcRows.Scan(&source, &cnt)
					entries = append(entries, srcEntry{source, cnt})
					totalReqs += cnt
				}
				srcRows.Close()
				for _, e := range entries {
					pct := float64(e.count) / float64(totalReqs) * 100
					bar := miniBar(float64(e.count), float64(totalReqs), 20)
					fmt.Printf("        %s %s%-20s%s %s%6d%s (%s%.1f%%%s)\n",
						bar, colorWhite, e.name, colorReset,
						colorYellow, e.count, colorReset,
						colorDim, pct, colorReset)
				}
			}

			// File extension breakdown
			fmt.Printf("\n      %sFile extension breakdown (top 15):%s\n", colorBold, colorReset)
			extRows, err := db.Query(`SELECT COALESCE(fileExtension, '(none)'), COUNT(*) FROM ai_code_hashes GROUP BY fileExtension ORDER BY COUNT(*) DESC LIMIT 15`)
			if err == nil {
				var firstCount int
				first := true
				for extRows.Next() {
					var ext string
					var cnt int
					extRows.Scan(&ext, &cnt)
					if first {
						firstCount = cnt
						first = false
					}
					bar := miniBar(float64(cnt), float64(firstCount), 15)
					fmt.Printf("        %s %s%-15s%s %s%6d%s\n",
						bar, colorWhite, ext, colorReset, colorYellow, cnt, colorReset)
				}
				extRows.Close()
			}

			// Model breakdown
			fmt.Printf("\n      %sModel breakdown (top 10):%s\n", colorBold, colorReset)
			modelRows, err := db.Query(`SELECT COALESCE(model, '(none)'), COUNT(*) FROM ai_code_hashes GROUP BY model ORDER BY COUNT(*) DESC LIMIT 10`)
			if err == nil {
				var firstCount int
				first := true
				for modelRows.Next() {
					var model string
					var cnt int
					modelRows.Scan(&model, &cnt)
					if first {
						firstCount = cnt
						first = false
					}
					bar := miniBar(float64(cnt), float64(firstCount), 15)
					fmt.Printf("        %s %s%-40s%s %s%6d%s\n",
						bar, colorWhite, truncStr(model, 40), colorReset, colorYellow, cnt, colorReset)
				}
				modelRows.Close()
			}

			// Daily trend (last 14 days)
			fmt.Printf("\n      %sDaily request trend (last 14 days):%s\n", colorBold, colorReset)
			trendRows, err := db.Query(`
				SELECT strftime('%Y-%m-%d', createdAt/1000, 'unixepoch', 'localtime') as day, COUNT(*)
				FROM ai_code_hashes
				WHERE createdAt >= (strftime('%s', 'now', '-14 days') * 1000)
				GROUP BY day ORDER BY day`)
			if err == nil {
				var maxDayCount int
				type dayEntry struct {
					day   string
					count int
				}
				var days []dayEntry
				for trendRows.Next() {
					var day string
					var cnt int
					trendRows.Scan(&day, &cnt)
					days = append(days, dayEntry{day, cnt})
					if cnt > maxDayCount {
						maxDayCount = cnt
					}
				}
				trendRows.Close()
				for _, d := range days {
					bar := miniBar(float64(d.count), float64(maxDayCount), 30)
					fmt.Printf("        %s%s%s %s %s%5d%s\n",
						colorDim, d.day, colorReset, bar, colorYellow, d.count, colorReset)
				}
			}
		}

		// Scored commits details
		if table == "scored_commits" && count > 0 {
			fmt.Printf("\n      %sAI contribution stats:%s\n", colorBold, colorReset)
			var avgAI sql.NullFloat64
			db.QueryRow(`SELECT AVG(CAST(v2AiPercentage AS REAL)) FROM scored_commits WHERE v2AiPercentage IS NOT NULL AND v2AiPercentage != ''`).Scan(&avgAI)
			if avgAI.Valid {
				bar := miniBar(avgAI.Float64, 100, 20)
				fmt.Printf("        Avg AI %%: %s %s%.1f%%%s\n", bar, colorYellow, avgAI.Float64, colorReset)
			}

			var totalAdded, totalRemoved sql.NullInt64
			db.QueryRow(`SELECT SUM(linesAdded), SUM(linesDeleted) FROM scored_commits`).Scan(&totalAdded, &totalRemoved)
			if totalAdded.Valid {
				kvLine("Total lines added", fmt.Sprintf("%d", totalAdded.Int64))
				kvLine("Total lines removed", fmt.Sprintf("%d", totalRemoved.Int64))
			}
		}
	}
}

// ────────────────────── state DB probe ──────────────────────

func probeStateDB() {
	section("💾", "State DB: state.vscdb")
	dbPath := stateDBPath()
	kvLine("Path", dbPath)

	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?mode=ro", dbPath))
	if err != nil {
		failLine(fmt.Sprintf("Cannot open: %v", err))
		return
	}
	defer db.Close()

	// List tables
	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type='table' ORDER BY name")
	if err != nil {
		failLine(fmt.Sprintf("Cannot list tables: %v", err))
		return
	}
	var tables []string
	for rows.Next() {
		var name string
		rows.Scan(&name)
		tables = append(tables, name)
	}
	rows.Close()
	kvLine("Tables", strings.Join(tables, ", "))

	// ItemTable: interesting keys
	fmt.Printf("\n    %s📋 ItemTable — interesting keys:%s\n", colorCyan, colorReset)

	interestingPrefixes := []string{
		"cursorAuth/",
		"cursorSettings/",
		"cursor.",
		"freeBestOfN.",
		"aiCodeTracking.",
		"feature",
		"telemetry",
		"usage",
		"team",
		"billing",
		"subscription",
		"notification",
	}

	keyRows, err := db.Query("SELECT key, LENGTH(value) FROM ItemTable ORDER BY key")
	if err == nil {
		for keyRows.Next() {
			var key string
			var valLen int
			keyRows.Scan(&key, &valLen)
			interesting := false
			for _, prefix := range interestingPrefixes {
				if strings.HasPrefix(strings.ToLower(key), strings.ToLower(prefix)) {
					interesting = true
					break
				}
			}
			if interesting {
				// Read value for small entries
				var value string
				db.QueryRow("SELECT value FROM ItemTable WHERE key = ?", key).Scan(&value)
				display := value
				if len(display) > 80 {
					display = display[:80] + "..."
				}
				if strings.Contains(key, "Token") || strings.Contains(key, "token") || strings.Contains(key, "secret") {
					display = fmt.Sprintf("[%d chars]", len(value))
				}
				fmt.Printf("      %s%-45s%s %s%s%s\n", colorDim, key, colorReset, colorWhite, display, colorReset)
			}
		}
		keyRows.Close()
	}

	// Count all keys by prefix
	fmt.Printf("\n    %s📋 ItemTable — key prefix distribution:%s\n", colorCyan, colorReset)
	prefixRows, err := db.Query(`
		SELECT
			CASE
				WHEN key LIKE 'cursorAuth/%' THEN 'cursorAuth/'
				WHEN key LIKE 'cursorSettings/%' THEN 'cursorSettings/'
				WHEN key LIKE 'cursor.%' THEN 'cursor.'
				WHEN key LIKE 'aiCodeTracking%' THEN 'aiCodeTracking.'
				WHEN key LIKE 'freeBestOfN%' THEN 'freeBestOfN.'
				WHEN key LIKE 'workbench%' THEN 'workbench.'
				WHEN key LIKE 'terminal%' THEN 'terminal.'
				WHEN key LIKE 'debug%' THEN 'debug.'
				WHEN key LIKE 'editor%' THEN 'editor.'
				WHEN key LIKE 'git%' THEN 'git.'
				WHEN key LIKE 'explorer%' THEN 'explorer.'
				ELSE '(other)'
			END as prefix,
			COUNT(*) as cnt
		FROM ItemTable
		GROUP BY prefix
		ORDER BY cnt DESC
	`)
	if err == nil {
		for prefixRows.Next() {
			var prefix string
			var cnt int
			prefixRows.Scan(&prefix, &cnt)
			fmt.Printf("      %s%-30s%s %s%d keys%s\n", colorDim, prefix, colorReset, colorYellow, cnt, colorReset)
		}
		prefixRows.Close()
	}

	// cursorDiskKV: composer sessions
	fmt.Printf("\n    %s📋 cursorDiskKV — composer sessions:%s\n", colorCyan, colorReset)
	var composerCount int
	db.QueryRow("SELECT COUNT(*) FROM cursorDiskKV WHERE key LIKE 'composerData:%'").Scan(&composerCount)
	kvLine("Total composer sessions", fmt.Sprintf("%d", composerCount))

	// Mode breakdown from composer sessions
	modeRows, err := db.Query(`
		SELECT json_extract(value, '$.unifiedMode') as mode, COUNT(*) as cnt
		FROM cursorDiskKV
		WHERE key LIKE 'composerData:%'
		  AND json_extract(value, '$.unifiedMode') IS NOT NULL
		GROUP BY mode ORDER BY cnt DESC`)
	if err == nil {
		fmt.Printf("      %sMode breakdown:%s\n", colorBold, colorReset)
		for modeRows.Next() {
			var mode sql.NullString
			var cnt int
			modeRows.Scan(&mode, &cnt)
			name := "(null)"
			if mode.Valid {
				name = mode.String
			}
			bar := miniBar(float64(cnt), float64(composerCount), 15)
			fmt.Printf("        %s %s%-15s%s %s%d%s\n",
				bar, colorWhite, name, colorReset, colorYellow, cnt, colorReset)
		}
		modeRows.Close()
	}

	// Sample a composer session to see ALL available JSON fields
	fmt.Printf("\n      %sSample composer session fields:%s\n", colorBold, colorReset)
	var sampleJSON string
	err = db.QueryRow(`
		SELECT value FROM cursorDiskKV
		WHERE key LIKE 'composerData:%'
		  AND json_extract(value, '$.usageData') IS NOT NULL
		ORDER BY json_extract(value, '$.createdAt') DESC
		LIMIT 1`).Scan(&sampleJSON)
	if err == nil {
		var parsed map[string]interface{}
		if json.Unmarshal([]byte(sampleJSON), &parsed) == nil {
			keys := make([]string, 0, len(parsed))
			for k := range parsed {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				v := parsed[k]
				switch val := v.(type) {
				case map[string]interface{}:
					fmt.Printf("        %s%-30s%s → {%d keys}\n", colorCyan, k, colorReset, len(val))
					subkeys := make([]string, 0, len(val))
					for sk := range val {
						subkeys = append(subkeys, sk)
					}
					sort.Strings(subkeys)
					for _, sk := range subkeys {
						sv := val[sk]
						fmt.Printf("          %s%-28s%s → %v\n", colorDim, sk, colorReset, truncStr(fmt.Sprintf("%v", sv), 60))
					}
				case []interface{}:
					fmt.Printf("        %s%-30s%s → [%d items]\n", colorCyan, k, colorReset, len(val))
				case string:
					fmt.Printf("        %s%-30s%s → %s\"%s\"%s\n", colorDim, k, colorReset, colorGreen, truncStr(val, 60), colorReset)
				case float64:
					if val == float64(int64(val)) {
						fmt.Printf("        %s%-30s%s → %s%.0f%s\n", colorDim, k, colorReset, colorYellow, val, colorReset)
					} else {
						fmt.Printf("        %s%-30s%s → %s%.4f%s\n", colorDim, k, colorReset, colorYellow, val, colorReset)
					}
				case bool:
					fmt.Printf("        %s%-30s%s → %v\n", colorDim, k, colorReset, val)
				default:
					fmt.Printf("        %s%-30s%s → %v\n", colorDim, k, colorReset, truncStr(fmt.Sprintf("%v", v), 60))
				}
			}
		}
	}

	// cursorDiskKV: other key patterns
	fmt.Printf("\n    %s📋 cursorDiskKV — all key prefixes:%s\n", colorCyan, colorReset)
	kvPrefixRows, err := db.Query(`
		SELECT
			CASE
				WHEN key LIKE 'composerData:%' THEN 'composerData:'
				WHEN key LIKE 'globalState:%' THEN 'globalState:'
				WHEN key LIKE 'chat:%' THEN 'chat:'
				WHEN key LIKE 'terminal:%' THEN 'terminal:'
				WHEN key LIKE 'bugReport:%' THEN 'bugReport:'
				WHEN key LIKE 'notepads:%' THEN 'notepads:'
				WHEN key LIKE 'context:%' THEN 'context:'
				ELSE SUBSTR(key, 1, INSTR(key || ':', ':'))
			END as prefix,
			COUNT(*) as cnt
		FROM cursorDiskKV
		GROUP BY prefix
		ORDER BY cnt DESC
	`)
	if err == nil {
		for kvPrefixRows.Next() {
			var prefix string
			var cnt int
			kvPrefixRows.Scan(&prefix, &cnt)
			fmt.Printf("      %s%-30s%s %s%d entries%s\n", colorDim, prefix, colorReset, colorYellow, cnt, colorReset)
		}
		kvPrefixRows.Close()
	}
}

// ────────────────────── filesystem probe ──────────────────────

func probeCursorFiles() {
	section("📁", "~/.cursor/")
	cursorDir := filepath.Join(homeDir(), ".cursor")
	walkDir(cursorDir, 0, 3)

	section("📁", "Cursor App Support")
	appDir := cursorAppSupportDir()
	walkDir(appDir, 0, 2)
}

func walkDir(dir string, depth, maxDepth int) {
	if depth > maxDepth {
		return
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if depth == 0 {
			failLine(fmt.Sprintf("Cannot read: %v", err))
		}
		return
	}

	indent := strings.Repeat("  ", depth)
	for _, e := range entries {
		name := e.Name()
		// Skip noisy directories
		if name == "node_modules" || name == "CachedData" || name == "Cache" ||
			name == "GPUCache" || name == "blob_storage" || name == "Code Cache" ||
			name == "DawnCache" || name == "GraphiteDawnCache" ||
			name == "Local Storage" || name == "Session Storage" ||
			name == "shared_proto_db" || name == "WebStorage" ||
			name == "Crashpad" || name == "Service Worker" {
			fmt.Printf("    %s%s📂 %s/ %s(skipped)%s\n", indent, colorDim, name, colorDim, colorReset)
			continue
		}

		if e.IsDir() {
			fmt.Printf("    %s%s📂 %s/%s\n", indent, colorBlue, name, colorReset)
			walkDir(filepath.Join(dir, name), depth+1, maxDepth)
		} else {
			info, _ := e.Info()
			size := ""
			if info != nil {
				s := info.Size()
				if s > 1024*1024 {
					size = fmt.Sprintf("%.1f MB", float64(s)/1024/1024)
				} else if s > 1024 {
					size = fmt.Sprintf("%.1f KB", float64(s)/1024)
				} else {
					size = fmt.Sprintf("%d B", s)
				}
			}
			icon := "📄"
			if strings.HasSuffix(name, ".db") || strings.HasSuffix(name, ".vscdb") {
				icon = "💾"
			} else if strings.HasSuffix(name, ".json") {
				icon = "📋"
			} else if strings.HasSuffix(name, ".log") {
				icon = "📝"
			}
			fmt.Printf("    %s%s %s%s%s %s%s%s\n", indent, icon, colorWhite, name, colorReset, colorDim, size, colorReset)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
