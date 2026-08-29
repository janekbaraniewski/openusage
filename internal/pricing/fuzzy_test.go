package pricing

import (
	"strings"
	"testing"
)

func TestNormalizeModelKey(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"claude-3.5-sonnet", "claude-3-5-sonnet"},
		{"claude-3-5-sonnet-20241022", "claude-3-5-sonnet"},
		{"openai/gpt-4o", "gpt-4o"},
		{"/ 0", "0"},
		{"anthropic.claude-3-5-sonnet-20241022-v2:0", "claude-3-5-sonnet"},
		{"GPT-4-Turbo-preview", "gpt-4-turbo"},
		{"gpt-4.0-mini-2024-07-18", "gpt-4-0-mini-2024-07-18"}, // ymd-only suffix is not stripped (no -20yyMMdd)
		{"  ", ""},
	}
	for _, c := range cases {
		got := normalizeModelKey(c.in)
		if got != c.want {
			t.Errorf("normalize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestBestFuzzyMatch(t *testing.T) {
	keys := []string{
		"claude-3-5-sonnet-20241022",
		"claude-3-5-haiku-20241022",
		"claude-3-opus-20240229",
		"openai/gpt-4o",
		"gemini-1.5-pro",
		"deepseek-chat",
	}

	cases := []struct {
		name    string
		input   string
		wantHit bool
		wantKey string
	}{
		{"exact", "claude-3-5-sonnet-20241022", true, "claude-3-5-sonnet-20241022"},
		{"dotted-alias", "claude-3.5-sonnet", true, "claude-3-5-sonnet-20241022"},
		{"strip-prefix", "anthropic/claude-3.5-sonnet", true, "claude-3-5-sonnet-20241022"},
		{"date-suffix", "claude-3-5-sonnet-20251101", true, "claude-3-5-sonnet-20241022"},
		{"family-only-no-match", "claude", false, ""},
		{"unknown-family", "totally-unknown-model", false, ""},
		{"gpt-stripped", "gpt-4o", true, "openai/gpt-4o"},
		{"gemini-dotted", "gemini-1-5-pro", true, "gemini-1.5-pro"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := bestFuzzyMatch(c.input, keys)
			if ok != c.wantHit {
				t.Fatalf("hit = %v, want %v (got=%q)", ok, c.wantHit, got)
			}
			if ok && got != c.wantKey {
				t.Errorf("key = %q, want %q", got, c.wantKey)
			}
		})
	}
}

func TestApplyAlias(t *testing.T) {
	if got := applyAlias("sonnet"); got != "claude-3-5-sonnet" {
		t.Errorf("applyAlias(sonnet) = %q, want claude-3-5-sonnet", got)
	}
	if got := applyAlias("unknown-key"); got != "unknown-key" {
		t.Errorf("applyAlias should pass through unknowns; got %q", got)
	}
}

func FuzzBestFuzzyMatch(f *testing.F) {
	for _, seed := range []string{
		"claude-3.5-sonnet",
		"anthropic/claude-3-5-sonnet-20241022",
		"openai/gpt-4o",
		"gemini-1.5-pro-preview",
		"unknown-model",
		"",
	} {
		f.Add(seed)
	}

	keys := []string{
		"claude-3-5-sonnet-20241022",
		"claude-3-5-haiku-20241022",
		"openai/gpt-4o",
		"gemini-1.5-pro",
		"deepseek-chat",
	}

	f.Fuzz(func(t *testing.T, model string) {
		// Normalization is used as a cache/index key. It must not carry
		// whitespace across its prefix and punctuation transformations.
		normalized := normalizeModelKey(model)
		if got := strings.TrimSpace(normalized); got != normalized {
			t.Fatalf("normalization retained surrounding whitespace: %q", normalized)
		}

		candidates := fuzzyCandidates(model)
		seen := make(map[string]struct{}, len(candidates))
		for _, candidate := range candidates {
			if strings.TrimSpace(candidate) == "" {
				t.Fatal("fuzzyCandidates returned an empty candidate")
			}
			if _, ok := seen[candidate]; ok {
				t.Fatalf("fuzzyCandidates returned duplicate %q", candidate)
			}
			seen[candidate] = struct{}{}
		}

		matched, ok := bestFuzzyMatch(model, keys)
		if !ok {
			return
		}
		for _, key := range keys {
			if matched == key {
				return
			}
		}
		t.Fatalf("bestFuzzyMatch returned key not present in source table: %q", matched)
	})
}
