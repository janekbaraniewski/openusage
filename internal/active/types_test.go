package active

import (
	"encoding/json"
	"testing"
)

func TestSelectionJSONFieldNames(t *testing.T) {
	sel := Selection{
		Selected: "codex:default",
		Display:  "codex",
		Pinned:   true,
		Severity: SeverityWarn,
		Label:    "2d 0h/6d 21h",
		Source:   "events",
		Status:   "ok",
	}
	data, err := json.Marshal(sel)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var round map[string]any
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"selected", "display", "pinned", "severity", "label", "source", "status"} {
		if _, ok := round[key]; !ok {
			t.Errorf("missing JSON key %q in %s", key, data)
		}
	}
	if round["severity"] != "warn" {
		t.Errorf("severity = %v, want warn", round["severity"])
	}
}

func TestFactsOmitsUnknownTimestamps(t *testing.T) {
	data, err := json.Marshal(Facts{PctRemaining: nil})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got := string(data); got != "{}" {
		t.Errorf("empty Facts marshalled to %s, want {}", got)
	}
}
