package main

import (
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func buildDemoSnapshots() map[string]core.UsageSnapshot {
	return buildDemoSnapshotsAt(time.Now())
}

func buildDemoSnapshotsAt(now time.Time) map[string]core.UsageSnapshot {
	snaps := map[string]core.UsageSnapshot{
		"gemini-cli":  buildGeminiCLIDemoSnapshot(now),
		"copilot":     buildCopilotDemoSnapshot(now),
		"cursor-ide":  buildCursorDemoSnapshot(now),
		"claude-code": buildClaudeCodeDemoSnapshot(now),
		"codex-cli":   buildCodexDemoSnapshot(now),
		"openrouter":  buildOpenRouterDemoSnapshot(now),
		"ollama":      buildOllamaDemoSnapshot(now),
	}

	return snaps
}
