package core

import "strings"

func NormalizeUsageSnapshot(s UsageSnapshot) UsageSnapshot {
	s.EnsureMaps()

	for k, v := range s.Raw {
		if k == "" || v == "" {
			continue
		}
		if isDiagnosticKey(k) {
			if _, ok := s.Diagnostics[k]; !ok {
				s.Diagnostics[k] = v
			}
			continue
		}
		if _, ok := s.Attributes[k]; !ok {
			s.Attributes[k] = v
		}
	}

	return s
}

func isDiagnosticKey(key string) bool {
	lk := strings.ToLower(strings.TrimSpace(key))
	if lk == "" {
		return false
	}
	return strings.Contains(lk, "error") ||
		strings.Contains(lk, "warning") ||
		strings.HasSuffix(lk, "_err") ||
		strings.HasSuffix(lk, "_warn")
}
