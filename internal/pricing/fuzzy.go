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

	// thinking variants share base-model rates
	"claude-opus-4-5-thinking":   "claude-opus-4-5",
	"claude-sonnet-4-5-thinking": "claude-sonnet-4-5",
	"claude-haiku-4-5-thinking":  "claude-haiku-4-5",
	"claude-opus-4-6-thinking":   "claude-opus-4-6",
	"claude-sonnet-4-6-thinking": "claude-sonnet-4-6",
	"claude-haiku-4-6-thinking":  "claude-haiku-4-6",

	// Anthropic vendor prefixes occasionally arrive in word-order form
	"anthropic-claude-4-5-opus":   "claude-opus-4-5",
	"anthropic-claude-4-5-sonnet": "claude-sonnet-4-5",
	"anthropic-claude-4-5-haiku":  "claude-haiku-4-5",
	"anthropic-claude-4-6-opus":   "claude-opus-4-6",
	"anthropic-claude-4-6-sonnet": "claude-sonnet-4-6",
	"anthropic-claude-4-6-haiku":  "claude-haiku-4-6",

	// Gemini reasoning-effort tiers map to the same base rates
	"gemini-3-pro-high":   "gemini-3-pro",
	"gemini-3-pro-low":    "gemini-3-pro",
	"gemini-3-1-pro-high": "gemini-3-1-pro",
	"gemini-3-1-pro-low":  "gemini-3-1-pro",
	"gemini-3-flash":      "gemini-3-flash-preview",

	// Kimi quantisation / specific-version variants → base
	"kimi-k2-5-nvfp4":       "kimi-k2-5",
	"kimi-k2-instruct-0905": "kimi-k2-5",
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

// fuzzyKeyIndex is the normalised-key view of an upstream pricing table.
// Building it walks the entire key set (regex normalisation + family
// bucketing), so callers that share a key set across many lookups
// should cache the index rather than rebuild it per call.
type fuzzyKeyIndex struct {
	sourceLen int

	// byNorm maps a normalised key to every raw upstream key that
	// normalised to it. Used for exact-candidate hits.
	byNorm map[string][]string

	// byFamily groups normalised keys by their family token so the
	// prefix-walk only inspects keys in the same family.
	byFamily map[string][]string
}

func buildFuzzyKeyIndex(table map[string]Price) *fuzzyKeyIndex {
	idx := &fuzzyKeyIndex{
		sourceLen: len(table),
		byNorm:    make(map[string][]string, len(table)),
		byFamily:  make(map[string][]string, 32),
	}
	for k := range table {
		nk := normalizeModelKey(k)
		if _, seen := idx.byNorm[nk]; !seen {
			idx.byFamily[familyToken(nk)] = append(idx.byFamily[familyToken(nk)], nk)
		}
		idx.byNorm[nk] = append(idx.byNorm[nk], k)
	}
	return idx
}

// bestFuzzyMatch picks the closest key in `keys` (a flat list of canonical
// model identifiers from an upstream) for the supplied raw model name. It
// returns the matched key and true on success.
//
// Strategy: exact match wins, then longest-shared-prefix among entries
// that share the same family token (first 2 segments of the normalized
// key). This avoids cross-family collisions like "claude" matching
// "code-claude".
//
// When two candidates tie on prefix score, the entry with a higher-ranked
// upstream namespace prefix wins (e.g. "anthropic/claude-..." beats
// "bedrock/claude-...") so reseller listings don't override the original
// creator's published rates.
//
// This is the slow path for ad-hoc lookups (notably tests). Hot callers
// should construct a fuzzyKeyIndex once and call bestFuzzyMatchIndexed.
func bestFuzzyMatch(model string, keys []string) (string, bool) {
	if len(keys) == 0 {
		return "", false
	}
	table := make(map[string]Price, len(keys))
	for _, k := range keys {
		table[k] = Price{}
	}
	return bestFuzzyMatchIndexed(model, buildFuzzyKeyIndex(table))
}

func bestFuzzyMatchIndexed(model string, idx *fuzzyKeyIndex) (string, bool) {
	if idx == nil || idx.sourceLen == 0 {
		return "", false
	}

	for _, cand := range fuzzyCandidates(model) {
		if hits, ok := idx.byNorm[cand]; ok {
			return pickPreferred(hits), true
		}
	}

	if !isFuzzyEligible(model) {
		return "", false
	}

	target := normalizeModelKey(model)
	if target == "" {
		return "", false
	}
	family := familyToken(target)
	bestScore := 0
	bestKey := ""
	for _, normKey := range idx.byFamily[family] {
		score := sharedPrefixLen(target, normKey)
		if score < bestScore {
			continue
		}
		candidate := pickPreferred(idx.byNorm[normKey])
		if score > bestScore {
			bestScore = score
			bestKey = candidate
			continue
		}
		bestKey = preferOriginal(bestKey, candidate)
	}
	// require a meaningful overlap so we don't pair unrelated families
	if bestScore >= len(family)+2 {
		return bestKey, true
	}
	return "", false
}

func pickPreferred(keys []string) string {
	if len(keys) == 0 {
		return ""
	}
	best := keys[0]
	for _, k := range keys[1:] {
		best = preferOriginal(best, k)
	}
	return best
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
