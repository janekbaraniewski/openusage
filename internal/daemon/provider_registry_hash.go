package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/providers"
)

// ProviderRegistryHash returns a stable fingerprint for the set of registered providers.
func ProviderRegistryHash() string {
	all := providers.AllProviders()
	if len(all) == 0 {
		return ""
	}

	ids := make([]string, 0, len(all))
	for _, p := range all {
		id := strings.TrimSpace(p.ID())
		if id == "" {
			id = strings.TrimSpace(p.Spec().ID)
		}
		if id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return ""
	}

	sort.Strings(ids)
	sum := sha256.Sum256([]byte(strings.Join(ids, ",")))
	return hex.EncodeToString(sum[:])
}
