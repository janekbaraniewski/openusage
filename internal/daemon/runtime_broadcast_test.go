package daemon

import (
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestSnapshotFingerprint_Empty(t *testing.T) {
	fp := snapshotFingerprint(nil)
	if fp != "" {
		t.Fatalf("nil snapshots: got %q, want empty", fp)
	}

	fp = snapshotFingerprint(map[string]core.UsageSnapshot{})
	if fp != "" {
		t.Fatalf("empty snapshots: got %q, want empty", fp)
	}
}

func TestSnapshotFingerprint_Deterministic(t *testing.T) {
	ts := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	snaps := map[string]core.UsageSnapshot{
		"openai":    {Timestamp: ts},
		"anthropic": {Timestamp: ts.Add(time.Hour)},
	}

	fp1 := snapshotFingerprint(snaps)
	fp2 := snapshotFingerprint(snaps)
	if fp1 != fp2 {
		t.Fatalf("fingerprint not deterministic: %q != %q", fp1, fp2)
	}
}

func TestSnapshotFingerprint_DiffersOnTimestampChange(t *testing.T) {
	ts := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	snaps1 := map[string]core.UsageSnapshot{
		"openai": {Timestamp: ts},
	}
	snaps2 := map[string]core.UsageSnapshot{
		"openai": {Timestamp: ts.Add(time.Second)},
	}

	fp1 := snapshotFingerprint(snaps1)
	fp2 := snapshotFingerprint(snaps2)
	if fp1 == fp2 {
		t.Fatal("fingerprints should differ when timestamp changes")
	}
}

func TestSnapshotFingerprint_DiffersOnKeyChange(t *testing.T) {
	ts := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	snaps1 := map[string]core.UsageSnapshot{
		"openai": {Timestamp: ts},
	}
	snaps2 := map[string]core.UsageSnapshot{
		"anthropic": {Timestamp: ts},
	}

	fp1 := snapshotFingerprint(snaps1)
	fp2 := snapshotFingerprint(snaps2)
	if fp1 == fp2 {
		t.Fatal("fingerprints should differ when keys change")
	}
}

func TestSnapshotFingerprint_DiffersOnMetricCountChange(t *testing.T) {
	ts := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	snaps1 := map[string]core.UsageSnapshot{
		"claude_code": {Timestamp: ts, Metrics: map[string]core.Metric{"rpm": {}}},
	}
	snaps2 := map[string]core.UsageSnapshot{
		"claude_code": {Timestamp: ts, Metrics: map[string]core.Metric{"rpm": {}, "tpm": {}}},
	}

	fp1 := snapshotFingerprint(snaps1)
	fp2 := snapshotFingerprint(snaps2)
	if fp1 == fp2 {
		t.Fatal("fingerprints should differ when metric count changes (telemetry enrichment)")
	}
}
