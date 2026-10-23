package dsh

import (
	"context"
	"fmt"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

const (
	telemetrySchemaVersion = "dsh_session_v4"
	telemetryClientName    = "dsh"
)

// System implements shared.TelemetrySource.
func (p *Provider) System() string { return p.ID() }

func (p *Provider) DefaultCollectOptions() shared.TelemetryCollectOptions {
	return shared.TelemetryCollectOptions{
		Paths: map[string]string{
			PathHintSessionsDirKey: defaultSessionsDir(),
		},
	}
}

// Collect implements shared.TelemetrySource. It emits one message_usage event
// per model response that reported tokens and one tool_usage event per
// completed tool call.
//
// A session's events are returned when its log is first seen and again each
// time the log changes; unchanged logs contribute nothing. Every event carries
// an id derived from the session and message or call id, so a re-emitted
// session deduplicates on ingest.
func (p *Provider) Collect(ctx context.Context, opts shared.TelemetryCollectOptions) ([]shared.TelemetryEvent, error) {
	dir := shared.ExpandHome(opts.Path(PathHintSessionsDirKey, defaultSessionsDir()))
	if !dirExists(dir) {
		return nil, nil
	}
	accountID := opts.Path("account_id", DefaultAccountID)

	sessions, err := p.sessions.load(ctx, dir)
	if err != nil {
		return nil, fmt.Errorf("collect dsh telemetry files: %w", err)
	}

	p.emittedMu.Lock()
	defer p.emittedMu.Unlock()
	if p.emitted == nil {
		p.emitted = make(map[string]shared.FileSignature)
	}

	var out []shared.TelemetryEvent
	seen := make(map[string]bool, len(sessions))
	for _, loaded := range sessions {
		seen[loaded.path] = true
		if signature, ok := p.emitted[loaded.path]; ok && signature.Equal(loaded.signature) {
			continue
		}
		out = append(out, sessionTelemetryEvents(loaded.path, loaded.session, accountID)...)
		p.emitted[loaded.path] = loaded.signature
	}
	for path := range p.emitted {
		if !seen[path] {
			delete(p.emitted, path)
		}
	}
	return out, nil
}

// ParseHookPayload implements shared.TelemetrySource.
// dsh does not support hook-based telemetry.
func (p *Provider) ParseHookPayload(_ []byte, _ shared.TelemetryCollectOptions) ([]shared.TelemetryEvent, error) {
	return nil, shared.ErrHookUnsupported
}

// sessionTelemetryEvents turns one parsed session into telemetry events.
func sessionTelemetryEvents(path string, session *sessionData, accountID string) []shared.TelemetryEvent {
	if session == nil {
		return nil
	}
	workspace := shared.SanitizeWorkspace(session.CWD)

	out := make([]shared.TelemetryEvent, 0, len(session.Messages)+len(session.Tools))
	for _, msg := range session.Messages {
		out = append(out, shared.TelemetryEvent{
			SchemaVersion: telemetrySchemaVersion,
			Channel:       shared.TelemetryChannelJSONL,
			OccurredAt:    msg.Time,
			AccountID:     accountID,
			WorkspaceID:   workspace,
			SessionID:     session.ID,
			TurnID:        telemetryTurnID(session.ID, msg.Turn),
			MessageID:     session.ID + ":" + msg.ID,
			ProviderID:    telemetryProviderID(msg.Upstream),
			AgentName:     telemetryClientName,
			EventType:     shared.TelemetryEventTypeMessageUsage,
			ModelRaw:      msg.Model,
			TokenUsage: core.TokenUsage{
				InputTokens:      core.Int64Ptr(msg.Input),
				OutputTokens:     core.Int64Ptr(msg.Output),
				ReasoningTokens:  core.Int64Ptr(msg.Reasoning),
				CacheReadTokens:  core.Int64Ptr(msg.CacheRead),
				CacheWriteTokens: core.Int64Ptr(msg.CacheWrite),
				TotalTokens:      core.Int64Ptr(msg.Total),
				Requests:         core.Int64Ptr(1),
				CostUSD:          estimateCost(msg),
			},
			Status:  shared.TelemetryStatusOK,
			Payload: telemetryPayload(path, msg.Upstream),
		})
	}

	for _, tool := range session.Tools {
		payload := telemetryPayload(path, tool.Upstream)
		if tool.File != "" {
			payload["file"] = tool.File
		}
		status := shared.TelemetryStatusOK
		if tool.IsError {
			status = shared.TelemetryStatusError
		} else {
			// A failed edit changed nothing.
			if tool.LinesAdded > 0 {
				payload["lines_added"] = tool.LinesAdded
			}
			if tool.LinesRemoved > 0 {
				payload["lines_removed"] = tool.LinesRemoved
			}
		}
		out = append(out, shared.TelemetryEvent{
			SchemaVersion: telemetrySchemaVersion,
			Channel:       shared.TelemetryChannelJSONL,
			OccurredAt:    tool.Time,
			AccountID:     accountID,
			WorkspaceID:   workspace,
			SessionID:     session.ID,
			TurnID:        telemetryTurnID(session.ID, tool.Turn),
			ToolCallID:    tool.CallID,
			ProviderID:    telemetryProviderID(tool.Upstream),
			AgentName:     telemetryClientName,
			EventType:     shared.TelemetryEventTypeToolUsage,
			ModelRaw:      tool.Model,
			TokenUsage: core.TokenUsage{
				Requests: core.Int64Ptr(1),
			},
			ToolName: tool.Name,
			Status:   status,
			Payload:  payload,
		})
	}
	return out
}

func telemetryPayload(path, upstream string) map[string]any {
	payload := map[string]any{
		"source_file": path,
		"client":      telemetryClientName,
	}
	if upstream != "" {
		payload["upstream_provider"] = upstream
	}
	return payload
}

func telemetryTurnID(sessionID string, turn *int64) string {
	if turn == nil {
		return ""
	}
	return fmt.Sprintf("%s:turn:%d", sessionID, *turn)
}
