package daemon

import (
	"testing"
	"time"
)

func TestRequestCollectionCoalescesWithoutMarkingDataIngested(t *testing.T) {
	svc := &Service{collectNow: make(chan struct{}, 1)}
	svc.requestCollection()
	svc.requestCollection()

	if got := len(svc.collectNow); got != 1 {
		t.Fatalf("queued collection requests = %d, want 1 coalesced request", got)
	}
	if svc.dataIngested.Load() {
		t.Fatal("source file change must not mark database data as ingested")
	}
}

func TestReadModelCacheLoopDisabledWithoutExporter(t *testing.T) {
	svc := &Service{}
	if svc.readModelCacheLoopEnabled() {
		t.Fatal("local daemon should build the read model on demand")
	}
}

func TestTelemetryPayloadMaintenanceBacksOffAfterDrain(t *testing.T) {
	if got := nextTelemetryPayloadMaintenanceDelay(true); got != 10*time.Minute {
		t.Fatalf("backlog delay = %s, want 10m", got)
	}
	if got := nextTelemetryPayloadMaintenanceDelay(false); got != 6*time.Hour {
		t.Fatalf("drained delay = %s, want 6h", got)
	}
}
