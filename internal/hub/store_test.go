package hub

import (
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

	snap, ok := snaps["work-mac:openai:personal"]
	if !ok {
		t.Fatal("expected key 'work-mac:openai:personal'")
	}
	if snap.AccountID != "work-mac:openai:personal" {
		t.Errorf("AccountID = %q, want work-mac:openai:personal", snap.AccountID)
	}
	if snap.ProviderID != "openai" {
		t.Errorf("ProviderID = %q, want openai (must be unchanged)", snap.ProviderID)
	}

	snap2, ok := snaps["home-linux:claude_code:default"]
	if !ok {
		t.Fatal("expected key 'home-linux:claude_code:default'")
	}
	if snap2.ProviderID != "claude_code" {
		t.Errorf("ProviderID = %q, want claude_code", snap2.ProviderID)
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
