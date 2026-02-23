package providers

import (
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/alibaba_cloud"
	"github.com/janekbaraniewski/openusage/internal/providers/anthropic"
	"github.com/janekbaraniewski/openusage/internal/providers/claude_code"
	"github.com/janekbaraniewski/openusage/internal/providers/codex"
	"github.com/janekbaraniewski/openusage/internal/providers/copilot"
	"github.com/janekbaraniewski/openusage/internal/providers/cursor"
	"github.com/janekbaraniewski/openusage/internal/providers/deepseek"
	"github.com/janekbaraniewski/openusage/internal/providers/gemini_api"
	"github.com/janekbaraniewski/openusage/internal/providers/gemini_cli"
	"github.com/janekbaraniewski/openusage/internal/providers/groq"
	"github.com/janekbaraniewski/openusage/internal/providers/mistral"
	"github.com/janekbaraniewski/openusage/internal/providers/ollama"
	"github.com/janekbaraniewski/openusage/internal/providers/openai"
	"github.com/janekbaraniewski/openusage/internal/providers/opencode"
	"github.com/janekbaraniewski/openusage/internal/providers/openrouter"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
	"github.com/janekbaraniewski/openusage/internal/providers/xai"
)

func AllProviders() []core.UsageProvider {
	return []core.UsageProvider{
		openai.New(),
		anthropic.New(),
		alibaba_cloud.New(),
		openrouter.New(),
		groq.New(),
		mistral.New(),
		deepseek.New(),
		xai.New(),
		opencode.New(),
		gemini_api.New(),
		gemini_cli.New(),
		ollama.New(),
		copilot.New(),
		cursor.New(),
		claude_code.New(),
		codex.New(),
	}
}

// AllTelemetrySources derives telemetry-capable providers from the primary provider registry.
func AllTelemetrySources() []shared.TelemetrySource {
	providers := AllProviders()
	out := make([]shared.TelemetrySource, 0, len(providers))
	for _, provider := range providers {
		if source, ok := provider.(shared.TelemetrySource); ok {
			out = append(out, source)
		}
	}
	return out
}

func TelemetrySourceBySystem(system string) (shared.TelemetrySource, bool) {
	target := strings.TrimSpace(system)
	if target == "" {
		return nil, false
	}
	for _, source := range AllTelemetrySources() {
		if strings.EqualFold(source.System(), target) {
			return source, true
		}
	}
	return nil, false
}
