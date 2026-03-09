package ollama

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/parsers"
)

func (p *Provider) fetchLocalAPI(ctx context.Context, baseURL string, snap *core.UsageSnapshot) (bool, error) {
	var hasData bool

	statusOK, err := p.fetchLocalStatus(ctx, baseURL, snap)
	if err != nil {
		return false, err
	}
	hasData = hasData || statusOK

	versionOK, err := p.fetchLocalVersion(ctx, baseURL, snap)
	if err != nil {
		return false, err
	}
	hasData = hasData || versionOK

	meOK, err := p.fetchLocalMe(ctx, baseURL, snap)
	if err != nil {
		return hasData, err
	}
	hasData = hasData || meOK

	models, tagsOK, err := p.fetchLocalTags(ctx, baseURL, snap)
	if err != nil {
		return hasData, err
	}
	hasData = hasData || tagsOK

	if len(models) > 0 {
		if err := p.fetchModelDetails(ctx, baseURL, models, snap); err != nil {
			snap.SetDiagnostic("model_details_error", err.Error())
		}
	}

	psOK, err := p.fetchLocalPS(ctx, baseURL, snap)
	if err != nil {
		return hasData, err
	}
	hasData = hasData || psOK

	return hasData, nil
}

func (p *Provider) fetchLocalVersion(ctx context.Context, baseURL string, snap *core.UsageSnapshot) (bool, error) {
	var resp versionResponse
	code, headers, err := doJSONRequest(ctx, http.MethodGet, baseURL+"/api/version", "", &resp, p.Client())
	if err != nil {
		return false, fmt.Errorf("ollama: local version request failed: %w", err)
	}
	for k, v := range parsers.RedactHeaders(headers) {
		if strings.EqualFold(k, "X-Request-Id") || strings.EqualFold(k, "X-Build-Time") || strings.EqualFold(k, "X-Build-Commit") {
			snap.Raw["local_version_"+normalizeHeaderKey(k)] = v
		}
	}
	if code != http.StatusOK {
		return false, fmt.Errorf("ollama: local version endpoint returned HTTP %d", code)
	}
	if resp.Version != "" {
		snap.SetAttribute("cli_version", resp.Version)
		return true, nil
	}
	return false, nil
}

func (p *Provider) fetchLocalStatus(ctx context.Context, baseURL string, snap *core.UsageSnapshot) (bool, error) {
	var resp map[string]any
	code, _, err := doJSONRequest(ctx, http.MethodGet, baseURL+"/api/status", "", &resp, p.Client())
	if err != nil {
		return false, nil
	}
	if code == http.StatusNotFound || code == http.StatusMethodNotAllowed {
		return false, nil
	}
	if code != http.StatusOK {
		return false, nil
	}

	cloud := anyMapCaseInsensitive(resp, "cloud")
	if len(cloud) == 0 {
		return false, nil
	}

	var hasData bool
	if disabled, ok := anyBoolCaseInsensitive(cloud, "disabled"); ok {
		snap.SetAttribute("cloud_disabled", strconv.FormatBool(disabled))
		hasData = true
	}
	if source := anyStringCaseInsensitive(cloud, "source"); source != "" {
		snap.SetAttribute("cloud_source", source)
		hasData = true
	}
	return hasData, nil
}

func (p *Provider) fetchLocalMe(ctx context.Context, baseURL string, snap *core.UsageSnapshot) (bool, error) {
	var resp map[string]any
	code, _, err := doJSONRequest(ctx, http.MethodPost, baseURL+"/api/me", "", &resp, p.Client())
	if err != nil {
		return false, nil
	}

	switch code {
	case http.StatusOK:
		return applyCloudUserPayload(resp, snap, p.now()), nil
	case http.StatusUnauthorized, http.StatusForbidden:
		if signinURL := anyStringCaseInsensitive(resp, "signin_url", "sign_in_url"); signinURL != "" {
			snap.SetAttribute("signin_url", signinURL)
			return true, nil
		}
		return false, nil
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		return false, nil
	default:
		snap.SetDiagnostic("local_me_status", fmt.Sprintf("HTTP %d", code))
		return false, nil
	}
}

func (p *Provider) fetchLocalTags(ctx context.Context, baseURL string, snap *core.UsageSnapshot) ([]tagModel, bool, error) {
	var resp tagsResponse
	code, headers, err := doJSONRequest(ctx, http.MethodGet, baseURL+"/api/tags", "", &resp, p.Client())
	if err != nil {
		return nil, false, fmt.Errorf("ollama: local tags request failed: %w", err)
	}
	for k, v := range parsers.RedactHeaders(headers) {
		if strings.EqualFold(k, "X-Request-Id") {
			snap.Raw["local_tags_"+normalizeHeaderKey(k)] = v
		}
	}
	if code != http.StatusOK {
		return nil, false, fmt.Errorf("ollama: local tags endpoint returned HTTP %d", code)
	}

	totalModels := float64(len(resp.Models))
	setValueMetric(snap, "models_total", totalModels, "models", "current")

	var localCount, cloudCount int
	var localBytes, cloudBytes int64
	for _, model := range resp.Models {
		if isCloudModel(model) {
			cloudCount++
			if model.Size > 0 {
				cloudBytes += model.Size
			}
			continue
		}

		localCount++
		if model.Size > 0 {
			localBytes += model.Size
		}
	}

	setValueMetric(snap, "models_local", float64(localCount), "models", "current")
	setValueMetric(snap, "models_cloud", float64(cloudCount), "models", "current")
	setValueMetric(snap, "model_storage_bytes", float64(localBytes), "bytes", "current")
	setValueMetric(snap, "cloud_model_stub_bytes", float64(cloudBytes), "bytes", "current")

	if len(resp.Models) > 0 {
		snap.Raw["models_top"] = summarizeModels(resp.Models, 6)
	}

	return resp.Models, true, nil
}

func (p *Provider) fetchLocalPS(ctx context.Context, baseURL string, snap *core.UsageSnapshot) (bool, error) {
	var resp processResponse
	code, _, err := doJSONRequest(ctx, http.MethodGet, baseURL+"/api/ps", "", &resp, p.Client())
	if err != nil {
		return false, fmt.Errorf("ollama: local process list request failed: %w", err)
	}
	if code != http.StatusOK {
		return false, fmt.Errorf("ollama: local process list endpoint returned HTTP %d", code)
	}

	setValueMetric(snap, "loaded_models", float64(len(resp.Models)), "models", "current")

	var loadedBytes int64
	var loadedVRAM int64
	maxContext := 0
	for _, m := range resp.Models {
		loadedBytes += m.Size
		loadedVRAM += m.SizeVRAM
		if m.ContextLength > maxContext {
			maxContext = m.ContextLength
		}
	}

	setValueMetric(snap, "loaded_model_bytes", float64(loadedBytes), "bytes", "current")
	setValueMetric(snap, "loaded_vram_bytes", float64(loadedVRAM), "bytes", "current")
	if maxContext > 0 {
		setValueMetric(snap, "context_window", float64(maxContext), "tokens", "current")
	}

	if len(resp.Models) > 0 {
		loadedNames := make([]string, 0, len(resp.Models))
		for _, m := range resp.Models {
			name := normalizeModelName(m.Name)
			if name == "" {
				continue
			}
			loadedNames = append(loadedNames, name)
		}
		if len(loadedNames) > 0 {
			snap.Raw["loaded_models"] = strings.Join(loadedNames, ", ")
		}
	}

	return true, nil
}

func (p *Provider) fetchModelDetails(ctx context.Context, baseURL string, models []tagModel, snap *core.UsageSnapshot) error {
	var toolsCount, visionCount, thinkingCount int
	var maxCtx int64
	var totalParams float64

	for _, model := range models {
		name := normalizeModelName(model.Name)
		if name == "" {
			continue
		}

		var show showResponse
		code, err := doJSONPostRequest(ctx, baseURL+"/api/show", map[string]string{"name": model.Name}, &show, p.Client())
		if err != nil || code != http.StatusOK {
			continue
		}

		prefix := "model_" + sanitizeMetricPart(name)

		capSet := make(map[string]bool, len(show.Capabilities))
		for _, cap := range show.Capabilities {
			capSet[strings.TrimSpace(strings.ToLower(cap))] = true
		}
		if capSet["tools"] {
			toolsCount++
			snap.SetAttribute(prefix+"_capability_tools", "true")
		}
		if capSet["vision"] {
			visionCount++
			snap.SetAttribute(prefix+"_capability_vision", "true")
		}
		if capSet["thinking"] {
			thinkingCount++
			snap.SetAttribute(prefix+"_capability_thinking", "true")
		}

		if show.Details.QuantizationLevel != "" {
			snap.SetAttribute(prefix+"_quantization", show.Details.QuantizationLevel)
		}

		if ctxVal, ok := extractContextLength(show.ModelInfo); ok && ctxVal > 0 {
			setValueMetric(snap, prefix+"_context_length", float64(ctxVal), "tokens", "current")
			if ctxVal > maxCtx {
				maxCtx = ctxVal
			}
		}

		if ps := parseParameterSize(show.Details.ParameterSize); ps > 0 {
			totalParams += ps
		}

		rec := core.ModelUsageRecord{
			RawModelID: name,
			RawSource:  "api_show",
			Window:     "current",
		}
		rec.SetDimension("provider", "ollama")
		if capSet["tools"] {
			rec.SetDimension("capability_tools", "true")
		}
		if capSet["vision"] {
			rec.SetDimension("capability_vision", "true")
		}
		if capSet["thinking"] {
			rec.SetDimension("capability_thinking", "true")
		}
		snap.AppendModelUsage(rec)
	}

	setValueMetric(snap, "models_with_tools", float64(toolsCount), "models", "current")
	setValueMetric(snap, "models_with_vision", float64(visionCount), "models", "current")
	setValueMetric(snap, "models_with_thinking", float64(thinkingCount), "models", "current")
	if maxCtx > 0 {
		setValueMetric(snap, "max_context_length", float64(maxCtx), "tokens", "current")
	}
	if totalParams > 0 {
		setValueMetric(snap, "total_parameters", totalParams, "params", "current")
	}

	return nil
}

func extractContextLength(modelInfo map[string]any) (int64, bool) {
	if len(modelInfo) == 0 {
		return 0, false
	}
	for k, v := range modelInfo {
		if !strings.HasSuffix(strings.ToLower(k), ".context_length") {
			continue
		}
		switch val := v.(type) {
		case float64:
			return int64(val), true
		case int64:
			return val, true
		case json.Number:
			n, err := val.Int64()
			if err == nil {
				return n, true
			}
		}
	}
	return 0, false
}

func parseParameterSize(s string) float64 {
	s = strings.TrimSpace(strings.ToUpper(s))
	if s == "" {
		return 0
	}
	multiplier := 1.0
	if strings.HasSuffix(s, "B") {
		s = strings.TrimSuffix(s, "B")
		multiplier = 1e9
	}
	if strings.HasSuffix(s, "M") {
		s = strings.TrimSuffix(s, "M")
		multiplier = 1e6
	}
	val, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return val * multiplier
}
