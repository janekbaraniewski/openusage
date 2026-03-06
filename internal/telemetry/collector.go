package telemetry

import "context"

type Collector interface {
	Name() string
	Collect(ctx context.Context) ([]IngestRequest, error)
}
