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

// zaiLimitWindow identifies which rolling quota window a limits[] row belongs
// to. Z.AI encodes window durations as a (unit, number) pair: unit 3 with
// number 5 is the 5-hour rolling window, unit 6 with number 1 is the weekly
// window.
type zaiLimitWindow struct {
	key    string // metric key suffix, e.g. "five_hour"
	window string // window tag surfaced on the metric, e.g. "5h"
}

// resolveZaiLimitWindow maps a (unit, number) pair onto a supported quota
// window. ok is false when the pair is unknown, so callers can skip the row
// instead of guessing (and mislabeling an unsupported duration as 5h).
func resolveZaiLimitWindow(unit, number float64, hasUnit, hasNumber bool) (zaiLimitWindow, bool) {
	if !hasUnit || !hasNumber {
		return zaiLimitWindow{}, false
	}
	switch {
	case unit == 3 && number == 5:
		return zaiLimitWindow{key: "five_hour", window: "5h"}, true
	case unit == 6 && number == 1:
		return zaiLimitWindow{key: "seven_day", window: "7d"}, true
	default:
		return zaiLimitWindow{}, false
	}
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
	// resolvedWindows tracks windows described by a row carrying explicit
	// unit/number metadata. A legacy unit-less TOKENS_LIMIT row also maps onto
	// the 5h window, and a payload mixing both shapes must not produce a
	// result that depends on the order of limits[].
	resolvedWindows := make(map[string]bool)
	for _, row := range rows {
		kind := strings.ToUpper(strings.TrimSpace(firstStringFromMap(row, "type", "limitType")))

		// Z.AI reports percentage as an integer 0-100: 1 means 1%, not a
		// 0..1 fraction, so it is never rescaled here.
		percentage, hasPct := parseNumberFromMap(row, "percentage", "usedPercent", "used_percentage")
		if hasPct {
			percentage = clamp(percentage, 0, 100)
		}

		limit, hasLimit := parseNumberFromMap(row, "usage", "limit", "quota")
		current, hasCurrent := parseNumberFromMap(row, "currentValue", "current", "used")

		switch kind {
		// TOKENS_LIMIT was the pre-credits shape; CREDIT_LIMIT is the same
		// windowed quota metered in credits instead of tokens.
		case "TOKENS_LIMIT", "CREDIT_LIMIT":
			unit, hasUnit := parseNumberFromMap(row, "unit")
			number, hasNumber := parseNumberFromMap(row, "number")
			window, hasWindow := resolveZaiLimitWindow(unit, number, hasUnit, hasNumber)
			if !hasWindow {
				// Legacy payloads carried a single TOKENS_LIMIT row with no
				// window metadata at all; it always described the 5-hour
				// window, so keep honoring that shape.
				if kind == "TOKENS_LIMIT" && !hasUnit && !hasNumber {
					window = zaiLimitWindow{key: "five_hour", window: "5h"}
				} else {
					// Recognized limit type, unsupported duration: record that
					// quota data exists but emit no mislabeled window metrics.
					found = true
					continue
				}
			}

			if !hasPct && hasLimit && hasCurrent && limit > 0 {
				percentage = clamp(current/limit*100, 0, 100)
				hasPct = true
			}

			usageKey := "usage_" + window.key
			if !hasWindow && resolvedWindows[window.key] {
				// An explicit unit/number row already described this window;
				// keep it rather than letting the legacy copy overwrite the
				// metrics or the reset timestamp.
				found = true
				continue
			}
			if hasWindow {
				resolvedWindows[window.key] = true
			}
			if hasPct {
				snap.Metrics[usageKey] = core.Metric{
					Used:   core.Float64Ptr(percentage),
					Limit:  core.Float64Ptr(100),
					Unit:   "%",
					Window: window.window,
				}
				if percentage >= 100 {
					state.limited = true
				} else if percentage >= 80 {
					state.nearLimit = true
				}
			}

			detailUnit := "tokens"
			if kind == "CREDIT_LIMIT" {
				detailUnit = "credits"
			}
			if hasLimit && hasCurrent {
				remaining := math.Max(limit-current, 0)
				snap.Metrics[detailUnit+"_"+window.key] = core.Metric{
					Limit:     core.Float64Ptr(limit),
					Used:      core.Float64Ptr(current),
					Remaining: core.Float64Ptr(remaining),
					Unit:      detailUnit,
					Window:    window.window,
				}
			}

			if resetRaw := firstAnyFromMap(row, "nextResetTime", "resetTime", "reset_at"); resetRaw != nil {
				if reset, ok := parseTimeValue(resetRaw); ok {
					snap.Resets[usageKey] = reset
				}
			}
			found = true

		case "TIME_LIMIT":
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
