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
