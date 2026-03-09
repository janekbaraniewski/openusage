package openrouter

import "strings"

func generationByokCost(g generationEntry) float64 {
	if !g.IsByok && g.UpstreamInferenceCost == nil {
		return 0
	}
	if g.UpstreamInferenceCost != nil && *g.UpstreamInferenceCost > 0 {
		return *g.UpstreamInferenceCost
	}
	if g.TotalCost > 0 {
		return g.TotalCost
	}
	return g.Usage
}

func resolveGenerationHostingProvider(g generationEntry) string {
	name, _ := resolveGenerationHostingProviderWithSource(g)
	return name
}

func resolveGenerationHostingProviderWithSource(g generationEntry) (string, providerResolutionSource) {
	if name := providerNameFromResponses(g.ProviderResponses); name != "" {
		return name, providerSourceResponses
	}
	if name := providerNameFromGenerationEntry(g); name != "" {
		return name, providerSourceEntryField
	}
	if name := providerNameFromUpstreamID(g.UpstreamID); name != "" {
		return name, providerSourceUpstreamID
	}
	if name := strings.TrimSpace(g.ProviderName); name != "" && !isLikelyRouterClientProviderName(name) {
		return name, providerSourceProviderName
	}
	if name := providerNameFromModel(g.Model); name != "" {
		return name, providerSourceModelPrefix
	}
	return strings.TrimSpace(g.ProviderName), providerSourceFallbackLabel
}

func providerNameFromResponses(responses []generationProviderResponse) string {
	if len(responses) == 0 {
		return ""
	}
	for i := len(responses) - 1; i >= 0; i-- {
		name := generationProviderResponseName(responses[i])
		if name == "" {
			continue
		}
		if responses[i].Status != nil && *responses[i].Status >= 200 && *responses[i].Status < 300 {
			return name
		}
	}
	for i := len(responses) - 1; i >= 0; i-- {
		name := generationProviderResponseName(responses[i])
		if name != "" {
			return name
		}
	}
	return ""
}

func generationProviderResponseName(resp generationProviderResponse) string {
	for _, candidate := range []string{
		resp.ProviderName,
		resp.Provider,
		resp.ProviderID,
	} {
		name := strings.TrimSpace(candidate)
		if name != "" && !isLikelyRouterClientProviderName(name) {
			return name
		}
	}
	return ""
}

func providerNameFromGenerationEntry(g generationEntry) string {
	for _, candidate := range []string{
		g.UpstreamProviderName,
		g.UpstreamProvider,
		g.ProviderSlug,
		g.ProviderID,
		g.Provider,
	} {
		name := strings.TrimSpace(candidate)
		if name != "" && !isLikelyRouterClientProviderName(name) {
			return name
		}
	}
	return ""
}

func providerNameFromModel(model string) string {
	norm := normalizeModelName(model)
	if norm == "" {
		return ""
	}
	slash := strings.IndexByte(norm, '/')
	if slash <= 0 {
		for _, prefix := range knownModelVendorPrefixes {
			if norm == prefix || strings.HasPrefix(norm, prefix+"-") || strings.HasPrefix(norm, prefix+"_") {
				return prefix
			}
		}
		return ""
	}
	return norm[:slash]
}

func providerNameFromUpstreamID(upstreamID string) string {
	id := strings.TrimSpace(upstreamID)
	if id == "" {
		return ""
	}
	for _, sep := range []string{"/", ":", "|"} {
		if idx := strings.Index(id, sep); idx > 0 {
			candidate := strings.TrimSpace(id[:idx])
			if isLikelyProviderSlug(candidate) {
				return candidate
			}
		}
	}
	return ""
}

func isLikelyProviderSlug(candidate string) bool {
	if candidate == "" {
		return false
	}
	slug := strings.ToLower(sanitizeName(candidate))
	if slug == "" || slug == "unknown" {
		return false
	}
	switch slug {
	case "chatcmpl", "msg", "resp", "response", "gen", "cmpl", "request", "req", "run", "completion":
		return false
	}
	return true
}

func isLikelyRouterClientProviderName(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return true
	}
	clean := strings.NewReplacer(" ", "", "-", "", "_", "", ".", "").Replace(n)
	switch clean {
	case "unknown", "openrouter", "openrouterauto", "openusage":
		return true
	}
	return strings.Contains(clean, "openrouter") || strings.Contains(clean, "openusage")
}
