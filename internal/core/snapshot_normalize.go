package core

import "strings"

func NormalizeUsageSnapshotWithConfig(s UsageSnapshot, modelCfg ModelNormalizationConfig) UsageSnapshot {
	s.EnsureMaps()

	for k, v := range s.Raw {
		if k == "" || v == "" {
			continue
		}
		if isDiagnosticKey(k) {
			if _, ok := s.Diagnostics[k]; !ok {
				s.Diagnostics[k] = v
			}
			continue
		}
		if _, ok := s.Attributes[k]; !ok {
			s.Attributes[k] = v
		}
	}

	modelCfg = NormalizeModelNormalizationConfig(modelCfg)
	if modelCfg.Enabled {
		if len(s.ModelUsage) == 0 {
			s.ModelUsage = BuildModelUsageFromSnapshotMetrics(s)
		}
		s.ModelUsage = normalizeModelUsageRecords(s, modelCfg)
	}
	normalizeAnalyticsDailySeries(&s)

	return s
}

func isDiagnosticKey(key string) bool {
	lk := strings.ToLower(strings.TrimSpace(key))
	if lk == "" {
		return false
	}
	return strings.Contains(lk, "error") ||
		strings.Contains(lk, "warning") ||
		strings.HasSuffix(lk, "_err") ||
		strings.HasSuffix(lk, "_warn")
}

func normalizeModelUsageRecords(s UsageSnapshot, cfg ModelNormalizationConfig) []ModelUsageRecord {
	if len(s.ModelUsage) == 0 {
		return nil
	}

	out := make([]ModelUsageRecord, 0, len(s.ModelUsage))
	for _, rec := range s.ModelUsage {
		rec.RawModelID = strings.TrimSpace(rec.RawModelID)
		if rec.RawModelID == "" {
			continue
		}
		if rec.Window == "" {
			rec.Window = "unknown"
		}
		rec.SetDimension("provider_id", s.ProviderID)
		rec.SetDimension("account_id", s.AccountID)

		if rec.TotalTokens == nil {
			total := float64(0)
			hasAny := false
			if rec.InputTokens != nil {
				total += *rec.InputTokens
				hasAny = true
			}
			if rec.OutputTokens != nil {
				total += *rec.OutputTokens
				hasAny = true
			}
			if hasAny {
				rec.TotalTokens = Float64Ptr(total)
			}
		}

		identity := normalizeCanonicalModel(s.ProviderID, rec.RawModelID, cfg)
		rec.CanonicalLineageID = identity.LineageID
		rec.CanonicalReleaseID = identity.ReleaseID
		rec.CanonicalVendor = identity.Vendor
		rec.CanonicalFamily = identity.Family
		rec.CanonicalVariant = identity.Variant
		rec.Canonical = identity.Canonical
		rec.Confidence = identity.Confidence
		rec.Reason = identity.Reason
		groupID := rec.CanonicalLineageID
		if cfg.GroupBy == ModelNormalizationGroupRelease && rec.CanonicalReleaseID != "" {
			groupID = rec.CanonicalReleaseID
		}
		if groupID != "" && rec.Confidence >= cfg.MinConfidence {
			rec.SetDimension("canonical_group_id", groupID)
		}
		out = append(out, rec)
	}

	return out
}
