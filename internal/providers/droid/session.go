package droid

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// errDroidParse marks a settings.json file that could not be JSON-decoded.
// The walker counts these so we can surface a parse_errors diagnostic.
var errDroidParse = errors.New("droid: settings.json parse error")

// droidSettings mirrors the JSON shape of Droid's per-session settings file
// at ~/.factory/sessions/<uuid>.settings.json. Upstream uses camelCase keys.
type droidSettings struct {
	Model                 string           `json:"model,omitempty"`
	ProviderLock          string           `json:"providerLock,omitempty"`
	ProviderLockTimestamp string           `json:"providerLockTimestamp,omitempty"`
	TokenUsage            *droidTokenUsage `json:"tokenUsage,omitempty"`
}

type droidTokenUsage struct {
	InputTokens         *int64 `json:"inputTokens,omitempty"`
	OutputTokens        *int64 `json:"outputTokens,omitempty"`
	CacheCreationTokens *int64 `json:"cacheCreationTokens,omitempty"`
	CacheReadTokens     *int64 `json:"cacheReadTokens,omitempty"`
	ThinkingTokens      *int64 `json:"thinkingTokens,omitempty"`
}

// droidSession is the flattened representation we emit downstream.
type droidSession struct {
	SessionID  string
	Model      string
	Provider   string
	Input      int64
	Output     int64
	CacheRead  int64
	CacheWrite int64
	Thinking   int64
	Timestamp  time.Time
}

// parseDroidSession reads one settings.json file and returns a session
// record. Returns (nil, nil) for files we can't or shouldn't surface:
// missing file, malformed JSON, no token usage, all-zero tokens.
func parseDroidSession(settingsPath string) (*droidSession, error) {
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("droid: reading %s: %w", settingsPath, err)
	}

	var settings droidSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, errDroidParse
	}
	if settings.TokenUsage == nil {
		return nil, nil
	}

	input := nonNegative(settings.TokenUsage.InputTokens)
	output := nonNegative(settings.TokenUsage.OutputTokens)
	cacheWrite := nonNegative(settings.TokenUsage.CacheCreationTokens)
	cacheRead := nonNegative(settings.TokenUsage.CacheReadTokens)
	thinking := nonNegative(settings.TokenUsage.ThinkingTokens)

	if input == 0 && output == 0 && cacheWrite == 0 && cacheRead == 0 && thinking == 0 {
		return nil, nil
	}

	// Session ID = UUID extracted from filename (strip `.settings.json`).
	base := filepath.Base(settingsPath)
	sessionID := strings.TrimSuffix(base, ".settings.json")

	// Provider: providerLock wins; fall back to inferring from model.
	provider := strings.TrimSpace(settings.ProviderLock)

	// Model: prefer settings.Model; fall back to scanning the JSONL companion.
	model := normalizeDroidModel(settings.Model)
	if model == "" {
		jsonlPath := strings.TrimSuffix(settingsPath, ".settings.json") + ".jsonl"
		if m := extractModelFromJSONL(jsonlPath); m != "" {
			model = normalizeDroidModel(m)
		}
	}
	if model == "" {
		model = defaultModelForProvider(provider)
	}
	if provider == "" {
		provider = inferProviderFromModel(model)
	}

	// Timestamp: providerLockTimestamp (RFC3339) → file mtime → zero.
	var ts time.Time
	if settings.ProviderLockTimestamp != "" {
		if parsed, err := time.Parse(time.RFC3339, settings.ProviderLockTimestamp); err == nil {
			ts = parsed.UTC()
		}
	}
	if ts.IsZero() {
		if info, statErr := os.Stat(settingsPath); statErr == nil {
			ts = info.ModTime().UTC()
		}
	}

	return &droidSession{
		SessionID:  sessionID,
		Model:      model,
		Provider:   provider,
		Input:      input,
		Output:     output,
		CacheRead:  cacheRead,
		CacheWrite: cacheWrite,
		Thinking:   thinking,
		Timestamp:  ts,
	}, nil
}

// normalizeDroidModel matches the upstream normalisation: strip `custom:`
// prefix, drop `[...]` bracket annotations, lowercase, dots to hyphens,
// collapse consecutive hyphens.
//
//	"custom:Claude-Opus-4.5-Thinking-[Anthropic]-0" -> "claude-opus-4-5-thinking-0"
func normalizeDroidModel(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	s = strings.TrimPrefix(s, "custom:")

	// Strip [...] bracket regions.
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch r {
		case '[':
			depth++
		case ']':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 {
				b.WriteRune(r)
			}
		}
	}
	s = b.String()

	// Trim trailing hyphens.
	s = strings.TrimRight(s, "-")
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, ".", "-")

	// Collapse runs of hyphens.
	var out strings.Builder
	prevHyphen := false
	for _, r := range s {
		if r == '-' {
			if prevHyphen {
				continue
			}
			prevHyphen = true
		} else {
			prevHyphen = false
		}
		out.WriteRune(r)
	}
	return out.String()
}

// extractModelFromJSONL scans up to 500 lines of the JSONL session log for a
// `Model: <name>` pattern (typically inside a system-reminder block) and
// returns the captured model text.
func extractModelFromJSONL(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	// Allow long JSONL lines.
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	const maxLines = 500
	for i := 0; i < maxLines && scanner.Scan(); i++ {
		line := scanner.Text()
		idx := strings.Index(line, "Model:")
		if idx < 0 {
			continue
		}
		after := line[idx+len("Model:"):]
		// Capture until `[`, `\`, `"`, or newline.
		end := len(after)
		for k, r := range after {
			if r == '[' || r == '\\' || r == '"' {
				end = k
				break
			}
		}
		candidate := strings.TrimSpace(after[:end])
		if candidate != "" {
			return candidate
		}
	}
	return ""
}

// inferProviderFromModel returns a best-effort provider for a model id when
// providerLock is missing. Conservative: only handles the obvious prefixes.
func inferProviderFromModel(model string) string {
	m := strings.ToLower(model)
	switch {
	case strings.HasPrefix(m, "claude") || strings.Contains(m, "opus") || strings.Contains(m, "sonnet") || strings.Contains(m, "haiku"):
		return "anthropic"
	case strings.HasPrefix(m, "gpt") || strings.HasPrefix(m, "o1") || strings.HasPrefix(m, "o3") || strings.HasPrefix(m, "o4"):
		return "openai"
	case strings.HasPrefix(m, "gemini"):
		return "google"
	case strings.HasPrefix(m, "grok"):
		return "xai"
	default:
		return "droid"
	}
}

// defaultModelForProvider returns a fallback model id when neither the
// settings.model field nor the JSONL probe produced anything.
func defaultModelForProvider(provider string) string {
	switch strings.ToLower(provider) {
	case "anthropic":
		return "claude-unknown"
	case "openai":
		return "gpt-unknown"
	case "google":
		return "gemini-unknown"
	case "xai":
		return "grok-unknown"
	case "":
		return "droid-unknown"
	default:
		return strings.ToLower(provider) + "-unknown"
	}
}

func nonNegative(p *int64) int64 {
	if p == nil {
		return 0
	}
	v := *p
	if v < 0 {
		return 0
	}
	return v
}
