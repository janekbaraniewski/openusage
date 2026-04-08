package ollama

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"

	_ "github.com/mattn/go-sqlite3"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

const (
	telemetrySQLiteSchema = "ollama_sqlite_v1"
)

// System implements shared.TelemetrySource.
func (p *Provider) System() string { return p.ID() }

func (p *Provider) DefaultCollectOptions() shared.TelemetryCollectOptions {
	return shared.TelemetryCollectOptions{
		Paths: map[string]string{
			"db_path": defaultDesktopDBPath(),
		},
	}
}

// Collect implements shared.TelemetrySource. It reads the Ollama desktop
// SQLite database and emits TelemetryEvent records for assistant messages
// and tool calls.
func (p *Provider) Collect(ctx context.Context, opts shared.TelemetryCollectOptions) ([]shared.TelemetryEvent, error) {
	defaultDBPath := defaultDesktopDBPath()
	dbPath := shared.ExpandHome(opts.Path("db_path", defaultDBPath))
	if strings.TrimSpace(dbPath) == "" {
		return nil, nil
	}
	if _, err := os.Stat(dbPath); err != nil {
		return nil, nil
	}

	accountID := strings.TrimSpace(opts.Path("account_id", ""))

	events, err := collectTelemetryFromSQLite(ctx, dbPath)
	if err != nil {
		return nil, err
	}

	if accountID != "" {
		for i := range events {
			events[i].AccountID = core.FirstNonEmpty(accountID, events[i].AccountID)
		}
	}

	return events, nil
}

// ParseHookPayload implements shared.TelemetrySource. Ollama does not
// support hook-based telemetry.
func (p *Provider) ParseHookPayload(_ []byte, _ shared.TelemetryCollectOptions) ([]shared.TelemetryEvent, error) {
	return nil, shared.ErrHookUnsupported
}

// defaultDesktopDBPath returns the platform default path for the Ollama
// desktop database without requiring an AccountConfig.
func defaultDesktopDBPath() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	// resolveDesktopDBPath handles platform specifics; use a simple
	// fallback for the common macOS/Linux case.
	return resolveDesktopDBPath(coreAccountConfigForHome(home))
}

// collectTelemetryFromSQLite opens the Ollama desktop database and
// returns message-usage and tool-usage telemetry events.
func collectTelemetryFromSQLite(ctx context.Context, dbPath string) ([]shared.TelemetryEvent, error) {
	if strings.TrimSpace(dbPath) == "" {
		return nil, nil
	}
	fi, err := os.Stat(dbPath)
	if err != nil {
		return nil, nil
	}
	dbMtime := fi.ModTime().UTC()

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("ollama: opening desktop db for telemetry: %w", err)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ollama: pinging desktop db for telemetry: %w", err)
	}

	if !sqliteTableExists(ctx, db, "messages") {
		return nil, nil
	}

	var out []shared.TelemetryEvent
	seenMessages := map[string]bool{}
	seenTools := map[string]bool{}

	// --- Message usage events ---
	hasThinking, _ := tableHasColumn(ctx, db, "messages", "thinking")
	thinkingExpr := `''`
	if hasThinking {
		thinkingExpr = `COALESCE(thinking, '')`
	}

	msgQuery := fmt.Sprintf(`
		SELECT m.id, m.chat_id, m.role, m.model_name,
			COALESCE(m.content, ''), %s,
			COALESCE(m.created_at, '')
		FROM messages m
		ORDER BY m.chat_id, datetime(m.created_at), m.id`, thinkingExpr)

	rows, err := db.QueryContext(ctx, msgQuery)
	if err != nil {
		return nil, fmt.Errorf("ollama: querying messages for telemetry: %w", err)
	}
	defer rows.Close()

	pendingInputChars := 0
	currentChat := ""

	for rows.Next() {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}

		var (
			msgID     int64
			chatID    string
			role      sql.NullString
			modelName sql.NullString
			content   sql.NullString
			thinking  sql.NullString
			createdAt sql.NullString
		)
		if err := rows.Scan(&msgID, &chatID, &role, &modelName, &content, &thinking, &createdAt); err != nil {
			continue
		}

		if chatID != currentChat {
			pendingInputChars = 0
			currentChat = chatID
		}

		roleVal := strings.ToLower(strings.TrimSpace(role.String))
		contentLen := len(content.String)
		thinkingLen := len(thinking.String)

		if roleVal == "user" {
			pendingInputChars += contentLen + thinkingLen
			continue
		}
		if roleVal != "assistant" {
			continue
		}

		model := normalizeModelName(strings.TrimSpace(modelName.String))
		if model == "" {
			continue
		}

		messageKey := fmt.Sprintf("%d", msgID)
		if seenMessages[messageKey] {
			continue
		}
		seenMessages[messageKey] = true

		occurredAt := shared.FlexParseTime(createdAt.String)
		if occurredAt.IsZero() {
			occurredAt = dbMtime // fallback: use DB file mtime (stable across restarts)
		}

		inputTokens := int64(estimateTokensFromChars(pendingInputChars))
		outputTokens := int64(estimateTokensFromChars(contentLen + thinkingLen))
		totalTokens := inputTokens + outputTokens
		pendingInputChars = 0

		out = append(out, shared.TelemetryEvent{
			SchemaVersion: telemetrySQLiteSchema,
			Channel:       shared.TelemetryChannelSQLite,
			OccurredAt:    occurredAt,
			SessionID:     chatID,
			MessageID:     messageKey,
			ProviderID:    "ollama",
			AgentName:     "ollama",
			EventType:     shared.TelemetryEventTypeMessageUsage,
			ModelRaw:      model,
			TokenUsage: core.TokenUsage{
				InputTokens:  core.Int64Ptr(inputTokens),
				OutputTokens: core.Int64Ptr(outputTokens),
				TotalTokens:  core.Int64Ptr(totalTokens),
				Requests:     core.Int64Ptr(1),
			},
			Status: shared.TelemetryStatusOK,
			Payload: map[string]any{
				"source": map[string]any{
					"db_path": dbPath,
					"table":   "messages",
				},
				"db": map[string]any{
					"message_id": msgID,
					"chat_id":    chatID,
				},
				"estimation": "chars_div_4",
			},
		})
	}
	if err := rows.Err(); err != nil {
		return out, fmt.Errorf("ollama: iterating messages for telemetry: %w", err)
	}

	// --- Tool usage events ---
	hasFunctionName, _ := tableHasColumn(ctx, db, "tool_calls", "function_name")
	if hasFunctionName && sqliteTableExists(ctx, db, "tool_calls") {
		toolRows, err := db.QueryContext(ctx, `
			SELECT tc.id, tc.message_id, tc.function_name,
				COALESCE(m.chat_id, ''), COALESCE(m.created_at, '')
			FROM tool_calls tc
			LEFT JOIN messages m ON tc.message_id = m.id
			WHERE trim(tc.function_name) != ''
			ORDER BY m.created_at, tc.id`)
		if err == nil {
			defer toolRows.Close()

			for toolRows.Next() {
				if ctx.Err() != nil {
					return out, ctx.Err()
				}

				var (
					toolCallID   int64
					messageID    int64
					functionName string
					chatID       string
					createdAt    string
				)
				if err := toolRows.Scan(&toolCallID, &messageID, &functionName, &chatID, &createdAt); err != nil {
					continue
				}

				functionName = strings.TrimSpace(functionName)
				if functionName == "" {
					continue
				}

				toolKey := fmt.Sprintf("tc_%d", toolCallID)
				if seenTools[toolKey] {
					continue
				}
				seenTools[toolKey] = true

				occurredAt := shared.FlexParseTime(createdAt)
				if occurredAt.IsZero() {
					occurredAt = dbMtime
				}

				out = append(out, shared.TelemetryEvent{
					SchemaVersion: telemetrySQLiteSchema,
					Channel:       shared.TelemetryChannelSQLite,
					OccurredAt:    occurredAt,
					SessionID:     chatID,
					MessageID:     fmt.Sprintf("%d", messageID),
					ToolCallID:    toolKey,
					ProviderID:    "ollama",
					AgentName:     "ollama",
					EventType:     shared.TelemetryEventTypeToolUsage,
					TokenUsage: core.TokenUsage{
						Requests: core.Int64Ptr(1),
					},
					ToolName: strings.ToLower(functionName),
					Status:   shared.TelemetryStatusOK,
					Payload: map[string]any{
						"source": map[string]any{
							"db_path": dbPath,
							"table":   "tool_calls",
						},
						"db": map[string]any{
							"tool_call_id": toolCallID,
							"message_id":   messageID,
						},
					},
				})
			}
			if err := toolRows.Err(); err != nil {
				return out, fmt.Errorf("ollama: iterating tool_calls for telemetry: %w", err)
			}
		}
	}

	return out, nil
}

// sqliteTableExists checks whether a table exists in the SQLite database.
func sqliteTableExists(ctx context.Context, db *sql.DB, table string) bool {
	var count int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&count)
	return err == nil && count > 0
}

// coreAccountConfigForHome builds a zero-value AccountConfig so that
// resolveDesktopDBPath falls through to platform defaults.
func coreAccountConfigForHome(_ string) core.AccountConfig {
	return core.AccountConfig{}
}
