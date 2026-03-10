package cursor

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func (p *Provider) readTrackingDB(ctx context.Context, dbPath string, snap *core.UsageSnapshot) error {
	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?mode=ro", dbPath))
	if err != nil {
		return fmt.Errorf("opening tracking DB: %w", err)
	}
	defer db.Close()

	if !cursorTableExists(ctx, db, "ai_code_hashes") {
		return nil
	}

	trackingRecords, err := loadTrackingRecords(ctx, db, p.clock)
	if err != nil {
		return err
	}
	totalRequests := len(trackingRecords)
	if totalRequests > 0 {
		total := float64(totalRequests)
		snap.Metrics["total_ai_requests"] = core.Metric{Used: &total, Unit: "requests", Window: "all-time"}
	}

	today := p.now().Format("2006-01-02")
	todayCount := 0
	for _, record := range trackingRecords {
		if record.OccurredDay == today {
			todayCount++
		}
	}
	if todayCount > 0 {
		tc := float64(todayCount)
		snap.Metrics["requests_today"] = core.Metric{Used: &tc, Unit: "requests", Window: "1d"}
	}

	p.readTrackingSourceBreakdown(trackingRecords, snap, today)
	p.readTrackingDailyRequests(trackingRecords, snap)
	p.readTrackingModelBreakdown(trackingRecords, snap, today)
	p.readTrackingLanguageBreakdown(trackingRecords, snap)
	p.readScoredCommits(ctx, db, snap)
	p.readDeletedFiles(ctx, db, snap)
	p.readTrackedFileContent(ctx, db, snap)
	return nil
}

func (p *Provider) readScoredCommits(ctx context.Context, db *sql.DB, snap *core.UsageSnapshot) {
	var totalCommits int
	if db.QueryRowContext(ctx, `SELECT COUNT(*) FROM scored_commits WHERE linesAdded IS NOT NULL AND linesAdded > 0`).Scan(&totalCommits) != nil || totalCommits == 0 {
		return
	}

	rows, err := db.QueryContext(ctx, `
		SELECT v2AiPercentage, linesAdded, linesDeleted,
		       tabLinesAdded, tabLinesDeleted,
		       composerLinesAdded, composerLinesDeleted,
		       humanLinesAdded, humanLinesDeleted,
		       blankLinesAdded, blankLinesDeleted
		FROM scored_commits
		WHERE linesAdded IS NOT NULL AND linesAdded > 0
		ORDER BY scoredAt DESC`)
	if err != nil {
		return
	}
	defer rows.Close()

	var (
		sumAIPct      float64
		countWithPct  int
		totalTabAdd   int
		totalTabDel   int
		totalCompAdd  int
		totalCompDel  int
		totalHumanAdd int
		totalHumanDel int
		totalBlankAdd int
		totalBlankDel int
		totalLinesAdd int
		totalLinesDel int
	)

	for rows.Next() {
		var pctStr sql.NullString
		var linesAdded, linesDeleted sql.NullInt64
		var tabAdd, tabDel, compAdd, compDel, humanAdd, humanDel sql.NullInt64
		var blankAdd, blankDel sql.NullInt64
		if rows.Scan(&pctStr, &linesAdded, &linesDeleted, &tabAdd, &tabDel, &compAdd, &compDel, &humanAdd, &humanDel, &blankAdd, &blankDel) != nil {
			continue
		}
		if pctStr.Valid && pctStr.String != "" {
			if v, err := strconv.ParseFloat(pctStr.String, 64); err == nil {
				sumAIPct += v
				countWithPct++
			}
		}
		if linesAdded.Valid {
			totalLinesAdd += int(linesAdded.Int64)
		}
		if linesDeleted.Valid {
			totalLinesDel += int(linesDeleted.Int64)
		}
		if tabAdd.Valid {
			totalTabAdd += int(tabAdd.Int64)
		}
		if tabDel.Valid {
			totalTabDel += int(tabDel.Int64)
		}
		if compAdd.Valid {
			totalCompAdd += int(compAdd.Int64)
		}
		if compDel.Valid {
			totalCompDel += int(compDel.Int64)
		}
		if humanAdd.Valid {
			totalHumanAdd += int(humanAdd.Int64)
		}
		if humanDel.Valid {
			totalHumanDel += int(humanDel.Int64)
		}
		if blankAdd.Valid {
			totalBlankAdd += int(blankAdd.Int64)
		}
		if blankDel.Valid {
			totalBlankDel += int(blankDel.Int64)
		}
	}

	tc := float64(totalCommits)
	snap.Metrics["scored_commits"] = core.Metric{Used: &tc, Unit: "commits", Window: "all-time"}
	snap.Raw["scored_commits_total"] = strconv.Itoa(totalCommits)

	if countWithPct > 0 {
		avgPct := math.Round((sumAIPct/float64(countWithPct))*10) / 10
		hundred := 100.0
		remaining := hundred - avgPct
		snap.Metrics["ai_code_percentage"] = core.Metric{
			Used:      &avgPct,
			Remaining: &remaining,
			Limit:     &hundred,
			Unit:      "%",
			Window:    "all-commits",
		}
		snap.Raw["ai_code_pct_avg"] = fmt.Sprintf("%.1f%%", avgPct)
		snap.Raw["ai_code_pct_sample"] = strconv.Itoa(countWithPct)
	}

	if totalLinesAdd > 0 || totalLinesDel > 0 {
		snap.Raw["commit_total_lines_added"] = strconv.Itoa(totalLinesAdd)
		snap.Raw["commit_total_lines_deleted"] = strconv.Itoa(totalLinesDel)
	}
	if totalTabAdd > 0 || totalCompAdd > 0 || totalHumanAdd > 0 {
		snap.Raw["commit_tab_lines"] = strconv.Itoa(totalTabAdd)
		snap.Raw["commit_composer_lines"] = strconv.Itoa(totalCompAdd)
		snap.Raw["commit_human_lines"] = strconv.Itoa(totalHumanAdd)
	}
	if totalTabDel > 0 || totalCompDel > 0 || totalHumanDel > 0 {
		snap.Raw["commit_tab_lines_deleted"] = strconv.Itoa(totalTabDel)
		snap.Raw["commit_composer_lines_deleted"] = strconv.Itoa(totalCompDel)
		snap.Raw["commit_human_lines_deleted"] = strconv.Itoa(totalHumanDel)
	}
	if totalBlankAdd > 0 || totalBlankDel > 0 {
		snap.Raw["commit_blank_lines_added"] = strconv.Itoa(totalBlankAdd)
		snap.Raw["commit_blank_lines_deleted"] = strconv.Itoa(totalBlankDel)
	}
}

func (p *Provider) readDeletedFiles(ctx context.Context, db *sql.DB, snap *core.UsageSnapshot) {
	var count int
	if db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_deleted_files`).Scan(&count) == nil && count > 0 {
		v := float64(count)
		snap.Metrics["ai_deleted_files"] = core.Metric{Used: &v, Unit: "files", Window: "all-time"}
	}
}

func (p *Provider) readTrackedFileContent(ctx context.Context, db *sql.DB, snap *core.UsageSnapshot) {
	var count int
	if db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tracked_file_content`).Scan(&count) == nil && count > 0 {
		v := float64(count)
		snap.Metrics["ai_tracked_files"] = core.Metric{Used: &v, Unit: "files", Window: "all-time"}
	}
}

func chooseTrackingTimeExpr(ctx context.Context, db *sql.DB) string {
	columns := cursorTableColumns(ctx, db, "ai_code_hashes")
	hasCreatedAt := columns["createdat"]
	hasTimestamp := columns["timestamp"]
	switch {
	case hasCreatedAt && hasTimestamp:
		return "COALESCE(createdAt, timestamp)"
	case hasCreatedAt:
		return "createdAt"
	case hasTimestamp:
		return "timestamp"
	default:
		return "0"
	}
}

func (p *Provider) readTrackingSourceBreakdown(records []cursorTrackingRecord, snap *core.UsageSnapshot, today string) {
	clientTotals := map[string]float64{"ide": 0, "cli_agents": 0, "other": 0}
	sourceTotals := make(map[string]int)
	todaySourceTotals := make(map[string]int)
	var sourceSummary []string
	for _, record := range records {
		sourceTotals[record.Source]++
		if record.OccurredDay == today {
			todaySourceTotals[record.Source]++
		}
	}
	for source, count := range sourceTotals {
		value := float64(count)
		sourceKey := sanitizeCursorMetricName(source)
		snap.Metrics["source_"+sourceKey+"_requests"] = core.Metric{Used: &value, Unit: "requests", Window: "all-time"}
		ifaceValue := value
		snap.Metrics["interface_"+sourceKey] = core.Metric{Used: &ifaceValue, Unit: "calls", Window: "all-time"}
		clientTotals[cursorClientBucket(source)] += value
		sourceSummary = append(sourceSummary, fmt.Sprintf("%s %d", sourceLabel(source), count))
	}
	if len(sourceSummary) > 0 {
		snap.Raw["source_usage"] = strings.Join(sourceSummary, " · ")
	}
	for bucket, value := range clientTotals {
		if value <= 0 {
			continue
		}
		v := value
		snap.Metrics["client_"+bucket+"_sessions"] = core.Metric{Used: &v, Unit: "sessions", Window: "all-time"}
	}

	var todaySummary []string
	for source, count := range todaySourceTotals {
		if count <= 0 {
			continue
		}
		value := float64(count)
		sourceKey := sanitizeCursorMetricName(source)
		snap.Metrics["source_"+sourceKey+"_requests_today"] = core.Metric{Used: &value, Unit: "requests", Window: "1d"}
		todaySummary = append(todaySummary, fmt.Sprintf("%s %d", sourceLabel(source), count))
	}
	if len(todaySummary) > 0 {
		snap.Raw["source_usage_today"] = strings.Join(todaySummary, " · ")
	}
}

func (p *Provider) readTrackingDailyRequests(records []cursorTrackingRecord, snap *core.UsageSnapshot) {
	totalByDay := make(map[string]float64)
	byClientDay := map[string]map[string]float64{
		"ide":        make(map[string]float64),
		"cli_agents": make(map[string]float64),
		"other":      make(map[string]float64),
	}
	bySourceDay := make(map[string]map[string]float64)

	for _, record := range records {
		if record.OccurredDay == "" {
			continue
		}
		totalByDay[record.OccurredDay] += 1
		clientKey := cursorClientBucket(record.Source)
		byClientDay[clientKey][record.OccurredDay] += 1
		sourceKey := sanitizeCursorMetricName(record.Source)
		if bySourceDay[sourceKey] == nil {
			bySourceDay[sourceKey] = make(map[string]float64)
		}
		bySourceDay[sourceKey][record.OccurredDay] += 1
	}

	if len(totalByDay) > 1 {
		snap.DailySeries["analytics_requests"] = mapToSortedDailyPoints(totalByDay)
	}
	for clientKey, pointsByDay := range byClientDay {
		if len(pointsByDay) < 2 {
			continue
		}
		snap.DailySeries["usage_client_"+clientKey] = mapToSortedDailyPoints(pointsByDay)
	}
	for sourceKey, pointsByDay := range bySourceDay {
		if len(pointsByDay) < 2 {
			continue
		}
		snap.DailySeries["usage_source_"+sourceKey] = mapToSortedDailyPoints(pointsByDay)
	}
}

func (p *Provider) readTrackingModelBreakdown(records []cursorTrackingRecord, snap *core.UsageSnapshot, today string) {
	modelTotals := make(map[string]int)
	todayModelTotals := make(map[string]int)
	byModelDay := make(map[string]map[string]float64)
	var modelSummary []string
	for _, record := range records {
		modelTotals[record.Model]++
		if record.OccurredDay == today {
			todayModelTotals[record.Model]++
		}
		modelKey := sanitizeCursorMetricName(record.Model)
		if byModelDay[modelKey] == nil {
			byModelDay[modelKey] = make(map[string]float64)
		}
		if record.OccurredDay != "" {
			byModelDay[modelKey][record.OccurredDay]++
		}
	}
	for model, count := range modelTotals {
		if count <= 0 {
			continue
		}
		value := float64(count)
		modelKey := sanitizeCursorMetricName(model)
		snap.Metrics["model_"+modelKey+"_requests"] = core.Metric{Used: &value, Unit: "requests", Window: "all-time"}
		modelSummary = append(modelSummary, fmt.Sprintf("%s %d", sourceLabel(model), count))
	}
	if len(modelSummary) > 0 {
		snap.Raw["model_usage"] = strings.Join(modelSummary, " · ")
	}
	for model, count := range todayModelTotals {
		if count <= 0 {
			continue
		}
		value := float64(count)
		modelKey := sanitizeCursorMetricName(model)
		snap.Metrics["model_"+modelKey+"_requests_today"] = core.Metric{Used: &value, Unit: "requests", Window: "1d"}
	}
	for modelKey, pointsByDay := range byModelDay {
		if len(pointsByDay) < 2 {
			continue
		}
		snap.DailySeries["usage_model_"+modelKey] = mapToSortedDailyPoints(pointsByDay)
	}
}

func (p *Provider) readTrackingLanguageBreakdown(records []cursorTrackingRecord, snap *core.UsageSnapshot) {
	langTotals := make(map[string]int)
	var langSummary []string
	for _, record := range records {
		if strings.TrimSpace(record.FileExt) == "" {
			continue
		}
		langTotals[record.FileExt]++
	}
	for ext, count := range langTotals {
		value := float64(count)
		langName := extensionToLanguage(ext)
		langKey := sanitizeCursorMetricName(langName)
		snap.Metrics["lang_"+langKey] = core.Metric{Used: &value, Unit: "requests", Window: "all-time"}
		langSummary = append(langSummary, fmt.Sprintf("%s %d", langName, count))
	}
	if len(langSummary) > 0 {
		snap.Raw["language_usage"] = strings.Join(langSummary, " · ")
	}
}

var extToLang = map[string]string{
	".ts": "TypeScript", ".tsx": "TypeScript", ".js": "JavaScript", ".jsx": "JavaScript",
	".py": "Python", ".go": "Go", ".rs": "Rust", ".rb": "Ruby",
	".java": "Java", ".kt": "Kotlin", ".kts": "Kotlin",
	".cs": "C#", ".fs": "F#",
	".cpp": "C++", ".cc": "C++", ".cxx": "C++", ".hpp": "C++",
	".c": "C", ".h": "C/C++",
	".swift": "Swift", ".m": "Obj-C",
	".php": "PHP", ".lua": "Lua", ".r": "R",
	".scala": "Scala", ".clj": "Clojure", ".ex": "Elixir", ".exs": "Elixir",
	".hs": "Haskell", ".erl": "Erlang",
	".html": "HTML", ".htm": "HTML", ".css": "CSS", ".scss": "SCSS", ".less": "LESS",
	".json": "JSON", ".yaml": "YAML", ".yml": "YAML", ".toml": "TOML", ".xml": "XML",
	".md": "Markdown", ".mdx": "Markdown",
	".sql": "SQL", ".graphql": "GraphQL", ".gql": "GraphQL",
	".sh": "Shell", ".bash": "Shell", ".zsh": "Shell", ".fish": "Shell",
	".dockerfile": "Docker", ".tf": "Terraform", ".hcl": "HCL",
	".vue": "Vue", ".svelte": "Svelte", ".astro": "Astro",
	".dart": "Dart", ".zig": "Zig", ".nim": "Nim", ".v": "V",
	".proto": "Protobuf", ".wasm": "WASM",
}

func extensionToLanguage(ext string) string {
	ext = strings.ToLower(strings.TrimSpace(ext))
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	if lang, ok := extToLang[ext]; ok {
		return lang
	}
	return strings.TrimPrefix(ext, ".")
}

func mapToSortedDailyPoints(byDay map[string]float64) []core.TimePoint {
	if len(byDay) == 0 {
		return nil
	}
	days := core.SortedStringKeys(byDay)
	points := make([]core.TimePoint, 0, len(days))
	for _, day := range days {
		points = append(points, core.TimePoint{Date: day, Value: byDay[day]})
	}
	return points
}

func cursorClientBucket(source string) string {
	s := strings.ToLower(strings.TrimSpace(source))
	switch {
	case s == "":
		return "other"
	case strings.Contains(s, "cloud"), strings.Contains(s, "web"), s == "background-agent", s == "background_agent":
		return "cloud_agents"
	case strings.Contains(s, "cli"), strings.Contains(s, "agent"), strings.Contains(s, "terminal"), strings.Contains(s, "cmd"):
		return "cli_agents"
	case s == "composer", s == "tab", s == "human", strings.Contains(s, "ide"), strings.Contains(s, "editor"):
		return "ide"
	default:
		return "other"
	}
}

func sanitizeCursorMetricName(source string) string {
	s := strings.ToLower(strings.TrimSpace(source))
	if s == "" {
		return "unknown"
	}
	var b strings.Builder
	lastUnderscore := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			lastUnderscore = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "unknown"
	}
	return out
}

func sourceLabel(source string) string {
	trimmed := strings.TrimSpace(source)
	if trimmed == "" {
		return "unknown"
	}
	return trimmed
}
