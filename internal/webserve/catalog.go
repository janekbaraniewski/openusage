package webserve

import (
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers"
)

func providerCatalog() []CatalogEntry {
	all := providers.AllProviders()
	out := make([]CatalogEntry, 0, len(all))
	for _, p := range all {
		name := p.Describe().Name
		if name == "" {
			name = p.ID()
		}
		out = append(out, CatalogEntry{ID: p.ID(), Name: name})
	}
	return out
}

// stripRaw returns a deep-cloned snapshot list with Raw maps cleared.
// Provider probes sometimes stash credential hints there.
func stripRaw(snaps []core.UsageSnapshot) []core.UsageSnapshot {
	if len(snaps) == 0 {
		return []core.UsageSnapshot{}
	}
	out := make([]core.UsageSnapshot, len(snaps))
	for i, snap := range snaps {
		clone := snap.DeepClone()
		clone.Raw = nil
		out[i] = clone
	}
	return out
}
