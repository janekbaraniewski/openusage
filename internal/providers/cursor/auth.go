package cursor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

type authJSONFile struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
}

func resolveCursorAccessToken(acct core.AccountConfig) (string, LocalAuthInfo) {
	var auth LocalAuthInfo
	token := strings.TrimSpace(acct.Token)
	if token == "" {
		token = strings.TrimSpace(acct.Path("token", ""))
	}
	if token == "" {
		for _, path := range authFileCandidates(acct) {
			if t := readAuthJSONToken(path); t != "" {
				token = t
				break
			}
		}
	}
	if token != "" {
		return token, auth
	}

	dbPath := strings.TrimSpace(acct.Path("state_db", ""))
	if dbPath == "" && acct.Path("auth_file", "") == "" && acct.Path("config_dir", "") == "" {
		dbPath = DefaultStateDBPath()
	}
	if dbPath != "" && fileExists(dbPath) {
		if info, err := ExtractLocalAuth(dbPath); err == nil {
			auth = info
			token = strings.TrimSpace(info.AccessToken)
		}
	}
	return token, auth
}

func authFileCandidates(acct core.AccountConfig) []string {
	var out []string
	if path := strings.TrimSpace(acct.Path("auth_file", "")); path != "" {
		out = append(out, path)
	}
	configDir := strings.TrimSpace(acct.Path("config_dir", ""))
	if configDir != "" {
		out = append(out, filepath.Join(filepath.Dir(configDir), ".config", "cursor", "auth.json"))
	}
	return out
}

func readAuthJSONToken(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var file authJSONFile
	if err := json.Unmarshal(data, &file); err != nil {
		return ""
	}
	return strings.TrimSpace(file.AccessToken)
}
