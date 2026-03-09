package cursor

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

func (p *Provider) readStateDB(ctx context.Context, dbPath string, snap *core.UsageSnapshot) error {
	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?mode=ro", dbPath))
	if err != nil {
		return fmt.Errorf("opening state DB: %w", err)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("state DB not accessible: %w", err)
	}

	dailyStatsRecords, err := loadDailyStatsRecords(ctx, db)
	if err != nil {
		dailyStatsRecords = nil
	}
	composerRecords, err := loadComposerSessionRecords(ctx, db)
	if err != nil {
		log.Printf("[cursor] composerData query error: %v", err)
	}
	bubbleRecords, err := loadBubbleRecords(ctx, db)
	if err != nil {
		log.Printf("[cursor] bubbleId query error: %v", err)
	}

	p.readDailyStatsToday(dailyStatsRecords, snap)
	p.readDailyStatsSeries(dailyStatsRecords, snap)
	p.readComposerSessions(composerRecords, snap)
	p.readStateMetadata(ctx, db, snap)
	p.readToolUsage(bubbleRecords, snap)
	return nil
}

func (p *Provider) readDailyStatsToday(records []cursorDailyStatsRecord, snap *core.UsageSnapshot) {
	today := p.now().Format("2006-01-02")
	yesterday := p.now().AddDate(0, 0, -1).Format("2006-01-02")
	var stats *dailyStats
	for i := range records {
		switch records[i].Date {
		case today:
			stats = &records[i].Stats
		case yesterday:
			if stats == nil {
				stats = &records[i].Stats
			}
		}
	}
	if stats == nil {
		return
	}

	if stats.TabSuggestedLines > 0 {
		suggested := float64(stats.TabSuggestedLines)
		accepted := float64(stats.TabAcceptedLines)
		snap.Metrics["tab_suggested_lines"] = core.Metric{Used: &suggested, Unit: "lines", Window: "1d"}
		snap.Metrics["tab_accepted_lines"] = core.Metric{Used: &accepted, Unit: "lines", Window: "1d"}
	}
	if stats.ComposerSuggestedLines > 0 {
		suggested := float64(stats.ComposerSuggestedLines)
		accepted := float64(stats.ComposerAcceptedLines)
		snap.Metrics["composer_suggested_lines"] = core.Metric{Used: &suggested, Unit: "lines", Window: "1d"}
		snap.Metrics["composer_accepted_lines"] = core.Metric{Used: &accepted, Unit: "lines", Window: "1d"}
	}
}

func (p *Provider) readComposerSessions(records []cursorComposerSessionRecord, snap *core.UsageSnapshot) {
	var (
		totalCostCents     float64
		totalRequests      int
		totalSessions      int
		totalLinesAdded    int
		totalLinesRemoved  int
		totalFilesChanged  int
		totalFilesCreated  int
		totalFilesRemoved  int
		agenticSessions    int
		nonAgenticSessions int
		totalContextUsed   float64
		totalContextLimit  float64
		contextSampleCount int
		subagentTypes      = make(map[string]int)
		modelCosts         = make(map[string]float64)
		modelRequests      = make(map[string]int)
		modeSessions       = make(map[string]int)
		forceModes         = make(map[string]int)
		statusCounts       = make(map[string]int)
		dailyCost          = make(map[string]float64)
		dailyRequests      = make(map[string]float64)
		todayCostCents     float64
		todayRequests      int
	)

	now := p.now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	for _, record := range records {
		totalSessions++
		if record.Mode != "" {
			modeSessions[record.Mode]++
		}
		if record.IsAgentic != nil {
			if *record.IsAgentic {
				agenticSessions++
			} else {
				nonAgenticSessions++
			}
		}
		if record.ForceMode != "" {
			forceModes[record.ForceMode]++
		}
		if record.Status != "" {
			statusCounts[record.Status]++
		}
		totalLinesAdded += record.LinesAdded
		totalLinesRemoved += record.LinesRemoved
		if record.FilesChanged > 0 {
			totalFilesChanged += record.FilesChanged
		}
		if record.AddedFiles > 0 {
			totalFilesCreated += record.AddedFiles
		}
		if record.RemovedFiles > 0 {
			totalFilesRemoved += record.RemovedFiles
		}
		if record.ContextTokensUsed > 0 && record.ContextTokenLimit > 0 {
			totalContextUsed += record.ContextTokensUsed
			totalContextLimit += record.ContextTokenLimit
			contextSampleCount++
		}
		if record.SubagentType != "" {
			subagentTypes[record.SubagentType]++
		}

		var sessionDay string
		if !record.OccurredAt.IsZero() {
			sessionDay = record.OccurredAt.In(now.Location()).Format("2006-01-02")
		}
		for model, usage := range record.Usage {
			totalCostCents += usage.CostInCents
			totalRequests += usage.Amount
			modelCosts[model] += usage.CostInCents
			modelRequests[model] += usage.Amount
			if sessionDay != "" {
				dailyCost[sessionDay] += usage.CostInCents
				dailyRequests[sessionDay] += float64(usage.Amount)
			}
			if !record.OccurredAt.IsZero() && record.OccurredAt.After(todayStart) {
				todayCostCents += usage.CostInCents
				todayRequests += usage.Amount
			}
		}
	}

	if totalSessions == 0 {
		return
	}

	totalCostUSD := totalCostCents / 100.0
	snap.Metrics["composer_cost"] = core.Metric{Used: &totalCostUSD, Unit: "USD", Window: "all-time"}
	if todayCostCents > 0 {
		todayCostUSD := todayCostCents / 100.0
		snap.Metrics["today_cost"] = core.Metric{Used: &todayCostUSD, Unit: "USD", Window: "1d"}
	}
	if todayRequests > 0 {
		tr := float64(todayRequests)
		snap.Metrics["today_composer_requests"] = core.Metric{Used: &tr, Unit: "requests", Window: "1d"}
	}

	sessions := float64(totalSessions)
	snap.Metrics["composer_sessions"] = core.Metric{Used: &sessions, Unit: "sessions", Window: "all-time"}
	reqs := float64(totalRequests)
	snap.Metrics["composer_requests"] = core.Metric{Used: &reqs, Unit: "requests", Window: "all-time"}

	if totalLinesAdded > 0 {
		la := float64(totalLinesAdded)
		snap.Metrics["composer_lines_added"] = core.Metric{Used: &la, Unit: "lines", Window: "all-time"}
	}
	if totalLinesRemoved > 0 {
		lr := float64(totalLinesRemoved)
		snap.Metrics["composer_lines_removed"] = core.Metric{Used: &lr, Unit: "lines", Window: "all-time"}
	}

	for model, costCents := range modelCosts {
		costUSD := costCents / 100.0
		modelKey := sanitizeCursorMetricName(model)
		snap.Metrics["model_"+modelKey+"_cost"] = core.Metric{Used: &costUSD, Unit: "USD", Window: "all-time"}
		if reqCount, ok := modelRequests[model]; ok {
			r := float64(reqCount)
			if existing, exists := snap.Metrics["model_"+modelKey+"_requests"]; exists && existing.Used != nil {
				combined := *existing.Used + r
				snap.Metrics["model_"+modelKey+"_requests"] = core.Metric{Used: &combined, Unit: "requests", Window: "all-time"}
			} else {
				snap.Metrics["model_"+modelKey+"_requests"] = core.Metric{Used: &r, Unit: "requests", Window: "all-time"}
			}
		}
		rec := core.ModelUsageRecord{RawModelID: model, RawSource: "composer", Window: "all-time", CostUSD: core.Float64Ptr(costUSD)}
		if reqCount, ok := modelRequests[model]; ok {
			rec.Requests = core.Float64Ptr(float64(reqCount))
		}
		snap.AppendModelUsage(rec)
	}

	for mode, count := range modeSessions {
		v := float64(count)
		snap.Metrics["mode_"+sanitizeCursorMetricName(mode)+"_sessions"] = core.Metric{Used: &v, Unit: "sessions", Window: "all-time"}
	}
	if totalFilesChanged > 0 {
		v := float64(totalFilesChanged)
		snap.Metrics["composer_files_changed"] = core.Metric{Used: &v, Unit: "files", Window: "all-time"}
	}
	if totalFilesCreated > 0 {
		v := float64(totalFilesCreated)
		snap.Metrics["composer_files_created"] = core.Metric{Used: &v, Unit: "files", Window: "all-time"}
	}
	if totalFilesRemoved > 0 {
		v := float64(totalFilesRemoved)
		snap.Metrics["composer_files_removed"] = core.Metric{Used: &v, Unit: "files", Window: "all-time"}
	}
	if agenticSessions > 0 {
		v := float64(agenticSessions)
		snap.Metrics["agentic_sessions"] = core.Metric{Used: &v, Unit: "sessions", Window: "all-time"}
	}
	if nonAgenticSessions > 0 {
		v := float64(nonAgenticSessions)
		snap.Metrics["non_agentic_sessions"] = core.Metric{Used: &v, Unit: "sessions", Window: "all-time"}
	}
	for forceMode, count := range forceModes {
		v := float64(count)
		snap.Metrics["mode_"+sanitizeCursorMetricName(forceMode)+"_sessions"] = core.Metric{Used: &v, Unit: "sessions", Window: "all-time"}
	}
	if contextSampleCount > 0 {
		avgPct := math.Round(((totalContextUsed/totalContextLimit)*100)*10) / 10
		hundred := 100.0
		remaining := hundred - avgPct
		snap.Metrics["composer_context_pct"] = core.Metric{
			Used:      &avgPct,
			Remaining: &remaining,
			Limit:     &hundred,
			Unit:      "%",
			Window:    "avg",
		}
	}
	for subagentType, count := range subagentTypes {
		v := float64(count)
		snap.Metrics["subagent_"+sanitizeCursorMetricName(subagentType)+"_sessions"] = core.Metric{Used: &v, Unit: "sessions", Window: "all-time"}
	}

	snap.Raw["composer_total_cost"] = fmt.Sprintf("$%.2f", totalCostUSD)
	snap.Raw["composer_total_sessions"] = strconv.Itoa(totalSessions)
	snap.Raw["composer_total_requests"] = strconv.Itoa(totalRequests)
	if totalLinesAdded > 0 {
		snap.Raw["composer_lines_added"] = strconv.Itoa(totalLinesAdded)
		snap.Raw["composer_lines_removed"] = strconv.Itoa(totalLinesRemoved)
	}

	if len(dailyCost) > 1 {
		points := make([]core.TimePoint, 0, len(dailyCost))
		for day, cents := range dailyCost {
			points = append(points, core.TimePoint{Date: day, Value: cents / 100.0})
		}
		sort.Slice(points, func(i, j int) bool { return points[i].Date < points[j].Date })
		snap.DailySeries["analytics_cost"] = points
	}
	if len(dailyRequests) > 1 {
		points := mapToSortedDailyPoints(dailyRequests)
		if existing, ok := snap.DailySeries["analytics_requests"]; ok && len(existing) > 0 {
			snap.DailySeries["analytics_requests"] = mergeDailyPoints(existing, points)
		} else {
			snap.DailySeries["composer_requests_daily"] = points
		}
	}
}

func mergeDailyPoints(a, b []core.TimePoint) []core.TimePoint {
	byDay := make(map[string]float64)
	for _, point := range a {
		byDay[point.Date] += point.Value
	}
	for _, point := range b {
		if byDay[point.Date] < point.Value {
			byDay[point.Date] = point.Value
		}
	}
	return mapToSortedDailyPoints(byDay)
}

func (p *Provider) readStateMetadata(ctx context.Context, db *sql.DB, snap *core.UsageSnapshot) {
	var email string
	if db.QueryRowContext(ctx, `SELECT value FROM ItemTable WHERE key = 'cursorAuth/cachedEmail'`).Scan(&email) == nil && email != "" {
		snap.Raw["account_email"] = email
	}

	var promptCount string
	if db.QueryRowContext(ctx, `SELECT value FROM ItemTable WHERE key = 'freeBestOfN.promptCount'`).Scan(&promptCount) == nil && promptCount != "" {
		if v, err := strconv.ParseFloat(promptCount, 64); err == nil && v > 0 {
			snap.Metrics["total_prompts"] = core.Metric{Used: &v, Unit: "prompts", Window: "all-time"}
			snap.Raw["total_prompts"] = promptCount
		}
	}

	var membership string
	if db.QueryRowContext(ctx, `SELECT value FROM ItemTable WHERE key = 'cursorAuth/stripeMembershipType'`).Scan(&membership) == nil && membership != "" {
		if snap.Raw["membership_type"] == "" {
			snap.Raw["membership_type"] = membership
		}
	}
}

func (p *Provider) readToolUsage(records []cursorBubbleRecord, snap *core.UsageSnapshot) {
	toolCounts := make(map[string]int)
	statusCounts := make(map[string]int)
	totalCalls := 0

	for _, record := range records {
		if strings.TrimSpace(record.ToolName) == "" {
			continue
		}
		name := normalizeToolName(record.ToolName)
		toolCounts[name]++
		totalCalls++
		if strings.TrimSpace(record.ToolStatus) != "" {
			statusCounts[record.ToolStatus]++
		}
	}

	if totalCalls == 0 {
		return
	}

	tc := float64(totalCalls)
	snap.Metrics["tool_calls_total"] = core.Metric{Used: &tc, Unit: "calls", Window: "all-time"}
	for name, count := range toolCounts {
		v := float64(count)
		snap.Metrics["tool_"+sanitizeCursorMetricName(name)] = core.Metric{Used: &v, Unit: "calls", Window: "all-time"}
	}
	if completed, ok := statusCounts["completed"]; ok && completed > 0 {
		v := float64(completed)
		snap.Metrics["tool_completed"] = core.Metric{Used: &v, Unit: "calls", Window: "all-time"}
	}
	if errored, ok := statusCounts["error"]; ok && errored > 0 {
		v := float64(errored)
		snap.Metrics["tool_errored"] = core.Metric{Used: &v, Unit: "calls", Window: "all-time"}
	}
	if cancelled, ok := statusCounts["cancelled"]; ok && cancelled > 0 {
		v := float64(cancelled)
		snap.Metrics["tool_cancelled"] = core.Metric{Used: &v, Unit: "calls", Window: "all-time"}
	}

	completed := float64(statusCounts["completed"])
	successPct := math.Round((completed/float64(totalCalls))*1000) / 10
	hundred := 100.0
	remaining := hundred - successPct
	snap.Metrics["tool_success_rate"] = core.Metric{
		Used:      &successPct,
		Remaining: &remaining,
		Limit:     &hundred,
		Unit:      "%",
		Window:    "all-time",
	}
	snap.Raw["tool_calls_total"] = strconv.Itoa(totalCalls)
	snap.Raw["tool_completed"] = strconv.Itoa(statusCounts["completed"])
	snap.Raw["tool_errored"] = strconv.Itoa(statusCounts["error"])
	snap.Raw["tool_cancelled"] = strconv.Itoa(statusCounts["cancelled"])
}

func normalizeToolName(raw string) string {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "unknown"
	}
	if strings.HasPrefix(name, "mcp-") || strings.HasPrefix(name, "mcp_") {
		return normalizeCursorMCPName(name)
	}
	name = strings.TrimSuffix(name, "_v2")
	name = strings.TrimSuffix(name, "_v3")
	return name
}

func normalizeCursorMCPName(name string) string {
	if strings.HasPrefix(name, "mcp-") {
		rest := name[4:]
		parts := strings.SplitN(rest, "-user-", 2)
		if len(parts) == 2 {
			server := parts[0]
			afterUser := parts[1]
			serverDash := server + "-"
			if strings.HasPrefix(afterUser, serverDash) {
				return "mcp__" + server + "__" + afterUser[len(serverDash):]
			}
			if idx := strings.LastIndex(afterUser, "-"); idx > 0 {
				return "mcp__" + server + "__" + afterUser[idx+1:]
			}
			return "mcp__" + server + "__" + afterUser
		}
		if idx := strings.Index(rest, "-"); idx > 0 {
			return "mcp__" + rest[:idx] + "__" + rest[idx+1:]
		}
		return "mcp__" + rest + "__"
	}

	if strings.HasPrefix(name, "mcp_") {
		rest := name[4:]
		if idx := strings.Index(rest, "_"); idx > 0 {
			return "mcp__" + rest[:idx] + "__" + rest[idx+1:]
		}
		return "mcp__" + rest + "__"
	}
	return name
}

func (p *Provider) readDailyStatsSeries(records []cursorDailyStatsRecord, snap *core.UsageSnapshot) {
	for _, record := range records {
		stats := record.Stats
		dateStr := record.Date
		if stats.TabSuggestedLines > 0 || stats.TabAcceptedLines > 0 {
			snap.DailySeries["tab_suggested"] = append(snap.DailySeries["tab_suggested"], core.TimePoint{Date: dateStr, Value: float64(stats.TabSuggestedLines)})
			snap.DailySeries["tab_accepted"] = append(snap.DailySeries["tab_accepted"], core.TimePoint{Date: dateStr, Value: float64(stats.TabAcceptedLines)})
		}
		if stats.ComposerSuggestedLines > 0 || stats.ComposerAcceptedLines > 0 {
			snap.DailySeries["composer_suggested"] = append(snap.DailySeries["composer_suggested"], core.TimePoint{Date: dateStr, Value: float64(stats.ComposerSuggestedLines)})
			snap.DailySeries["composer_accepted"] = append(snap.DailySeries["composer_accepted"], core.TimePoint{Date: dateStr, Value: float64(stats.ComposerAcceptedLines)})
		}
		totalLines := float64(stats.TabSuggestedLines + stats.ComposerSuggestedLines)
		if totalLines > 0 {
			snap.DailySeries["total_lines"] = append(snap.DailySeries["total_lines"], core.TimePoint{Date: dateStr, Value: totalLines})
		}
	}
}

func formatTimestamp(s string) string {
	t := shared.FlexParseTime(s)
	if t.IsZero() {
		return s
	}
	return t.Format("Jan 02, 2006 15:04 MST")
}
