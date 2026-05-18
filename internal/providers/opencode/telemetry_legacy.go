package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

// telemetryLegacySchema marks events parsed from the pre-v1.2 OpenCode
// JSON-file storage layout (one JSON per message under
// ~/.local/share/opencode/storage/message/<session>/<id>.json).
const telemetryLegacySchema = "opencode_legacy_v1"

// CollectTelemetryFromLegacyStorage walks the pre-v1.2 OpenCode message storage
// directory and parses each *.json file as one assistant message. Returns nil
// when the directory does not exist (legacy storage is optional).
func CollectTelemetryFromLegacyStorage(ctx context.Context, storageDir string) ([]shared.TelemetryEvent, error) {
	storageDir = strings.TrimSpace(storageDir)
	if storageDir == "" {
		return nil, nil
	}
	info, err := os.Stat(storageDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat opencode legacy storage: %w", err)
	}
	if !info.IsDir() {
		return nil, nil
	}

	var out []shared.TelemetryEvent
	walkErr := filepath.WalkDir(storageDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // tolerate transient FS errors
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".json") {
			return nil
		}
		ev, ok := parseLegacyMessageFile(path)
		if !ok {
			return nil
		}
		out = append(out, ev)
		return nil
	})
	if walkErr != nil && walkErr != context.Canceled && walkErr != context.DeadlineExceeded {
		return out, fmt.Errorf("walk opencode legacy storage: %w", walkErr)
	}
	return out, nil
}

// parseLegacyMessageFile loads one legacy message JSON and projects it to a
// TelemetryEvent. Returns ok=false when the file is not an assistant message
// with usage information.
func parseLegacyMessageFile(path string) (shared.TelemetryEvent, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return shared.TelemetryEvent{}, false
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return shared.TelemetryEvent{}, false
	}
	if strings.ToLower(shared.FirstPathString(payload, []string{"role"})) != "assistant" {
		return shared.TelemetryEvent{}, false
	}
	u := extractUsage(payload)
	if !hasUsage(u) {
		return shared.TelemetryEvent{}, false
	}

	messageID := core.FirstNonEmpty(
		shared.FirstPathString(payload, []string{"id"}),
		shared.FirstPathString(payload, []string{"messageID"}),
		strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
	)
	sessionID := core.FirstNonEmpty(
		shared.FirstPathString(payload, []string{"sessionID"}),
		shared.FirstPathString(payload, []string{"session_id"}),
		// One layout puts the session ID in the parent directory name.
		filepath.Base(filepath.Dir(path)),
	)
	turnID := core.FirstNonEmpty(
		shared.FirstPathString(payload, []string{"parentID"}),
		shared.FirstPathString(payload, []string{"turnID"}),
	)
	providerID := core.FirstNonEmpty(
		shared.FirstPathString(payload, []string{"providerID"}),
		shared.FirstPathString(payload, []string{"model", "providerID"}),
		"opencode",
	)
	modelRaw := core.FirstNonEmpty(
		shared.FirstPathString(payload, []string{"modelID"}),
		shared.FirstPathString(payload, []string{"model", "modelID"}),
	)

	completedAt := ptrInt64FromFloat(shared.FirstPathNumber(payload, []string{"time", "completed"}))
	createdAt := ptrInt64FromFloat(shared.FirstPathNumber(payload, []string{"time", "created"}))
	occurredAt := shared.UnixAuto(completedAt)
	if completedAt <= 0 {
		occurredAt = shared.UnixAuto(createdAt)
	}
	if occurredAt.IsZero() {
		if fi, err := os.Stat(path); err == nil {
			occurredAt = fi.ModTime().UTC()
		}
	}

	return shared.TelemetryEvent{
		SchemaVersion: telemetryLegacySchema,
		Channel:       shared.TelemetryChannelJSONL,
		OccurredAt:    occurredAt,
		WorkspaceID: shared.SanitizeWorkspace(core.FirstNonEmpty(
			shared.FirstPathString(payload, []string{"path", "cwd"}),
			shared.FirstPathString(payload, []string{"path", "root"}),
		)),
		SessionID:  sessionID,
		TurnID:     turnID,
		MessageID:  messageID,
		ProviderID: providerID,
		AgentName:  core.FirstNonEmpty(shared.FirstPathString(payload, []string{"agent"}), "opencode"),
		EventType:  shared.TelemetryEventTypeMessageUsage,
		ModelRaw:   modelRaw,
		TokenUsage: core.TokenUsage{
			InputTokens:      u.InputTokens,
			OutputTokens:     u.OutputTokens,
			ReasoningTokens:  u.ReasoningTokens,
			CacheReadTokens:  u.CacheReadTokens,
			CacheWriteTokens: u.CacheWriteTokens,
			TotalTokens:      u.TotalTokens,
			CostUSD:          u.CostUSD,
			Requests:         core.Int64Ptr(1),
		},
		Status: finishStatus(shared.FirstPathString(payload, []string{"finish"})),
		Payload: map[string]any{
			"source": map[string]any{
				"file":   path,
				"layout": "legacy_storage_v1",
			},
			"message": map[string]any{
				"provider_id": providerID,
				"model_id":    modelRaw,
				"role":        "assistant",
				"finish":      shared.FirstPathString(payload, []string{"finish"}),
			},
		},
	}, true
}

// discoverOpenCodeChannelDBs locates any opencode*.db SQLite files across the
// install channels OpenCode is known to ship under
// (~/.local/share/opencode{,-canary,-beta,-nightly}). Returns absolute paths
// in deterministic order. Caller is responsible for opening / dedup'ing.
func discoverOpenCodeChannelDBs(homeDir string) []string {
	homeDir = strings.TrimSpace(homeDir)
	if homeDir == "" {
		return nil
	}
	candidates := []string{
		filepath.Join(homeDir, ".local", "share", "opencode", "opencode.db"),
		filepath.Join(homeDir, ".local", "share", "opencode-canary", "opencode.db"),
		filepath.Join(homeDir, ".local", "share", "opencode-beta", "opencode.db"),
		filepath.Join(homeDir, ".local", "share", "opencode-nightly", "opencode.db"),
		filepath.Join(homeDir, "Library", "Application Support", "opencode", "opencode.db"),
	}
	var out []string
	seen := map[string]bool{}
	for _, p := range candidates {
		if seen[p] {
			continue
		}
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			out = append(out, p)
			seen[p] = true
		}
	}
	return out
}
