package main

import (
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

const (
	defaultCodexSessionsDir     = "~/.codex/sessions"
	defaultClaudeProjectsDir    = "~/.claude/projects"
	defaultClaudeProjectsAltDir = "~/.config/claude/projects"
	defaultOpenCodeDBPath       = "~/.local/share/opencode/opencode.db"
)

func defaultTelemetryOptionsForSource(sourceSystem string) shared.TelemetryCollectOptions {
	return telemetryOptionsForSource(
		sourceSystem,
		defaultCodexSessionsDir,
		defaultClaudeProjectsDir,
		defaultClaudeProjectsAltDir,
		nil,
		"",
		defaultOpenCodeDBPath,
	)
}

func cloneSnapshotsMap(in map[string]core.UsageSnapshot) map[string]core.UsageSnapshot {
	out := make(map[string]core.UsageSnapshot, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func stabilizeReadModelSnapshots(
	current map[string]core.UsageSnapshot,
	previous map[string]core.UsageSnapshot,
) map[string]core.UsageSnapshot {
	if len(current) == 0 || len(previous) == 0 {
		return current
	}
	out := cloneSnapshotsMap(current)
	for accountID, snap := range out {
		prev, ok := previous[accountID]
		if !ok {
			continue
		}
		if isDegradedReadModelSnapshot(snap) && !isDegradedReadModelSnapshot(prev) {
			out[accountID] = prev
		}
	}
	return out
}

func isDegradedReadModelSnapshot(snap core.UsageSnapshot) bool {
	return snap.Status == core.StatusUnknown &&
		len(snap.Metrics) == 0 &&
		len(snap.Resets) == 0 &&
		len(snap.DailySeries) == 0 &&
		len(snap.ModelUsage) == 0 &&
		strings.TrimSpace(snap.Message) == ""
}

func seedSnapshotsForAccounts(accounts []daemonReadModelAccount, message string) map[string]core.UsageSnapshot {
	out := make(map[string]core.UsageSnapshot, len(accounts))
	now := time.Now().UTC()
	for _, account := range accounts {
		accountID := strings.TrimSpace(account.AccountID)
		providerID := strings.TrimSpace(account.ProviderID)
		if accountID == "" || providerID == "" {
			continue
		}
		out[accountID] = core.UsageSnapshot{
			ProviderID:  providerID,
			AccountID:   accountID,
			Timestamp:   now,
			Status:      core.StatusUnknown,
			Message:     strings.TrimSpace(message),
			Metrics:     map[string]core.Metric{},
			Resets:      map[string]time.Time{},
			Attributes:  map[string]string{},
			Diagnostics: map[string]string{},
			Raw:         map[string]string{},
			DailySeries: map[string][]core.TimePoint{},
		}
	}
	return out
}
