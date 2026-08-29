package active

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// PinMetaKey is the daemon_meta row used to persist the active-provider pin.
const PinMetaKey = "active_pin"

// PinState is a user-forced provider selection.
type PinState struct {
	Key      string    `json:"key"`
	PinnedAt time.Time `json:"pinned_at"`
}

// Encode serializes the pin for storage in daemon_meta.
func (p PinState) Encode() (string, error) {
	data, err := json.Marshal(p)
	if err != nil {
		return "", fmt.Errorf("active: encoding pin state: %w", err)
	}
	return string(data), nil
}

// DecodePinState parses a stored pin. An empty or blank value means no pin.
func DecodePinState(raw string) (PinState, error) {
	if strings.TrimSpace(raw) == "" {
		return PinState{}, nil
	}
	var out PinState
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return PinState{}, fmt.Errorf("active: decoding pin state: %w", err)
	}
	return out, nil
}

// LivePin reports whether the stored pin is still in force. Activity on the
// pinned provider itself does not release it; activity on another provider
// after the pin was set does.
func LivePin(state PinState, lastEvents map[string]time.Time) (string, bool) {
	if strings.TrimSpace(state.Key) == "" {
		return "", false
	}
	for key, ts := range lastEvents {
		if key == state.Key {
			continue
		}
		if ts.After(state.PinnedAt) {
			return "", false
		}
	}
	return state.Key, true
}
