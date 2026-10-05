package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func kimiQuotaNotice(snap core.UsageSnapshot, width int) []string {
	if snap.ProviderID != "kimi_cli" {
		return nil
	}
	var text string
	switch snap.Attributes["quota_state"] {
	case "stale":
		text = "Stale quota"
		if fetchedAt, err := time.Parse(time.RFC3339Nano, snap.Attributes["quota_fetched_at"]); err == nil {
			text += " · " + fetchedAt.Local().Format("02 Jan 15:04")
		}
	case "unavailable":
		text = "Quota unavailable"
	default:
		return nil
	}
	lines := []string{dimStyle.Render(ansi.Truncate(text, width, "…"))}
	reason := snap.Diagnostics["quota"]
	if reason == "" {
		reason = snap.Diagnostics["quota_error"]
	}
	if reason == "" && snap.Status == core.StatusError {
		reason = snap.Message
		if strings.Contains(reason, "deadline exceeded") {
			reason = "Kimi poll timed out"
		}
	}
	switch {
	case strings.Contains(reason, "deadline exceeded"), strings.Contains(reason, "Timeout"), strings.Contains(reason, "timeout"):
		reason = "Kimi API timed out"
	case strings.Contains(reason, "HTTP 401"):
		reason = "Kimi CLI authentication required"
	}
	if reason != "" {
		lines = append(lines, dimStyle.Render(ansi.Truncate(reason, width, "…")))
	}
	return lines
}

func kimiQuotaStale(snap core.UsageSnapshot) bool {
	return snap.ProviderID == "kimi_cli" && snap.Attributes["quota_state"] == "stale"
}
