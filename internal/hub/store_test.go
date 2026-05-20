package hub

import (
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func makeSnap(providerID, accountID string) core.UsageSnapshot {
	snap := core.NewUsageSnapshot(providerID, accountID)
	snap.Status = core.StatusOK
	v := 100.0
	snap.Metrics["rpm"] = core.Metric{Limit: &v, Remaining: &v, Unit: "requests", Window: "1m"}
	return snap
}

func TestStore_IngestAndSnapshots(t *testing.T) {
	store := NewStore(5 * time.Minute)

	store.Ingest(core.RemoteEnvelope{
		Machine:   "work-mac",
		SentAt:    time.Now(),
		Snapshots: []core.UsageSnapshot{makeSnap("openai", "personal")},
	})
	store.Ingest(core.RemoteEnvelope{
		Machine:   "home-linux",
		SentAt:    time.Now(),
		Snapshots: []core.UsageSnapshot{makeSnap("claude_code", "default")},
	})

	snaps := store.Snapshots()
	if len(snaps) != 2 {
		t.Fatalf("expected 2 snapshots, got %d", len(snaps))
	}

	wantKey1 := "work-mac:openai:personal"
	snap, ok := snaps[wantKey1]
	if !ok {
		t.Fatalf("expected key %q", wantKey1)
	}
	if snap.AccountID != wantKey1 {
		t.Errorf("AccountID = %q, want %q (map key and AccountID must match)", snap.AccountID, wantKey1)
	}
	if snap.ProviderID != "openai" {
		t.Errorf("ProviderID = %q, want openai (must be unchanged)", snap.ProviderID)
	}

	wantKey2 := "home-linux:claude_code:default"
	snap2, ok := snaps[wantKey2]
	if !ok {
		t.Fatalf("expected key %q", wantKey2)
	}
	if snap2.AccountID != wantKey2 {
		t.Errorf("AccountID = %q, want %q", snap2.AccountID, wantKey2)
	}
	if snap2.ProviderID != "claude_code" {
		t.Errorf("ProviderID = %q, want claude_code", snap2.ProviderID)
	}
}

// TestStore_KeysAreReadable pins the invariant that nothing returned by
// Snapshots() leaks the internal \x1f separator. Without this guard the
// hub keys would surface in the HTTP JSON response, m.providerOrder, and
// (transitively) settings.json — collapsing in terminals and producing
// unusable persisted account IDs.
func TestStore_KeysAreReadable(t *testing.T) {
	store := NewStore(5 * time.Minute)
	store.Ingest(core.RemoteEnvelope{
		Machine:   "work-mac",
		SentAt:    time.Now(),
		Snapshots: []core.UsageSnapshot{makeSnap("openai", "personal")},
	})
	isControl := func(r rune) bool { return r < 0x20 }
	for key, snap := range store.Snapshots() {
		if strings.IndexFunc(key, isControl) >= 0 {
			t.Errorf("map key %q contains control char — leaks to JSON/TUI/config", key)
		}
		if strings.IndexFunc(snap.AccountID, isControl) >= 0 {
			t.Errorf("snap.AccountID %q contains control char", snap.AccountID)
		}
	}
}

// TestStore_NoCollisionAcrossProviders verifies the regression fix from PR #139
// review: two providers on the same machine sharing an AccountID must not
// overwrite each other in the flat Snapshots map.
func TestStore_NoCollisionAcrossProviders(t *testing.T) {
	store := NewStore(5 * time.Minute)
	store.Ingest(core.RemoteEnvelope{
		Machine: "work-mac",
		SentAt:  time.Now(),
		Snapshots: []core.UsageSnapshot{
			makeSnap("openai", "default"),
			makeSnap("anthropic", "default"),
		},
	})

	snaps := store.Snapshots()
	if len(snaps) != 2 {
		t.Fatalf("expected 2 snapshots (one per provider), got %d", len(snaps))
	}
	if _, ok := snaps["work-mac:openai:default"]; !ok {
		t.Error("missing key 'work-mac:openai:default'")
	}
	if _, ok := snaps["work-mac:anthropic:default"]; !ok {
		t.Error("missing key 'work-mac:anthropic:default'")
	}
}

// TestStore_DisplayKeyCollision documents the known trade-off of using
// ":" as the public separator: two distinct (machine, providerID,
// accountID) triples whose ":" joins happen to coincide produce one entry
// in the output, because the second write lands at the same map key. The
// contrived example below relies on a provider literally named "mac" —
// providers are a fixed snake_case set in this codebase, so this case is
// not reachable today, but the test pins the documented behaviour for
// anyone considering loosening the provider naming rule.
func TestStore_DisplayKeyCollision(t *testing.T) {
	store := NewStore(5 * time.Minute)
	store.Ingest(core.RemoteEnvelope{
		Machine:   "work:mac",
		SentAt:    time.Now(),
		Snapshots: []core.UsageSnapshot{makeSnap("openai", "default")},
	})
	store.Ingest(core.RemoteEnvelope{
		Machine:   "work",
		SentAt:    time.Now(),
		Snapshots: []core.UsageSnapshot{makeSnap("mac", "openai:default")},
	})

	snaps := store.Snapshots()
	// Both rows join to "work:mac:openai:default". Go map iteration is
	// non-deterministic, so whichever machine is visited last overwrites
	// the other in out[]. The net result is always exactly 1 entry.
	if len(snaps) != 1 {
		t.Fatalf("expected 1 snapshot after printable-key collision, got %d", len(snaps))
	}
	if _, ok := snaps["work:mac:openai:default"]; !ok {
		t.Error("missing colliding key 'work:mac:openai:default'")
	}
}

func TestStore_StaleEviction(t *testing.T) {
	store := NewStore(10 * time.Millisecond)

	store.Ingest(core.RemoteEnvelope{
		Machine:   "old-machine",
		SentAt:    time.Now(),
		Snapshots: []core.UsageSnapshot{makeSnap("openai", "acct")},
	})

	time.Sleep(30 * time.Millisecond)

	snaps := store.Snapshots()
	if len(snaps) != 0 {
		t.Errorf("expected stale entry to be pruned, got %d snapshots", len(snaps))
	}
}

func TestStore_OverwriteSameMachine(t *testing.T) {
	store := NewStore(5 * time.Minute)

	store.Ingest(core.RemoteEnvelope{
		Machine:   "machine1",
		SentAt:    time.Now(),
		Snapshots: []core.UsageSnapshot{makeSnap("openai", "acct1"), makeSnap("anthropic", "acct2")},
	})
	store.Ingest(core.RemoteEnvelope{
		Machine:   "machine1",
		SentAt:    time.Now(),
		Snapshots: []core.UsageSnapshot{makeSnap("openai", "acct1")},
	})

	snaps := store.Snapshots()
	if len(snaps) != 1 {
		t.Errorf("expected 1 snapshot after overwrite, got %d", len(snaps))
	}
}

func TestStore_EmptyMachineIgnored(t *testing.T) {
	store := NewStore(5 * time.Minute)
	store.Ingest(core.RemoteEnvelope{
		Machine:   "",
		Snapshots: []core.UsageSnapshot{makeSnap("openai", "acct")},
	})
	if len(store.Snapshots()) != 0 {
		t.Error("expected empty-machine envelope to be ignored")
	}
}

func TestStore_MachineNames(t *testing.T) {
	store := NewStore(5 * time.Minute)
	store.Ingest(core.RemoteEnvelope{Machine: "alpha", Snapshots: nil})
	store.Ingest(core.RemoteEnvelope{Machine: "beta", Snapshots: nil})

	names := store.MachineNames()
	if len(names) != 2 {
		t.Errorf("expected 2 machine names, got %d", len(names))
	}
}
