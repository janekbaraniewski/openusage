package core

import (
	"sort"
	"strings"
)

type LanguageUsageEntry struct {
	Name     string
	Requests float64
}

type MCPFunctionUsageEntry struct {
	RawName string
	Calls   float64
}

type MCPServerUsageEntry struct {
	RawName   string
	Calls     float64
	Functions []MCPFunctionUsageEntry
}

type ProjectUsageEntry struct {
	Name       string
	Requests   float64
	Requests1d float64
	Series     []TimePoint
}

func ExtractLanguageUsage(s UsageSnapshot) ([]LanguageUsageEntry, map[string]bool) {
	byLang := make(map[string]float64)
	usedKeys := make(map[string]bool)

	for key, metric := range s.Metrics {
		if metric.Used == nil || !strings.HasPrefix(key, "lang_") {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(key, "lang_"))
		if name == "" {
			continue
		}
		byLang[name] += *metric.Used
		usedKeys[key] = true
	}

	if len(byLang) == 0 {
		return nil, nil
	}

	out := make([]LanguageUsageEntry, 0, len(byLang))
	for name, requests := range byLang {
		if requests <= 0 {
			continue
		}
		out = append(out, LanguageUsageEntry{
			Name:     name,
			Requests: requests,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Requests != out[j].Requests {
			return out[i].Requests > out[j].Requests
		}
		return out[i].Name < out[j].Name
	})
	return out, usedKeys
}

func ExtractMCPUsage(s UsageSnapshot) ([]MCPServerUsageEntry, map[string]bool) {
	usedKeys := make(map[string]bool)
	serverMap := make(map[string]*MCPServerUsageEntry)

	for key, metric := range s.Metrics {
		if metric.Used == nil || !strings.HasPrefix(key, "mcp_") {
			continue
		}
		usedKeys[key] = true
		if key == "mcp_calls_total" || key == "mcp_calls_total_today" || key == "mcp_servers_active" {
			continue
		}
		if strings.HasSuffix(key, "_today") {
			continue
		}

		rest := strings.TrimPrefix(key, "mcp_")
		if !strings.HasSuffix(rest, "_total") {
			continue
		}

		rawServerName := strings.TrimSpace(strings.TrimSuffix(rest, "_total"))
		if rawServerName == "" {
			continue
		}
		serverMap[rawServerName] = &MCPServerUsageEntry{
			RawName: rawServerName,
			Calls:   *metric.Used,
		}
	}

	for key, metric := range s.Metrics {
		if metric.Used == nil || !strings.HasPrefix(key, "mcp_") {
			continue
		}
		if key == "mcp_calls_total" || key == "mcp_calls_total_today" || key == "mcp_servers_active" {
			continue
		}
		if strings.HasSuffix(key, "_today") || strings.HasSuffix(key, "_total") {
			continue
		}

		rest := strings.TrimPrefix(key, "mcp_")
		for rawServerName, server := range serverMap {
			prefix := rawServerName + "_"
			if !strings.HasPrefix(rest, prefix) {
				continue
			}
			funcName := strings.TrimSpace(strings.TrimPrefix(rest, prefix))
			if funcName == "" {
				break
			}
			server.Functions = append(server.Functions, MCPFunctionUsageEntry{
				RawName: funcName,
				Calls:   *metric.Used,
			})
			break
		}
	}

	if len(serverMap) == 0 {
		return nil, usedKeys
	}

	out := make([]MCPServerUsageEntry, 0, len(serverMap))
	for _, server := range serverMap {
		if server.Calls <= 0 {
			continue
		}
		sort.Slice(server.Functions, func(i, j int) bool {
			if server.Functions[i].Calls != server.Functions[j].Calls {
				return server.Functions[i].Calls > server.Functions[j].Calls
			}
			return server.Functions[i].RawName < server.Functions[j].RawName
		})
		out = append(out, *server)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Calls != out[j].Calls {
			return out[i].Calls > out[j].Calls
		}
		return out[i].RawName < out[j].RawName
	})
	return out, usedKeys
}

func ExtractProjectUsage(s UsageSnapshot) ([]ProjectUsageEntry, map[string]bool) {
	byProject := make(map[string]*ProjectUsageEntry)
	usedKeys := make(map[string]bool)
	seriesByProject := make(map[string]map[string]float64)

	ensure := func(name string) *ProjectUsageEntry {
		if _, ok := byProject[name]; !ok {
			byProject[name] = &ProjectUsageEntry{Name: name}
		}
		return byProject[name]
	}

	for key, metric := range s.Metrics {
		if metric.Used == nil {
			continue
		}
		name, field, ok := parseProjectMetricKey(key)
		if !ok {
			continue
		}
		project := ensure(name)
		switch field {
		case "requests":
			project.Requests = *metric.Used
		case "requests_today":
			project.Requests1d = *metric.Used
		}
		usedKeys[key] = true
	}

	for key, points := range s.DailySeries {
		if !strings.HasPrefix(key, "usage_project_") {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(key, "usage_project_"))
		if name == "" || len(points) == 0 {
			continue
		}
		mergeBreakdownSeriesByDay(seriesByProject, name, points)
	}

	for name, pointsByDay := range seriesByProject {
		project := ensure(name)
		project.Series = breakdownSortedSeries(pointsByDay)
		if project.Requests <= 0 {
			project.Requests = sumBreakdownSeries(project.Series)
		}
	}

	out := make([]ProjectUsageEntry, 0, len(byProject))
	for _, project := range byProject {
		if project.Requests <= 0 && len(project.Series) == 0 {
			continue
		}
		out = append(out, *project)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Requests != out[j].Requests {
			return out[i].Requests > out[j].Requests
		}
		return out[i].Name < out[j].Name
	})
	return out, usedKeys
}

func parseProjectMetricKey(key string) (name, field string, ok bool) {
	const prefix = "project_"
	if !strings.HasPrefix(key, prefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(key, prefix)
	if strings.HasSuffix(rest, "_requests_today") {
		return strings.TrimSuffix(rest, "_requests_today"), "requests_today", true
	}
	if strings.HasSuffix(rest, "_requests") {
		return strings.TrimSuffix(rest, "_requests"), "requests", true
	}
	return "", "", false
}

func mergeBreakdownSeriesByDay(seriesByName map[string]map[string]float64, name string, points []TimePoint) {
	if name == "" || len(points) == 0 {
		return
	}
	if seriesByName[name] == nil {
		seriesByName[name] = make(map[string]float64)
	}
	for _, point := range points {
		if point.Date == "" {
			continue
		}
		seriesByName[name][point.Date] += point.Value
	}
}

func breakdownSortedSeries(pointsByDay map[string]float64) []TimePoint {
	if len(pointsByDay) == 0 {
		return nil
	}
	days := make([]string, 0, len(pointsByDay))
	for day := range pointsByDay {
		days = append(days, day)
	}
	sort.Strings(days)

	points := make([]TimePoint, 0, len(days))
	for _, day := range days {
		points = append(points, TimePoint{
			Date:  day,
			Value: pointsByDay[day],
		})
	}
	return points
}

func sumBreakdownSeries(points []TimePoint) float64 {
	total := 0.0
	for _, point := range points {
		total += point.Value
	}
	return total
}
