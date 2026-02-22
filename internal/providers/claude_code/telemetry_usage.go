package claude_code

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

const telemetryScannerBufferSize = 8 * 1024 * 1024

type telemetrySource struct{}

func NewTelemetrySource() shared.TelemetrySource { return telemetrySource{} }

func (telemetrySource) System() string { return "claude_code" }

func (telemetrySource) Collect(ctx context.Context, opts shared.TelemetryCollectOptions) ([]shared.TelemetryEvent, error) {
	defaultProjectsDir, defaultAltProjectsDir := DefaultTelemetryProjectsDirs()
	projectsDir := shared.ExpandHome(opts.Path("projects_dir", defaultProjectsDir))
	altProjectsDir := shared.ExpandHome(opts.Path("alt_projects_dir", defaultAltProjectsDir))

	files := shared.CollectFilesByExt([]string{projectsDir, altProjectsDir}, map[string]bool{".jsonl": true})
	if len(files) == 0 {
		return nil, nil
	}

	var out []shared.TelemetryEvent
	for _, file := range files {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		events, err := ParseTelemetryConversationFile(file)
		if err != nil {
			continue
		}
		out = append(out, events...)
	}
	return out, nil
}

func (telemetrySource) ParseHookPayload(_ []byte, _ shared.TelemetryCollectOptions) ([]shared.TelemetryEvent, error) {
	return nil, shared.ErrHookUnsupported
}

// DefaultTelemetryProjectsDirs returns the default Claude Code conversation roots.
func DefaultTelemetryProjectsDirs() (string, string) {
	home, _ := os.UserHomeDir()
	if strings.TrimSpace(home) == "" {
		return "", ""
	}
	return filepath.Join(home, ".claude", "projects"), filepath.Join(home, ".config", "claude", "projects")
}

// ParseTelemetryConversationFile parses a Claude Code conversation JSONL file
// and emits message/tool telemetry events.
func ParseTelemetryConversationFile(path string) ([]shared.TelemetryEvent, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	seenUsage := make(map[string]bool)
	seenTools := make(map[string]bool)
	var out []shared.TelemetryEvent

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 512*1024), telemetryScannerBufferSize)
	lineNumber := 0

	for scanner.Scan() {
		lineNumber++
		var entry jsonlEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		if entry.Type != "assistant" || entry.Message == nil || entry.Message.Usage == nil {
			continue
		}

		usageKey := claudeTelemetryUsageDedupKey(entry)
		if usageKey != "" && seenUsage[usageKey] {
			continue
		}
		if usageKey != "" {
			seenUsage[usageKey] = true
		}

		ts := time.Now().UTC()
		if parsed, err := shared.ParseTimestampString(entry.Timestamp); err == nil {
			ts = parsed
		}

		model := strings.TrimSpace(entry.Message.Model)
		if model == "" {
			model = "unknown"
		}

		usage := entry.Message.Usage
		totalTokens := int64(
			usage.InputTokens +
				usage.OutputTokens +
				usage.CacheReadInputTokens +
				usage.CacheCreationInputTokens +
				usage.ReasoningTokens,
		)
		cost := estimateCost(model, usage)

		turnID := shared.FirstNonEmpty(entry.RequestID, entry.Message.ID)
		if turnID == "" {
			turnID = fmt.Sprintf("%s:%d", strings.TrimSpace(entry.SessionID), lineNumber)
		}
		messageID := strings.TrimSpace(entry.Message.ID)
		if messageID == "" {
			messageID = turnID
		}

		out = append(out, shared.TelemetryEvent{
			SchemaVersion:    "claude_jsonl_v1",
			Channel:          shared.TelemetryChannelJSONL,
			OccurredAt:       ts,
			AccountID:        "claude-code",
			WorkspaceID:      shared.SanitizeWorkspace(entry.CWD),
			SessionID:        strings.TrimSpace(entry.SessionID),
			TurnID:           turnID,
			MessageID:        messageID,
			ProviderID:       "anthropic",
			AgentName:        "claude_code",
			EventType:        shared.TelemetryEventTypeMessageUsage,
			ModelRaw:         model,
			InputTokens:      shared.Int64Ptr(int64(usage.InputTokens)),
			OutputTokens:     shared.Int64Ptr(int64(usage.OutputTokens)),
			ReasoningTokens:  shared.Int64Ptr(int64(usage.ReasoningTokens)),
			CacheReadTokens:  shared.Int64Ptr(int64(usage.CacheReadInputTokens)),
			CacheWriteTokens: shared.Int64Ptr(int64(usage.CacheCreationInputTokens)),
			TotalTokens:      shared.Int64Ptr(totalTokens),
			CostUSD:          shared.Float64Ptr(cost),
			Status:           shared.TelemetryStatusOK,
			Payload: map[string]any{
				"file": path,
				"line": lineNumber,
			},
		})

		for idx, part := range entry.Message.Content {
			if part.Type != "tool_use" {
				continue
			}
			toolKey := claudeTelemetryToolDedupKey(entry, idx, part)
			if toolKey != "" && seenTools[toolKey] {
				continue
			}
			if toolKey != "" {
				seenTools[toolKey] = true
			}

			toolName := strings.ToLower(strings.TrimSpace(part.Name))
			if toolName == "" {
				toolName = "unknown"
			}

			out = append(out, shared.TelemetryEvent{
				SchemaVersion: "claude_jsonl_v1",
				Channel:       shared.TelemetryChannelJSONL,
				OccurredAt:    ts,
				AccountID:     "claude-code",
				WorkspaceID:   shared.SanitizeWorkspace(entry.CWD),
				SessionID:     strings.TrimSpace(entry.SessionID),
				TurnID:        turnID,
				MessageID:     messageID,
				ToolCallID:    strings.TrimSpace(part.ID),
				ProviderID:    "anthropic",
				AgentName:     "claude_code",
				EventType:     shared.TelemetryEventTypeToolUsage,
				ModelRaw:      model,
				ToolName:      toolName,
				Requests:      shared.Int64Ptr(1),
				Status:        shared.TelemetryStatusOK,
				Payload: map[string]any{
					"file": path,
					"line": lineNumber,
				},
			})
		}
	}

	if err := scanner.Err(); err != nil {
		return out, err
	}
	return out, nil
}

func claudeTelemetryUsageDedupKey(entry jsonlEntry) string {
	if id := strings.TrimSpace(entry.RequestID); id != "" {
		return "req:" + id
	}
	if entry.Message != nil {
		if id := strings.TrimSpace(entry.Message.ID); id != "" {
			return "msg:" + id
		}
		if entry.Message.Usage != nil {
			u := entry.Message.Usage
			return fmt.Sprintf("fp:%s|%s|%s|%d|%d|%d|%d|%d",
				strings.TrimSpace(entry.SessionID),
				strings.TrimSpace(entry.Timestamp),
				strings.TrimSpace(entry.Message.Model),
				u.InputTokens,
				u.OutputTokens,
				u.CacheReadInputTokens,
				u.CacheCreationInputTokens,
				u.ReasoningTokens,
			)
		}
	}
	return ""
}

func claudeTelemetryToolDedupKey(entry jsonlEntry, idx int, part jsonlContent) string {
	base := strings.TrimSpace(entry.RequestID)
	if base == "" && entry.Message != nil {
		base = strings.TrimSpace(entry.Message.ID)
	}
	if base == "" {
		base = strings.TrimSpace(entry.SessionID) + "|" + strings.TrimSpace(entry.Timestamp)
	}
	if id := strings.TrimSpace(part.ID); id != "" {
		return base + "|tool:" + id
	}
	name := strings.ToLower(strings.TrimSpace(part.Name))
	if name == "" {
		name = "unknown"
	}
	return fmt.Sprintf("%s|tool:%s|%d", base, name, idx)
}
