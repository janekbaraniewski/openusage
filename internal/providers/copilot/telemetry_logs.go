package copilot

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

type logTokenDelta struct {
	Timestamp time.Time
	Used      int64
	Limit     int64
}

var compactionRe = regexp.MustCompile(
	`^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d+Z)\s+\[INFO\]\s+CompactionProcessor:\s+Utilization\s+[\d.]+%\s+\((\d+)/(\d+)\s+tokens\)`,
)

func parseCopilotLogTokenDeltas(logsDir string) []logTokenDelta {
	if logsDir == "" {
		return nil
	}
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		return nil
	}

	var observations []logTokenDelta
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}
		f, err := os.Open(filepath.Join(logsDir, entry.Name()))
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			m := compactionRe.FindStringSubmatch(scanner.Text())
			if m == nil {
				continue
			}
			ts, err := time.Parse(time.RFC3339Nano, m[1])
			if err != nil {
				continue
			}
			used, _ := strconv.ParseInt(m[2], 10, 64)
			limit, _ := strconv.ParseInt(m[3], 10, 64)
			observations = append(observations, logTokenDelta{Timestamp: ts, Used: used, Limit: limit})
		}
		_ = f.Close()
	}
	if len(observations) < 2 {
		return nil
	}

	sort.Slice(observations, func(i, j int) bool {
		return observations[i].Timestamp.Before(observations[j].Timestamp)
	})

	deltas := make([]logTokenDelta, 0, len(observations)-1)
	for i := 1; i < len(observations); i++ {
		diff := observations[i].Used - observations[i-1].Used
		if diff > 0 {
			deltas = append(deltas, logTokenDelta{
				Timestamp: observations[i].Timestamp,
				Used:      diff,
				Limit:     observations[i].Limit,
			})
		}
	}
	return deltas
}

func enrichSyntheticTokenEstimates(events []shared.TelemetryEvent, deltas []logTokenDelta) {
	if len(deltas) == 0 {
		return
	}
	for i := range events {
		ev := &events[i]
		if ev.EventType != shared.TelemetryEventTypeMessageUsage || ev.InputTokens != nil || len(ev.Payload) == 0 {
			continue
		}
		if syn, _ := ev.Payload["synthetic"].(bool); !syn {
			continue
		}
		var bestDelta *logTokenDelta
		bestGap := 30 * time.Second
		for j := range deltas {
			gap := ev.OccurredAt.Sub(deltas[j].Timestamp)
			if gap < 0 {
				gap = -gap
			}
			if gap < bestGap {
				bestGap = gap
				bestDelta = &deltas[j]
			}
		}
		if bestDelta != nil {
			ev.InputTokens = core.Int64Ptr(bestDelta.Used)
			ev.Payload["estimated_tokens"] = true
		}
	}
}

func defaultCopilotLogsPath() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, defaultCopilotLogsDir)
}
