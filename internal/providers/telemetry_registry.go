package providers

import (
	"github.com/janekbaraniewski/openusage/internal/providers/claude_code"
	"github.com/janekbaraniewski/openusage/internal/providers/codex"
	"github.com/janekbaraniewski/openusage/internal/providers/opencode"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

// AllTelemetrySources returns registered provider telemetry adapters.
func AllTelemetrySources() []shared.TelemetrySource {
	return []shared.TelemetrySource{
		codex.NewTelemetrySource(),
		claude_code.NewTelemetrySource(),
		opencode.NewTelemetrySource(),
	}
}

func TelemetrySourceBySystem(system string) (shared.TelemetrySource, bool) {
	for _, source := range AllTelemetrySources() {
		if source.System() == system {
			return source, true
		}
	}
	return nil, false
}
