package opencode

import (
	"context"
	"fmt"

	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

// collectSQLiteV2Events reads the session_message table used by newer OpenCode
// builds, which replaced the message/part tables. Only assistant rows carry
// token usage and cost; their data column has the same shape as a legacy
// message row.
func collectSQLiteV2Events(ctx context.Context, q sqliteQuerier, dbPath string) ([]shared.TelemetryEvent, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT m.id, m.session_id, m.time_created, m.time_updated, m.data, COALESCE(s.directory, '')
		FROM session_message m
		LEFT JOIN session_v2 s ON s.id = m.session_id
		WHERE m.type = 'assistant'
		ORDER BY m.time_updated ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query opencode sqlite session_message rows: %w", err)
	}
	defer rows.Close()

	var out []shared.TelemetryEvent
	for rows.Next() {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}

		var (
			messageIDRaw string
			sessionIDRaw string
			timeCreated  int64
			timeUpdated  int64
			messageJSON  string
			sessionDir   string
		)
		if err := rows.Scan(&messageIDRaw, &sessionIDRaw, &timeCreated, &timeUpdated, &messageJSON, &sessionDir); err != nil {
			return out, fmt.Errorf("scan opencode sqlite session_message row: %w", err)
		}

		payload := decodeJSONMap([]byte(messageJSON))
		normalizeSessionMessageModel(payload)
		u := extractUsage(payload)
		completedAt := ptrInt64FromFloat(shared.FirstPathNumber(payload, []string{"time", "completed"}))
		createdAt := ptrInt64FromFloat(shared.FirstPathNumber(payload, []string{"time", "created"}))
		if !hasUsage(u) {
			continue
		}

		out = append(out, buildSQLiteMessageTableEvent(
			dbPath,
			"session_message",
			messageIDRaw,
			sessionIDRaw,
			timeCreated,
			timeUpdated,
			completedAt,
			createdAt,
			sessionDir,
			payload,
			partSummary{},
			u,
		))
	}
	if err := rows.Err(); err != nil {
		return out, fmt.Errorf("iterate opencode sqlite session_message rows: %w", err)
	}
	return out, nil
}

// normalizeSessionMessageModel copies the model id from the session_message
// shape ({"model": {"id": ...}}) into the modelID key that the legacy message
// readers look up. An explicit modelID is left untouched.
func normalizeSessionMessageModel(payload map[string]any) {
	model, ok := payload["model"].(map[string]any)
	if !ok {
		return
	}
	if _, has := model["modelID"]; has {
		return
	}
	if id, ok := model["id"]; ok {
		model["modelID"] = id
	}
}
