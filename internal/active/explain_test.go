package active

import (
	"strings"
	"testing"
)

func TestExplainNamesTheDecidingRule(t *testing.T) {
	tests := []struct {
		name     string
		in       SelectInput
		contains []string
	}{
		{
			name: "events tier",
			in: SelectInput{Candidates: []Candidate{
				{Key: "codex:default", ProviderID: "codex", LastEventAt: tp("2026-08-15T12:00:00Z")},
				{Key: "claude_code:default", ProviderID: "claude_code", LastEventAt: tp("2026-08-15T10:00:00Z")},
			}},
			contains: []string{"codex:default", "most recent event", "2 candidate"},
		},
		{
			name: "priority tier because nothing has events",
			in: SelectInput{
				PriorityOrder: []string{"groq"},
				Candidates:    []Candidate{{Key: "groq:default", ProviderID: "groq"}},
			},
			contains: []string{"no provider has events", "priority order"},
		},
		{
			name: "pinned",
			in: SelectInput{
				PinnedKey:  "codex:default",
				Candidates: []Candidate{{Key: "codex:default", ProviderID: "codex"}},
			},
			contains: []string{"pinned"},
		},
		{
			name:     "nothing to select",
			in:       SelectInput{},
			contains: []string{"no candidates"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Explain(tc.in)
			for _, want := range tc.contains {
				if !strings.Contains(got, want) {
					t.Errorf("Explain() = %q, missing %q", got, want)
				}
			}
		})
	}
}
