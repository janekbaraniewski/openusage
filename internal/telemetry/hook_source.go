package telemetry

import (
	"fmt"

	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

func ParseSourceHookPayload(
	source shared.TelemetrySource,
	raw []byte,
	options shared.TelemetryCollectOptions,
	accountOverride string,
) ([]IngestRequest, error) {
	if source == nil {
		return nil, fmt.Errorf("nil source")
	}

	events, err := source.ParseHookPayload(raw, options)
	if err != nil {
		return nil, err
	}

	out := make([]IngestRequest, 0, len(events))
	for _, ev := range events {
		out = append(out, mapProviderEvent(source.System(), ev, accountOverride))
	}
	return out, nil
}
