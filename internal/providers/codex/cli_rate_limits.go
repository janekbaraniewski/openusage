package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

type codexCLIRateLimitsSnapshot struct {
	Credits           *usageCredits            `json:"credits,omitempty"`
	IndividualLimit   *creditLimitDetails      `json:"individual_limit,omitempty"`
	IndividualLimitV2 *creditLimitDetails      `json:"individualLimit,omitempty"`
	Primary           *codexCLIRateLimitWindow `json:"primary,omitempty"`
	PrimaryWindow     *codexCLIRateLimitWindow `json:"primary_window,omitempty"`
	Secondary         *codexCLIRateLimitWindow `json:"secondary,omitempty"`
	SecondaryWindow   *codexCLIRateLimitWindow `json:"secondary_window,omitempty"`
	PlanType          string                   `json:"plan_type,omitempty"`
	PlanTypeV2        string                   `json:"planType,omitempty"`
}

// codexCLIRateLimitWindow is the current app-server shape. Codex has used
// both camelCase and snake_case fields across app-server versions, so the
// parser accepts both forms and flexible numeric values.
type codexCLIRateLimitWindow struct {
	UsedPercent         any `json:"usedPercent,omitempty"`
	UsedPercentLegacy   any `json:"used_percent,omitempty"`
	WindowDurationMins  any `json:"windowDurationMins,omitempty"`
	WindowMinutesLegacy any `json:"window_minutes,omitempty"`
	ResetsAt            any `json:"resetsAt,omitempty"`
	ResetsAtLegacy      any `json:"resets_at,omitempty"`
}

type codexCLIRateLimitsResult struct {
	RateLimits            *codexCLIRateLimitsSnapshot           `json:"rate_limits,omitempty"`
	RateLimitsV2          *codexCLIRateLimitsSnapshot           `json:"rateLimits,omitempty"`
	RateLimitsByLimitID   map[string]codexCLIRateLimitsSnapshot `json:"rate_limits_by_limit_id,omitempty"`
	RateLimitsByLimitIDV2 map[string]codexCLIRateLimitsSnapshot `json:"rateLimitsByLimitId,omitempty"`
}

type codexCLIRateLimitsCandidate struct {
	limitID  string
	snapshot codexCLIRateLimitsSnapshot
}

type codexRPCMessage struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  json.RawMessage `json:"error,omitempty"`
}

var fetchCodexRateLimitsRPC = fetchCodexRateLimitsRPCProcess

func (p *Provider) fetchCLIRateLimits(ctx context.Context, acct core.AccountConfig, configDir string, snap *core.UsageSnapshot) (bool, error) {
	authPath := filepath.Join(configDir, "auth.json")
	if override := acct.Hint("auth_file", ""); override != "" {
		authPath = override
	}
	if _, err := os.Stat(authPath); err != nil {
		return false, nil
	}

	result, err := fetchCodexRateLimitsRPC(ctx, acct, configDir)
	if err != nil {
		return false, err
	}
	return applyCodexCLIRateLimits(result, snap), nil
}

func applyCodexCLIRateLimits(result codexCLIRateLimitsResult, snap *core.UsageSnapshot) bool {
	if snap == nil {
		return false
	}

	candidates := make([]codexCLIRateLimitsCandidate, 0, 1+len(result.RateLimitsByLimitID)+len(result.RateLimitsByLimitIDV2))
	if result.RateLimitsV2 != nil {
		candidates = append(candidates, codexCLIRateLimitsCandidate{limitID: "codex", snapshot: *result.RateLimitsV2})
	}
	if result.RateLimits != nil {
		candidates = append(candidates, codexCLIRateLimitsCandidate{limitID: "codex", snapshot: *result.RateLimits})
	}
	for limitID, candidate := range result.RateLimitsByLimitIDV2 {
		candidates = append(candidates, codexCLIRateLimitsCandidate{limitID: limitID, snapshot: candidate})
	}
	for limitID, candidate := range result.RateLimitsByLimitID {
		candidates = append(candidates, codexCLIRateLimitsCandidate{limitID: limitID, snapshot: candidate})
	}

	applied := false
	creditLimitApplied := false
	rateLimitMetricsApplied := false
	for _, candidate := range candidates {
		snapshot := candidate.snapshot
		planType := core.FirstNonEmpty(snapshot.PlanTypeV2, snapshot.PlanType)
		if planType != "" {
			snap.Raw["plan_type"] = planType
			applied = true
		}
		primaryKey, secondaryKey := "rate_limit_primary", "rate_limit_secondary"
		if limitID := sanitizeMetricName(candidate.limitID); limitID != "" && limitID != "codex" {
			primaryKey = "rate_limit_" + limitID + "_primary"
			secondaryKey = "rate_limit_" + limitID + "_secondary"
		}
		if applyCodexCLIRateLimitWindows(snapshot, snap, primaryKey, secondaryKey) > 0 {
			rateLimitMetricsApplied = true
			applied = true
		}
		if snapshot.Credits != nil {
			applyUsageCredits(snapshot.Credits, snap)
			applied = true
		}
		if !creditLimitApplied {
			details := firstCreditLimit(snapshot.IndividualLimitV2, snapshot.IndividualLimit)
			if applyCreditLimitDetails(details, snap, "cli") {
				creditLimitApplied = true
				applied = true
			}
		}
	}
	if applied {
		snap.Raw["quota_api"] = "cli_rpc"
	}
	if rateLimitMetricsApplied {
		snap.Raw["rate_limit_source"] = "cli_rpc"
	}
	return applied
}

func applyCodexCLIRateLimitWindows(
	candidate codexCLIRateLimitsSnapshot,
	snap *core.UsageSnapshot,
	primaryKey, secondaryKey string,
) int {
	primary := candidate.Primary
	if primary == nil {
		primary = candidate.PrimaryWindow
	}
	secondary := candidate.Secondary
	if secondary == nil {
		secondary = candidate.SecondaryWindow
	}

	applied := 0
	if applyCodexCLIRateLimitWindow(primary, primaryKey, snap) {
		applied++
	}
	if applyCodexCLIRateLimitWindow(secondary, secondaryKey, snap) {
		applied++
	}
	return applied
}

func applyCodexCLIRateLimitWindow(
	window *codexCLIRateLimitWindow,
	key string,
	snap *core.UsageSnapshot,
) bool {
	if window == nil || snap == nil || key == "" {
		return false
	}
	used, ok := parseFlexibleNumber(window.UsedPercent)
	if !ok {
		used, ok = parseFlexibleNumber(window.UsedPercentLegacy)
	}
	if !ok {
		return false
	}
	used = clampPercent(used)
	remaining := 100 - used
	minutes, _ := parseFlexibleNumber(window.WindowDurationMins)
	if minutes <= 0 {
		minutes, _ = parseFlexibleNumber(window.WindowMinutesLegacy)
	}
	snap.EnsureMaps()
	limit := float64(100)
	snap.Metrics[key] = core.Metric{
		Limit:     &limit,
		Used:      &used,
		Remaining: &remaining,
		Unit:      "%",
		Window:    formatWindow(int(minutes)),
	}

	resetAt, _ := parseFlexibleNumber(window.ResetsAt)
	if resetAt <= 0 {
		resetAt, _ = parseFlexibleNumber(window.ResetsAtLegacy)
	}
	if resetAt > 0 {
		snap.Resets[key] = time.Unix(int64(resetAt), 0)
	}
	return true
}

func fetchCodexRateLimitsRPCProcess(ctx context.Context, acct core.AccountConfig, configDir string) (codexCLIRateLimitsResult, error) {
	binary := acct.Binary
	if binary == "" {
		binary = acct.Hint("codex_binary", "codex")
	}
	if strings.TrimSpace(binary) == "" {
		binary = "codex"
	}

	rpcCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(rpcCtx, binary, "app-server")
	cmd.Stderr = io.Discard
	if configDir != "" {
		cmd.Env = append(os.Environ(), "CODEX_HOME="+configDir)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return codexCLIRateLimitsResult{}, fmt.Errorf("codex: creating app-server stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return codexCLIRateLimitsResult{}, fmt.Errorf("codex: creating app-server stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return codexCLIRateLimitsResult{}, fmt.Errorf("codex: starting app-server: %w", err)
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4*1024), 512*1024)

	if err := writeCodexRPCRequest(stdin, `{"id":1,"method":"initialize","params":{"clientInfo":{"name":"openusage","version":"dev"}}}`); err != nil {
		return codexCLIRateLimitsResult{}, err
	}
	if _, err := readCodexRPCResponse(scanner, 1); err != nil {
		return codexCLIRateLimitsResult{}, fmt.Errorf("codex: app-server initialize failed: %w", err)
	}
	if err := writeCodexRPCRequest(stdin, `{"method":"initialized","params":{}}`); err != nil {
		return codexCLIRateLimitsResult{}, err
	}
	if err := writeCodexRPCRequest(stdin, `{"id":2,"method":"account/rateLimits/read","params":{}}`); err != nil {
		return codexCLIRateLimitsResult{}, err
	}
	message, err := readCodexRPCResponse(scanner, 2)
	if err != nil {
		if rpcCtx.Err() != nil {
			return codexCLIRateLimitsResult{}, fmt.Errorf("codex: app-server rate limits timed out: %w", rpcCtx.Err())
		}
		return codexCLIRateLimitsResult{}, fmt.Errorf("codex: reading app-server rate limits: %w", err)
	}
	if len(message.Error) > 0 && string(message.Error) != "null" {
		return codexCLIRateLimitsResult{}, fmt.Errorf("codex: app-server rate limits error: %s", string(message.Error))
	}
	var result codexCLIRateLimitsResult
	if err := json.Unmarshal(message.Result, &result); err != nil {
		return codexCLIRateLimitsResult{}, fmt.Errorf("codex: parsing app-server rate limits: %w", err)
	}
	return result, nil
}

func writeCodexRPCRequest(stdin io.Writer, request string) error {
	if _, err := io.WriteString(stdin, request+"\n"); err != nil {
		return fmt.Errorf("codex: writing app-server request: %w", err)
	}
	return nil
}

func readCodexRPCResponse(scanner *bufio.Scanner, id int) (codexRPCMessage, error) {
	for scanner.Scan() {
		var message codexRPCMessage
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
			continue
		}
		if strings.TrimSpace(string(message.ID)) == fmt.Sprintf("%d", id) {
			return message, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return codexRPCMessage{}, fmt.Errorf("reading app-server response: %w", err)
	}
	return codexRPCMessage{}, fmt.Errorf("app-server returned no response for request %d", id)
}
