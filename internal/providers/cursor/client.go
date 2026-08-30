package cursor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultCursorAPIBase = "https://api2.cursor.sh"
	livePlanCacheTTL     = 45 * time.Second
)

// Client talks to Cursor DashboardService over Connect JSON.
type Client struct {
	baseURL    string
	httpClient *http.Client

	cacheMu    sync.Mutex
	cacheToken string
	cache      livePlanUsage
	cacheAt    time.Time
}

func NewClient(baseURL string, httpClient *http.Client) *Client {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultCursorAPIBase
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
	}
}

type planUsageBlock struct {
	AutoPercentUsed  *float64 `json:"autoPercentUsed"`
	APIPercentUsed   *float64 `json:"apiPercentUsed"`
	TotalPercentUsed *float64 `json:"totalPercentUsed"`
	TotalSpend       *float64 `json:"totalSpend"`
	Limit            *float64 `json:"limit"`
	IncludedSpend    *float64 `json:"includedSpend"`
}

type periodUsageResponse struct {
	BillingCycleStart   json.RawMessage `json:"billingCycleStart"`
	BillingCycleEnd     json.RawMessage `json:"billingCycleEnd"`
	PlanUsage           *planUsageBlock `json:"planUsage"`
	PlanPercentUsed     float64         `json:"planPercentUsed"`
	PlanAutoPercentUsed float64         `json:"planAutoPercentUsed"`
	PlanApiPercentUsed  float64         `json:"planApiPercentUsed"`
}

type planInfoResponse struct {
	PlanName        string          `json:"planName"`
	BillingCycleEnd json.RawMessage `json:"billingCycleEnd"`
}

type hardLimitResponse struct {
	NoUsageBasedAllowed *bool    `json:"noUsageBasedAllowed"`
	HardLimit           *float64 `json:"hardLimit"`
	HardLimitUSD        *float64 `json:"hardLimitUSD"`
}

type livePlanUsage struct {
	Included *float64
	Auto     *float64
	API      *float64
	ResetAt  time.Time
	PlanName string
	Ondemand string // "disabled", "enabled", or empty
}

func (c *Client) dashboardPOST(ctx context.Context, token, method string, dest any) error {
	url := fmt.Sprintf("%s/aiserver.v1.DashboardService/%s", c.baseURL, method)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader([]byte("{}")))
	if err != nil {
		return fmt.Errorf("cursor: create %s request: %w", method, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("cursor: request %s: %w", method, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("cursor: read %s body: %w", method, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("cursor: %s status %d", method, resp.StatusCode)
	}
	if err := json.Unmarshal(body, dest); err != nil {
		return fmt.Errorf("cursor: decode %s: %w", method, err)
	}
	return nil
}

func (c *Client) fetchLivePlanUsage(ctx context.Context, token string) (livePlanUsage, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return livePlanUsage{}, fmt.Errorf("cursor: missing session token")
	}
	c.cacheMu.Lock()
	if c.cacheToken == token && !c.cacheAt.IsZero() && time.Since(c.cacheAt) < livePlanCacheTTL {
		out := c.cache
		c.cacheMu.Unlock()
		return out, nil
	}
	c.cacheMu.Unlock()

	var period periodUsageResponse
	if err := c.dashboardPOST(ctx, token, "GetCurrentPeriodUsage", &period); err != nil {
		return livePlanUsage{}, err
	}
	out := livePlanFromPeriod(period)

	var plan planInfoResponse
	if err := c.dashboardPOST(ctx, token, "GetPlanInfo", &plan); err == nil {
		if name := strings.TrimSpace(plan.PlanName); name != "" {
			out.PlanName = name
		}
		if out.ResetAt.IsZero() {
			out.ResetAt = parseFlexibleTime(plan.BillingCycleEnd)
		}
	}

	var hard hardLimitResponse
	if err := c.dashboardPOST(ctx, token, "GetHardLimit", &hard); err == nil {
		switch {
		case hard.NoUsageBasedAllowed != nil && *hard.NoUsageBasedAllowed:
			out.Ondemand = "disabled"
		case hard.HardLimit != nil && *hard.HardLimit == 0 && (hard.HardLimitUSD == nil || *hard.HardLimitUSD == 0):
			out.Ondemand = "disabled"
		case hard.NoUsageBasedAllowed != nil || hard.HardLimit != nil || hard.HardLimitUSD != nil:
			out.Ondemand = "enabled"
		}
	}
	c.cacheMu.Lock()
	c.cacheToken = token
	c.cache = out
	c.cacheAt = time.Now()
	c.cacheMu.Unlock()
	return out, nil
}

func livePlanFromPeriod(period periodUsageResponse) livePlanUsage {
	out := livePlanUsage{ResetAt: parseFlexibleTime(period.BillingCycleEnd)}
	if period.PlanUsage != nil {
		out.Included = period.PlanUsage.TotalPercentUsed
		out.Auto = period.PlanUsage.AutoPercentUsed
		out.API = period.PlanUsage.APIPercentUsed
	}
	if out.Included == nil && period.PlanPercentUsed != 0 {
		v := period.PlanPercentUsed
		out.Included = &v
	}
	if out.Auto == nil && period.PlanAutoPercentUsed != 0 {
		v := period.PlanAutoPercentUsed
		out.Auto = &v
	}
	if out.API == nil && period.PlanApiPercentUsed != 0 {
		v := period.PlanApiPercentUsed
		out.API = &v
	}
	return out
}

func parseFlexibleTime(raw json.RawMessage) time.Time {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return time.Time{}
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		s = strings.TrimSpace(s)
		if s == "" {
			return time.Time{}
		}
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t.UTC()
		}
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return t.UTC()
		}
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return unixMaybeMillis(n)
		}
	}
	var n float64
	if json.Unmarshal(raw, &n) == nil && n > 0 {
		return unixMaybeMillis(int64(n))
	}
	return time.Time{}
}

func unixMaybeMillis(n int64) time.Time {
	if n > 1_000_000_000_000 {
		return time.UnixMilli(n).UTC()
	}
	return time.Unix(n, 0).UTC()
}
