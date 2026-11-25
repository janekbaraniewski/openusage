package kimi_cli

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

const telemetrySchemaVersion = "kimi_cli_v1"

func (p *Provider) System() string { return p.ID() }

func (p *Provider) DefaultCollectOptions() shared.TelemetryCollectOptions {
	return shared.TelemetryCollectOptions{
		Paths: map[string]string{
			PathHintSessionsDirKey: defaultSessionsDir(),
		},
	}
}

func (p *Provider) Collect(ctx context.Context, opts shared.TelemetryCollectOptions) ([]shared.TelemetryEvent, error) {
	sessionsDir := shared.ExpandHome(opts.Path(PathHintSessionsDirKey, defaultSessionsDir()))
	if sessionsDir == "" || !dirExists(sessionsDir) {
		return nil, nil
	}
	accountID := strings.TrimSpace(opts.Path("account_id", DefaultAccountID))
	configPath := shared.ExpandHome(opts.Path(PathHintConfigPathKey, filepath.Join(filepath.Dir(sessionsDir), "config.json")))
	fallbackModel := readKimiConfigModel(configPath)
	files, err := shared.CollectFilesWithStat([]string{sessionsDir}, map[string]bool{".jsonl": true})
	if err != nil {
		return nil, fmt.Errorf("kimi_cli: collecting telemetry files: %w", err)
	}

	p.telemetryMu.Lock()
	defer p.telemetryMu.Unlock()
	if p.telemetryReaders == nil {
		p.telemetryReaders = make(map[string]*shared.IncrementalReader)
	}

	var out []shared.TelemetryEvent
	for path, info := range files {
		if filepath.Base(path) != "wire.jsonl" {
			continue
		}
		if err := ctx.Err(); err != nil {
			return out, err
		}
		reader := p.telemetryReaders[path]
		if reader == nil {
			reader = &shared.IncrementalReader{MaxLineBytes: 1024 * 1024}
			p.telemetryReaders[path] = reader
		}
		_, err := reader.Read(path, info, func(line []byte, lineNo int) {
			var rec kimiWireRecord
			if json.Unmarshal(line, &rec) != nil {
				return
			}
			entry, ok := rec.toEntry(kimiWireSessionID(path), fallbackModel)
			if !ok {
				return
			}
			turnID := fmt.Sprintf("%s:usage:%d", entry.SessionID, lineNo)
			out = append(out, shared.TelemetryEvent{
				SchemaVersion: telemetrySchemaVersion,
				Channel:       shared.TelemetryChannelJSONL,
				OccurredAt:    entry.Timestamp,
				AccountID:     accountID,
				SessionID:     entry.SessionID,
				TurnID:        turnID,
				MessageID:     turnID,
				ProviderID:    ID,
				AgentName:     ID,
				EventType:     shared.TelemetryEventTypeMessageUsage,
				ModelRaw:      entry.Model,
				TokenUsage: core.TokenUsage{
					InputTokens:      core.Int64Ptr(entry.Input),
					OutputTokens:     core.Int64Ptr(entry.Output),
					CacheReadTokens:  core.Int64Ptr(entry.CacheRead),
					CacheWriteTokens: core.Int64Ptr(entry.CacheWrite),
					TotalTokens:      core.Int64Ptr(entry.Input + entry.Output + entry.CacheRead + entry.CacheWrite),
					Requests:         core.Int64Ptr(1),
				},
				Status: shared.TelemetryStatusOK,
				Payload: map[string]any{
					"source_file":       path,
					"client":            "CLI",
					"upstream_provider": entry.Provider,
				},
			})
		})
		if err != nil {
			continue
		}
	}
	return out, nil
}

func (p *Provider) ParseHookPayload(_ []byte, _ shared.TelemetryCollectOptions) ([]shared.TelemetryEvent, error) {
	return nil, shared.ErrHookUnsupported
}
