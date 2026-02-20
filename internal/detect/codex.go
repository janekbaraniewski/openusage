package detect

import (
	"encoding/base64"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func detectCodex(result *Result) {
	bin := findBinary("codex")
	if bin == "" {
		return
	}

	home := homeDir()
	configDir := filepath.Join(home, ".codex")

	tool := DetectedTool{
		Name:       "OpenAI Codex CLI",
		BinaryPath: bin,
		ConfigDir:  configDir,
		Type:       "cli",
	}
	result.Tools = append(result.Tools, tool)

	log.Printf("[detect] Found Codex CLI at %s", bin)

	sessionsDir := filepath.Join(configDir, "sessions")
	authFile := filepath.Join(configDir, "auth.json")

	hasSessions := dirExists(sessionsDir)
	hasAuth := fileExists(authFile)

	if !hasSessions && !hasAuth {
		log.Printf("[detect] Codex CLI found but no session/auth data at expected locations")
		return
	}

	log.Printf("[detect] Codex CLI data found (sessions=%v, auth=%v)", hasSessions, hasAuth)

	acct := core.AccountConfig{
		ID:        "codex-cli",
		Provider:  "codex",
		Auth:      "local",
		Binary:    bin,
		ExtraData: make(map[string]string),
	}

	acct.ExtraData["config_dir"] = configDir

	if hasSessions {
		acct.ExtraData["sessions_dir"] = sessionsDir
	}

	if hasAuth {
		acct.ExtraData["auth_file"] = authFile
		email, accountID, planType := extractCodexAuth(authFile)
		if email != "" {
			acct.ExtraData["email"] = email
			log.Printf("[detect] Codex account: %s", email)
		}
		if accountID != "" {
			acct.ExtraData["account_id"] = accountID
		}
		if planType != "" {
			acct.ExtraData["plan_type"] = planType
			log.Printf("[detect] Codex plan: %s", planType)
		}
	}

	addAccount(result, acct)
}

type codexAuthFile struct {
	Tokens    codexTokens `json:"tokens"`
	AccountID string      `json:"account_id"`
}

type codexTokens struct {
	IDToken      string `json:"id_token"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

func extractCodexAuth(authFile string) (email, accountID, planType string) {
	data, err := os.ReadFile(authFile)
	if err != nil {
		log.Printf("[detect] Cannot read Codex auth.json: %v", err)
		return "", "", ""
	}

	var auth codexAuthFile
	if err := json.Unmarshal(data, &auth); err != nil {
		log.Printf("[detect] Cannot parse Codex auth.json: %v", err)
		return "", "", ""
	}

	accountID = auth.AccountID

	if auth.Tokens.IDToken != "" {
		claims := decodeJWTPayload(auth.Tokens.IDToken)
		if claims != nil {
			if e, ok := claims["email"].(string); ok {
				email = e
			}
			if authData, ok := claims["https://api.openai.com/auth"].(map[string]interface{}); ok {
				if pt, ok := authData["chatgpt_plan_type"].(string); ok {
					planType = pt
				}
			}
		}
	}

	return email, accountID, planType
}

func decodeJWTPayload(token string) map[string]interface{} {
	parts := strings.SplitN(token, ".", 3)
	if len(parts) < 2 {
		return nil
	}

	decoded, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}

	var claims map[string]interface{}
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return nil
	}
	return claims
}
