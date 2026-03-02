package core

import (
	"testing"
	"time"
)

func float64Ptr(v float64) *float64 { return &v }

func TestMetricPercent(t *testing.T) {
	tests := []struct {
		name string
		m    Metric
		want float64
	}{
		{
			name: "remaining and limit",
			m:    Metric{Limit: float64Ptr(100), Remaining: float64Ptr(75), Unit: "requests", Window: "1m"},
			want: 75.0,
		},
		{
			name: "used and limit",
			m:    Metric{Limit: float64Ptr(1000), Used: float64Ptr(400), Unit: "tokens", Window: "1m"},
			want: 60.0,
		},
		{
			name: "no data",
			m:    Metric{Unit: "requests", Window: "1m"},
			want: -1,
		},
		{
			name: "zero limit",
			m:    Metric{Limit: float64Ptr(0), Remaining: float64Ptr(0), Unit: "requests", Window: "1m"},
			want: -1,
		},
		{
			name: "fully consumed",
			m:    Metric{Limit: float64Ptr(100), Remaining: float64Ptr(0), Unit: "requests", Window: "1m"},
			want: 0.0,
		},
		{
			name: "fully available",
			m:    Metric{Limit: float64Ptr(100), Remaining: float64Ptr(100), Unit: "requests", Window: "1m"},
			want: 100.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.m.Percent()
			if got != tt.want {
				t.Errorf("Percent() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUsageSnapshotWorstPercent(t *testing.T) {
	snap := UsageSnapshot{
		Timestamp: time.Now(),
		Metrics: map[string]Metric{
			"rpm": {Limit: float64Ptr(100), Remaining: float64Ptr(80), Unit: "requests", Window: "1m"},
			"tpm": {Limit: float64Ptr(10000), Remaining: float64Ptr(500), Unit: "tokens", Window: "1m"},
		},
	}

	got := snap.WorstPercent()
	want := 5.0 // 500/10000 = 5%
	if got != want {
		t.Errorf("WorstPercent() = %v, want %v", got, want)
	}
}

func TestUsageSnapshotDeepClone(t *testing.T) {
	original := UsageSnapshot{
		ProviderID: "test",
		AccountID:  "acct1",
		Status:     StatusOK,
		Metrics: map[string]Metric{
			"rpm": {Limit: float64Ptr(100), Remaining: float64Ptr(80), Used: float64Ptr(20), Unit: "requests", Window: "1m"},
			"tpm": {Limit: float64Ptr(10000), Used: float64Ptr(500), Unit: "tokens", Window: "1m"},
		},
		Resets:      map[string]time.Time{"rpm_reset": time.Now()},
		Attributes:  map[string]string{"plan": "pro"},
		Diagnostics: map[string]string{"note": "ok"},
		Raw:         map[string]string{"debug": "info"},
		DailySeries: map[string][]TimePoint{
			"cost": {{Date: "2025-01-01", Value: 1.5}},
		},
	}

	clone := original.DeepClone()

	// Mutate clone maps
	clone.Metrics["new_key"] = Metric{Unit: "test"}
	*clone.Metrics["rpm"].Used = 999
	clone.Attributes["plan"] = "free"
	clone.Diagnostics["note"] = "changed"
	clone.Raw["debug"] = "mutated"
	clone.DailySeries["cost"][0].Value = 999
	clone.DailySeries["new_series"] = []TimePoint{{Date: "2025-02-01", Value: 2}}

	// Verify original is unchanged
	if len(original.Metrics) != 2 {
		t.Fatalf("original metrics count = %d, want 2", len(original.Metrics))
	}
	if _, ok := original.Metrics["new_key"]; ok {
		t.Fatal("original should not have new_key")
	}
	if *original.Metrics["rpm"].Used != 20 {
		t.Fatalf("original rpm.Used = %v, want 20", *original.Metrics["rpm"].Used)
	}
	if original.Attributes["plan"] != "pro" {
		t.Fatalf("original plan = %q, want pro", original.Attributes["plan"])
	}
	if original.Diagnostics["note"] != "ok" {
		t.Fatalf("original note = %q, want ok", original.Diagnostics["note"])
	}
	if original.Raw["debug"] != "info" {
		t.Fatalf("original debug = %q, want info", original.Raw["debug"])
	}
	if original.DailySeries["cost"][0].Value != 1.5 {
		t.Fatalf("original cost series = %v, want 1.5", original.DailySeries["cost"][0].Value)
	}
	if _, ok := original.DailySeries["new_series"]; ok {
		t.Fatal("original should not have new_series")
	}
}

func TestDeepCloneSnapshots(t *testing.T) {
	snaps := map[string]UsageSnapshot{
		"a": {
			ProviderID: "test",
			Metrics:    map[string]Metric{"rpm": {Used: float64Ptr(10), Unit: "requests"}},
		},
	}

	cloned := DeepCloneSnapshots(snaps)

	// Mutate clone
	cloned["b"] = UsageSnapshot{ProviderID: "new"}
	*cloned["a"].Metrics["rpm"].Used = 999

	// Original unchanged
	if len(snaps) != 1 {
		t.Fatalf("original map len = %d, want 1", len(snaps))
	}
	if *snaps["a"].Metrics["rpm"].Used != 10 {
		t.Fatalf("original rpm.Used = %v, want 10", *snaps["a"].Metrics["rpm"].Used)
	}
}

func TestDeepCloneSnapshotsNil(t *testing.T) {
	if got := DeepCloneSnapshots(nil); got != nil {
		t.Fatalf("DeepCloneSnapshots(nil) = %v, want nil", got)
	}
}

func TestUsageSnapshotWorstPercentNoData(t *testing.T) {
	snap := UsageSnapshot{
		Timestamp: time.Now(),
		Metrics:   map[string]Metric{},
	}

	got := snap.WorstPercent()
	want := float64(-1)
	if got != want {
		t.Errorf("WorstPercent() = %v, want %v", got, want)
	}
}
