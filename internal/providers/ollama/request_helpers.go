package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

func doJSONRequest(ctx context.Context, method, url, apiKey string, out any, client *http.Client) (int, http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return 0, nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return resp.StatusCode, resp.Header, nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, resp.Header, err
	}
	if len(body) == 0 {
		return resp.StatusCode, resp.Header, nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return resp.StatusCode, resp.Header, err
	}
	return resp.StatusCode, resp.Header, nil
}

func doJSONPostRequest(ctx context.Context, url string, body any, out any, client *http.Client) (int, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return resp.StatusCode, nil
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, err
	}
	if len(respBody) == 0 {
		return resp.StatusCode, nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return resp.StatusCode, err
	}
	return resp.StatusCode, nil
}

func sanitizeMetricPart(input string) string {
	s := strings.ToLower(strings.TrimSpace(input))
	s = nonAlnumRe.ReplaceAllString(s, "_")
	s = strings.Trim(s, "_")
	if s == "" {
		return "unknown"
	}
	return s
}

func normalizeModelName(input string) string {
	s := strings.TrimSpace(strings.ToLower(input))
	if s == "" {
		return ""
	}
	s = strings.Trim(strings.TrimPrefix(s, "models/"), "/")
	if strings.HasPrefix(s, "ollama.com/") {
		s = strings.TrimPrefix(s, "ollama.com/")
	}
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	return strings.TrimSpace(strings.TrimSuffix(s, ":latest"))
}

func cloudEndpointURL(base, path string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		base = defaultCloudBaseURL
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}

func resolveCloudSessionCookie(acct core.AccountConfig) string {
	for _, key := range []string{"cloud_session_cookie", "session_cookie", "cookie"} {
		if v := strings.TrimSpace(acct.Hint(key, "")); v != "" {
			return v
		}
	}
	return strings.TrimSpace(os.Getenv("OLLAMA_SESSION_COOKIE"))
}

func fetchCloudUsageFromSettingsPage(ctx context.Context, cloudBaseURL, apiKey string, acct core.AccountConfig, snap *core.UsageSnapshot, client *http.Client) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cloudEndpointURL(cloudBaseURL, "/settings"), nil)
	if err != nil {
		return false, fmt.Errorf("ollama: creating settings request: %w", err)
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	if cookie := resolveCloudSessionCookie(acct); cookie != "" {
		req.Header.Set("Cookie", cookie)
	}

	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("ollama: cloud settings request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("ollama: cloud settings endpoint returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("ollama: reading cloud settings response: %w", err)
	}

	pcts := make(map[string]float64)
	for _, match := range settingsUsageRe.FindAllStringSubmatch(string(body), -1) {
		if len(match) < 3 {
			continue
		}
		value, convErr := strconv.ParseFloat(strings.TrimSpace(match[2]), 64)
		if convErr == nil {
			pcts[strings.ToLower(strings.TrimSpace(match[1]))] = value
		}
	}
	resets := make(map[string]time.Time)
	for _, match := range settingsResetRe.FindAllStringSubmatch(string(body), -1) {
		if len(match) < 3 {
			continue
		}
		if t, ok := parseAnyTime(strings.TrimSpace(match[2])); ok {
			resets[strings.ToLower(strings.TrimSpace(match[1]))] = t
		}
	}

	found := false
	if value, ok := pcts["session usage"]; ok {
		snap.Metrics["usage_five_hour"] = core.Metric{Used: core.Float64Ptr(value), Unit: "%", Window: "5h"}
		if t, ok := resets["session usage"]; ok {
			snap.Resets["usage_five_hour"] = t
			snap.SetAttribute("block_end", t.Format(time.RFC3339))
			snap.SetAttribute("block_start", t.Add(-5*time.Hour).Format(time.RFC3339))
		}
		found = true
	}
	if value, ok := pcts["weekly usage"]; ok {
		snap.Metrics["usage_weekly"] = core.Metric{Used: core.Float64Ptr(value), Unit: "%", Window: "1w"}
		snap.Metrics["usage_one_day"] = core.Metric{Used: core.Float64Ptr(value), Unit: "%", Window: "1d"}
		if t, ok := resets["weekly usage"]; ok {
			snap.Resets["usage_weekly"] = t
			snap.Resets["usage_one_day"] = t
		}
		found = true
	}
	return found, nil
}

func setValueMetric(snap *core.UsageSnapshot, key string, value float64, unit, window string) {
	snap.Metrics[key] = core.Metric{
		Used:      core.Float64Ptr(value),
		Remaining: core.Float64Ptr(value),
		Unit:      unit,
		Window:    window,
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func summarizeModels(models []tagModel, limit int) string {
	if len(models) == 0 || limit <= 0 {
		return ""
	}
	out := make([]string, 0, limit)
	for i := 0; i < len(models) && i < limit; i++ {
		name := normalizeModelName(models[i].Name)
		if name == "" {
			name = normalizeModelName(models[i].Model)
		}
		if name != "" {
			out = append(out, name)
		}
	}
	return strings.Join(out, ", ")
}

func normalizeHeaderKey(k string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(k)), "-", "_")
}

func isCloudModel(model tagModel) bool {
	name := strings.ToLower(strings.TrimSpace(model.Name))
	mdl := strings.ToLower(strings.TrimSpace(model.Model))
	if strings.HasSuffix(name, ":cloud") || strings.HasSuffix(mdl, ":cloud") {
		return true
	}
	return strings.TrimSpace(model.RemoteHost) != "" || strings.TrimSpace(model.RemoteModel) != ""
}

func anyValueCaseInsensitive(m map[string]any, keys ...string) (any, bool) {
	if len(m) == 0 {
		return nil, false
	}
	want := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if norm := normalizeLookupKey(key); norm != "" {
			want[norm] = struct{}{}
		}
	}
	for key, value := range m {
		if _, ok := want[normalizeLookupKey(key)]; ok {
			return value, true
		}
	}
	return nil, false
}

func anyStringCaseInsensitive(m map[string]any, keys ...string) string {
	value, ok := anyValueCaseInsensitive(m, keys...)
	if !ok {
		return ""
	}
	switch val := value.(type) {
	case string:
		return strings.TrimSpace(val)
	case fmt.Stringer:
		return strings.TrimSpace(val.String())
	default:
		return ""
	}
}

func anyMapCaseInsensitive(m map[string]any, keys ...string) map[string]any {
	value, ok := anyValueCaseInsensitive(m, keys...)
	if !ok {
		return nil
	}
	out, _ := value.(map[string]any)
	return out
}

func anyBoolCaseInsensitive(m map[string]any, keys ...string) (bool, bool) {
	value, ok := anyValueCaseInsensitive(m, keys...)
	if !ok {
		return false, false
	}
	switch val := value.(type) {
	case bool:
		return val, true
	case string:
		b, err := strconv.ParseBool(strings.TrimSpace(val))
		return b, err == nil
	default:
		return false, false
	}
}

func anyFloatCaseInsensitive(m map[string]any, keys ...string) (float64, bool) {
	value, ok := anyValueCaseInsensitive(m, keys...)
	if !ok {
		return 0, false
	}
	return anyFloat(value)
}

func anyFloat(v any) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case int32:
		return float64(val), true
	case uint:
		return float64(val), true
	case uint64:
		return float64(val), true
	case uint32:
		return float64(val), true
	case json.Number:
		f, err := val.Float64()
		return f, err == nil
	case string:
		s := strings.TrimSpace(strings.TrimSuffix(val, "%"))
		f, err := strconv.ParseFloat(s, 64)
		return f, err == nil
	default:
		return 0, false
	}
}

func anyNullStringCaseInsensitive(m map[string]any, keys ...string) string {
	raw := anyMapCaseInsensitive(m, keys...)
	if len(raw) == 0 {
		return ""
	}
	if valid, ok := anyBoolCaseInsensitive(raw, "valid"); ok && !valid {
		return ""
	}
	return anyStringCaseInsensitive(raw, "string", "value")
}

func anyNullTimeCaseInsensitive(m map[string]any, keys ...string) (time.Time, bool) {
	raw := anyMapCaseInsensitive(m, keys...)
	if len(raw) == 0 {
		return time.Time{}, false
	}
	if valid, ok := anyBoolCaseInsensitive(raw, "valid"); ok && !valid {
		return time.Time{}, false
	}
	timeRaw := anyStringCaseInsensitive(raw, "time", "value")
	if timeRaw == "" {
		return time.Time{}, false
	}
	return parseAnyTime(timeRaw)
}

func normalizeLookupKey(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.ReplaceAll(s, "_", "")
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, ".", "")
	return s
}

func parseAnyTime(raw string) (time.Time, bool) {
	t, err := shared.ParseTimestampString(raw)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}
