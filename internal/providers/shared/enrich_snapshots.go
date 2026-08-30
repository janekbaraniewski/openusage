package shared

import (
	"context"
	"strings"
	"sync"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// SnapshotUsable reports whether a fetch result is worth applying during refresh.
func SnapshotUsable(snap core.UsageSnapshot) bool {
	if snap.Status == core.StatusAuth || snap.Status == core.StatusError {
		return len(snap.Metrics) > 0
	}
	return len(snap.Metrics) > 0 || (snap.Status != "" && snap.Status != core.StatusUnknown)
}

func accountMatchesProvider(providerID, accountID string, snap core.UsageSnapshot) bool {
	providerID = strings.TrimSpace(providerID)
	if snap.ProviderID == providerID {
		return true
	}
	accountID = strings.TrimSpace(accountID)
	if accountID == providerID {
		return true
	}
	return strings.HasPrefix(accountID, providerID+"-")
}

// EnrichSnapshotsWithFetch runs provider.Fetch for matching snapshots in parallel.
// When merge is nil, a usable fetch replaces the snapshot; otherwise merge combines
// the daemon read-model base with the live fetch result.
func EnrichSnapshotsWithFetch(
	ctx context.Context,
	providerID string,
	fetch func(context.Context, core.AccountConfig) (core.UsageSnapshot, error),
	accounts []core.AccountConfig,
	snaps map[string]core.UsageSnapshot,
	merge func(base, fresh core.UsageSnapshot) core.UsageSnapshot,
) {
	if fetch == nil || len(snaps) == 0 {
		return
	}
	byID := make(map[string]core.AccountConfig, len(accounts))
	for _, acct := range accounts {
		if id := strings.TrimSpace(acct.ID); id != "" {
			byID[id] = acct
		}
	}

	type result struct {
		id   string
		snap core.UsageSnapshot
		ok   bool
	}
	ids := make([]string, 0, len(snaps))
	for id, snap := range snaps {
		if !accountMatchesProvider(providerID, id, snap) {
			continue
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return
	}

	results := make(chan result, len(ids))
	var wg sync.WaitGroup
	for _, id := range ids {
		acct, ok := byID[id]
		if !ok {
			acct = core.AccountConfig{ID: id, Provider: providerID}
		}
		wg.Add(1)
		go func(id string, acct core.AccountConfig) {
			defer wg.Done()
			if err := ctx.Err(); err != nil {
				results <- result{id: id}
				return
			}
			fresh, err := fetch(ctx, acct)
			if err != nil || !SnapshotUsable(fresh) {
				results <- result{id: id}
				return
			}
			results <- result{id: id, snap: fresh, ok: true}
		}(id, acct)
	}
	wg.Wait()
	close(results)
	for res := range results {
		if !res.ok {
			continue
		}
		if merge != nil {
			snaps[res.id] = merge(snaps[res.id], res.snap)
		} else {
			snaps[res.id] = res.snap
		}
	}
}

// OverlayLiveFetch merges live fetch fields into a daemon read-model snapshot,
// preserving telemetry-derived collections from the base snapshot.
func OverlayLiveFetch(base, fresh core.UsageSnapshot) core.UsageSnapshot {
	if !SnapshotUsable(fresh) {
		return base
	}
	merged := base
	merged.EnsureMaps()
	fresh.EnsureMaps()
	if !fresh.Timestamp.IsZero() {
		merged.Timestamp = fresh.Timestamp
	}
	if fresh.Status != "" {
		merged.Status = fresh.Status
	}
	if fresh.Message != "" {
		merged.Message = fresh.Message
	}
	for k, v := range fresh.Metrics {
		merged.Metrics[k] = v
	}
	for k, v := range fresh.Resets {
		merged.Resets[k] = v
	}
	for k, v := range fresh.Attributes {
		merged.Attributes[k] = v
	}
	for k, v := range fresh.Diagnostics {
		merged.Diagnostics[k] = v
	}
	for k, v := range fresh.Raw {
		merged.Raw[k] = v
	}
	return merged
}
