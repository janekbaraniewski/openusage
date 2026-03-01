package detect

import (
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func detectZAICodingHelper(result *Result) {
	home := homeDir()
	if home == "" {
		return
	}

	configDir := filepath.Join(home, ".chelper")
	configFile := filepath.Join(configDir, "config.yaml")
	if !fileExists(configFile) {
		return
	}

	content, err := os.ReadFile(configFile)
	if err != nil {
		log.Printf("[detect] Failed reading Z.AI coding-helper config: %v", err)
		return
	}
	cfg := parseZAIHelperConfig(string(content))

	planType := sanitizeYAMLValue(cfg["plan"])
	apiKey := sanitizeYAMLValue(cfg["api_key"])
	if planType == "" && apiKey == "" {
		return
	}

	acct := core.AccountConfig{
		ID:        "zai-coding-plan-auto",
		Provider:  "zai",
		Auth:      "api_key",
		APIKeyEnv: "ZAI_API_KEY",
		ExtraData: map[string]string{
			"source":      "chelper",
			"config_file": configFile,
		},
	}
	if planType != "" {
		acct.ExtraData["plan_type"] = planType
	}
	if apiKey != "" {
		acct.Token = apiKey
	}

	addAccount(result, acct)
	log.Printf("[detect] Found Z.AI coding-helper config at %s", configFile)

	if bin := findBinary("chelper"); bin != "" {
		result.Tools = append(result.Tools, DetectedTool{
			Name:       "Z.AI Coding Helper",
			BinaryPath: bin,
			ConfigDir:  configDir,
			Type:       "cli",
		})
		log.Printf("[detect] Found Z.AI coding-helper binary at %s", bin)
	}
}

func parseZAIHelperConfig(content string) map[string]string {
	out := make(map[string]string)
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])
		out[key] = value
	}
	return out
}

func sanitizeYAMLValue(raw string) string {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.TrimPrefix(trimmed, "\"")
	trimmed = strings.TrimSuffix(trimmed, "\"")
	trimmed = strings.TrimPrefix(trimmed, "'")
	trimmed = strings.TrimSuffix(trimmed, "'")
	return strings.TrimSpace(trimmed)
}
