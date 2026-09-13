package codex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func stubCodexRPC(t *testing.T, dir, body string, rpcErr error) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte(`{"tokens":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	var result codexCLIRateLimitsResult
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatal(err)
	}
	old := fetchCodexRateLimitsRPC
	t.Cleanup(func() { fetchCodexRateLimitsRPC = old })
	fetchCodexRateLimitsRPC = func(context.Context, core.AccountConfig, string) (codexCLIRateLimitsResult, error) {
		return result, rpcErr
	}
}

func TestRPCQuotaBuckets(t *testing.T) {
	var result codexCLIRateLimitsResult
	err := json.Unmarshal([]byte(`{
	 "rateLimits":{"limitId":"codex","primary":{"usedPercent":99,"windowDurationMins":300}},
	 "rateLimitsByLimitId":{
	  "codex":{"limitId":"codex","primary":{"usedPercent":34.5,"windowDurationMins":10080,"resetsAt":1789805712},"secondary":null},
	  "codex_bengalfox":{"limitId":"codex_bengalfox","limitName":"Special pool","primary":{"usedPercent":0,"windowDurationMins":300},"secondary":{"usedPercent":100,"windowDurationMins":10080}}
	 }
	}`), &result)
	if err != nil {
		t.Fatal(err)
	}
	snap := core.NewUsageSnapshot("codex", "test")
	snap.Metrics["rate_limit_secondary"] = core.Metric{Used: core.Float64Ptr(77)}
	snap.Resets["rate_limit_secondary"] = time.Now()
	if !applyCodexCLIRateLimits(result, &snap) {
		t.Fatal("no windows")
	}
	if len(snap.Metrics) != 3 {
		t.Fatalf("windows mixed or missing: %+v", snap.Metrics)
	}
	weekly := snap.Metrics["rate_limit_primary"]
	if weekly.Window != "7d" || weekly.Remaining == nil || *weekly.Remaining != 65.5 || snap.Resets["rate_limit_primary"].Unix() != 1789805712 {
		t.Fatalf("wrong weekly quota: %+v", weekly)
	}
	if _, ok := snap.Resets["rate_limit_secondary"]; ok {
		t.Fatal("stale reset retained")
	}
	if *snap.Metrics["rate_limit_codex_bengalfox_primary"].Remaining != 100 || *snap.Metrics["rate_limit_codex_bengalfox_secondary"].Remaining != 0 {
		t.Fatal("0/100 endpoints not preserved")
	}
	if snap.Raw["rate_limit_codex_bengalfox_primary_bucket"] != "Special pool" {
		t.Fatal("bucket label lost")
	}
}

func TestRPCMissingInvalidAndLegacyWindows(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		windows    int
	}{
		{"missing", `{}`, 0},
		{"null", `{"rateLimits":{"primary":null,"secondary":null}}`, 0},
		{"missing percent", `{"rateLimits":{"primary":{"windowDurationMins":300}}}`, 0},
		{"invalid percent", `{"rateLimits":{"primary":{"usedPercent":101,"windowDurationMins":300}}}`, 0},
		{"invalid duration", `{"rateLimits":{"primary":{"usedPercent":0,"windowDurationMins":-1}}}`, 0},
		{"legacy", `{"rate_limits":{"primary":{"used_percent":25,"window_minutes":300,"resets_at":1789805712}}}`, 1},
		{"identity conflict", `{"rateLimitsByLimitId":{"codex":{"limitId":"another","primary":{"usedPercent":0,"windowDurationMins":300}}}}`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var result codexCLIRateLimitsResult
			if err := json.Unmarshal([]byte(tc.body), &result); err != nil {
				t.Fatal(err)
			}
			snap := core.NewUsageSnapshot("codex", "test")
			applyCodexCLIRateLimits(result, &snap)
			if len(snap.Metrics) != tc.windows {
				t.Fatalf("got %+v", snap.Metrics)
			}
		})
	}
}

func TestRPCProcess(t *testing.T) {
	for _, tc := range []struct {
		name, script, wantErr string
		timeout               time.Duration
	}{
		{"success", `
test "$1" = app-server && test "$2" = --stdio || exit 2
read first
printf '%s\n' '{"id":1,"result":{}}'
read notification
read request
printf '%s\n' '{"id":2,"result":{"rateLimits":{"primary":{"usedPercent":0,"windowDurationMins":300}}}}'
`, "", time.Second},
		{"early exit", `exit 2`, "exit status 2", time.Second},
		{"initialize error", `read first; printf '%s\n' '{"id":1,"error":{"code":-32602,"message":"private diagnostic"}}'`, "RPC code -32602", time.Second},
		{"request error", `read first; printf '%s\n' '{"id":1,"result":{}}'; read notification; read request; printf '%s\n' '{"id":2,"error":{"code":403}}'`, "RPC code 403", time.Second},
		{"timeout", `read first; exec sleep 5`, "timed out", 100 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			binary := filepath.Join(dir, "codex")
			if err := os.WriteFile(binary, []byte("#!/bin/sh\n"+tc.script+"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), tc.timeout)
			defer cancel()
			result, err := fetchCodexRateLimitsRPCProcess(ctx, core.AccountConfig{Binary: binary}, dir)
			if tc.wantErr == "" {
				if err != nil || result.RateLimitsV2 == nil {
					t.Fatalf("result=%+v err=%v", result, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantErr) || strings.Contains(err.Error(), "private diagnostic") {
				t.Fatalf("got %v, want %s", err, tc.wantErr)
			}
		})
	}
}

func TestFetchRPCWithoutLocalHistory(t *testing.T) {
	dir := t.TempDir()
	stubCodexRPC(t, dir, `{"rateLimits":{"limitId":"codex","primary":{"usedPercent":34,"windowDurationMins":10080}}}`, nil)
	snap, err := New().Fetch(context.Background(), core.AccountConfig{ID: "isolated", Provider: "codex", RuntimeHints: map[string]string{"config_dir": dir}})
	if err != nil || snap.Status != core.StatusOK || snap.Metrics["rate_limit_primary"].Remaining == nil || *snap.Metrics["rate_limit_primary"].Remaining != 66 {
		t.Fatalf("quota without logs: %+v err=%v", snap, err)
	}
}

func TestQuotaRequestPrecedesHistoryScan(t *testing.T) {
	dir := t.TempDir()
	stubCodexRPC(t, dir, `{"rateLimits":{"primary":{"usedPercent":34,"windowDurationMins":10080}}}`, nil)
	stub := fetchCodexRateLimitsRPC
	fetchCodexRateLimitsRPC = func(ctx context.Context, acct core.AccountConfig, configDir string) (codexCLIRateLimitsResult, error) {
		// History only becomes available after the quota request. This also
		// ensures its old limits cannot overwrite the fresh RPC windows.
		if err := os.MkdirAll(filepath.Join(dir, "sessions"), 0700); err != nil {
			t.Fatal(err)
		}
		body := `{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"output_tokens":20,"total_tokens":120}},"rate_limits":{"primary":{"used_percent":99,"window_minutes":300}}}}` + "\n"
		if err := os.WriteFile(filepath.Join(dir, "sessions", "test.jsonl"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		return stub(ctx, acct, configDir)
	}
	snap, err := New().Fetch(context.Background(), core.AccountConfig{ID: "test", RuntimeHints: map[string]string{"config_dir": dir}})
	if err != nil {
		t.Fatal(err)
	}
	if snap.Metrics["session_total_tokens"].Used == nil {
		t.Fatal("history was scanned before quota")
	}
	met := snap.Metrics["rate_limit_primary"]
	if met.Window != "7d" || met.Remaining == nil || *met.Remaining != 66 {
		t.Fatalf("old history overrode RPC quota: %+v", met)
	}
}
