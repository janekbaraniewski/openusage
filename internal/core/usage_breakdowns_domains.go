package core

import (
	"sort"
	"strings"
)

func HasLanguageUsage(s UsageSnapshot) bool {
	langs, _ := ExtractLanguageUsage(s)
	return len(langs) > 0
}

func HasMCPUsage(s UsageSnapshot) bool {
	servers, _ := ExtractMCPUsage(s)
	return len(servers) > 0
}

func IncludeDetailMetricKey(key string) bool {
	return !strings.HasPrefix(strings.TrimSpace(key), "mcp_")
}

func ExtractMCPBreakdown(s UsageSnapshot) ([]MCPServerUsageEntry, map[string]bool) {
	servers, usedKeys := ExtractMCPUsage(s)
	if len(servers) == 0 {
		return nil, usedKeys
	}

	byServer := make(map[string]*MCPServerUsageEntry, len(servers))
	for i := range servers {
		server := servers[i]
		byServer[server.RawName] = &server
	}

	for key, points := range s.DailySeries {
		if !strings.HasPrefix(key, "usage_mcp_") || len(points) == 0 {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(key, "usage_mcp_"))
		if name == "" {
			continue
		}
		server, ok := byServer[name]
		if !ok {
			continue
		}
		server.Series = points
		if server.Calls <= 0 {
			server.Calls = sumBreakdownSeries(points)
		}
	}

	out := make([]MCPServerUsageEntry, 0, len(byServer))
	for _, server := range byServer {
		if server.Calls <= 0 && len(server.Series) == 0 {
			continue
		}
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

func ExtractModelBreakdown(s UsageSnapshot) ([]ModelBreakdownEntry, map[string]bool) {
	type agg struct {
		cost       float64
		input      float64
		output     float64
		requests   float64
		requests1d float64
		series     []TimePoint
	}
	byModel := make(map[string]*agg)
	usedKeys := make(map[string]bool)

	ensure := func(name string) *agg {
		if _, ok := byModel[name]; !ok {
			byModel[name] = &agg{}
		}
		return byModel[name]
	}

	recordInput := func(name string, value float64, key string) {
		ensure(name).input += value
		usedKeys[key] = true
	}
	recordOutput := func(name string, value float64, key string) {
		ensure(name).output += value
		usedKeys[key] = true
	}
	recordCost := func(name string, value float64, key string) {
		ensure(name).cost += value
		usedKeys[key] = true
	}
	recordRequests := func(name string, value float64, key string) {
		ensure(name).requests += value
		usedKeys[key] = true
	}
	recordRequests1d := func(name string, value float64, key string) {
		ensure(name).requests1d += value
		usedKeys[key] = true
	}

	for key, metric := range s.Metrics {
		if metric.Used == nil {
			continue
		}
		switch {
		case strings.HasPrefix(key, "model_") && strings.HasSuffix(key, "_requests_today"):
			recordRequests1d(strings.TrimSuffix(strings.TrimPrefix(key, "model_"), "_requests_today"), *metric.Used, key)
		case strings.HasPrefix(key, "model_") && strings.HasSuffix(key, "_requests"):
			recordRequests(strings.TrimSuffix(strings.TrimPrefix(key, "model_"), "_requests"), *metric.Used, key)
		default:
			rawModel, kind, ok := parseModelMetricKey(key)
			if !ok {
				continue
			}
			switch kind {
			case modelMetricInput:
				recordInput(rawModel, *metric.Used, key)
			case modelMetricOutput:
				recordOutput(rawModel, *metric.Used, key)
			case modelMetricCostUSD:
				recordCost(rawModel, *metric.Used, key)
			}
		}
	}

	for key, points := range s.DailySeries {
		if !strings.HasPrefix(key, "usage_model_") || len(points) == 0 {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(key, "usage_model_"))
		if name == "" {
			continue
		}
		entry := ensure(name)
		entry.series = points
		if entry.requests <= 0 {
			entry.requests = sumBreakdownSeries(points)
		}
	}

	out := make([]ModelBreakdownEntry, 0, len(byModel))
	for name, entry := range byModel {
		if entry.cost <= 0 && entry.input <= 0 && entry.output <= 0 && entry.requests <= 0 && len(entry.series) == 0 {
			continue
		}
		out = append(out, ModelBreakdownEntry{
			Name:       name,
			Cost:       entry.cost,
			Input:      entry.input,
			Output:     entry.output,
			Requests:   entry.requests,
			Requests1d: entry.requests1d,
			Series:     entry.series,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		ti := out[i].Input + out[i].Output
		tj := out[j].Input + out[j].Output
		if ti != tj {
			return ti > tj
		}
		if out[i].Cost != out[j].Cost {
			return out[i].Cost > out[j].Cost
		}
		if out[i].Requests != out[j].Requests {
			return out[i].Requests > out[j].Requests
		}
		return out[i].Name < out[j].Name
	})
	return out, usedKeys
}

func ExtractProviderBreakdown(s UsageSnapshot) ([]ProviderBreakdownEntry, map[string]bool) {
	type agg struct {
		cost     float64
		input    float64
		output   float64
		requests float64
	}
	type fieldState struct {
		cost     bool
		input    bool
		output   bool
		requests bool
	}
	byProvider := make(map[string]*agg)
	usedKeys := make(map[string]bool)
	fieldsByProvider := make(map[string]*fieldState)

	ensure := func(name string) *agg {
		if _, ok := byProvider[name]; !ok {
			byProvider[name] = &agg{}
		}
		return byProvider[name]
	}
	ensureFields := func(name string) *fieldState {
		if _, ok := fieldsByProvider[name]; !ok {
			fieldsByProvider[name] = &fieldState{}
		}
		return fieldsByProvider[name]
	}
	recordCost := func(name string, value float64, key string) {
		ensure(name).cost += value
		ensureFields(name).cost = true
		usedKeys[key] = true
	}
	recordInput := func(name string, value float64, key string) {
		ensure(name).input += value
		ensureFields(name).input = true
		usedKeys[key] = true
	}
	recordOutput := func(name string, value float64, key string) {
		ensure(name).output += value
		ensureFields(name).output = true
		usedKeys[key] = true
	}
	recordRequests := func(name string, value float64, key string) {
		ensure(name).requests += value
		ensureFields(name).requests = true
		usedKeys[key] = true
	}

	for key, metric := range s.Metrics {
		if metric.Used == nil || !strings.HasPrefix(key, "provider_") {
			continue
		}
		switch {
		case strings.HasSuffix(key, "_cost_usd"):
			recordCost(strings.TrimSuffix(strings.TrimPrefix(key, "provider_"), "_cost_usd"), *metric.Used, key)
		case strings.HasSuffix(key, "_cost") && !strings.HasSuffix(key, "_byok_cost"):
			recordCost(strings.TrimSuffix(strings.TrimPrefix(key, "provider_"), "_cost"), *metric.Used, key)
		case strings.HasSuffix(key, "_input_tokens"):
			recordInput(strings.TrimSuffix(strings.TrimPrefix(key, "provider_"), "_input_tokens"), *metric.Used, key)
		case strings.HasSuffix(key, "_output_tokens"):
			recordOutput(strings.TrimSuffix(strings.TrimPrefix(key, "provider_"), "_output_tokens"), *metric.Used, key)
		case strings.HasSuffix(key, "_requests"):
			recordRequests(strings.TrimSuffix(strings.TrimPrefix(key, "provider_"), "_requests"), *metric.Used, key)
		}
	}
	for key, metric := range s.Metrics {
		if metric.Used == nil || !strings.HasPrefix(key, "provider_") || !strings.HasSuffix(key, "_byok_cost") {
			continue
		}
		base := strings.TrimSuffix(strings.TrimPrefix(key, "provider_"), "_byok_cost")
		if base == "" || ensureFields(base).cost {
			continue
		}
		recordCost(base, *metric.Used, key)
	}

	meta := snapshotBreakdownMetaEntries(s)
	for key, raw := range meta {
		if usedKeys[key] || !strings.HasPrefix(key, "provider_") {
			continue
		}
		switch {
		case strings.HasSuffix(key, "_cost") && !strings.HasSuffix(key, "_byok_cost"):
			value, ok := parseBreakdownNumeric(raw)
			if !ok {
				continue
			}
			base := strings.TrimSuffix(strings.TrimPrefix(key, "provider_"), "_cost")
			if base == "" || ensureFields(base).cost {
				continue
			}
			recordCost(base, value, key)
		case strings.HasSuffix(key, "_input_tokens"), strings.HasSuffix(key, "_prompt_tokens"):
			value, ok := parseBreakdownNumeric(raw)
			if !ok {
				continue
			}
			base := strings.TrimSuffix(strings.TrimPrefix(key, "provider_"), "_input_tokens")
			base = strings.TrimSuffix(base, "_prompt_tokens")
			if base == "" || ensureFields(base).input {
				continue
			}
			recordInput(base, value, key)
		case strings.HasSuffix(key, "_output_tokens"), strings.HasSuffix(key, "_completion_tokens"):
			value, ok := parseBreakdownNumeric(raw)
			if !ok {
				continue
			}
			base := strings.TrimSuffix(strings.TrimPrefix(key, "provider_"), "_output_tokens")
			base = strings.TrimSuffix(base, "_completion_tokens")
			if base == "" || ensureFields(base).output {
				continue
			}
			recordOutput(base, value, key)
		case strings.HasSuffix(key, "_requests"):
			value, ok := parseBreakdownNumeric(raw)
			if !ok {
				continue
			}
			base := strings.TrimSuffix(strings.TrimPrefix(key, "provider_"), "_requests")
			if base == "" || ensureFields(base).requests {
				continue
			}
			recordRequests(base, value, key)
		}
	}
	for key, raw := range meta {
		if usedKeys[key] || !strings.HasPrefix(key, "provider_") || !strings.HasSuffix(key, "_byok_cost") {
			continue
		}
		value, ok := parseBreakdownNumeric(raw)
		if !ok {
			continue
		}
		base := strings.TrimSuffix(strings.TrimPrefix(key, "provider_"), "_byok_cost")
		if base == "" || ensureFields(base).cost {
			continue
		}
		recordCost(base, value, key)
	}

	out := make([]ProviderBreakdownEntry, 0, len(byProvider))
	for name, entry := range byProvider {
		if entry.cost <= 0 && entry.input <= 0 && entry.output <= 0 && entry.requests <= 0 {
			continue
		}
		out = append(out, ProviderBreakdownEntry{
			Name:     name,
			Cost:     entry.cost,
			Input:    entry.input,
			Output:   entry.output,
			Requests: entry.requests,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		ti := out[i].Input + out[i].Output
		tj := out[j].Input + out[j].Output
		if ti != tj {
			return ti > tj
		}
		if out[i].Cost != out[j].Cost {
			return out[i].Cost > out[j].Cost
		}
		if out[i].Requests != out[j].Requests {
			return out[i].Requests > out[j].Requests
		}
		return out[i].Name < out[j].Name
	})
	return out, usedKeys
}

func ExtractUpstreamProviderBreakdown(s UsageSnapshot) ([]ProviderBreakdownEntry, map[string]bool) {
	type agg struct {
		cost     float64
		input    float64
		output   float64
		requests float64
	}
	byProvider := make(map[string]*agg)
	usedKeys := make(map[string]bool)

	ensure := func(name string) *agg {
		if _, ok := byProvider[name]; !ok {
			byProvider[name] = &agg{}
		}
		return byProvider[name]
	}

	for key, metric := range s.Metrics {
		if metric.Used == nil || !strings.HasPrefix(key, "upstream_") {
			continue
		}
		switch {
		case strings.HasSuffix(key, "_cost_usd"):
			ensure(strings.TrimSuffix(strings.TrimPrefix(key, "upstream_"), "_cost_usd")).cost += *metric.Used
			usedKeys[key] = true
		case strings.HasSuffix(key, "_input_tokens"):
			ensure(strings.TrimSuffix(strings.TrimPrefix(key, "upstream_"), "_input_tokens")).input += *metric.Used
			usedKeys[key] = true
		case strings.HasSuffix(key, "_output_tokens"):
			ensure(strings.TrimSuffix(strings.TrimPrefix(key, "upstream_"), "_output_tokens")).output += *metric.Used
			usedKeys[key] = true
		case strings.HasSuffix(key, "_requests"):
			ensure(strings.TrimSuffix(strings.TrimPrefix(key, "upstream_"), "_requests")).requests += *metric.Used
			usedKeys[key] = true
		}
	}

	out := make([]ProviderBreakdownEntry, 0, len(byProvider))
	for name, entry := range byProvider {
		out = append(out, ProviderBreakdownEntry{
			Name:     name,
			Cost:     entry.cost,
			Input:    entry.input,
			Output:   entry.output,
			Requests: entry.requests,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		ti := out[i].Input + out[i].Output
		tj := out[j].Input + out[j].Output
		if ti != tj {
			return ti > tj
		}
		if out[i].Requests != out[j].Requests {
			return out[i].Requests > out[j].Requests
		}
		return out[i].Name < out[j].Name
	})
	if len(out) == 0 {
		return nil, nil
	}
	return out, usedKeys
}

func ExtractClientBreakdown(s UsageSnapshot) ([]ClientBreakdownEntry, map[string]bool) {
	byClient := make(map[string]*ClientBreakdownEntry)
	usedKeys := make(map[string]bool)
	tokenSeriesByClient := make(map[string]map[string]float64)
	usageClientSeriesByClient := make(map[string]map[string]float64)
	usageSourceSeriesByClient := make(map[string]map[string]float64)
	hasAllTimeRequests := make(map[string]bool)
	requestsTodayFallback := make(map[string]float64)
	hasAnyClientMetrics := false

	ensure := func(name string) *ClientBreakdownEntry {
		if _, ok := byClient[name]; !ok {
			byClient[name] = &ClientBreakdownEntry{Name: name}
		}
		return byClient[name]
	}

	for key, metric := range s.Metrics {
		if metric.Used == nil {
			continue
		}
		if strings.HasPrefix(key, "client_") {
			name, field, ok := parseClientMetricKey(key)
			if !ok {
				continue
			}
			name = canonicalizeClientBucket(name)
			hasAnyClientMetrics = true
			client := ensure(name)
			switch field {
			case "total_tokens":
				client.Total = *metric.Used
			case "input_tokens":
				client.Input = *metric.Used
			case "output_tokens":
				client.Output = *metric.Used
			case "cached_tokens":
				client.Cached = *metric.Used
			case "reasoning_tokens":
				client.Reasoning = *metric.Used
			case "requests":
				client.Requests = *metric.Used
				hasAllTimeRequests[name] = true
			case "sessions":
				client.Sessions = *metric.Used
			}
			usedKeys[key] = true
			continue
		}
		if strings.HasPrefix(key, "source_") {
			sourceName, field, ok := parseSourceMetricKey(key)
			if !ok {
				continue
			}
			clientName := canonicalizeClientBucket(sourceName)
			client := ensure(clientName)
			switch field {
			case "requests":
				client.Requests += *metric.Used
				hasAllTimeRequests[clientName] = true
			case "requests_today":
				requestsTodayFallback[clientName] += *metric.Used
			}
			usedKeys[key] = true
		}
	}

	for clientName, value := range requestsTodayFallback {
		if hasAllTimeRequests[clientName] {
			continue
		}
		client := ensure(clientName)
		if client.Requests <= 0 {
			client.Requests = value
		}
	}

	hasAnyClientSeries := false
	for key := range s.DailySeries {
		if strings.HasPrefix(key, "tokens_client_") || strings.HasPrefix(key, "usage_client_") {
			hasAnyClientSeries = true
			break
		}
	}

	for key, points := range s.DailySeries {
		if len(points) == 0 {
			continue
		}
		switch {
		case strings.HasPrefix(key, "tokens_client_"):
			name := canonicalizeClientBucket(strings.TrimPrefix(key, "tokens_client_"))
			if name == "" {
				continue
			}
			mergeBreakdownSeriesByDay(tokenSeriesByClient, name, points)
		case strings.HasPrefix(key, "usage_client_"):
			name := canonicalizeClientBucket(strings.TrimPrefix(key, "usage_client_"))
			if name == "" {
				continue
			}
			mergeBreakdownSeriesByDay(usageClientSeriesByClient, name, points)
		case strings.HasPrefix(key, "usage_source_"):
			if hasAnyClientMetrics || hasAnyClientSeries {
				continue
			}
			name := canonicalizeClientBucket(strings.TrimPrefix(key, "usage_source_"))
			if name == "" {
				continue
			}
			mergeBreakdownSeriesByDay(usageSourceSeriesByClient, name, points)
		}
	}

	for name, pointsByDay := range tokenSeriesByClient {
		client := ensure(name)
		client.Series = breakdownSortedSeries(pointsByDay)
		client.SeriesKind = "tokens"
		if client.Total <= 0 {
			client.Total = sumBreakdownSeries(client.Series)
		}
	}
	for name, pointsByDay := range usageClientSeriesByClient {
		client := ensure(name)
		if client.SeriesKind == "tokens" {
			continue
		}
		client.Series = breakdownSortedSeries(pointsByDay)
		client.SeriesKind = "requests"
		if client.Requests <= 0 {
			client.Requests = sumBreakdownSeries(client.Series)
		}
	}
	for name, pointsByDay := range usageSourceSeriesByClient {
		client := ensure(name)
		if client.SeriesKind != "" {
			continue
		}
		client.Series = breakdownSortedSeries(pointsByDay)
		client.SeriesKind = "requests"
		if client.Requests <= 0 {
			client.Requests = sumBreakdownSeries(client.Series)
		}
	}

	out := make([]ClientBreakdownEntry, 0, len(byClient))
	for _, client := range byClient {
		if breakdownClientValue(*client) <= 0 && client.Sessions <= 0 && client.Requests <= 0 && len(client.Series) == 0 {
			continue
		}
		out = append(out, *client)
	}
	sort.Slice(out, func(i, j int) bool {
		vi := breakdownClientTokenValue(out[i])
		vj := breakdownClientTokenValue(out[j])
		if vi != vj {
			return vi > vj
		}
		if out[i].Requests != out[j].Requests {
			return out[i].Requests > out[j].Requests
		}
		if out[i].Sessions != out[j].Sessions {
			return out[i].Sessions > out[j].Sessions
		}
		return out[i].Name < out[j].Name
	})
	return out, usedKeys
}

func ExtractInterfaceClientBreakdown(s UsageSnapshot) ([]ClientBreakdownEntry, map[string]bool) {
	byName := make(map[string]*ClientBreakdownEntry)
	usedKeys := make(map[string]bool)
	usageSeriesByName := make(map[string]map[string]float64)

	ensure := func(name string) *ClientBreakdownEntry {
		if _, ok := byName[name]; !ok {
			byName[name] = &ClientBreakdownEntry{Name: name}
		}
		return byName[name]
	}

	for key, metric := range s.Metrics {
		if metric.Used == nil || !strings.HasPrefix(key, "interface_") {
			continue
		}
		name := canonicalizeClientBucket(strings.TrimPrefix(key, "interface_"))
		if name == "" {
			continue
		}
		ensure(name).Requests += *metric.Used
		usedKeys[key] = true
	}

	for key, points := range s.DailySeries {
		if len(points) == 0 {
			continue
		}
		switch {
		case strings.HasPrefix(key, "usage_client_"):
			name := canonicalizeClientBucket(strings.TrimPrefix(key, "usage_client_"))
			if name != "" {
				mergeBreakdownSeriesByDay(usageSeriesByName, name, points)
			}
		case strings.HasPrefix(key, "usage_source_"):
			name := canonicalizeClientBucket(strings.TrimPrefix(key, "usage_source_"))
			if name != "" {
				mergeBreakdownSeriesByDay(usageSeriesByName, name, points)
			}
		}
	}

	for name, pointsByDay := range usageSeriesByName {
		entry := ensure(name)
		entry.Series = breakdownSortedSeries(pointsByDay)
		entry.SeriesKind = "requests"
		if entry.Requests <= 0 {
			entry.Requests = sumBreakdownSeries(entry.Series)
		}
	}

	out := make([]ClientBreakdownEntry, 0, len(byName))
	for _, entry := range byName {
		if entry.Requests <= 0 && len(entry.Series) == 0 {
			continue
		}
		out = append(out, *entry)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Requests != out[j].Requests {
			return out[i].Requests > out[j].Requests
		}
		return out[i].Name < out[j].Name
	})
	if len(out) == 0 {
		return nil, nil
	}
	return out, usedKeys
}

var actualToolAggregateKeys = map[string]bool{
	"tool_calls_total":  true,
	"tool_completed":    true,
	"tool_errored":      true,
	"tool_cancelled":    true,
	"tool_success_rate": true,
}

func ExtractActualToolUsage(s UsageSnapshot) ([]ActualToolUsageEntry, map[string]bool) {
	byTool := make(map[string]float64)
	usedKeys := make(map[string]bool)

	for key, metric := range s.Metrics {
		if metric.Used == nil {
			continue
		}
		if !strings.HasPrefix(key, "tool_") {
			continue
		}
		if actualToolAggregateKeys[key] {
			usedKeys[key] = true
			continue
		}
		if strings.HasSuffix(key, "_today") || strings.HasSuffix(key, "_1d") || strings.HasSuffix(key, "_7d") || strings.HasSuffix(key, "_30d") {
			usedKeys[key] = true
			continue
		}
		name := strings.TrimPrefix(key, "tool_")
		if name == "" {
			continue
		}
		if IsMCPToolMetricName(name) {
			usedKeys[key] = true
			continue
		}
		byTool[name] += *metric.Used
		usedKeys[key] = true
	}

	if len(byTool) == 0 {
		return nil, usedKeys
	}

	out := make([]ActualToolUsageEntry, 0, len(byTool))
	for name, calls := range byTool {
		if calls <= 0 {
			continue
		}
		out = append(out, ActualToolUsageEntry{
			RawName: name,
			Calls:   calls,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Calls != out[j].Calls {
			return out[i].Calls > out[j].Calls
		}
		return out[i].RawName < out[j].RawName
	})
	return out, usedKeys
}

func IsMCPToolMetricName(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		return false
	}
	if strings.HasPrefix(normalized, "mcp_") {
		return true
	}
	if strings.Contains(normalized, "_mcp_server_") || strings.Contains(normalized, "-mcp-server-") {
		return true
	}
	return strings.HasSuffix(normalized, "_mcp")
}
