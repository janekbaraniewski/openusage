package ollama

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func (p *Provider) fetchDesktopDB(ctx context.Context, acct core.AccountConfig, snap *core.UsageSnapshot) (bool, error) {
	dbPath := resolveDesktopDBPath(acct)
	if dbPath == "" || !fileExists(dbPath) {
		return false, nil
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return false, fmt.Errorf("ollama: opening desktop db: %w", err)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return false, fmt.Errorf("ollama: pinging desktop db: %w", err)
	}

	snap.Raw["desktop_db_path"] = dbPath

	setCountMetric := func(key string, count int64, unit, window string) {
		setValueMetric(snap, key, float64(count), unit, window)
	}

	totalChats, err := queryCount(ctx, db, `SELECT COUNT(*) FROM chats`)
	if err == nil {
		setCountMetric("total_conversations", totalChats, "chats", "all-time")
	}

	totalMessages, err := queryCount(ctx, db, `SELECT COUNT(*) FROM messages`)
	if err == nil {
		setCountMetric("total_messages", totalMessages, "messages", "all-time")
	}

	totalUserMessages, err := queryCount(ctx, db, `SELECT COUNT(*) FROM messages WHERE role = 'user'`)
	if err == nil {
		setCountMetric("total_user_messages", totalUserMessages, "messages", "all-time")
	}

	totalAssistantMessages, err := queryCount(ctx, db, `SELECT COUNT(*) FROM messages WHERE role = 'assistant'`)
	if err == nil {
		setCountMetric("total_assistant_messages", totalAssistantMessages, "messages", "all-time")
	}

	totalToolCalls, err := queryCount(ctx, db, `SELECT COUNT(*) FROM tool_calls`)
	if err == nil {
		setCountMetric("total_tool_calls", totalToolCalls, "calls", "all-time")
	}

	totalAttachments, err := queryCount(ctx, db, `SELECT COUNT(*) FROM attachments`)
	if err == nil {
		setCountMetric("total_attachments", totalAttachments, "attachments", "all-time")
	}

	sessionsToday, err := queryCount(ctx, db, `SELECT COUNT(*) FROM chats WHERE date(created_at) = date('now', 'localtime')`)
	if err == nil {
		setCountMetric("sessions_today", sessionsToday, "sessions", "today")
	}

	messagesToday, err := queryCount(ctx, db, `SELECT COUNT(*) FROM messages WHERE date(created_at) = date('now', 'localtime')`)
	if err == nil {
		setCountMetric("messages_today", messagesToday, "messages", "today")
	}

	userMessagesToday, err := queryCount(ctx, db, `SELECT COUNT(*) FROM messages WHERE role = 'user' AND date(created_at) = date('now', 'localtime')`)
	if err == nil {
		setCountMetric("requests_today", userMessagesToday, "requests", "today")
	}

	sessions5h, err := queryCount(ctx, db, `SELECT COUNT(*) FROM chats WHERE datetime(created_at) >= datetime('now', '-5 hours')`)
	if err == nil {
		setCountMetric("sessions_5h", sessions5h, "sessions", "5h")
	}

	sessions1d, err := queryCount(ctx, db, `SELECT COUNT(*) FROM chats WHERE datetime(created_at) >= datetime('now', '-24 hours')`)
	if err == nil {
		setCountMetric("sessions_1d", sessions1d, "sessions", "1d")
	}

	messages5h, err := queryCount(ctx, db, `SELECT COUNT(*) FROM messages WHERE datetime(created_at) >= datetime('now', '-5 hours')`)
	if err == nil {
		setCountMetric("messages_5h", messages5h, "messages", "5h")
	}

	messages1d, err := queryCount(ctx, db, `SELECT COUNT(*) FROM messages WHERE datetime(created_at) >= datetime('now', '-24 hours')`)
	if err == nil {
		setCountMetric("messages_1d", messages1d, "messages", "1d")
	}

	requests5h, err := queryCount(ctx, db, `SELECT COUNT(*) FROM messages WHERE role = 'user' AND datetime(created_at) >= datetime('now', '-5 hours')`)
	if err == nil {
		setCountMetric("requests_5h", requests5h, "requests", "5h")
	}

	requests1d, err := queryCount(ctx, db, `SELECT COUNT(*) FROM messages WHERE role = 'user' AND datetime(created_at) >= datetime('now', '-24 hours')`)
	if err == nil {
		setCountMetric("requests_1d", requests1d, "requests", "1d")
	}

	toolCallsToday, err := queryCount(ctx, db, `SELECT COUNT(*)
		FROM tool_calls tc
		JOIN messages m ON tc.message_id = m.id
		WHERE date(m.created_at) = date('now', 'localtime')`)
	if err == nil {
		setCountMetric("tool_calls_today", toolCallsToday, "calls", "today")
	}

	toolCalls5h, err := queryCount(ctx, db, `SELECT COUNT(*)
		FROM tool_calls tc
		JOIN messages m ON tc.message_id = m.id
		WHERE datetime(m.created_at) >= datetime('now', '-5 hours')`)
	if err == nil {
		setCountMetric("tool_calls_5h", toolCalls5h, "calls", "5h")
	}

	toolCalls1d, err := queryCount(ctx, db, `SELECT COUNT(*)
		FROM tool_calls tc
		JOIN messages m ON tc.message_id = m.id
		WHERE datetime(m.created_at) >= datetime('now', '-24 hours')`)
	if err == nil {
		setCountMetric("tool_calls_1d", toolCalls1d, "calls", "1d")
	}

	attachmentsToday, err := queryCount(ctx, db, `SELECT COUNT(*)
		FROM attachments a
		JOIN messages m ON a.message_id = m.id
		WHERE date(m.created_at) = date('now', 'localtime')`)
	if err == nil {
		setCountMetric("attachments_today", attachmentsToday, "attachments", "today")
	}

	if err := populateModelUsageFromDB(ctx, db, snap); err != nil {
		snap.SetDiagnostic("desktop_model_usage_error", err.Error())
	}
	if err := populateEstimatedTokenUsageFromDB(ctx, db, snap, p.now()); err != nil {
		snap.SetDiagnostic("desktop_token_estimate_error", err.Error())
	}
	if err := populateSourceUsageFromDB(ctx, db, snap); err != nil {
		snap.SetDiagnostic("desktop_source_usage_error", err.Error())
	}
	if err := populateToolUsageFromDB(ctx, db, snap); err != nil {
		snap.SetDiagnostic("desktop_tool_usage_error", err.Error())
	}
	if err := populateDailySeriesFromDB(ctx, db, snap); err != nil {
		snap.SetDiagnostic("desktop_daily_series_error", err.Error())
	}
	if err := populateThinkingMetricsFromDB(ctx, db, snap); err != nil {
		snap.SetDiagnostic("desktop_thinking_error", err.Error())
	}
	if err := populateSettingsFromDB(ctx, db, snap); err != nil {
		snap.SetDiagnostic("desktop_settings_error", err.Error())
	}
	if err := populateCachedUserFromDB(ctx, db, snap); err != nil {
		snap.SetDiagnostic("desktop_user_error", err.Error())
	}

	return true, nil
}
