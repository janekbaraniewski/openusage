package active

import (
	"testing"
	"time"
)

func at(s string) time.Time {
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return ts
}

func TestLivePin(t *testing.T) {
	pinnedAt := at("2026-08-15T12:00:00Z")
	state := PinState{Key: "codex:default", PinnedAt: pinnedAt}

	tests := []struct {
		name      string
		state     PinState
		events    map[string]time.Time
		wantKey   string
		wantAlive bool
	}{
		{
			name:  "activity on the pinned provider holds the pin",
			state: state,
			events: map[string]time.Time{
				"codex:default": at("2026-08-15T12:30:00Z"),
			},
			wantKey: "codex:default", wantAlive: true,
		},
		{
			name:  "activity elsewhere releases the pin",
			state: state,
			events: map[string]time.Time{
				"codex:default":       at("2026-08-15T12:30:00Z"),
				"claude_code:default": at("2026-08-15T12:05:00Z"),
			},
			wantAlive: false,
		},
		{
			name:  "older activity elsewhere does not release",
			state: state,
			events: map[string]time.Time{
				"claude_code:default": at("2026-08-15T11:00:00Z"),
			},
			wantKey: "codex:default", wantAlive: true,
		},
		{
			name:      "no events at all holds the pin",
			state:     state,
			events:    map[string]time.Time{},
			wantKey:   "codex:default",
			wantAlive: true,
		},
		{
			name:      "empty state is not a pin",
			state:     PinState{},
			events:    map[string]time.Time{},
			wantAlive: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			key, alive := LivePin(tc.state, tc.events)
			if alive != tc.wantAlive {
				t.Fatalf("alive = %v, want %v", alive, tc.wantAlive)
			}
			if alive && key != tc.wantKey {
				t.Errorf("key = %q, want %q", key, tc.wantKey)
			}
		})
	}
}

func TestPinStateJSONRoundTrip(t *testing.T) {
	in := PinState{Key: "codex:default", PinnedAt: at("2026-08-15T12:00:00Z")}
	encoded, err := in.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	out, err := DecodePinState(encoded)
	if err != nil {
		t.Fatalf("DecodePinState: %v", err)
	}
	if out.Key != in.Key || !out.PinnedAt.Equal(in.PinnedAt) {
		t.Errorf("round trip = %+v, want %+v", out, in)
	}
}

func TestDecodePinStateEmptyIsNoPin(t *testing.T) {
	out, err := DecodePinState("")
	if err != nil {
		t.Fatalf("DecodePinState: %v", err)
	}
	if out.Key != "" {
		t.Errorf("key = %q, want empty", out.Key)
	}
}
