package daemon

import (
	"testing"
	"time"
)

func TestIngestedSinceReportsFreshDataOnly(t *testing.T) {
	s := &Service{}

	// Nothing ingested yet: no cached entry can be considered stale.
	if s.ingestedSince(time.Now().Add(-time.Hour)) {
		t.Fatal("ingestedSince = true before any ingest, want false")
	}

	before := time.Now().Add(-time.Second)
	s.markDataIngested()
	after := time.Now().Add(time.Second)

	if !s.dataIngested.Load() {
		t.Fatal("markDataIngested did not arm the background-refresh flag")
	}
	// A cache entry built before the ingest is stale and should refresh.
	if !s.ingestedSince(before) {
		t.Fatal("ingestedSince(before ingest) = false, want true")
	}
	// A cache entry built after the ingest is current and must not refresh.
	if s.ingestedSince(after) {
		t.Fatal("ingestedSince(after ingest) = true, want false")
	}
}

func TestIngestedSinceRequiresStrictlyNewerData(t *testing.T) {
	s := &Service{}
	s.lastIngestAt.Store(time.Unix(0, 1_000).UnixNano())

	at := time.Unix(0, 1_000)
	// Equal timestamps mean the cached entry already reflects that ingest.
	if s.ingestedSince(at) {
		t.Fatal("ingestedSince(equal timestamp) = true, want false")
	}
	if !s.ingestedSince(at.Add(-time.Nanosecond)) {
		t.Fatal("ingestedSince(older timestamp) = false, want true")
	}
}
