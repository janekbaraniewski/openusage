package providers

import (
	"github.com/janekbaraniewski/agentusage/internal/core"
	"github.com/janekbaraniewski/agentusage/internal/providers/anthropic"
	"github.com/janekbaraniewski/agentusage/internal/providers/claude_code"
	"github.com/janekbaraniewski/agentusage/internal/providers/codex"
	"github.com/janekbaraniewski/agentusage/internal/providers/copilot"
	"github.com/janekbaraniewski/agentusage/internal/providers/cursor"
	"github.com/janekbaraniewski/agentusage/internal/providers/deepseek"
	"github.com/janekbaraniewski/agentusage/internal/providers/gemini_api"
	"github.com/janekbaraniewski/agentusage/internal/providers/gemini_cli"
	"github.com/janekbaraniewski/agentusage/internal/providers/groq"
	"github.com/janekbaraniewski/agentusage/internal/providers/mistral"
	"github.com/janekbaraniewski/agentusage/internal/providers/openai"
	"github.com/janekbaraniewski/agentusage/internal/providers/openrouter"
	"github.com/janekbaraniewski/agentusage/internal/providers/xai"
)

// AllProviders returns all built-in provider adapters.
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
