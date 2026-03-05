package copilot

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

const (
	telemetrySchemaVersion   = "copilot_v1"
	defaultCopilotSessionDir = ".copilot/session-state"
)

// System returns the telemetry system identifier for the copilot provider.
func (p *Provider) System() string { return p.ID() }

// Collect scans copilot session-state directories for events.jsonl files and
// extracts usage telemetry events from assistant.usage entries.
func (p *Provider) Collect(ctx context.Context, opts shared.TelemetryCollectOptions) ([]shared.TelemetryEvent, error) {
	sessionDir := shared.ExpandHome(opts.Path("sessions_dir", defaultCopilotSessionsDir()))
	if sessionDir == "" {
		return nil, nil
	}

	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		return nil, nil
	}

	var out []shared.TelemetryEvent
	for _, entry := range entries {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		if !entry.IsDir() {
			continue
		}
		eventsPath := filepath.Join(sessionDir, entry.Name(), "events.jsonl")
		events, err := parseCopilotTelemetrySessionFile(eventsPath, entry.Name())
		if err != nil {
			continue
		}
		out = append(out, events...)
	}
	return out, nil
}

// ParseHookPayload is not supported for the copilot provider.
func (p *Provider) ParseHookPayload(_ []byte, _ shared.TelemetryCollectOptions) ([]shared.TelemetryEvent, error) {
	return nil, shared.ErrHookUnsupported
}

// defaultCopilotSessionsDir returns the default copilot session-state directory.
func defaultCopilotSessionsDir() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, defaultCopilotSessionDir)
}

// parseCopilotTelemetrySessionFile parses a single session's events.jsonl and
// produces telemetry events from assistant.usage and assistant.message entries.
func parseCopilotTelemetrySessionFile(path, sessionID string) ([]shared.TelemetryEvent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(data), "\n")
	currentModel := ""
	workspaceID := ""
	turnIndex := 0

	var out []shared.TelemetryEvent
	for lineNum, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var evt sessionEvent
		if json.Unmarshal([]byte(line), &evt) != nil {
			continue
		}

		switch evt.Type {
		case "session.start":
			var start sessionStartData
			if json.Unmarshal(evt.Data, &start) == nil {
				if start.Context.CWD != "" {
					workspaceID = shared.SanitizeWorkspace(start.Context.CWD)
				}
			}

		case "session.model_change":
			var mc modelChangeData
			if json.Unmarshal(evt.Data, &mc) == nil && mc.NewModel != "" {
				currentModel = mc.NewModel
			}

		case "session.info":
			var info sessionInfoData
			if json.Unmarshal(evt.Data, &info) == nil && info.InfoType == "model" {
				if m := extractModelFromInfoMsg(info.Message); m != "" {
					currentModel = m
				}
			}

		case "assistant.usage":
			var usage assistantUsageData
			if json.Unmarshal(evt.Data, &usage) != nil {
				continue
			}

			model := usage.Model
			if model == "" {
				model = currentModel
			}
			if model == "" {
				continue
			}

			turnIndex++
			occurredAt := time.Now().UTC()
			if ts := shared.FlexParseTime(evt.Timestamp); !ts.IsZero() {
				occurredAt = ts
			}

			turnID := fmt.Sprintf("%s:%d", sessionID, turnIndex)
			if strings.TrimSpace(evt.ID) != "" {
				turnID = strings.TrimSpace(evt.ID)
			}
			messageID := fmt.Sprintf("%s:%d", sessionID, lineNum+1)

			totalTokens := int64(usage.InputTokens + usage.OutputTokens)

			te := shared.TelemetryEvent{
				SchemaVersion: telemetrySchemaVersion,
				Channel:       shared.TelemetryChannelJSONL,
				OccurredAt:    occurredAt,
				AccountID:     "copilot",
				WorkspaceID:   workspaceID,
				SessionID:     sessionID,
				TurnID:        turnID,
				MessageID:     messageID,
				ProviderID:    "copilot",
				AgentName:     "copilot",
				EventType:     shared.TelemetryEventTypeMessageUsage,
				ModelRaw:      model,
				InputTokens:   shared.Int64Ptr(int64(usage.InputTokens)),
				OutputTokens:  shared.Int64Ptr(int64(usage.OutputTokens)),
				TotalTokens:   shared.Int64Ptr(totalTokens),
				Requests:      shared.Int64Ptr(1),
				Status:        shared.TelemetryStatusOK,
				Payload: map[string]any{
					"source_file":       path,
					"line":              lineNum + 1,
					"upstream_provider": "github",
				},
			}

			if usage.CacheReadTokens > 0 {
				te.CacheReadTokens = shared.Int64Ptr(int64(usage.CacheReadTokens))
			}
			if usage.CacheWriteTokens > 0 {
				te.CacheWriteTokens = shared.Int64Ptr(int64(usage.CacheWriteTokens))
			}
			if usage.Cost > 0 {
				te.CostUSD = shared.Float64Ptr(usage.Cost)
			}

			out = append(out, te)
		}
	}

	return out, nil
}
