package core

import (
	"sort"
	"strconv"
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

type ModelBreakdownEntry struct {
	Name       string
	Cost       float64
	Input      float64
	Output     float64
	Requests   float64
	Requests1d float64
	Series     []TimePoint
}

type ProviderBreakdownEntry struct {
	Name     string
	Cost     float64
	Input    float64
	Output   float64
	Requests float64
}

type ClientBreakdownEntry struct {
	Name       string
	Total      float64
	Input      float64
	Output     float64
	Cached     float64
	Reasoning  float64
	Requests   float64
	Sessions   float64
	SeriesKind string
	Series     []TimePoint
}

type ActualToolUsageEntry struct {
	RawName string
	Calls   float64
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
	return SortedTimePoints(pointsByDay)
}

func sumBreakdownSeries(points []TimePoint) float64 {
	total := 0.0
	for _, point := range points {
		total += point.Value
	}
	return total
}

func parseSourceMetricKey(key string) (name, field string, ok bool) {
	const prefix = "source_"
	if !strings.HasPrefix(key, prefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(key, prefix)
	for _, suffix := range []string{"_requests_today", "_requests"} {
		if strings.HasSuffix(rest, suffix) {
			return strings.TrimSuffix(rest, suffix), strings.TrimPrefix(suffix, "_"), true
		}
	}
	return "", "", false
}

func parseClientMetricKey(key string) (name, field string, ok bool) {
	const prefix = "client_"
	if !strings.HasPrefix(key, prefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(key, prefix)
	for _, suffix := range []string{
		"_total_tokens", "_input_tokens", "_output_tokens",
		"_cached_tokens", "_reasoning_tokens", "_requests", "_sessions",
	} {
		if strings.HasSuffix(rest, suffix) {
			return strings.TrimSuffix(rest, suffix), strings.TrimPrefix(suffix, "_"), true
		}
	}
	return "", "", false
}

func canonicalizeClientBucket(name string) string {
	bucket := sourceAsClientBucket(name)
	switch bucket {
	case "codex", "openusage":
		return "cli_agents"
	}
	return bucket
}

func sourceAsClientBucket(source string) string {
	s := strings.ToLower(strings.TrimSpace(source))
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, " ", "_")
	if s == "" || s == "unknown" {
		return "other"
	}

	switch s {
	case "composer", "tab", "human", "vscode", "ide", "editor", "cursor":
		return "ide"
	case "cloud", "cloud_agent", "cloud_agents", "web", "web_agent", "background_agent":
		return "cloud_agents"
	case "cli", "terminal", "agent", "agents", "cli_agents":
		return "cli_agents"
	case "desktop", "desktop_app":
		return "desktop_app"
	}

	if strings.Contains(s, "cloud") || strings.Contains(s, "web") {
		return "cloud_agents"
	}
	if strings.Contains(s, "cli") || strings.Contains(s, "terminal") || strings.Contains(s, "agent") {
		return "cli_agents"
	}
	if strings.Contains(s, "compose") || strings.Contains(s, "tab") || strings.Contains(s, "ide") || strings.Contains(s, "editor") {
		return "ide"
	}
	return s
}

func snapshotBreakdownMetaEntries(s UsageSnapshot) map[string]string {
	if len(s.Raw) == 0 && len(s.Attributes) == 0 && len(s.Diagnostics) == 0 {
		return nil
	}
	meta := make(map[string]string, len(s.Raw)+len(s.Attributes)+len(s.Diagnostics))
	for key, raw := range s.Attributes {
		meta[key] = raw
	}
	for key, raw := range s.Diagnostics {
		if _, ok := meta[key]; !ok {
			meta[key] = raw
		}
	}
	for key, raw := range s.Raw {
		if _, ok := meta[key]; !ok {
			meta[key] = raw
		}
	}
	return meta
}

func parseBreakdownNumeric(raw string) (float64, bool) {
	s := strings.TrimSpace(strings.ReplaceAll(raw, ",", ""))
	if s == "" {
		return 0, false
	}
	s = strings.TrimPrefix(s, "$")
	s = strings.TrimSuffix(s, "%")
	if idx := strings.IndexByte(s, ' '); idx > 0 {
		s = s[:idx]
	}
	if idx := strings.IndexByte(s, '/'); idx > 0 {
		s = s[:idx]
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func breakdownClientTokenValue(client ClientBreakdownEntry) float64 {
	if client.Total > 0 {
		return client.Total
	}
	if client.Input > 0 || client.Output > 0 || client.Cached > 0 || client.Reasoning > 0 {
		return client.Input + client.Output + client.Cached + client.Reasoning
	}
	return 0
}

func breakdownClientValue(client ClientBreakdownEntry) float64 {
	if value := breakdownClientTokenValue(client); value > 0 {
		return value
	}
	if client.Requests > 0 {
		return client.Requests
	}
	if len(client.Series) > 0 {
		return sumBreakdownSeries(client.Series)
	}
	return 0
}
