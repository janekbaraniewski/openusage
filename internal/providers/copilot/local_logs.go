package copilot

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

type logSummary struct {
	DefaultModel  string
	SessionTokens map[string]logTokenEntry
	SessionBurn   map[string]float64
}

func (p *Provider) readLogs(copilotDir string, snap *core.UsageSnapshot) logSummary {
	ls := logSummary{
		SessionTokens: make(map[string]logTokenEntry),
		SessionBurn:   make(map[string]float64),
	}
	sessionEntries := make(map[string][]logTokenEntry)
	logDir := filepath.Join(copilotDir, "logs")
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return ls
	}

	var allTokenEntries []logTokenEntry

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(logDir, entry.Name()))
		if err != nil {
			continue
		}

		var currentSessionID string
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)

			if strings.Contains(line, "Workspace initialized:") {
				if idx := strings.Index(line, "Workspace initialized:"); idx >= 0 {
					rest := strings.TrimSpace(line[idx+len("Workspace initialized:"):])
					if spIdx := strings.Index(rest, " "); spIdx > 0 {
						currentSessionID = rest[:spIdx]
					} else if rest != "" {
						currentSessionID = rest
					}
				}
			}

			if strings.Contains(line, "Using default model:") {
				if idx := strings.Index(line, "Using default model:"); idx >= 0 {
					m := strings.TrimSpace(line[idx+len("Using default model:"):])
					if m != "" {
						ls.DefaultModel = m
					}
				}
			}

			if strings.Contains(line, "CompactionProcessor: Utilization") {
				te := parseCompactionLine(line)
				if te.Total > 0 {
					allTokenEntries = append(allTokenEntries, te)
					if currentSessionID != "" {
						sessionEntries[currentSessionID] = append(sessionEntries[currentSessionID], te)
					}
				}
			}
		}
	}

	if ls.DefaultModel != "" {
		snap.Raw["default_model"] = ls.DefaultModel
	}

	for sessionID, entries := range sessionEntries {
		sortCompactionEntries(entries)
		last := entries[len(entries)-1]
		ls.SessionTokens[sessionID] = last

		burn := 0.0
		for idx, te := range entries {
			if idx == 0 {
				if te.Used > 0 {
					burn += float64(te.Used)
				}
				continue
			}
			delta := te.Used - entries[idx-1].Used
			if delta > 0 {
				burn += float64(delta)
			}
		}
		if burn > 0 {
			ls.SessionBurn[sessionID] = burn
		}
	}

	if last, ok := newestCompactionEntry(allTokenEntries); ok {
		snap.Raw["context_window_tokens"] = fmt.Sprintf("%d/%d", last.Used, last.Total)
		pct := float64(last.Used) / float64(last.Total) * 100
		snap.Raw["context_window_pct"] = fmt.Sprintf("%.1f%%", pct)
		used := float64(last.Used)
		limit := float64(last.Total)
		snap.Metrics["context_window"] = core.Metric{
			Limit:     &limit,
			Used:      &used,
			Remaining: core.Float64Ptr(limit - used),
			Unit:      "tokens",
			Window:    "session",
		}
	}

	return ls
}
