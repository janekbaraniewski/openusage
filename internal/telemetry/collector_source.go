package telemetry

import (
	"context"

	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

type SourceCollector struct {
	Source          shared.TelemetrySource
	Options         shared.TelemetryCollectOptions
	AccountOverride string
}

func NewSourceCollector(
	source shared.TelemetrySource,
	options shared.TelemetryCollectOptions,
	accountOverride string,
) *SourceCollector {
	return &SourceCollector{
		Source:          source,
		Options:         options,
		AccountOverride: accountOverride,
	}
}

func (c *SourceCollector) Name() string {
	if c == nil || c.Source == nil {
		return ""
	}
	return c.Source.System()
}

func (c *SourceCollector) Collect(ctx context.Context) ([]IngestRequest, error) {
	if c == nil || c.Source == nil {
		return nil, nil
	}

	events, err := c.Source.Collect(ctx, c.Options)
	if err != nil {
		return nil, err
	}

	out := make([]IngestRequest, 0, len(events))
	for _, ev := range events {
		out = append(out, mapProviderEvent(c.Source.System(), ev, c.AccountOverride))
	}
	return out, nil
}
