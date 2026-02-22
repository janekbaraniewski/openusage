package core

import "strings"

const (
	ModelNormalizationGroupLineage = "lineage"
	ModelNormalizationGroupRelease = "release"
)

type ModelNormalizationOverride struct {
	Provider         string `json:"provider,omitempty"`
	RawModelID       string `json:"raw_model_id"`
	CanonicalLineage string `json:"canonical_lineage_id"`
	CanonicalRelease string `json:"canonical_release_id,omitempty"`
	CanonicalModel   string `json:"canonical_model,omitempty"`
}

type ModelNormalizationConfig struct {
	Enabled       bool                         `json:"enabled"`
	GroupBy       string                       `json:"group_by,omitempty"`       // lineage | release
	MinConfidence float64                      `json:"min_confidence,omitempty"` // 0..1
	Overrides     []ModelNormalizationOverride `json:"overrides,omitempty"`
}

func DefaultModelNormalizationConfig() ModelNormalizationConfig {
	return ModelNormalizationConfig{
		Enabled:       true,
		GroupBy:       ModelNormalizationGroupLineage,
		MinConfidence: 0.80,
	}
}

func NormalizeModelNormalizationConfig(cfg ModelNormalizationConfig) ModelNormalizationConfig {
	if cfg.GroupBy == "" {
		cfg.GroupBy = ModelNormalizationGroupLineage
	}
	if cfg.GroupBy != ModelNormalizationGroupLineage && cfg.GroupBy != ModelNormalizationGroupRelease {
		cfg.GroupBy = ModelNormalizationGroupLineage
	}
	if cfg.MinConfidence <= 0 {
		cfg.MinConfidence = 0.80
	}
	if cfg.MinConfidence > 1 {
		cfg.MinConfidence = 1
	}
	return cfg
}

type ModelUsageRecord struct {
	RawModelID string `json:"raw_model_id"`
	RawSource  string `json:"raw_source,omitempty"` // api | jsonl | sqlite | metrics_fallback

	CanonicalLineageID string `json:"canonical_lineage_id,omitempty"`
	CanonicalReleaseID string `json:"canonical_release_id,omitempty"`
	CanonicalVendor    string `json:"canonical_vendor,omitempty"`
	CanonicalFamily    string `json:"canonical_family,omitempty"`
	CanonicalVariant   string `json:"canonical_variant,omitempty"`
	Canonical          string `json:"canonical,omitempty"` // Canonical model name for consistent identification

	Confidence float64 `json:"confidence,omitempty"` // 0..1
	Reason     string  `json:"reason,omitempty"`

	Window     string            `json:"window,omitempty"`
	Dimensions map[string]string `json:"dimensions,omitempty"` // provider/account/client/endpoint

	InputTokens     *float64 `json:"input_tokens,omitempty"`
	OutputTokens    *float64 `json:"output_tokens,omitempty"`
	CachedTokens    *float64 `json:"cached_tokens,omitempty"`
	ReasoningTokens *float64 `json:"reasoning_tokens,omitempty"`
	TotalTokens     *float64 `json:"total_tokens,omitempty"`
	CostUSD         *float64 `json:"cost_usd,omitempty"`
	Requests        *float64 `json:"requests,omitempty"`
}

func (r *ModelUsageRecord) EnsureDimensions() {
	if r.Dimensions == nil {
		r.Dimensions = make(map[string]string)
	}
}

func (r *ModelUsageRecord) SetDimension(key, value string) {
	if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
		return
	}
	r.EnsureDimensions()
	r.Dimensions[key] = value
}

func (s *UsageSnapshot) AppendModelUsage(rec ModelUsageRecord) {
	if strings.TrimSpace(rec.RawModelID) == "" {
		return
	}
	s.ModelUsage = append(s.ModelUsage, rec)
}

func AppendModelUsageRecord(snap *UsageSnapshot, rec ModelUsageRecord) {
	if snap == nil {
		return
	}
	snap.AppendModelUsage(rec)
}

func Float64Ptr(v float64) *float64 {
	return &v
}
