package opencode

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

const (
	telemetryEventSchema  = "opencode_event_v1"
	telemetryHookSchema   = "opencode_hook_v1"
	telemetrySQLiteSchema = "opencode_sqlite_v1"
)

type eventEnvelope struct {
	Type       string          `json:"type"`
	Event      string          `json:"event"`
	Properties json.RawMessage `json:"properties"`
	Payload    json.RawMessage `json:"payload"`
}

type messageUpdatedProps struct {
	Info assistantInfo `json:"info"`
}

type assistantInfo struct {
	ID         string  `json:"id"`
	SessionID  string  `json:"sessionID"`
	Role       string  `json:"role"`
	ParentID   string  `json:"parentID"`
	ModelID    string  `json:"modelID"`
	ProviderID string  `json:"providerID"`
	Cost       float64 `json:"cost"`
	Tokens     struct {
		Input     int64 `json:"input"`
		Output    int64 `json:"output"`
		Reasoning int64 `json:"reasoning"`
		Cache     struct {
			Read  int64 `json:"read"`
			Write int64 `json:"write"`
		} `json:"cache"`
	} `json:"tokens"`
	Time struct {
		Created   int64 `json:"created"`
		Completed int64 `json:"completed"`
	} `json:"time"`
	Path struct {
		CWD string `json:"cwd"`
	} `json:"path"`
}

type toolPayload struct {
	SessionID  string `json:"sessionID"`
	MessageID  string `json:"messageID"`
	ToolCallID string `json:"toolCallID"`
	ToolName   string `json:"toolName"`
	Name       string `json:"name"`
	Timestamp  int64  `json:"timestamp"`
}

type hookToolExecuteAfterInput struct {
	Tool      string `json:"tool"`
	SessionID string `json:"sessionID"`
	CallID    string `json:"callID"`
}

type hookToolExecuteAfterOutput struct {
	Title string `json:"title"`
}

type hookChatMessageInput struct {
	SessionID string `json:"sessionID"`
	Agent     string `json:"agent"`
	MessageID string `json:"messageID"`
	Variant   string `json:"variant"`
	Model     struct {
		ProviderID string `json:"providerID"`
		ModelID    string `json:"modelID"`
	} `json:"model"`
}

type hookChatMessageOutput struct {
	Message struct {
		ID        string `json:"id"`
		SessionID string `json:"sessionID"`
		Role      string `json:"role"`
	} `json:"message"`
	PartsCount int64 `json:"parts_count"`
}

type usage struct {
	InputTokens      *int64
	OutputTokens     *int64
	ReasoningTokens  *int64
	CacheReadTokens  *int64
	CacheWriteTokens *int64
	TotalTokens      *int64
	CostUSD          *float64
}

type partSummary struct {
	PartsTotal  int64
	PartsByType map[string]int64
}

func (p *Provider) System() string { return p.ID() }

func (p *Provider) DefaultCollectOptions() shared.TelemetryCollectOptions {
	home, _ := os.UserHomeDir()
	return shared.TelemetryCollectOptions{
		Paths: map[string]string{
			"db_path": filepath.Join(home, ".local", "share", "opencode", "opencode.db"),
		},
		PathLists: map[string][]string{
			"events_dirs": {
				filepath.Join(home, ".opencode", "events"),
				filepath.Join(home, ".opencode", "logs"),
				filepath.Join(home, ".local", "state", "opencode", "events"),
				filepath.Join(home, ".local", "state", "opencode", "logs"),
			},
		},
	}
}

func (p *Provider) Collect(ctx context.Context, opts shared.TelemetryCollectOptions) ([]shared.TelemetryEvent, error) {
	defaults := p.DefaultCollectOptions()
	dbPath := shared.ExpandHome(opts.Path("db_path", defaults.Paths["db_path"]))
	eventsDirs := opts.PathsFor("events_dirs", defaults.PathLists["events_dirs"])
	eventsFile := opts.Path("events_file", "")
	accountID := strings.TrimSpace(opts.Path("account_id", ""))

	seenMessage := map[string]bool{}
	seenTools := map[string]bool{}
	var out []shared.TelemetryEvent

	if strings.TrimSpace(dbPath) != "" {
		events, err := CollectTelemetryFromSQLite(ctx, dbPath)
		if err == nil {
			appendDedupTelemetryEvents(&out, events, seenMessage, seenTools, accountID)
		}
	}

	roots := append([]string{}, eventsDirs...)
	if strings.TrimSpace(eventsFile) != "" {
		roots = append(roots, eventsFile)
	}
	files := shared.CollectFilesByExt(roots, map[string]bool{".jsonl": true, ".ndjson": true})
	for _, file := range files {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		events, err := ParseTelemetryEventFile(file)
		if err != nil {
			continue
		}
		appendDedupTelemetryEvents(&out, events, seenMessage, seenTools, accountID)
	}

	return out, nil
}

func (p *Provider) ParseHookPayload(raw []byte, opts shared.TelemetryCollectOptions) ([]shared.TelemetryEvent, error) {
	events, err := ParseTelemetryHookPayload(raw)
	if err != nil {
		return nil, err
	}
	accountID := strings.TrimSpace(opts.Path("account_id", ""))
	for i := range events {
		events[i].AccountID = core.FirstNonEmpty(accountID, events[i].AccountID)
	}
	return events, nil
}
