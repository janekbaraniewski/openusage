package active

import "strings"

var displayNames = map[string]string{
	"antigravity": "antigravity",
	"claude_code": "claude",
	"codex":       "codex",
	"copilot":     "copilot",
	"cursor":      "cursor",
	"gemini_cli":  "gemini",
	"gemini_api":  "gemini",
	"ollama":      "ollama",
	"opencode":    "opencode",
	"openrouter":  "openrouter",
}

// DisplayName is the short, lowercase provider name shown in a status bar.
func DisplayName(providerID string) string {
	if name, ok := displayNames[providerID]; ok {
		return name
	}
	if strings.TrimSpace(providerID) == "" {
		return "AI"
	}
	return strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(providerID, "_", " "), "-", " "))
}
