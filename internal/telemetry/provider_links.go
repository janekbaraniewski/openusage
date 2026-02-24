package telemetry

import (
	"sort"
	"strings"

	"github.com/samber/lo"
)

func normalizeProviderLinks(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for source, target := range in {
		s := strings.ToLower(strings.TrimSpace(source))
		t := strings.ToLower(strings.TrimSpace(target))
		if s == "" || t == "" {
			continue
		}
		out[s] = t
	}
	return out
}

func telemetrySourceProvidersForTarget(targetProvider string, links map[string]string) []string {
	target := strings.ToLower(strings.TrimSpace(targetProvider))
	if target == "" {
		return nil
	}

	set := map[string]bool{target: true}
	for source, mappedTarget := range links {
		if strings.EqualFold(strings.TrimSpace(mappedTarget), target) {
			source = strings.ToLower(strings.TrimSpace(source))
			if source != "" {
				set[source] = true
			}
		}
	}

	out := lo.Keys(set)
	sort.Strings(out)
	return out
}
