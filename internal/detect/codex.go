package detect

import (
	"encoding/base64"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/janekbaraniewski/agentusage/internal/core"
)

// detectCodex looks for the OpenAI Codex CLI and its local data.
//
// Codex CLI stores rich data locally at ~/.codex/:
//   - config.toml — model settings, project trust levels
//   - auth.json — OpenAI OAuth tokens (access_token, refresh_token, account_id)
//   - sessions/<year>/<month>/<day>/rollout-*.jsonl — per-session event logs
//     containing token_count events with:
//   - total_token_usage (input, cached, output, reasoning tokens)
//   - rate_limits.primary (used_percent, window_minutes, resets_at)
//   - rate_limits.secondary (used_percent, window_minutes, resets_at)
//   - rate_limits.credits (has_credits, unlimited, balance)
//   - model_context_window
//   - sqlite/codex-dev.db — automation and inbox data
//   - history.jsonl — prompt history
//   - version.json — current CLI version
//   - models_cache.json — cached model list
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

	// Extract account info from auth.json
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

// codexAuthFile mirrors the structure of ~/.codex/auth.json.
type codexAuthFile struct {
	Tokens    codexTokens `json:"tokens"`
	AccountID string      `json:"account_id"`
}

type codexTokens struct {
	IDToken      string `json:"id_token"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// extractCodexAuth reads the auth.json to extract account info.
// The id_token is a JWT that contains email and plan information.
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

	// The id_token is a JWT; decode the payload to extract email and plan type.
	// JWT format: header.payload.signature (all base64url encoded).
	if auth.Tokens.IDToken != "" {
		claims := decodeJWTPayload(auth.Tokens.IDToken)
		if claims != nil {
			if e, ok := claims["email"].(string); ok {
				email = e
			}
			// Plan type is nested in https://api.openai.com/auth
			if authData, ok := claims["https://api.openai.com/auth"].(map[string]interface{}); ok {
				if pt, ok := authData["chatgpt_plan_type"].(string); ok {
					planType = pt
				}
			}
		}
	}

	return email, accountID, planType
}

// decodeJWTPayload decodes a JWT token's payload without verifying the signature.
func decodeJWTPayload(token string) map[string]interface{} {
	parts := strings.SplitN(token, ".", 3)
	if len(parts) < 2 {
		return nil
	}

	// base64url decode the payload (part[1])
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
