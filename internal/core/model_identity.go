package core

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	reDateISO      = regexp.MustCompile(`(20\d{2})[-_](0[1-9]|1[0-2])[-_](0[1-9]|[12]\d|3[01])`)
	reDateCompact  = regexp.MustCompile(`\b(20\d{2})(0[1-9]|1[0-2])([0-2]\d|3[01])\b`)
	reVersionToken = regexp.MustCompile(`^\d+(?:\.\d+)?$`)
)

type canonicalModelIdentity struct {
	LineageID  string
	ReleaseID  string
	Vendor     string
	Family     string
	Variant    string
	Confidence float64
	Reason     string
	Canonical  string // Canonical model name for consistent identification
}

func normalizeCanonicalModel(providerID, rawModelID string, cfg ModelNormalizationConfig) canonicalModelIdentity {
	raw := strings.TrimSpace(rawModelID)
	if raw == "" {
		return canonicalModelIdentity{
			LineageID:  "unknown/unknown",
			Vendor:     "unknown",
			Family:     "unknown",
			Confidence: 0.10,
			Reason:     "empty",
		}
	}

	if ov, ok := findModelOverride(providerID, raw, cfg.Overrides); ok {
		lineage := strings.TrimSpace(ov.CanonicalLineage)
		if lineage == "" {
			lineage = "unknown/" + normalizeModelToken(raw)
		}
		release := strings.TrimSpace(ov.CanonicalRelease)
		vendor, family := parseVendorFamilyFromCanonical(lineage)
		identity := canonicalModelIdentity{
			LineageID:  lineage,
			ReleaseID:  release,
			Vendor:     vendor,
			Family:     family,
			Variant:    parseVariantFromCanonical(lineage),
			Confidence: 1.0,
			Reason:     "override",
			Canonical:  ov.CanonicalModel, // Add canonical model name from override
		}
		return identity
	}

	vendorFromProvider := canonicalVendorFromProvider(providerID)
	model := strings.ToLower(strings.TrimSpace(raw))
	model = strings.TrimPrefix(model, "models/")
	model = strings.Trim(model, "/")

	explicitVendor := ""
	if parts := strings.SplitN(model, "/", 2); len(parts) == 2 {
		if isKnownVendor(parts[0]) {
			explicitVendor = parts[0]
			model = parts[1]
		}
	}

	releaseDate := extractReleaseDate(model)
	modelNoDate := stripReleaseDate(model)
	if modelNoDate == "" {
		modelNoDate = model
	}
	norm := normalizeModelToken(modelNoDate)
	if norm == "" {
		norm = "unknown"
	}
	tokens := splitModelTokens(norm)

	vendor := explicitVendor
	if vendor == "" {
		vendor = detectVendorFromModel(tokens, vendorFromProvider)
	}
	family := detectFamily(tokens)
	variant := detectVariant(tokens)

	identity := canonicalModelIdentity{
		Vendor:     vendor,
		Family:     family,
		Variant:    variant,
		Confidence: 0.60,
		Reason:     "heuristic",
	}

	switch family {
	case "claude":
		claude := canonicalizeClaude(tokens)
		identity.Vendor = firstNonEmpty(identity.Vendor, "anthropic")
		identity.Family = "claude"
		identity.Variant = firstNonEmpty(claude.variant, identity.Variant)
		identity.LineageID = identity.Vendor + "/" + claude.lineage
		identity.Confidence = claude.confidence
		identity.Reason = claude.reason
		identity.Canonical = "anthropic/claude-" + firstNonEmpty(claude.variant, "unknown")
	case "gpt":
		gpt := canonicalizeGPT(tokens)
		identity.Vendor = firstNonEmpty(identity.Vendor, "openai")
		identity.Family = "gpt"
		identity.Variant = firstNonEmpty(gpt.variant, identity.Variant)
		identity.LineageID = identity.Vendor + "/" + gpt.lineage
		identity.Confidence = gpt.confidence
		identity.Reason = gpt.reason
		identity.Canonical = "openai/gpt-" + firstNonEmpty(gpt.variant, "unknown")
	case "gemini":
		gem := canonicalizeGemini(tokens)
		identity.Vendor = firstNonEmpty(identity.Vendor, "google")
		identity.Family = "gemini"
		identity.Variant = firstNonEmpty(gem.variant, identity.Variant)
		identity.LineageID = identity.Vendor + "/" + gem.lineage
		identity.Confidence = gem.confidence
		identity.Reason = gem.reason
		identity.Canonical = "google/gemini-" + firstNonEmpty(gem.variant, "unknown")
	default:
		v := identity.Vendor
		if v == "" {
			v = "unknown"
		}
		identity.Vendor = v
		if identity.Family == "" {
			identity.Family = "unknown"
		}
		identity.LineageID = v + "/" + norm
		if explicitVendor != "" {
			identity.Confidence = 0.90
			identity.Reason = "explicit_vendor"
		} else if v != "unknown" {
			identity.Confidence = 0.72
			identity.Reason = "provider_vendor"
		}
		identity.Canonical = v + "/" + norm
	}

	if releaseDate != "" {
		identity.ReleaseID = identity.LineageID + "@" + releaseDate
	}

	return identity
}

type canonicalBuild struct {
	lineage    string
	variant    string
	confidence float64
	reason     string
}

func canonicalizeClaude(tokens []string) canonicalBuild {
	variant := firstMatch(tokens, "opus", "sonnet", "haiku")
	version := extractVersionNearVariant(tokens, variant)
	if variant != "" && version != "" {
		return canonicalBuild{
			lineage:    fmt.Sprintf("claude-%s-%s", variant, version),
			variant:    variant,
			confidence: 0.95,
			reason:     "family_parse",
		}
	}
	if variant != "" {
		return canonicalBuild{
			lineage:    fmt.Sprintf("claude-%s", variant),
			variant:    variant,
			confidence: 0.82,
			reason:     "family_parse_variant_only",
		}
	}
	version = firstVersionToken(tokens)
	if version != "" {
		return canonicalBuild{
			lineage:    fmt.Sprintf("claude-%s", version),
			confidence: 0.78,
			reason:     "family_parse_version_only",
		}
	}
	return canonicalBuild{
		lineage:    "claude",
		confidence: 0.72,
		reason:     "family_only",
	}
}

func canonicalizeGPT(tokens []string) canonicalBuild {
	version := firstVersionToken(tokens)
	variant := firstMatch(tokens, "codex", "mini", "nano", "turbo", "chat", "pro")
	lineage := "gpt"
	if version != "" {
		lineage += "-" + version
	}
	if variant != "" {
		lineage += "-" + variant
	}
	confidence := 0.80
	if version != "" {
		confidence = 0.90
	}
	if variant != "" && version != "" {
		confidence = 0.93
	}
	return canonicalBuild{
		lineage:    lineage,
		variant:    variant,
		confidence: confidence,
		reason:     "family_parse",
	}
}

func canonicalizeGemini(tokens []string) canonicalBuild {
	version := firstVersionToken(tokens)
	variant := firstMatch(tokens, "pro", "flash", "ultra", "nano", "lite")
	lineage := "gemini"
	if version != "" {
		lineage += "-" + version
	}
	if variant != "" {
		lineage += "-" + variant
	}
	confidence := 0.80
	if version != "" {
		confidence = 0.88
	}
	return canonicalBuild{
		lineage:    lineage,
		variant:    variant,
		confidence: confidence,
		reason:     "family_parse",
	}
}

func findModelOverride(providerID, rawModelID string, overrides []ModelNormalizationOverride) (ModelNormalizationOverride, bool) {
	targetProvider := strings.ToLower(strings.TrimSpace(providerID))
	targetModel := strings.ToLower(strings.TrimSpace(rawModelID))
	for _, ov := range overrides {
		modelMatch := strings.ToLower(strings.TrimSpace(ov.RawModelID)) == targetModel
		if !modelMatch {
			continue
		}
		ovProvider := strings.ToLower(strings.TrimSpace(ov.Provider))
		if ovProvider == "" || ovProvider == targetProvider {
			return ov, true
		}
	}
	return ModelNormalizationOverride{}, false
}

func canonicalVendorFromProvider(providerID string) string {
	switch strings.ToLower(strings.TrimSpace(providerID)) {
	case "anthropic", "claude_code":
		return "anthropic"
	case "openai", "codex":
		return "openai"
	case "gemini_api", "gemini_cli":
		return "google"
	case "mistral":
		return "mistral"
	case "xai":
		return "xai"
	case "deepseek":
		return "deepseek"
	case "groq":
		return "groq"
	case "openrouter":
		return "openrouter"
	case "cursor":
		return "cursor"
	case "copilot":
		return "copilot"
	default:
		return ""
	}
}

func isKnownVendor(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "anthropic", "openai", "google", "mistral", "xai", "deepseek", "groq", "meta", "openrouter":
		return true
	default:
		return false
	}
}

func detectVendorFromModel(tokens []string, fallback string) string {
	if containsToken(tokens, "claude") {
		return "anthropic"
	}
	if containsToken(tokens, "gpt") || containsToken(tokens, "codex") {
		return "openai"
	}
	if containsToken(tokens, "gemini") {
		return "google"
	}
	if containsToken(tokens, "grok") {
		return "xai"
	}
	if containsToken(tokens, "mistral") || containsToken(tokens, "mixtral") || containsToken(tokens, "codestral") {
		return "mistral"
	}
	if containsToken(tokens, "deepseek") {
		return "deepseek"
	}
	if containsToken(tokens, "llama") {
		return "meta"
	}
	if fallback != "" {
		return fallback
	}
	return "unknown"
}

func detectFamily(tokens []string) string {
	switch {
	case containsToken(tokens, "claude"):
		return "claude"
	case containsToken(tokens, "gemini"):
		return "gemini"
	case containsToken(tokens, "gpt"), containsToken(tokens, "codex"):
		return "gpt"
	case containsToken(tokens, "grok"):
		return "grok"
	default:
		return ""
	}
}

func detectVariant(tokens []string) string {
	return firstMatch(tokens,
		"opus", "sonnet", "haiku",
		"mini", "nano", "turbo", "pro", "flash", "ultra",
		"codex",
	)
}

func extractReleaseDate(raw string) string {
	if m := reDateISO.FindStringSubmatch(raw); len(m) == 4 {
		return m[1] + m[2] + m[3]
	}
	if m := reDateCompact.FindStringSubmatch(raw); len(m) == 4 {
		return m[1] + m[2] + m[3]
	}
	return ""
}

func stripReleaseDate(raw string) string {
	out := reDateISO.ReplaceAllString(raw, "")
	out = reDateCompact.ReplaceAllString(out, "")
	out = strings.Trim(out, "-_ ")
	return out
}

func normalizeModelToken(raw string) string {
	if raw == "" {
		return "unknown"
	}
	var b strings.Builder
	b.Grow(len(raw))
	lastDash := false
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			lastDash = false
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
			lastDash = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == '.':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "unknown"
	}
	return out
}

func splitModelTokens(model string) []string {
	parts := strings.Split(normalizeModelToken(model), "-")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func firstVersionToken(tokens []string) string {
	for i, tok := range tokens {
		if !reVersionToken.MatchString(tok) {
			continue
		}
		// join major/minor split across adjacent tokens (e.g. 4,6 -> 4.6)
		if !strings.Contains(tok, ".") && i+1 < len(tokens) && isAllDigits(tokens[i+1]) {
			return tok + "." + tokens[i+1]
		}
		return tok
	}
	return ""
}

func extractVersionNearVariant(tokens []string, variant string) string {
	if variant == "" {
		return firstVersionToken(tokens)
	}
	idx := -1
	for i, t := range tokens {
		if t == variant {
			idx = i
			break
		}
	}
	if idx < 0 {
		return firstVersionToken(tokens)
	}
	// right side first
	for i := idx + 1; i < len(tokens); i++ {
		if reVersionToken.MatchString(tokens[i]) {
			if !strings.Contains(tokens[i], ".") && i+1 < len(tokens) && isAllDigits(tokens[i+1]) {
				return tokens[i] + "." + tokens[i+1]
			}
			return tokens[i]
		}
	}
	// then left side
	for i := idx - 1; i >= 0; i-- {
		if reVersionToken.MatchString(tokens[i]) {
			if !strings.Contains(tokens[i], ".") && i+1 < len(tokens) && isAllDigits(tokens[i+1]) && i+1 == idx-0 {
				return tokens[i] + "." + tokens[i+1]
			}
			return tokens[i]
		}
	}
	return ""
}

func parseVendorFamilyFromCanonical(lineage string) (vendor, family string) {
	lineage = strings.TrimSpace(lineage)
	if lineage == "" {
		return "unknown", "unknown"
	}
	parts := strings.SplitN(lineage, "/", 2)
	if len(parts) == 2 {
		vendor = parts[0]
		family = strings.SplitN(parts[1], "-", 2)[0]
		return vendor, family
	}
	return "unknown", strings.SplitN(lineage, "-", 2)[0]
}

func parseVariantFromCanonical(lineage string) string {
	parts := strings.SplitN(lineage, "/", 2)
	model := lineage
	if len(parts) == 2 {
		model = parts[1]
	}
	tokens := splitModelTokens(model)
	return detectVariant(tokens)
}

func containsToken(tokens []string, target string) bool {
	for _, tok := range tokens {
		if tok == target {
			return true
		}
	}
	return false
}

func firstMatch(tokens []string, candidates ...string) string {
	for _, candidate := range candidates {
		if containsToken(tokens, candidate) {
			return candidate
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
