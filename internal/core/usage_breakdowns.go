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
