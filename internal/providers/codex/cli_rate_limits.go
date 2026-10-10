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
	"slices"
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

	rpcCtx, cancel := context.WithTimeout(ctx, codexRPCTimeout(ctx, time.Now()))
	defer cancel()
	cmd := exec.CommandContext(rpcCtx, binary, "app-server", "--stdio")
	cmd.Stderr = io.Discard
	cmd.Env = codexAppServerEnv(os.Environ(), binary, configDir)

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
	// waitProcess reaps the child at most once; rpcCtx bounds it because
	// exec.CommandContext kills the process when the context is done.
	waited := false
	var waitErr error
	waitProcess := func() error {
		if !waited {
			waited = true
			waitErr = cmd.Wait()
		}
		return waitErr
	}
	defer func() {
		if !waited && cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = waitProcess()
	}()

	// writeRequest reports the process exit status when the child dies before
	// it reads the request: the write then fails with EPIPE, which would
	// otherwise mask the more useful exit error.
	writeRequest := func(request string) error {
		writeErr := writeCodexRPCRequest(stdin, request)
		if writeErr == nil {
			return nil
		}
		_ = stdin.Close()
		exitErr := waitProcess()
		if rpcCtx.Err() != nil {
			return fmt.Errorf("codex: app-server request timed out: %w", rpcCtx.Err())
		}
		if exitErr != nil {
			return fmt.Errorf("codex: app-server exited before request: %w", exitErr)
		}
		return writeErr
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4*1024), 512*1024)

	if err := writeRequest(`{"id":1,"method":"initialize","params":{"clientInfo":{"name":"openusage","version":"dev"}}}`); err != nil {
		return codexCLIRateLimitsResult{}, err
	}
	if _, err := readCodexRPCResponse(scanner, 1); err != nil {
		if rpcCtx.Err() != nil {
			return codexCLIRateLimitsResult{}, fmt.Errorf("codex: app-server initialize timed out: %w", rpcCtx.Err())
		}
		if err == io.EOF {
			return codexCLIRateLimitsResult{}, fmt.Errorf("codex: app-server exited before initialize: %w", waitProcess())
		}
		return codexCLIRateLimitsResult{}, fmt.Errorf("codex: app-server initialize failed: %w", err)
	}
	if err := writeRequest(`{"method":"initialized","params":{}}`); err != nil {
		return codexCLIRateLimitsResult{}, err
	}
	if err := writeRequest(`{"id":2,"method":"account/rateLimits/read","params":{}}`); err != nil {
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

// codexRPCMaxTimeout bounds the app-server round trip when the caller has no
// tighter deadline.
const codexRPCMaxTimeout = 8 * time.Second

// codexRPCTimeout returns how long the app-server RPC may run. The daemon
// polls each provider with an 8s deadline; letting the RPC use all of it
// left no time for the HTTP quota fallback, so the RPC gets at most half of
// the remaining budget.
func codexRPCTimeout(ctx context.Context, now time.Time) time.Duration {
	deadline, ok := ctx.Deadline()
	if !ok {
		return codexRPCMaxTimeout
	}
	half := deadline.Sub(now) / 2
	if half <= 0 {
		return time.Millisecond
	}
	return min(half, codexRPCMaxTimeout)
}

// codexAppServerEnv builds the app-server environment. When codex is resolved
// to an absolute path, its directory is put first on PATH: npm/nvm installs
// ship codex as a `#!/usr/bin/env node` script next to the node binary, and
// the daemon's launchd/systemd PATH usually does not include that directory.
func codexAppServerEnv(base []string, binary, configDir string) []string {
	env := make([]string, 0, len(base)+1)
	binDir := ""
	if filepath.IsAbs(binary) {
		binDir = filepath.Dir(binary)
	}
	pathSet := false
	for _, kv := range base {
		if binDir != "" && strings.HasPrefix(kv, "PATH=") {
			current := strings.TrimPrefix(kv, "PATH=")
			if !slices.Contains(filepath.SplitList(current), binDir) {
				if current == "" {
					kv = "PATH=" + binDir
				} else {
					kv = "PATH=" + binDir + string(os.PathListSeparator) + current
				}
			}
			pathSet = true
		}
		if configDir != "" && strings.HasPrefix(kv, "CODEX_HOME=") {
			continue
		}
		env = append(env, kv)
	}
	if binDir != "" && !pathSet {
		env = append(env, "PATH="+binDir)
	}
	if configDir != "" {
		env = append(env, "CODEX_HOME="+configDir)
	}
	return env
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
