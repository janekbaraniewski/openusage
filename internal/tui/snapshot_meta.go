package tui

import "github.com/janekbaraniewski/openusage/internal/core"

func snapshotMeta(snap core.UsageSnapshot, key string) string {
	if v, ok := snap.MetaValue(key); ok {
		return v
	}
	return ""
}

func snapshotMetaEntries(snap core.UsageSnapshot) map[string]string {
	out := make(map[string]string, len(snap.Attributes)+len(snap.Raw))
	for k, v := range snap.Attributes {
		out[k] = v
	}
	for k, v := range snap.Raw {
		if _, exists := out[k]; !exists {
			out[k] = v
		}
	}
	return out
}
