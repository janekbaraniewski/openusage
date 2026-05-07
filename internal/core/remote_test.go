package core

import (
	"encoding/json"
	"testing"
	"time"
)

func TestRemoteEnvelopeJSONRoundtrip(t *testing.T) {
	snap := NewUsageSnapshot("openai", "personal")
	snap.Status = StatusOK
	v := 100.0
	snap.Metrics["rpm"] = Metric{Limit: &v, Remaining: &v, Unit: "requests", Window: "1m"}

	env := RemoteEnvelope{
		Machine:   "work-mac",
		SentAt:    time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC),
		Snapshots: []UsageSnapshot{snap},
	}

	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got RemoteEnvelope
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Machine != env.Machine {
		t.Errorf("Machine = %q, want %q", got.Machine, env.Machine)
	}
	if !got.SentAt.Equal(env.SentAt) {
		t.Errorf("SentAt = %v, want %v", got.SentAt, env.SentAt)
	}
	if len(got.Snapshots) != 1 {
		t.Fatalf("Snapshots len = %d, want 1", len(got.Snapshots))
	}
	if got.Snapshots[0].ProviderID != "openai" {
		t.Errorf("ProviderID = %q, want openai", got.Snapshots[0].ProviderID)
	}
	if got.Snapshots[0].AccountID != "personal" {
		t.Errorf("AccountID = %q, want personal", got.Snapshots[0].AccountID)
	}
}

func TestRemoteEnvelopeEmptySnapshots(t *testing.T) {
	env := RemoteEnvelope{Machine: "host1", SentAt: time.Now()}
	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got RemoteEnvelope
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Machine != "host1" {
		t.Errorf("Machine = %q, want host1", got.Machine)
	}
	if len(got.Snapshots) != 0 {
		t.Errorf("expected empty snapshots, got %d", len(got.Snapshots))
	}
}
