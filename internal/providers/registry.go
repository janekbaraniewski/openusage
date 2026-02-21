package providers

import (
	"github.com/janekbaraniewski/openusage/internal/core"
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
	"github.com/janekbaraniewski/openusage/internal/providers/openai"
	"github.com/janekbaraniewski/openusage/internal/providers/openrouter"
	"github.com/janekbaraniewski/openusage/internal/providers/xai"
)

func AllProviders() []core.QuotaProvider {
	return []core.QuotaProvider{
		openai.New(),
		anthropic.New(),
		openrouter.New(),
		groq.New(),
		mistral.New(),
		deepseek.New(),
		xai.New(),
		gemini_api.New(),
		gemini_cli.New(),
		copilot.New(),
		cursor.New(),
		claude_code.New(),
		codex.New(),
	}
}
