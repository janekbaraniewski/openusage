package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers"
	"github.com/samber/lo"
)

// ProviderRegistryHash returns a stable fingerprint for the set of registered providers.
func ProviderRegistryHash() string {
	all := providers.AllProviders()
	if len(all) == 0 {
		return ""
	}

	ids := core.SortedCompactStrings(lo.Map(all, func(provider core.UsageProvider, _ int) string {
		id := strings.TrimSpace(provider.ID())
		if id == "" {
			id = strings.TrimSpace(provider.Spec().ID)
		}
		return id
	}))
	if len(ids) == 0 {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.Join(ids, ",")))
	return hex.EncodeToString(sum[:])
}
