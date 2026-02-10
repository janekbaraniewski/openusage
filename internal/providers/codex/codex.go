// Package codex implements a QuotaProvider that reads OpenAI Codex CLI's
// local session data for rich quota and usage tracking.
//
// Data sources:
//   - ~/.codex/sessions/<year>/<month>/<day>/rollout-*.jsonl — per-session JSONL
//     containing token_count events with rate limits, token usage, and credit info
//   - ~/.codex/auth.json — OAuth account info (email, plan type, account_id)
//   - ~/.codex/config.toml — current model configuration
//   - ~/.codex/version.json — CLI version
//
// The session JSONL files are the primary data source. Each session emits
// token_count events that contain:
//   - total_token_usage: input, cached_input, output, reasoning tokens
//   - rate_limits.primary: used_percent, window_minutes, resets_at
//   - rate_limits.secondary: used_percent, window_minutes, resets_at
//   - rate_limits.credits: has_credits, unlimited, balance
//   - model_context_window
package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/janekbaraniewski/agentusage/internal/core"
)

// pricingSummary lists approximate OpenAI model pricing used by Codex CLI (USD per 1M tokens, 2025).
const pricingSummary = "Approx USD per 1M tokens (OpenAI models used by Codex) · " +
	"o3: $2 input / $8 output · " +
	"o3-pro: $20 input / $80 output · " +
	"o4-mini: $1.10 input / $4.40 output · " +
	"o3-mini: $1.10 input / $4.40 output · " +
	"GPT-4.1: $2 input / $8 output · " +
	"GPT-4.1 mini: $0.40 input / $1.60 output · " +
	"GPT-4.1 nano: $0.10 input / $0.40 output · " +
	"GPT-4o: $2.50 input / $10 output · " +
	"GPT-4o mini: $0.15 input / $0.60 output · " +
	"Codex CLI uses credits-based billing; $5 free credits for new accounts"

// Provider reads Codex CLI's local session and config data.
type Provider struct{}

// New returns a new Codex provider instance.
func New() *Provider { return &Provider{} }

func (p *Provider) ID() string { return "codex" }

func (p *Provider) Describe() core.ProviderInfo {
	return core.ProviderInfo{
		Name:         "OpenAI Codex CLI",
		Capabilities: []string{"local_sessions", "rate_limits", "token_usage", "credits"},
		DocURL:       "https://github.com/openai/codex",
	}
}

// sessionEvent represents a single event from a Codex session JSONL file.
type sessionEvent struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

// eventPayload is the inner payload for event_msg type entries.
type eventPayload struct {
	Type       string      `json:"type"`
	Info       *tokenInfo  `json:"info,omitempty"`
	RateLimits *rateLimits `json:"rate_limits,omitempty"`
}

// tokenInfo holds token usage from a token_count event.
type tokenInfo struct {
	TotalTokenUsage    tokenUsage `json:"total_token_usage"`
	LastTokenUsage     tokenUsage `json:"last_token_usage"`
	ModelContextWindow int        `json:"model_context_window"`
}

// tokenUsage holds individual token counts.
type tokenUsage struct {
	InputTokens           int `json:"input_tokens"`
	CachedInputTokens     int `json:"cached_input_tokens"`
	OutputTokens          int `json:"output_tokens"`
	ReasoningOutputTokens int `json:"reasoning_output_tokens"`
	TotalTokens           int `json:"total_tokens"`
}

// rateLimits holds the rate limit information from a token_count event.
type rateLimits struct {
	Primary   *rateLimitBucket `json:"primary,omitempty"`
	Secondary *rateLimitBucket `json:"secondary,omitempty"`
	Credits   *creditInfo      `json:"credits,omitempty"`
	PlanType  *string          `json:"plan_type,omitempty"`
}

// rateLimitBucket holds a single rate limit bucket.
type rateLimitBucket struct {
	UsedPercent   float64 `json:"used_percent"`
	WindowMinutes int     `json:"window_minutes"`
	ResetsAt      int64   `json:"resets_at"` // Unix timestamp
}

// creditInfo holds credit/balance information.
type creditInfo struct {
	HasCredits bool     `json:"has_credits"`
	Unlimited  bool     `json:"unlimited"`
	Balance    *float64 `json:"balance"`
}

// versionInfo mirrors ~/.codex/version.json.
type versionInfo struct {
	LatestVersion string `json:"latest_version"`
	LastCheckedAt string `json:"last_checked_at"`
}

func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.QuotaSnapshot, error) {
	snap := core.QuotaSnapshot{
		ProviderID: p.ID(),
		AccountID:  acct.ID,
		Timestamp:  time.Now(),
		Status:     core.StatusOK,
		Metrics:    make(map[string]core.Metric),
		Resets:     make(map[string]time.Time),
		Raw:        make(map[string]string),
	}

	configDir := ""
	if acct.ExtraData != nil {
		configDir = acct.ExtraData["config_dir"]
	}
	if configDir == "" {
		home, _ := os.UserHomeDir()
		if home != "" {
			configDir = filepath.Join(home, ".codex")
		}
	}

	if configDir == "" {
		snap.Status = core.StatusError
		snap.Message = "Cannot determine Codex config directory"
		return snap, nil
	}

	var hasData bool

	// 1. Read the latest session for rate limits and token usage
	sessionsDir := filepath.Join(configDir, "sessions")
	if acct.ExtraData != nil && acct.ExtraData["sessions_dir"] != "" {
		sessionsDir = acct.ExtraData["sessions_dir"]
	}

	if err := p.readLatestSession(sessionsDir, &snap); err != nil {
		snap.Raw["session_error"] = err.Error()
	} else {
		hasData = true
	}

	// 2. Read version.json
	versionFile := filepath.Join(configDir, "version.json")
	if data, err := os.ReadFile(versionFile); err == nil {
		var ver versionInfo
		if json.Unmarshal(data, &ver) == nil && ver.LatestVersion != "" {
			snap.Raw["cli_version"] = ver.LatestVersion
		}
	}

	// 3. Add account metadata from detection
	if acct.ExtraData != nil {
		if email := acct.ExtraData["email"]; email != "" {
			snap.Raw["account_email"] = email
		}
		if planType := acct.ExtraData["plan_type"]; planType != "" {
			snap.Raw["plan_type"] = planType
		}
		if accountID := acct.ExtraData["account_id"]; accountID != "" {
			snap.Raw["account_id"] = accountID
		}
	}

	if !hasData {
		snap.Status = core.StatusUnknown
		snap.Message = "No Codex session data found"
		return snap, nil
	}

	snap.Raw["pricing_summary"] = pricingSummary

	snap.Message = "Codex CLI session data"
	return snap, nil
}

// readLatestSession finds the most recent session JSONL file and extracts
// the last token_count event for rate limit and token usage data.
func (p *Provider) readLatestSession(sessionsDir string, snap *core.QuotaSnapshot) error {
	latestFile, err := findLatestSessionFile(sessionsDir)
	if err != nil {
		return fmt.Errorf("finding latest session: %w", err)
	}

	snap.Raw["latest_session_file"] = filepath.Base(latestFile)

	// Parse the session JSONL to find the last token_count event
	lastPayload, err := findLastTokenCount(latestFile)
	if err != nil {
		return fmt.Errorf("reading session: %w", err)
	}

	if lastPayload == nil {
		return fmt.Errorf("no token_count events in latest session")
	}

	// Extract token usage metrics
	if lastPayload.Info != nil {
		info := lastPayload.Info
		total := info.TotalTokenUsage

		inputTokens := float64(total.InputTokens)
		snap.Metrics["session_input_tokens"] = core.Metric{
			Used:   &inputTokens,
			Unit:   "tokens",
			Window: "session",
		}

		outputTokens := float64(total.OutputTokens)
		snap.Metrics["session_output_tokens"] = core.Metric{
			Used:   &outputTokens,
			Unit:   "tokens",
			Window: "session",
		}

		cachedTokens := float64(total.CachedInputTokens)
		snap.Metrics["session_cached_tokens"] = core.Metric{
			Used:   &cachedTokens,
			Unit:   "tokens",
			Window: "session",
		}

		if total.ReasoningOutputTokens > 0 {
			reasoning := float64(total.ReasoningOutputTokens)
			snap.Metrics["session_reasoning_tokens"] = core.Metric{
				Used:   &reasoning,
				Unit:   "tokens",
				Window: "session",
			}
		}

		totalTokens := float64(total.TotalTokens)
		snap.Metrics["session_total_tokens"] = core.Metric{
			Used:   &totalTokens,
			Unit:   "tokens",
			Window: "session",
		}

		if info.ModelContextWindow > 0 {
			ctxWindow := float64(info.ModelContextWindow)
			ctxUsed := float64(total.InputTokens)
			snap.Metrics["context_window"] = core.Metric{
				Limit: &ctxWindow,
				Used:  &ctxUsed,
				Unit:  "tokens",
			}
		}
	}

	// Extract rate limits
	if lastPayload.RateLimits != nil {
		rl := lastPayload.RateLimits

		if rl.Primary != nil {
			limit := float64(100)
			used := rl.Primary.UsedPercent
			remaining := 100 - used
			windowStr := formatWindow(rl.Primary.WindowMinutes)
			snap.Metrics["rate_limit_primary"] = core.Metric{
				Limit:     &limit,
				Used:      &used,
				Remaining: &remaining,
				Unit:      "%",
				Window:    windowStr,
			}

			if rl.Primary.ResetsAt > 0 {
				resetTime := time.Unix(rl.Primary.ResetsAt, 0)
				snap.Resets["rate_limit_primary"] = resetTime
			}
		}

		if rl.Secondary != nil {
			limit := float64(100)
			used := rl.Secondary.UsedPercent
			remaining := 100 - used
			windowStr := formatWindow(rl.Secondary.WindowMinutes)
			snap.Metrics["rate_limit_secondary"] = core.Metric{
				Limit:     &limit,
				Used:      &used,
				Remaining: &remaining,
				Unit:      "%",
				Window:    windowStr,
			}

			if rl.Secondary.ResetsAt > 0 {
				resetTime := time.Unix(rl.Secondary.ResetsAt, 0)
				snap.Resets["rate_limit_secondary"] = resetTime
			}
		}

		// Determine overall status from rate limits
		if rl.Primary != nil && rl.Primary.UsedPercent >= 90 {
			snap.Status = core.StatusNearLimit
		}
		if rl.Secondary != nil && rl.Secondary.UsedPercent >= 90 {
			snap.Status = core.StatusNearLimit
		}
		if rl.Primary != nil && rl.Primary.UsedPercent >= 100 {
			snap.Status = core.StatusLimited
		}
		if rl.Secondary != nil && rl.Secondary.UsedPercent >= 100 {
			snap.Status = core.StatusLimited
		}

		if rl.Credits != nil {
			if rl.Credits.Unlimited {
				snap.Raw["credits"] = "unlimited"
			} else if rl.Credits.HasCredits {
				snap.Raw["credits"] = "available"
				if rl.Credits.Balance != nil {
					snap.Raw["credit_balance"] = fmt.Sprintf("$%.2f", *rl.Credits.Balance)
				}
			} else {
				snap.Raw["credits"] = "none"
			}
		}

		if rl.PlanType != nil {
			snap.Raw["plan_type"] = *rl.PlanType
		}
	}

	return nil
}

// findLatestSessionFile finds the most recently modified session JSONL file.
func findLatestSessionFile(sessionsDir string) (string, error) {
	var files []string

	err := filepath.Walk(sessionsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if !info.IsDir() && strings.HasSuffix(path, ".jsonl") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("walking sessions dir: %w", err)
	}

	if len(files) == 0 {
		return "", fmt.Errorf("no session files found in %s", sessionsDir)
	}

	// Sort by modification time, newest first
	sort.Slice(files, func(i, j int) bool {
		si, _ := os.Stat(files[i])
		sj, _ := os.Stat(files[j])
		if si == nil || sj == nil {
			return false
		}
		return si.ModTime().After(sj.ModTime())
	})

	return files[0], nil
}

// findLastTokenCount reads a session JSONL file and returns the last
// token_count event payload.
func findLastTokenCount(path string) (*eventPayload, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lastPayload *eventPayload

	scanner := bufio.NewScanner(f)
	// Increase buffer for potentially large JSONL lines
	buf := make([]byte, 0, 256*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		var event sessionEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}

		if event.Type != "event_msg" {
			continue
		}

		var payload eventPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			continue
		}

		if payload.Type == "token_count" {
			lastPayload = &payload
		}
	}

	return lastPayload, scanner.Err()
}

// formatWindow converts window minutes to a human-readable string.
func formatWindow(minutes int) string {
	if minutes <= 0 {
		return ""
	}
	if minutes < 60 {
		return fmt.Sprintf("%dm", minutes)
	}
	hours := minutes / 60
	remaining := minutes % 60
	if remaining == 0 {
		if hours >= 24 {
			days := hours / 24
			leftover := hours % 24
			if leftover == 0 {
				return fmt.Sprintf("%dd", days)
			}
			return fmt.Sprintf("%dd%dh", days, leftover)
		}
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh%dm", hours, remaining)
}
