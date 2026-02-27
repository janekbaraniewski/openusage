package main

import (
	"math/rand"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func buildDemoSnapshots() map[string]core.UsageSnapshot {
	now := time.Now()
	rng := rand.New(rand.NewSource(now.UnixNano()))

	snaps := map[string]core.UsageSnapshot{
		"gemini-cli":  buildGeminiCLIDemoSnapshot(now),
		"copilot":     buildCopilotDemoSnapshot(now),
		"cursor-ide":  buildCursorDemoSnapshot(now),
		"claude-code": buildClaudeCodeDemoSnapshot(now),
		"codex-cli":   buildCodexDemoSnapshot(now),
		"openrouter":  buildOpenRouterDemoSnapshot(now),
	}

	randomizeDemoSnapshots(snaps, now, rng)

	return snaps
}
