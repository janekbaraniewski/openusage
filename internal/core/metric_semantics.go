package core

func MetricUsedPercent(key string, m Metric) float64 {
	if key == "context_window" {
		return -1
	}
	if m.Unit == "%" && m.Used != nil {
		return *m.Used
	}
	if m.Limit != nil && m.Remaining != nil && *m.Limit > 0 {
		return (*m.Limit - *m.Remaining) / *m.Limit * 100
	}
	if m.Limit != nil && m.Used != nil && *m.Limit > 0 {
		return *m.Used / *m.Limit * 100
	}
	return -1
}
