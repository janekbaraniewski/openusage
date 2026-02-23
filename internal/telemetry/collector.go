package telemetry

import (
	"context"
	"strings"
)

type Collector interface {
	Name() string
	Collect(ctx context.Context) ([]IngestRequest, error)
}

func float64Ptr(v float64) *float64 {
	vv := v
	return &vv
}

func firstNonEmptyNonBlank(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
