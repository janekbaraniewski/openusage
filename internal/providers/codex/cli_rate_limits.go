package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

type codexCLIRateLimitsSnapshot struct {
	LimitID           string              `json:"limitId,omitempty"`
	LimitName         string              `json:"limitName,omitempty"`
	Primary           *codexCLIWindow     `json:"primary,omitempty"`
	Secondary         *codexCLIWindow     `json:"secondary,omitempty"`
	Credits           *usageCredits       `json:"credits,omitempty"`
	IndividualLimit   *creditLimitDetails `json:"individual_limit,omitempty"`
	IndividualLimitV2 *creditLimitDetails `json:"individualLimit,omitempty"`
	PlanType          string              `json:"plan_type,omitempty"`
	PlanTypeV2        string              `json:"planType,omitempty"`
}

type codexCLIWindow struct {
	UsedPercent        *float64 `json:"usedPercent"`
	WindowDurationMins int      `json:"windowDurationMins"`
	ResetsAt           int64    `json:"resetsAt"`
	usageWindowInfo
}

type codexCLIRateLimitsResult struct {
	RateLimits            *codexCLIRateLimitsSnapshot           `json:"rate_limits,omitempty"`
	RateLimitsV2          *codexCLIRateLimitsSnapshot           `json:"rateLimits,omitempty"`
	RateLimitsByLimitID   map[string]codexCLIRateLimitsSnapshot `json:"rate_limits_by_limit_id,omitempty"`
	RateLimitsByLimitIDV2 map[string]codexCLIRateLimitsSnapshot `json:"rateLimitsByLimitId,omitempty"`
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

	snap.EnsureMaps()
	clearRateLimitMetrics(snap)
	candidates := result.RateLimitsByLimitIDV2
	if len(candidates) == 0 {
		candidates = result.RateLimitsByLimitID
	}
	if len(candidates) == 0 {
		legacy := result.RateLimitsV2
		if legacy == nil {
			legacy = result.RateLimits
		}
		if legacy != nil {
			candidates = map[string]codexCLIRateLimitsSnapshot{core.FirstNonEmpty(legacy.LimitID, "codex"): *legacy}
		}
	}

	applied := false
	for _, id := range core.SortedStringKeys(candidates) {
		candidate := candidates[id]
		if candidate.LimitID != "" && candidate.LimitID != id {
			continue // Do not attribute a conflicting bucket to the wrong identity.
		}
		prefix := "rate_limit_" + sanitizeMetricName(id) + "_"
		if id == "codex" {
			prefix = "rate_limit_"
		}
		name := core.FirstNonEmpty(candidate.LimitName, id)
		for slot, window := range map[string]*codexCLIWindow{"primary": candidate.Primary, "secondary": candidate.Secondary} {
			key := prefix + slot
			snap.Raw[key+"_bucket"] = name
			if window == nil {
				continue
			}
			used := window.UsedPercent
			if used == nil {
				used = window.usageWindowInfo.UsedPercent
			}
			minutes := window.WindowDurationMins
			if minutes == 0 {
				minutes = resolveWindowMinutes(&window.usageWindowInfo)
			}
			if used == nil || math.IsNaN(*used) || math.IsInf(*used, 0) || *used < 0 || *used > 100 || minutes <= 0 {
				continue
			}
			snap.Metrics[key] = core.Metric{Used: core.Float64Ptr(*used), Remaining: core.Float64Ptr(100 - *used), Limit: core.Float64Ptr(100), Unit: "%", Window: formatWindow(minutes)}
			reset := window.ResetsAt
			if reset == 0 {
				reset = resolveWindowResetAt(&window.usageWindowInfo)
			}
			if reset > 0 {
				snap.Resets[key] = time.Unix(reset, 0)
			}
			applied = true
		}
		if id != "codex" {
			continue
		}
		planType := core.FirstNonEmpty(candidate.PlanTypeV2, candidate.PlanType)
		if planType != "" {
			snap.Raw["plan_type"] = planType
			applied = true
		}
		if candidate.Credits != nil {
			applyUsageCredits(candidate.Credits, snap)
			applied = true
		}
		if applyCreditLimitDetails(firstCreditLimit(candidate.IndividualLimitV2, candidate.IndividualLimit), snap, "cli") {
			applied = true
		}
	}
	if applied {
		snap.Raw["quota_api"] = "cli_rpc"
		snap.Raw["rate_limit_source"] = "cli_rpc"
	}
	return applied
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
	cmd := exec.CommandContext(rpcCtx, binary, "app-server", "--stdio")
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
		if rpcCtx.Err() != nil {
			return codexCLIRateLimitsResult{}, fmt.Errorf("codex: app-server initialize timed out: %w", rpcCtx.Err())
		}
		if err == io.EOF {
			return codexCLIRateLimitsResult{}, fmt.Errorf("codex: app-server exited before initialize: %v", cmd.Wait())
		}
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
			if len(message.Error) > 0 && string(message.Error) != "null" {
				var rpcError struct {
					Code int `json:"code"`
				}
				_ = json.Unmarshal(message.Error, &rpcError)
				return codexRPCMessage{}, fmt.Errorf("app-server request %d failed (RPC code %d)", id, rpcError.Code)
			}
			return message, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return codexRPCMessage{}, fmt.Errorf("reading app-server response: %w", err)
	}
	return codexRPCMessage{}, io.EOF
}
