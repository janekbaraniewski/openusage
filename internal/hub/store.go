package hub

import (
	"fmt"
	"sync"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

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
// Each snapshot's AccountID is rewritten to "{machine}:{originalAccountID}" and
// used as the map key, keeping ProviderID intact for TUI widget rendering.
//
// Stale machine entries (older than staleTimeout) are pruned in the same
// critical section that constructs the result, so callers observe a consistent
// view.
func (s *Store) Snapshots() map[string]core.UsageSnapshot {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make(map[string]core.UsageSnapshot)
	for machine, entry := range s.machines {
		if s.staleTimeout > 0 && now.Sub(entry.receivedAt) > s.staleTimeout {
			delete(s.machines, machine)
			continue
		}
		for _, snap := range entry.envelope.Snapshots {
			key := fmt.Sprintf("%s:%s", machine, snap.AccountID)
			clone := snap.DeepClone()
			clone.AccountID = key
			out[key] = clone
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
