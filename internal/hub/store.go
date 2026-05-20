package hub

import (
	"sync"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// keySeparator is for the *internal* dedup-detection map inside Snapshots.
// ASCII Unit Separator (0x1f) can never appear in user-supplied components,
// so two (machine, providerID, accountID) triples joined with it are equal
// iff they truly come from the same source. Must not leak into return
// values or onto the wire — terminals collapse control chars and JSON
// consumers escape them as "".
const keySeparator = "\x1f"

// displaySeparator joins the same triple into the public map key and into
// snap.AccountID. ":" is what the TUI renders, what HTTP /v1/snapshots
// publishes, and what (transitively) could be persisted to settings.json.
const displaySeparator = ":"

// Store holds the latest snapshot batch per machine, pruning stale entries.
type Store struct {
	mu           sync.Mutex
	machines     map[string]machineEntry
	staleTimeout time.Duration
}

func NewStore(staleTimeout time.Duration) *Store {
	return &Store{
		machines:     make(map[string]machineEntry),
		staleTimeout: staleTimeout,
	}
}

func (s *Store) Ingest(env core.RemoteEnvelope) {
	if env.Machine == "" {
		return
	}
	s.mu.Lock()
	s.machines[env.Machine] = machineEntry{
		envelope:   env,
		receivedAt: time.Now(),
	}
	s.mu.Unlock()
}

// Snapshots returns a flat map of UsageSnapshots from all non-stale machines.
// Both the map key and clone.AccountID are the printable
// "{machine}:{providerID}:{accountID}" form, safe to flow into the TUI, the
// HTTP JSON response, and (transitively) settings.json. ProviderID is part of
// the key because two providers on one machine can share an AccountID (e.g.
// both "default"). A separate \x1f-keyed seen-map guards against duplicate
// (providerID, accountID) pairs within a single machine's snapshot list —
// Ingest already ensures each machine name appears only once in s.machines, so
// cross-envelope dedup is not needed here. Stale entries are pruned in the
// same critical section so callers observe a consistent view.
func (s *Store) Snapshots() map[string]core.UsageSnapshot {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make(map[string]core.UsageSnapshot)
	seen := make(map[string]bool)
	for machine, entry := range s.machines {
		if s.staleTimeout > 0 && now.Sub(entry.receivedAt) > s.staleTimeout {
			delete(s.machines, machine)
			continue
		}
		for _, snap := range entry.envelope.Snapshots {
			internalKey := machine + keySeparator + snap.ProviderID + keySeparator + snap.AccountID
			if seen[internalKey] {
				continue
			}
			seen[internalKey] = true
			displayKey := machine + displaySeparator + snap.ProviderID + displaySeparator + snap.AccountID
			clone := snap.DeepClone()
			clone.AccountID = displayKey
			out[displayKey] = clone
		}
	}
	return out
}

// MachineNames returns the names of all non-stale machines currently in the store.
// Stale entries encountered during iteration are pruned in the same pass.
func (s *Store) MachineNames() []string {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()

	names := make([]string, 0, len(s.machines))
	for machine, entry := range s.machines {
		if s.staleTimeout > 0 && now.Sub(entry.receivedAt) > s.staleTimeout {
			delete(s.machines, machine)
			continue
		}
		names = append(names, machine)
	}
	return names
}
