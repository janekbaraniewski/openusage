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
	Credits           *usageCredits       `json:"credits,omitempty"`
	IndividualLimit   *creditLimitDetails `json:"individual_limit,omitempty"`
	IndividualLimitV2 *creditLimitDetails `json:"individualLimit,omitempty"`
	PlanType          string              `json:"plan_type,omitempty"`
	PlanTypeV2        string              `json:"planType,omitempty"`
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

	candidates := make([]codexCLIRateLimitsSnapshot, 0, 1+len(result.RateLimitsByLimitID)+len(result.RateLimitsByLimitIDV2))
	if result.RateLimitsV2 != nil {
		candidates = append(candidates, *result.RateLimitsV2)
	}
	if result.RateLimits != nil {
		candidates = append(candidates, *result.RateLimits)
	}
	for _, candidate := range result.RateLimitsByLimitIDV2 {
		candidates = append(candidates, candidate)
	}
	for _, candidate := range result.RateLimitsByLimitID {
		candidates = append(candidates, candidate)
	}

	applied := false
	creditLimitApplied := false
	for _, candidate := range candidates {
		planType := core.FirstNonEmpty(candidate.PlanTypeV2, candidate.PlanType)
		if planType != "" {
			snap.Raw["plan_type"] = planType
			applied = true
		}
		if candidate.Credits != nil {
			applyUsageCredits(candidate.Credits, snap)
			applied = true
		}
		if !creditLimitApplied {
			details := firstCreditLimit(candidate.IndividualLimitV2, candidate.IndividualLimit)
			if applyCreditLimitDetails(details, snap, "cli") {
				creditLimitApplied = true
				applied = true
			}
		}
	}
	if applied {
		snap.Raw["quota_api"] = "cli_rpc"
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
	cmd := exec.CommandContext(rpcCtx, binary, "-s", "read-only", "-a", "untrusted", "app-server")
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
