package pricing

import (
	"regexp"
	"strings"
)

// normalizeModelKey collapses common model-name variations into a single
// canonical form so we can compare names from disparate upstreams.
//
// Rules:
//   - lower-cased
//   - strip provider prefixes like "openai/", "anthropic/"
//   - strip vendor namespace from bedrock/vertex (e.g. "anthropic." prefix)
//   - strip date / build suffixes like "-20241022", "-v1:0", "-001"
//   - collapse "." vs "-" so "claude-3.5-sonnet" matches "claude-3-5-sonnet"
//   - drop runs of whitespace
func normalizeModelKey(model string) string {
	m := strings.TrimSpace(strings.ToLower(model))
	if m == "" {
		return ""
	}

	// strip leading provider segment ("openai/gpt-4o" -> "gpt-4o")
	if idx := strings.Index(m, "/"); idx >= 0 && idx < len(m)-1 {
		m = m[idx+1:]
	}
	// strip leading vendor namespace ("anthropic.claude-..." -> "claude-...")
	for _, prefix := range []string{
		"anthropic.",
		"openai.",
		"google.",
		"mistral.",
		"meta.",
		"amazon.",
		"cohere.",
		"deepseek.",
		"xai.",
	} {
		if strings.HasPrefix(m, prefix) {
			m = m[len(prefix):]
			break
		}
	}
	// collapse "." into "-" (claude-3.5 vs claude-3-5)
	m = strings.ReplaceAll(m, ".", "-")
	// strip date suffix and minor revision suffixes
	m = dateSuffixRE.ReplaceAllString(m, "")
	m = bedrockVersionRE.ReplaceAllString(m, "")
	m = trailingRevRE.ReplaceAllString(m, "")
	// collapse repeated dashes / whitespace
	m = strings.Trim(multiDashRE.ReplaceAllString(m, "-"), "-")
	return m
}

var (
	dateSuffixRE     = regexp.MustCompile(`-20\d{6}\b`)
	bedrockVersionRE = regexp.MustCompile(`-v\d+(?::\d+)?\b`)
	trailingRevRE    = regexp.MustCompile(`(-(?:preview|exp|beta|alpha|latest|stable))+$`)
	multiDashRE      = regexp.MustCompile(`-{2,}`)
)

// canonicalAliases maps short / colloquial model names to a canonical
// identifier that is more likely to match upstream tables.
var canonicalAliases = map[string]string{
	"gpt4":              "gpt-4",
	"gpt4o":             "gpt-4o",
	"gpt4-turbo":        "gpt-4-turbo",
	"gpt-3-5-turbo":     "gpt-3.5-turbo",
	"gemini-pro":        "gemini-1-5-pro",
	"gemini-flash":      "gemini-1-5-flash",
	"claude-3-5-sonnet": "claude-3-5-sonnet",
	"claude-3-5-haiku":  "claude-3-5-haiku",
	"claude-3-opus":     "claude-3-opus",
	"claude-opus":       "claude-3-opus",
	"claude-sonnet":     "claude-3-5-sonnet",
	"claude-haiku":      "claude-3-haiku",
	"sonnet":            "claude-3-5-sonnet",
	"opus":              "claude-3-opus",
	"haiku":             "claude-3-haiku",
}

// applyAlias rewrites a normalized key to its canonical form, if known.
func applyAlias(normalized string) string {
	if v, ok := canonicalAliases[normalized]; ok {
		return v
	}
	return normalized
}

// fuzzyCandidates returns plausible candidate keys (in priority order) to
// look up the model under, given the supplied raw model identifier.
func fuzzyCandidates(model string) []string {
	seen := make(map[string]struct{}, 6)
	out := []string{}
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}

	add(model)
	add(strings.ToLower(strings.TrimSpace(model)))

	norm := normalizeModelKey(model)
	add(norm)
	add(applyAlias(norm))

	// progressively chop trailing segments ("-2024", "-latest", "-001"…)
	parts := strings.Split(norm, "-")
	for i := len(parts) - 1; i >= 2; i-- {
		add(strings.Join(parts[:i], "-"))
	}
	return out
}

// bestFuzzyMatch picks the closest key in `keys` (a flat list of canonical
// model identifiers from an upstream) for the supplied raw model name. It
// returns the matched key and true on success.
//
// Strategy: exact match wins, then longest-shared-prefix among entries
// that share the same family token (first 2 segments of the normalized
// key). This avoids cross-family collisions like "claude" matching
// "code-claude".
func bestFuzzyMatch(model string, keys []string) (string, bool) {
	if len(keys) == 0 {
		return "", false
	}
	normIdx := make(map[string]string, len(keys))
	for _, k := range keys {
		normIdx[normalizeModelKey(k)] = k
	}

	for _, cand := range fuzzyCandidates(model) {
		if hit, ok := normIdx[cand]; ok {
			return hit, true
		}
	}

	target := normalizeModelKey(model)
	if target == "" {
		return "", false
	}
	family := familyToken(target)
	bestScore := 0
	bestKey := ""
	for normKey, raw := range normIdx {
		if familyToken(normKey) != family {
			continue
		}
		score := sharedPrefixLen(target, normKey)
		if score > bestScore {
			bestScore = score
			bestKey = raw
		}
	}
	// require a meaningful overlap so we don't pair unrelated families
	if bestScore >= len(family)+2 {
		return bestKey, true
	}
	return "", false
}

func familyToken(normalized string) string {
	if normalized == "" {
		return ""
	}
	parts := strings.SplitN(normalized, "-", 2)
	return parts[0]
}

func sharedPrefixLen(a, b string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}
