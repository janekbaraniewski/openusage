package makora

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// creditsStatusResponse mirrors GET /api/v1/credits/status.
type creditsStatusResponse struct {
	Balance          float64 `json:"balance"`
	Currency         string  `json:"currency"`
	HasPaymentMethod bool    `json:"has_payment_method"`
}

// userCreditsResponse mirrors GET /api/v1/credits/user. The monthly budget is
// reported as the amount on the monthly prepaid notification; the spend
// warning threshold is separate.
type userCreditsResponse struct {
	PayAsYouGoEnabled bool `json:"pay_as_you_go_enabled"`
	SpendingWarning   struct {
		Enabled bool    `json:"is_enabled"`
		Amount  float64 `json:"amount"`
	} `json:"spending_warning_notification"`
	MonthlyPrepaid struct {
		Enabled bool    `json:"is_enabled"`
		Amount  float64 `json:"amount"`
	} `json:"monthly_prepaid_notification"`
}

// rateCardResponse mirrors the public GET /api/v1/credits/default-rates.
// Pricing strings are USD per 1M tokens.
type rateCardResponse struct {
	UpdatedAt string      `json:"updated_at"`
	Models    []modelRate `json:"models"`
}

type modelRate struct {
	Model   string  `json:"model"`
	Pricing pricing `json:"pricing"`
}

type pricing struct {
	Prompt         string `json:"prompt"`
	Completion     string `json:"completion"`
	InputCacheRead string `json:"input_cache_read"`
}

// usageResponse mirrors GET /api/v1/usage/my-usage/between/{start}/{end}.
type usageResponse struct {
	Interval string     `json:"interval"`
	Usage    []usageRow `json:"usage"`
}

type usageRow struct {
	RequestTime      time.Time `json:"request_time"`
	Model            string    `json:"model"`
	RequestCount     int       `json:"request_count"`
	InputTokens      int       `json:"input_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	TotalTokens      int       `json:"total_tokens"`
	// CachedTokens is a SUBSET of InputTokens — non-cached input is
	// InputTokens − CachedTokens. Never add it on top of input.
	CachedTokens int `json:"cached_tokens"`
}

// rateCard is the parsed per-model price table (USD per 1M tokens).
type rateCard struct {
	UpdatedAt time.Time
	prices    map[string]pricing
}

func parseCreditsStatus(body []byte) (creditsStatusResponse, error) {
	var resp creditsStatusResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return resp, fmt.Errorf("parsing credits status: %w", err)
	}
	return resp, nil
}

func parseUserCredits(body []byte) (userCreditsResponse, error) {
	var resp userCreditsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return resp, fmt.Errorf("parsing user credits: %w", err)
	}
	return resp, nil
}

func parseRateCard(body []byte) (rateCard, error) {
	var resp rateCardResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return rateCard{}, fmt.Errorf("parsing rate card: %w", err)
	}
	rc := rateCard{prices: make(map[string]pricing, len(resp.Models))}
	if t, err := time.Parse(time.RFC3339, resp.UpdatedAt); err == nil {
		rc.UpdatedAt = t
	}
	for _, m := range resp.Models {
		if m.Model == "" {
			continue
		}
		rc.prices[m.Model] = m.Pricing
	}
	return rc, nil
}

// rate returns the per-1M-token prices for a model, defaulting to zeros when
// the model is not on the rate card so cost stays defined but conservative.
func (rc rateCard) rate(model string) (prompt, completion, cacheRead float64) {
	p, ok := rc.prices[model]
	if !ok {
		return 0, 0, 0
	}
	prompt = parsePrice(p.Prompt)
	completion = parsePrice(p.Completion)
	cacheRead = parsePrice(p.InputCacheRead)
	return
}

func parsePrice(s string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || v < 0 {
		return 0
	}
	return v
}

// parseUsage consumes the my-usage payload, computes per-model cost from the
// rate card (cached treated as a subset of input), and fills the snapshot's
// ModelUsage rows, DailySeries (cost + tokens), and aggregate spend metrics.
// Returns the total cost (USD) and total request count across the window.
func parseUsage(snap *core.UsageSnapshot, body []byte, interval string, rc rateCard) error {
	var resp usageResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("parsing usage: %w", err)
	}
	if interval != "" {
		resp.Interval = interval
	}

	type modelAgg struct {
		input  int
		output int
		cached int
		total  int
		reqs   int
		cost   float64
	}
	modelAggs := make(map[string]*modelAgg)
	dailyCost := make(map[string]float64)
	dailyTokens := make(map[string]float64)

	var totalCost float64
	var totalReqs int

	for i := range resp.Usage {
		row := resp.Usage[i]
		if row.Model == "" {
			continue
		}
		prompt, completion, cacheRead := rc.rate(row.Model)
		nonCached := row.InputTokens - row.CachedTokens
		if nonCached < 0 {
			nonCached = 0
		}
		cost := (float64(nonCached)/1e6)*prompt +
			(float64(row.CompletionTokens)/1e6)*completion +
			(float64(row.CachedTokens)/1e6)*cacheRead

		totalCost += cost
		totalReqs += row.RequestCount

		a := modelAggs[row.Model]
		if a == nil {
			a = &modelAgg{}
			modelAggs[row.Model] = a
		}
		a.input += row.InputTokens
		a.output += row.CompletionTokens
		a.cached += row.CachedTokens
		a.total += row.TotalTokens
		a.reqs += row.RequestCount
		a.cost += cost

		day := row.RequestTime.Format("2006-01-02")
		dailyCost[day] += cost
		dailyTokens[day] += float64(row.TotalTokens)
	}

	// Per-model rows for the detail panel.
	for model, a := range modelAggs {
		snap.AppendModelUsage(core.ModelUsageRecord{
			RawModelID:   model,
			RawSource:    "api",
			Window:       "30d",
			InputTokens:  core.Float64Ptr(float64(a.input)),
			OutputTokens: core.Float64Ptr(float64(a.output)),
			CachedTokens: core.Float64Ptr(float64(a.cached)),
			TotalTokens:  core.Float64Ptr(float64(a.total)),
			CostUSD:      core.Float64Ptr(a.cost),
			Requests:     core.Float64Ptr(float64(a.reqs)),
		})
	}

	// Daily series for the analytics/daily views.
	snap.DailySeries = make(map[string][]core.TimePoint, 2)
	snap.DailySeries["cost"] = dailyPoints(dailyCost)
	snap.DailySeries["tokens"] = dailyPoints(dailyTokens)

	snap.SetAttribute("usage_interval", resp.Interval)
	snap.SetAttribute("request_count_total", strconv.Itoa(totalReqs))
	snap.SetAttribute("30d_requests", strconv.Itoa(totalReqs))
	snap.SetAttribute("30d_models", strconv.Itoa(len(modelAggs)))

	snap.Metrics["monthly_spend"] = core.Metric{
		Used:   core.Float64Ptr(totalCost),
		Unit:   "USD",
		Window: "month",
	}
	snap.Metrics["30d_cost"] = core.Metric{
		Used:   core.Float64Ptr(totalCost),
		Unit:   "USD",
		Window: "30d",
	}

	setTodayTotals(snap, resp.Usage)

	return nil
}

// setTodayTotals records today's aggregate tokens/requests derived from the
// most recent date present in the usage window.
func setTodayTotals(snap *core.UsageSnapshot, rows []usageRow) {
	today := ""
	for _, r := range rows {
		if r.RequestTime.Format("2006-01-02") > today {
			today = r.RequestTime.Format("2006-01-02")
		}
	}
	if today == "" {
		return
	}
	var input, output, cached, total, reqs int
	for _, r := range rows {
		if r.RequestTime.Format("2006-01-02") != today {
			continue
		}
		input += r.InputTokens
		output += r.CompletionTokens
		cached += r.CachedTokens
		total += r.TotalTokens
		reqs += r.RequestCount
	}
	setIntMetric(snap, "today_input_tokens", input, "tokens", "today")
	setIntMetric(snap, "today_output_tokens", output, "tokens", "today")
	setIntMetric(snap, "today_cached_tokens", cached, "tokens", "today")
	setIntMetric(snap, "today_tokens", total, "tokens", "today")
	setIntMetric(snap, "today_requests", reqs, "requests", "today")
}

func setIntMetric(snap *core.UsageSnapshot, key string, v int, unit, window string) {
	val := float64(v)
	snap.Metrics[key] = core.Metric{Used: core.Float64Ptr(val), Unit: unit, Window: window}
}

func dailyPoints(m map[string]float64) []core.TimePoint {
	pts := make([]core.TimePoint, 0, len(m))
	for day, val := range m {
		pts = append(pts, core.TimePoint{Date: day, Value: val})
	}
	for i := 1; i < len(pts); i++ {
		for j := i; j > 0 && pts[j].Date < pts[j-1].Date; j-- {
			pts[j], pts[j-1] = pts[j-1], pts[j]
		}
	}
	return pts
}

// jwtPayloadEmail best-effort decodes the (unverified) body of a JWT to
// extract the email so it can be surfaced as account metadata. Returns ""
// on any failure — never an error.
func jwtPayloadEmail(token string) string {
	if token == "" {
		return ""
	}
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		SubInfo struct {
			Email string `json:"email"`
		} `json:"sub_info"`
		Email string `json:"email"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	if claims.SubInfo.Email != "" {
		return claims.SubInfo.Email
	}
	return claims.Email
}
