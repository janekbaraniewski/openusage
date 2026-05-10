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
