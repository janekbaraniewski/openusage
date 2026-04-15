package codex

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

func (p *Provider) readLatestSession(sessionsDir string, snap *core.UsageSnapshot) error {
	latestFile, err := findLatestSessionFile(sessionsDir)
	if err != nil {
		return fmt.Errorf("finding latest session: %w", err)
	}

	snap.Raw["latest_session_file"] = filepath.Base(latestFile)

	lastPayload, err := findLastTokenCount(latestFile)
	if err != nil {
		return fmt.Errorf("reading session: %w", err)
	}

	if lastPayload == nil {
		return fmt.Errorf("no token_count events in latest session")
	}

	if lastPayload.Info != nil {
		info := lastPayload.Info
		total := info.TotalTokenUsage

		inputTokens := float64(total.InputTokens)
		snap.Metrics["session_input_tokens"] = core.Metric{Used: &inputTokens, Unit: "tokens", Window: "session"}

		outputTokens := float64(total.OutputTokens)
		snap.Metrics["session_output_tokens"] = core.Metric{Used: &outputTokens, Unit: "tokens", Window: "session"}

		cachedTokens := float64(total.CachedInputTokens)
		snap.Metrics["session_cached_tokens"] = core.Metric{Used: &cachedTokens, Unit: "tokens", Window: "session"}

		if total.ReasoningOutputTokens > 0 {
			reasoning := float64(total.ReasoningOutputTokens)
			snap.Metrics["session_reasoning_tokens"] = core.Metric{Used: &reasoning, Unit: "tokens", Window: "session"}
		}

		totalTokens := float64(total.TotalTokens)
		snap.Metrics["session_total_tokens"] = core.Metric{Used: &totalTokens, Unit: "tokens", Window: "session"}

		if info.ModelContextWindow > 0 {
			ctxWindow := float64(info.ModelContextWindow)
			ctxUsed := float64(total.InputTokens)
			snap.Metrics["context_window"] = core.Metric{Limit: &ctxWindow, Used: &ctxUsed, Unit: "tokens"}
		}
	}

	if lastPayload.RateLimits != nil {
		rl := lastPayload.RateLimits
		rateLimitSet := false

		if rl.Primary != nil {
			limit := float64(100)
			used := rl.Primary.UsedPercent
			remaining := 100 - used
			windowStr := formatWindow(rl.Primary.WindowMinutes)
			snap.Metrics["rate_limit_primary"] = core.Metric{Limit: &limit, Used: &used, Remaining: &remaining, Unit: "%", Window: windowStr}

			if rl.Primary.ResetsAt > 0 {
				snap.Resets["rate_limit_primary"] = time.Unix(rl.Primary.ResetsAt, 0)
			}
			rateLimitSet = true
		}

		if rl.Secondary != nil {
			limit := float64(100)
			used := rl.Secondary.UsedPercent
			remaining := 100 - used
			windowStr := formatWindow(rl.Secondary.WindowMinutes)
			snap.Metrics["rate_limit_secondary"] = core.Metric{Limit: &limit, Used: &used, Remaining: &remaining, Unit: "%", Window: windowStr}

			if rl.Secondary.ResetsAt > 0 {
				snap.Resets["rate_limit_secondary"] = time.Unix(rl.Secondary.ResetsAt, 0)
			}
			rateLimitSet = true
		}

		if rl.Credits != nil {
			if rl.Credits.Unlimited {
				snap.Raw["credits"] = "unlimited"
			} else if rl.Credits.HasCredits {
				snap.Raw["credits"] = "available"
				if rl.Credits.Balance != nil {
					snap.Raw["credit_balance"] = fmt.Sprintf("$%.2f", *rl.Credits.Balance)
				}
			} else {
				snap.Raw["credits"] = "none"
			}
		}

		if rl.PlanType != nil {
			snap.Raw["plan_type"] = *rl.PlanType
		}
		if rateLimitSet && snap.Raw["rate_limit_source"] == "" {
			snap.Raw["rate_limit_source"] = "session"
		}
	}

	return nil
}

func findLatestSessionFile(sessionsDir string) (string, error) {
	fileInfos, err := shared.CollectFilesWithStat([]string{sessionsDir}, map[string]bool{".jsonl": true})
	if err != nil {
		return "", fmt.Errorf("collect codex latest session files: %w", err)
	}
	files := make([]string, 0, len(fileInfos))
	for path := range fileInfos {
		files = append(files, path)
	}

	if len(files) == 0 {
		return "", fmt.Errorf("no session files found in %s", sessionsDir)
	}

	sort.Slice(files, func(i, j int) bool {
		si := fileInfos[files[i]]
		sj := fileInfos[files[j]]
		if si == nil || sj == nil {
			return false
		}
		return si.ModTime().After(sj.ModTime())
	})

	return files[0], nil
}

func findLastTokenCount(path string) (*eventPayload, error) {
	var lastPayload *eventPayload
	if err := walkSessionFile(path, func(record sessionLine) error {
		if record.EventPayload == nil || record.EventPayload.Type != "token_count" {
			return nil
		}
		payload := *record.EventPayload
		lastPayload = &payload
		return nil
	}); err != nil {
		return nil, err
	}
	return lastPayload, nil
}

func (p *Provider) readDailySessionCounts(sessionsDir string, snap *core.UsageSnapshot) error {
	dayCounts := make(map[string]int)

	files, err := shared.CollectFilesByExt([]string{sessionsDir}, map[string]bool{".jsonl": true})
	if err != nil {
		return fmt.Errorf("collect codex daily session files: %w", err)
	}
	for _, path := range files {
		rel, relErr := filepath.Rel(sessionsDir, path)
		if relErr != nil {
			continue
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) >= 3 {
			dateStr := fmt.Sprintf("%s-%s-%s", parts[0], parts[1], parts[2])
			if _, parseErr := time.Parse("2006-01-02", dateStr); parseErr == nil {
				dayCounts[dateStr]++
			}
		}
	}

	if len(dayCounts) == 0 {
		return nil
	}

	dates := core.SortedStringKeys(dayCounts)

	for _, d := range dates {
		snap.DailySeries["sessions"] = append(snap.DailySeries["sessions"], core.TimePoint{
			Date:  d,
			Value: float64(dayCounts[d]),
		})
	}
	return nil
}

func formatWindow(minutes int) string {
	if minutes <= 0 {
		return ""
	}
	if minutes < 60 {
		return fmt.Sprintf("%dm", minutes)
	}
	hours := minutes / 60
	remaining := minutes % 60
	if remaining == 0 {
		if hours >= 24 {
			days := hours / 24
			leftover := hours % 24
			if leftover == 0 {
				return fmt.Sprintf("%dd", days)
			}
			return fmt.Sprintf("%dd%dh", days, leftover)
		}
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh%dm", hours, remaining)
}
