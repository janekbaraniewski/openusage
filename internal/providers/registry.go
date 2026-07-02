package providers

import (
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/alibaba_cloud"
	"github.com/janekbaraniewski/openusage/internal/providers/amp"
	"github.com/janekbaraniewski/openusage/internal/providers/anthropic"
	"github.com/janekbaraniewski/openusage/internal/providers/azure_openai"
	"github.com/janekbaraniewski/openusage/internal/providers/claude_code"
	"github.com/janekbaraniewski/openusage/internal/providers/codebuff"
	"github.com/janekbaraniewski/openusage/internal/providers/codex"
	"github.com/janekbaraniewski/openusage/internal/providers/copilot"
	"github.com/janekbaraniewski/openusage/internal/providers/crush"
	"github.com/janekbaraniewski/openusage/internal/providers/cursor"
	"github.com/janekbaraniewski/openusage/internal/providers/deepseek"
	"github.com/janekbaraniewski/openusage/internal/providers/droid"
	"github.com/janekbaraniewski/openusage/internal/providers/gemini_api"
	"github.com/janekbaraniewski/openusage/internal/providers/gemini_cli"
	"github.com/janekbaraniewski/openusage/internal/providers/goose"
	"github.com/janekbaraniewski/openusage/internal/providers/groq"
	"github.com/janekbaraniewski/openusage/internal/providers/hermes"
	"github.com/janekbaraniewski/openusage/internal/providers/kilocode"
	"github.com/janekbaraniewski/openusage/internal/providers/kimi_cli"
	"github.com/janekbaraniewski/openusage/internal/providers/kiro"
	"github.com/janekbaraniewski/openusage/internal/providers/mistral"
	"github.com/janekbaraniewski/openusage/internal/providers/moonshot"
	"github.com/janekbaraniewski/openusage/internal/providers/mux"
	"github.com/janekbaraniewski/openusage/internal/providers/ollama"
	"github.com/janekbaraniewski/openusage/internal/providers/openai"
	"github.com/janekbaraniewski/openusage/internal/providers/openclaw"
	"github.com/janekbaraniewski/openusage/internal/providers/opencode"
	"github.com/janekbaraniewski/openusage/internal/providers/openrouter"
	"github.com/janekbaraniewski/openusage/internal/providers/perplexity"
	"github.com/janekbaraniewski/openusage/internal/providers/pi"
	"github.com/janekbaraniewski/openusage/internal/providers/qwen_cli"
	"github.com/janekbaraniewski/openusage/internal/providers/roocode"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
	"github.com/janekbaraniewski/openusage/internal/providers/xai"
	"github.com/janekbaraniewski/openusage/internal/providers/zai"
	"github.com/janekbaraniewski/openusage/internal/providers/zed"
)

func AllProviders() []core.UsageProvider {
	return []core.UsageProvider{
		openai.New(),
		anthropic.New(),
		azure_openai.New(),
		alibaba_cloud.New(),
		openrouter.New(),
		perplexity.New(),
		groq.New(),
		mistral.New(),
		moonshot.New(),
		deepseek.New(),
		xai.New(),
		zai.New(),
		opencode.New(),
		gemini_api.New(),
		gemini_cli.New(),
		ollama.New(),
		copilot.New(),
		cursor.New(),
		claude_code.New(),
		codex.New(),
		amp.New(),
		goose.New(),
		hermes.New(),
		mux.New(),
		droid.New(),
		crush.New(),
		roocode.New(),
		kilocode.New(),
		kiro.New(),
		zed.New(),
		codebuff.New(),
		kimi_cli.New(),
		openclaw.New(),
		pi.New(),
		qwen_cli.New(),
	}
}

func TelemetrySourceBySystem(system string) (shared.TelemetrySource, bool) {
	target := strings.TrimSpace(system)
	if target == "" {
		return nil, false
	}
	for _, provider := range AllProviders() {
		source, ok := provider.(shared.TelemetrySource)
		if !ok {
			continue
		}
		if strings.EqualFold(source.System(), target) {
			return source, true
		}
	}
	return nil, false
}
