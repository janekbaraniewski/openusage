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
