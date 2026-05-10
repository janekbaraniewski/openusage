package copilot

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

const (
	telemetrySchemaVersion   = "copilot_v2"
	defaultCopilotSessionDir = ".copilot/session-state"
	defaultCopilotStoreDB    = ".copilot/session-store.db"
	defaultCopilotLogsDir    = ".copilot/logs"
)

type copilotTelemetryAssistantMessageData struct {
	MessageID    string          `json:"messageId"`
	ToolRequests json.RawMessage `json:"toolRequests"`
}

type copilotTelemetryToolRequest struct {
	ToolCallID string `json:"toolCallId"`
	RawName    string `json:"-"`
	Input      any    `json:"-"`
}

type copilotTelemetryToolExecutionStartData struct {
	ToolCallID string          `json:"toolCallId"`
	ToolName   string          `json:"toolName"`
	Arguments  json.RawMessage `json:"arguments"`
}

type copilotTelemetryToolExecutionCompleteData struct {
	ToolCallID string          `json:"toolCallId"`
	ToolName   string          `json:"toolName"`
	Success    *bool           `json:"success"`
	Status     string          `json:"status"`
	Result     json.RawMessage `json:"result"`
	Error      json.RawMessage `json:"error"`
}

type copilotTelemetrySessionContextChangedData struct {
	CWD        string `json:"cwd"`
	Repository string `json:"repository"`
}

type copilotTelemetryWorkspaceFileChangedData struct {
	Path      string `json:"path"`
	Operation string `json:"operation"`
}

type copilotTelemetryToolContext struct {
	MessageID string
	TurnID    string
	Model     string
	ToolName  string
	Payload   map[string]any
}

// System returns the telemetry system identifier for the copilot provider.
func (p *Provider) System() string { return p.ID() }

func (p *Provider) DefaultCollectOptions() shared.TelemetryCollectOptions {
	return shared.TelemetryCollectOptions{
		Paths: map[string]string{
			"sessions_dir":     defaultCopilotSessionsDir(),
			"session_store_db": defaultCopilotSessionStoreDB(),
		},
	}
}

// Collect scans copilot session-state directories for events.jsonl files and
// extracts usage telemetry events from assistant.usage entries.
func (p *Provider) Collect(ctx context.Context, opts shared.TelemetryCollectOptions) ([]shared.TelemetryEvent, error) {
	sessionDir := shared.ExpandHome(opts.Path("sessions_dir", defaultCopilotSessionsDir()))
	storeDB := shared.ExpandHome(opts.Path("session_store_db", defaultCopilotSessionStoreDB()))
	if sessionDir == "" {
		return nil, nil
	}

	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("copilot: read session directory: %w", err)
	}

	var out []shared.TelemetryEvent
	seenSessions := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		if !entry.IsDir() {
			continue
		}
		seenSessions[entry.Name()] = true
		eventsPath := filepath.Join(sessionDir, entry.Name(), "events.jsonl")
		events, err := parseCopilotTelemetrySessionFile(eventsPath, entry.Name())
		if err != nil {
			return nil, fmt.Errorf("copilot: parse session telemetry %s: %w", eventsPath, err)
		}
		out = append(out, events...)
	}

	// Fallback to durable session-store metadata for sessions that no longer have
	// events.jsonl state (Copilot rotates session-state aggressively).
	storeEvents, err := parseCopilotTelemetrySessionStore(ctx, storeDB, seenSessions)
	if err != nil {
		return nil, fmt.Errorf("copilot: parse session store telemetry: %w", err)
	}
	out = append(out, storeEvents...)

	// Enrich synthetic message_usage events with estimated token counts from
	// CompactionProcessor log entries.
	logsDir := shared.ExpandHome(opts.Path("logs_dir", defaultCopilotLogsPath()))
	if deltas := parseCopilotLogTokenDeltas(logsDir); len(deltas) > 0 {
		enrichSyntheticTokenEstimates(out, deltas)
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

func defaultCopilotSessionStoreDB() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, defaultCopilotStoreDB)
}
