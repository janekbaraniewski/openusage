package active

import (
	"testing"
	"time"
)

func tp(s string) *time.Time {
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return &ts
}

func TestSelect(t *testing.T) {
	tests := []struct {
		name       string
		in         SelectInput
		wantKey    string
		wantSource string
		wantFound  bool
	}{
		{
			name: "pin wins outright",
			in: SelectInput{
				PinnedKey: "openai:default",
				Candidates: []Candidate{
					{Key: "codex:default", ProviderID: "codex", LastEventAt: tp("2026-08-15T12:00:00Z")},
					{Key: "openai:default", ProviderID: "openai"},
				},
			},
			wantKey: "openai:default", wantSource: "pinned", wantFound: true,
		},
		{
			name: "most recent event wins",
			in: SelectInput{Candidates: []Candidate{
				{Key: "codex:default", ProviderID: "codex", LastEventAt: tp("2026-08-15T10:00:00Z")},
				{Key: "claude_code:default", ProviderID: "claude_code", LastEventAt: tp("2026-08-15T12:00:00Z")},
			}},
			wantKey: "claude_code:default", wantSource: "events", wantFound: true,
		},
		{
			name: "event-less provider loses to any event",
			in: SelectInput{
				PriorityOrder: []string{"openai", "codex"},
				Candidates: []Candidate{
					{Key: "openai:default", ProviderID: "openai"},
					{Key: "codex:default", ProviderID: "codex", LastEventAt: tp("2026-08-15T09:00:00Z")},
				},
			},
			wantKey: "codex:default", wantSource: "events", wantFound: true,
		},
		{
			name: "event-less eligible only when nothing has events",
			in: SelectInput{
				PriorityOrder: []string{"groq", "openai"},
				Candidates: []Candidate{
					{Key: "openai:default", ProviderID: "openai"},
					{Key: "groq:default", ProviderID: "groq"},
				},
			},
			wantKey: "groq:default", wantSource: "local", wantFound: true,
		},
		{
			name:      "no candidates",
			in:        SelectInput{},
			wantFound: false,
		},
		{
			name: "stale pin key not among candidates is ignored",
			in: SelectInput{
				PinnedKey: "gone:default",
				Candidates: []Candidate{
					{Key: "codex:default", ProviderID: "codex", LastEventAt: tp("2026-08-15T10:00:00Z")},
				},
			},
			wantKey: "codex:default", wantSource: "events", wantFound: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, source, found := Select(tc.in)
			if found != tc.wantFound {
				t.Fatalf("found = %v, want %v", found, tc.wantFound)
			}
			if !found {
				return
			}
			if got.Key != tc.wantKey {
				t.Errorf("key = %q, want %q", got.Key, tc.wantKey)
			}
			if source != tc.wantSource {
				t.Errorf("source = %q, want %q", source, tc.wantSource)
			}
		})
	}
}
