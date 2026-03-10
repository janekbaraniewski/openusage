package core

import "strings"

const (
	MetricSourceProviderNative = "provider_native"
	MetricSourceLocalObserved  = "local_observed"
	MetricSourceGitCommits     = "git_commits"
	MetricSourceTrackedEdits   = "tracked_edits"
	MetricSourceEstimated      = "estimated"
	MetricSourceInferred       = "inferred"
	MetricSourceSynthetic      = "synthetic"
)

func (m Metric) WithSource(source string) Metric {
	m.Source = strings.TrimSpace(source)
	return m
}

func (s *UsageSnapshot) SetMetricSource(key, source string) {
	if s == nil {
		return
	}
	source = strings.TrimSpace(source)
	if key == "" || source == "" {
		return
	}
	metric, ok := s.Metrics[key]
	if !ok {
		return
	}
	metric.Source = source
	s.Metrics[key] = metric
}

func (s *UsageSnapshot) SetMetricSourceByPrefix(prefix, source string) {
	if s == nil {
		return
	}
	prefix = strings.TrimSpace(prefix)
	source = strings.TrimSpace(source)
	if prefix == "" || source == "" {
		return
	}
	for key, metric := range s.Metrics {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		metric.Source = source
		s.Metrics[key] = metric
	}
}

func (s *UsageSnapshot) SetMetricSourceBySuffix(suffix, source string) {
	if s == nil {
		return
	}
	suffix = strings.TrimSpace(suffix)
	source = strings.TrimSpace(source)
	if suffix == "" || source == "" {
		return
	}
	for key, metric := range s.Metrics {
		if !strings.HasSuffix(key, suffix) {
			continue
		}
		metric.Source = source
		s.Metrics[key] = metric
	}
}

func (s *UsageSnapshot) SetMetricSourceByWindow(window, source string) {
	if s == nil {
		return
	}
	window = strings.TrimSpace(window)
	source = strings.TrimSpace(source)
	if window == "" || source == "" {
		return
	}
	for key, metric := range s.Metrics {
		if strings.TrimSpace(metric.Window) != window {
			continue
		}
		metric.Source = source
		s.Metrics[key] = metric
	}
}

func (s *UsageSnapshot) SetMissingMetricSource(source string) {
	if s == nil {
		return
	}
	source = strings.TrimSpace(source)
	if source == "" {
		return
	}
	for key, metric := range s.Metrics {
		if strings.TrimSpace(metric.Source) != "" {
			continue
		}
		metric.Source = source
		s.Metrics[key] = metric
	}
}

func MetricSourceLabel(source string) string {
	switch strings.TrimSpace(source) {
	case MetricSourceProviderNative:
		return "native"
	case MetricSourceLocalObserved:
		return "local"
	case MetricSourceGitCommits:
		return "git"
	case MetricSourceTrackedEdits:
		return "tracked"
	case MetricSourceEstimated:
		return "estimated"
	case MetricSourceInferred:
		return "inferred"
	case MetricSourceSynthetic:
		return "synthetic"
	default:
		return strings.TrimSpace(source)
	}
}
