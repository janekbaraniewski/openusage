package zai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func resolveAPIBases(acct core.AccountConfig) (codingBase, monitorBase, region string) {
	planType := ""
	if acct.RuntimeHints != nil {
		planType = strings.TrimSpace(acct.RuntimeHints["plan_type"])
	}

	isChina := strings.Contains(strings.ToLower(planType), "china")
	if acct.BaseURL != "" {
		base := strings.TrimRight(acct.BaseURL, "/")
		parsed, err := url.Parse(base)
		if err == nil && parsed.Scheme != "" && parsed.Host != "" {
			root := parsed.Scheme + "://" + parsed.Host
			path := strings.TrimRight(parsed.Path, "/")
			switch {
			case strings.Contains(path, "/api/coding/paas/v4"):
				codingBase = root + "/api/coding/paas/v4"
			case strings.HasSuffix(path, "/models"):
				codingBase = root + strings.TrimSuffix(path, "/models")
			case path == "" || path == "/":
				codingBase = root + "/api/coding/paas/v4"
			default:
				codingBase = root + path
			}
			monitorBase = root
			hostLower := strings.ToLower(parsed.Host)
			if strings.Contains(hostLower, "bigmodel.cn") {
				isChina = true
			}
		} else {
			codingBase = base
			monitorBase = strings.TrimSuffix(base, "/api/coding/paas/v4")
			monitorBase = strings.TrimSuffix(monitorBase, "/")
		}
	}

	if codingBase == "" || monitorBase == "" {
		if isChina {
			codingBase = defaultChinaCodingBaseURL
			monitorBase = defaultChinaMonitorBaseURL
		} else {
			codingBase = defaultGlobalCodingBaseURL
			monitorBase = defaultGlobalMonitorBaseURL
		}
	}

	region = "global"
	if isChina || strings.Contains(strings.ToLower(monitorBase), "bigmodel.cn") {
		region = "china"
	}
	return codingBase, monitorBase, region
}

// monitorOutcome categorises what evaluateMonitorEndpoint observed from a
// monitor-style endpoint response. Callers typically want to early-return
// in three of the four cases and only proceed to data extraction in
// outcomeOK; that lets each fetch* function keep its endpoint-specific
// extraction concentrated below the helper call.
type monitorOutcome int

const (
	outcomeOK        monitorOutcome = iota // envelope parsed, data present
	outcomeNoPackage                       // 429 + no-package code, OR envelope no-package code, OR empty data
	outcomeAuth                            // 401/403
	outcomeRateLimit                       // 429 without no-package code
	outcomeHTTPError                       // non-200 not handled above
)

// monitorEndpointResult is the structured return from evaluateMonitorEndpoint.
type monitorEndpointResult struct {
	Outcome  monitorOutcome
	Envelope monitorEnvelope
	Status   int
}

// evaluateMonitorEndpoint runs the shared "GET monitor endpoint, capture
// payload, classify response" pipeline used by fetchQuotaLimit /
// fetchModelUsage / fetchToolUsage. Each of those used to hand-roll the same
// 30-line block of status checks + envelope parse + no-package detection.
//
// Side effects on snap/state:
//   - body always recorded via captureEndpointPayload(name, body)
//   - on outcomeNoPackage: snap.Raw[rawKey]="limited"|"empty", state flags
//     populated as appropriate.
//   - on outcomes that abort: nothing else is touched; the caller decides
//     whether to surface the error.
//
// rawKey is the snap.Raw key prefix the caller wants for "limited"/"empty"
// markers (e.g. "quota_api", "model_usage_api"). includeTimeRange is passed
// straight through to requestMonitor.
func (p *Provider) evaluateMonitorEndpoint(
	ctx context.Context,
	monitorBase, apiKey, path string,
	includeTimeRange bool,
	name, rawKey string,
	snap *core.UsageSnapshot,
	state *providerState,
) (monitorEndpointResult, error) {
	status, body, err := p.requestMonitor(ctx, monitorBase, apiKey, path, includeTimeRange)
	if err != nil {
		return monitorEndpointResult{Status: status}, fmt.Errorf("zai: %s request failed: %w", name, err)
	}
	captureEndpointPayload(snap, name, body)

	switch {
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		return monitorEndpointResult{Outcome: outcomeAuth, Status: status}, fmt.Errorf("HTTP %d", status)
	case status == http.StatusTooManyRequests:
		code, msg := parseAPIError(body)
		if isNoPackageCode(code, msg) {
			state.limited = true
			state.noPackage = true
			state.limitedReason = "Insufficient balance or no active coding package"
			snap.Raw[rawKey] = "limited"
			return monitorEndpointResult{Outcome: outcomeNoPackage, Status: status}, nil
		}
		return monitorEndpointResult{Outcome: outcomeRateLimit, Status: status}, fmt.Errorf("HTTP 429")
	case status != http.StatusOK:
		return monitorEndpointResult{Outcome: outcomeHTTPError, Status: status}, fmt.Errorf("HTTP %d", status)
	}

	var envelope monitorEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return monitorEndpointResult{Status: status}, fmt.Errorf("parsing %s envelope: %w", name, err)
	}

	envCode := anyToString(envelope.Code)
	if envelope.Error != nil && envCode == "" {
		envCode = anyToString(envelope.Error.Code)
	}
	if isNoPackageCode(envCode, core.FirstNonEmpty(envelope.Msg, apiErrorMessage(envelope.Error))) {
		state.limited = true
		state.noPackage = true
		state.limitedReason = "Insufficient balance or no active coding package"
		snap.Raw[rawKey] = "limited"
		return monitorEndpointResult{Outcome: outcomeNoPackage, Envelope: envelope, Status: status}, nil
	}

	if isJSONEmpty(envelope.Data) {
		state.noPackage = true
		snap.Raw[rawKey] = "empty"
		return monitorEndpointResult{Outcome: outcomeNoPackage, Envelope: envelope, Status: status}, nil
	}

	return monitorEndpointResult{Outcome: outcomeOK, Envelope: envelope, Status: status}, nil
}

func doMonitorRequest(ctx context.Context, reqURL, token string, bearer bool, client *http.Client) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return 0, nil, fmt.Errorf("creating request: %w", err)
	}

	authValue := token
	if bearer {
		authValue = "Bearer " + token
	}
	req.Header.Set("Authorization", authValue)
	req.Header.Set("Accept-Language", "en-US,en")
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("reading response: %w", err)
	}
	return resp.StatusCode, body, nil
}

func applyQuotaData(raw json.RawMessage, snap *core.UsageSnapshot, state *providerState) bool {
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return false
	}

	rows := extractLimitRows(payload)
	if len(rows) == 0 {
		return false
	}

	found := false
	for _, row := range rows {
		kind := strings.ToUpper(strings.TrimSpace(firstStringFromMap(row, "type", "limitType")))
		percentage, hasPct := parseNumberFromMap(row, "percentage", "usedPercent", "used_percentage")
		if hasPct && percentage <= 1 {
			percentage *= 100
		}

		switch kind {
		case "TOKENS_LIMIT":
			if hasPct {
				snap.Metrics["usage_five_hour"] = core.Metric{
					Used:   core.Float64Ptr(clamp(percentage, 0, 100)),
					Limit:  core.Float64Ptr(100),
					Unit:   "%",
					Window: "5h",
				}
				if percentage >= 100 {
					state.limited = true
				} else if percentage >= 80 {
					state.nearLimit = true
				}
			}

			limit, hasLimit := parseNumberFromMap(row, "usage", "limit", "quota")
			current, hasCurrent := parseNumberFromMap(row, "currentValue", "current", "used")
			if hasLimit && hasCurrent {
				remaining := math.Max(limit-current, 0)
				snap.Metrics["tokens_five_hour"] = core.Metric{
					Limit:     core.Float64Ptr(limit),
					Used:      core.Float64Ptr(current),
					Remaining: core.Float64Ptr(remaining),
					Unit:      "tokens",
					Window:    "5h",
				}
			}

			if resetRaw := firstAnyFromMap(row, "nextResetTime", "resetTime", "reset_at"); resetRaw != nil {
				if reset, ok := parseTimeValue(resetRaw); ok {
					snap.Resets["usage_five_hour"] = reset
				}
			}
			found = true

		case "TIME_LIMIT":
			limit, hasLimit := parseNumberFromMap(row, "usage", "limit", "quota")
			current, hasCurrent := parseNumberFromMap(row, "currentValue", "current", "used")
			if hasLimit && hasCurrent {
				remaining := math.Max(limit-current, 0)
				snap.Metrics["mcp_monthly_usage"] = core.Metric{
					Limit:     core.Float64Ptr(limit),
					Used:      core.Float64Ptr(current),
					Remaining: core.Float64Ptr(remaining),
					Unit:      "calls",
					Window:    "1mo",
				}
				found = true
			}
			if hasPct {
				if percentage >= 100 {
					state.limited = true
				} else if percentage >= 80 {
					state.nearLimit = true
				}
			}
		}
	}

	return found
}
