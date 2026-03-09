package cursor

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func mergeAPIIntoSnapshot(dst, src *core.UsageSnapshot) {
	for key, metric := range src.Metrics {
		dst.Metrics[key] = metric
	}
	for key, reset := range src.Resets {
		dst.Resets[key] = reset
	}
	for key, raw := range src.Raw {
		dst.Raw[key] = raw
	}
	for key, series := range src.DailySeries {
		dst.DailySeries[key] = series
	}
	dst.ModelUsage = append(dst.ModelUsage, src.ModelUsage...)
	if src.Status != "" {
		dst.Status = src.Status
	}
	if src.Message != "" {
		dst.Message = src.Message
	}
}

type cursorSnapshotSignature struct {
	metrics     int
	resets      int
	raw         int
	dailySeries int
	modelUsage  int
}

func cursorSnapshotDataSignature(snap *core.UsageSnapshot) cursorSnapshotSignature {
	if snap == nil {
		return cursorSnapshotSignature{}
	}
	return cursorSnapshotSignature{
		metrics:     len(snap.Metrics),
		resets:      len(snap.Resets),
		raw:         len(snap.Raw),
		dailySeries: len(snap.DailySeries),
		modelUsage:  len(snap.ModelUsage),
	}
}

func (p *Provider) buildLocalOnlyMessage(snap *core.UsageSnapshot) {
	var parts []string

	if metric, ok := snap.Metrics["composer_cost"]; ok && metric.Used != nil && *metric.Used > 0 {
		parts = append(parts, fmt.Sprintf("$%.2f session cost", *metric.Used))
	}
	if metric, ok := snap.Metrics["total_ai_requests"]; ok && metric.Used != nil && *metric.Used > 0 {
		parts = append(parts, fmt.Sprintf("%.0f requests", *metric.Used))
	}
	if metric, ok := snap.Metrics["composer_sessions"]; ok && metric.Used != nil && *metric.Used > 0 {
		parts = append(parts, fmt.Sprintf("%.0f sessions", *metric.Used))
	}

	if len(parts) > 0 {
		snap.Message = strings.Join(parts, " · ") + " (API unavailable)"
		return
	}
	snap.Message = "Local Cursor IDE usage tracking (API unavailable)"
}

func extractTokenFromStateDB(dbPath string) string {
	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?mode=ro", dbPath))
	if err != nil {
		return ""
	}
	defer db.Close()

	var token string
	if db.QueryRow(`SELECT value FROM ItemTable WHERE key = 'cursorAuth/accessToken'`).Scan(&token) != nil {
		return ""
	}
	return strings.TrimSpace(token)
}
