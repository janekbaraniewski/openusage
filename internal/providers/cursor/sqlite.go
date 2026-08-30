package cursor

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// LocalAuthInfo is the login Cursor already stored in state.vscdb.
type LocalAuthInfo struct {
	AccessToken    string
	Email          string
	MembershipType string
	ExpiresAt      time.Time
}

// DefaultStateDBPath returns the platform path to Cursor's state.vscdb,
// including the WSL glob for a Windows Cursor install.
func DefaultStateDBPath() string {
	if envPath := strings.TrimSpace(os.Getenv("OPENUSAGE_CURSOR_STATE_DB")); envPath != "" {
		return envPath
	}
	if envPath := strings.TrimSpace(os.Getenv("CURSOR_STATE_DB")); envPath != "" {
		return envPath
	}

	for _, candidate := range stateDBCandidates() {
		if fileExists(candidate) {
			return candidate
		}
	}
	cands := stateDBCandidates()
	if len(cands) == 0 {
		return ""
	}
	return cands[0]
}

func stateDBCandidates() []string {
	var out []string
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return out
	}
	switch runtime.GOOS {
	case "darwin":
		out = append(out, filepath.Join(home, "Library", "Application Support", "Cursor", "User", "globalStorage", "state.vscdb"))
	case "windows":
		if appData := os.Getenv("APPDATA"); appData != "" {
			out = append(out, filepath.Join(appData, "Cursor", "User", "globalStorage", "state.vscdb"))
		} else {
			out = append(out, filepath.Join(home, "AppData", "Roaming", "Cursor", "User", "globalStorage", "state.vscdb"))
		}
	default:
		out = append(out, filepath.Join(home, ".config", "Cursor", "User", "globalStorage", "state.vscdb"))
		if matches, err := filepath.Glob("/mnt/c/Users/*/AppData/Roaming/Cursor/User/globalStorage/state.vscdb"); err == nil {
			out = append(out, matches...)
		}
	}
	return out
}

// ExtractLocalAuth reads credentials from Cursor's state.vscdb (read-only).
func ExtractLocalAuth(dbPath string) (LocalAuthInfo, error) {
	if strings.TrimSpace(dbPath) == "" {
		dbPath = DefaultStateDBPath()
	}
	if dbPath == "" {
		return LocalAuthInfo{}, fmt.Errorf("cursor: state database path not found")
	}
	if _, err := os.Stat(dbPath); err != nil {
		return LocalAuthInfo{}, fmt.Errorf("cursor: state database does not exist: %w", err)
	}

	dsn := fmt.Sprintf("file:%s?mode=ro&_busy_timeout=5000", dbPath)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return LocalAuthInfo{}, fmt.Errorf("cursor: open state db: %w", err)
	}
	defer db.Close()

	var info LocalAuthInfo
	_ = db.QueryRow(`SELECT value FROM ItemTable WHERE key = 'cursorAuth/accessToken'`).Scan(&info.AccessToken)
	_ = db.QueryRow(`SELECT value FROM ItemTable WHERE key = 'cursorAuth/cachedEmail'`).Scan(&info.Email)
	_ = db.QueryRow(`SELECT value FROM ItemTable WHERE key = 'cursorAuth/stripeMembershipType'`).Scan(&info.MembershipType)

	if info.AccessToken != "" {
		info.ExpiresAt = extractJWTExpiry(info.AccessToken)
		if info.Email == "" {
			info.Email = extractJWTEmail(info.AccessToken)
		}
	}
	return info, nil
}

func extractJWTExpiry(token string) time.Time {
	claims := decodeJWTClaims(token)
	if claims == nil {
		return time.Time{}
	}
	if exp, ok := claims["exp"].(float64); ok && exp > 0 {
		return time.Unix(int64(exp), 0).UTC()
	}
	return time.Time{}
}

func extractJWTEmail(token string) string {
	claims := decodeJWTClaims(token)
	if claims == nil {
		return ""
	}
	if email, ok := claims["email"].(string); ok {
		return email
	}
	return ""
}

func decodeJWTClaims(token string) map[string]any {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return nil
	}
	payload := parts[1]
	switch len(payload) % 4 {
	case 2:
		payload += "=="
	case 3:
		payload += "="
	}
	data, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		data, err = base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return nil
		}
	}
	var claims map[string]any
	if err := json.Unmarshal(data, &claims); err != nil {
		return nil
	}
	return claims
}
