// Package muse_code implements a local-data provider that scans the Muse
// Code CLI's session logs (session.jsonl files under the muse data dir) and
// aggregates per-model token totals, priced through the shared pricing
// engine. No network calls are made.
//
// Quota meters live in quota.go: the Responses SSE probe (same event the
// TUI's /usage view renders) is primary, the dashboard GraphQL replay is
// fallback. Both are undocumented and degrade to diagnostics, never errors.
package muse_code

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

const ID = "muse_code"

const DefaultAccountID = "muse-code"

const allTimeWindow = "all-time"

type Provider struct {
	providerbase.Base
	clock core.Clock
}

func New() *Provider {
	return &Provider{
		Base: providerbase.New(core.ProviderSpec{
			ID: ID,
			Info: core.ProviderInfo{
				Name:         "Muse Code",
				Capabilities: []string{"local_stats", "session_tracking", "model_tokens"},
				DocURL:       "https://developer.meta.com/ai/models/muse-spark/",
			},
			Auth: core.ProviderAuthSpec{
				Type:             core.ProviderAuthTypeLocal,
				DefaultAccountID: DefaultAccountID,
			},
			Setup: core.ProviderSetupSpec{
				Quickstart: []string{
					"Install Muse Code, run `muse login`, and complete at least one session.",
					"openusage auto-detects the sessions dir and auth file; no configuration required.",
					"Quota meters (experimental): automatic via macOS keychain file ~/.config/openusage/muse.json or META_API_KEY; no browser required.",
					"Plan label: the quota probe returns an opaque tier ID, so set provider_paths.plan_name (Everyday Usage, High Usage, or Power Usage) to name the plan on the tile.",
				},
			},
			Dashboard: dashboardWidget(),
		}),
		clock: core.SystemClock{},
	}
}

func (p *Provider) DetailWidget() core.DetailWidget {
	return detailWidget()
}

func (p *Provider) now() time.Time {
	if p != nil && p.clock != nil {
		return p.clock.Now()
	}
	return time.Now()
}

// HasCredential reports whether any Muse Code credential signal exists on
// this machine: an exported META_API_KEY, an explicit MUSE_AUTH_PATH file, or
// the auth.json written by `muse login` / `muse auth set`. Secrets are never
// read for content, only presence.
func HasCredential(acct core.AccountConfig) bool {
	if strings.TrimSpace(os.Getenv("META_API_KEY")) != "" {
		return true
	}
	if fileExists(authFilePath(acct)) {
		return true
	}
	// Also consider the persisted quota key file that both apps share
	// (~/.config/openusage/muse.json, written by Swift/Go after first
	// keychain read). A file-only quota setup with no auth.json should still
	// be considered credentialed for HasChanged/HasChanged and detection.
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if fileExists(filepath.Join(home, ".config", "openusage", "muse.json")) {
			return true
		}
	}
	return false
}

// HasChanged reports whether Muse Code data may have moved since the given
// time. Quota comes from the remote probe and drifts without any local
// session write (other devices, background usage), so credentialed accounts
// always re-poll — same rationale as codex. Otherwise scan nested .jsonl
// files: appends to a session transcript never bump the sessions root
// mtime, so a top-level stat alone misses active sessions and the daemon
// would re-serve a stale snapshot indefinitely.
func (p *Provider) HasChanged(acct core.AccountConfig, since time.Time) (bool, error) {
	// Check credential first, before dirs, so a file-only quota setup with
	// no sessions dir (new machine, cleaned logs, or META_API_KEY/muse.json
	// only) still polls for remote quota. Previously this returned false for
	// missing dirs before checking credential, hiding quota on empty-log
	// machines (P2-5).
	if HasCredential(acct) {
		return true, nil
	}
	dirs := resolveSessionsDirs(acct)
	if len(dirs) == 0 {
		return false, nil
	}
	files, err := shared.CollectFilesWithStat(dirs, map[string]bool{".jsonl": true})
	if err != nil {
		return true, nil
	}
	for _, info := range files {
		if info != nil && info.ModTime().After(since) {
			return true, nil
		}
	}
	return shared.AnyPathModifiedAfter(dirs, since), nil
}

func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
	if strings.TrimSpace(acct.Provider) == "" {
		acct.Provider = p.ID()
	}

	snap := core.NewUsageSnapshot(p.ID(), acct.ID)
	snap.Timestamp = p.now()
	snap.DailySeries = make(map[string][]core.TimePoint)

	dirs := resolveSessionsDirs(acct)
	if len(dirs) == 0 && !HasCredential(acct) {
		snap.Status = core.StatusAuth
		snap.Message = "Muse Code not detected (run `muse login` and complete a session)"
		return snap, nil
	}
	if len(dirs) > 0 {
		snap.Raw["sessions_dirs"] = strings.Join(dirs, string(os.PathListSeparator))
	}

	entries, err := readAllSessions(ctx, dirs)
	if err != nil {
		// Return snap, nil (not err) like codex does: the daemon renders
		// the StatusError tile with the diagnostic instead of treating
		// the fetch as failed and discarding the snapshot.
		snap.SetDiagnostic("walk_error", err.Error())
		snap.Status = core.StatusError
		snap.Message = "Failed to read Muse sessions directory"
		return snap, nil
	}
	// Also read tool calls for Tool Usage, even when model entries are empty
	// the quota should still show. Tool data is best-effort, like quota.
	// We do this before the len==0 check so the empty-log branch also gets
	// tool data if any (e.g. failed sessions with tool calls but no model_completed).
	toolEntries, _ := readAllToolCalls(ctx, dirs)
	if len(entries) == 0 {
		snap.Status = core.StatusOK
		snap.Message = "No Muse sessions recorded"
		// Still enrich quota if we have a key — Weekly 0% left / 100% blocked
		// should show even with no local sessions (P2-5). A new machine or
		// cleaned log dir with META_API_KEY / muse.json should still display
		// remote quota, not just "No sessions".
		enrichQuota(ctx, acct, &snap)
		applyPlanNameOverride(acct, &snap)
		// If quota added metrics, surface them alongside the no-sessions note
		// rather than hiding the quota behind the early return.
		if len(snap.Metrics) > 0 {
			if summary := quotaSummary(&snap); summary != "quota n/a" {
				snap.Message = summary + " · " + snap.Message
			}
		}
		if isMuseQuotaLimited(&snap) {
			snap.Status = core.StatusLimited
		}
		// Even with no model entries, tool calls can exist (e.g. failed
		// sessions) — populate those so Tool Usage shows something.
		if len(toolEntries) > 0 {
			populateToolMetrics(&snap, toolEntries)
		}
		return snap, nil
	}

	populateSnapshot(ctx, &snap, entries, p.now())
	// Add tool usage, like codex does, so the tile shows Tool Usage
	// instead of "No tool data". Muse's 1,920 tool_call events in the
	// 2026-09-07 session are now counted.
	if len(toolEntries) > 0 {
		populateToolMetrics(&snap, toolEntries)
	}
	snap.Status = core.StatusOK
	snap.Message = buildStatusMessage(snap)
	// Optional quota enrichment: quota is independent of local logs, but
	// enrich after populating so local spend and quota can coexist.
	enrichQuota(ctx, acct, &snap)
	applyPlanNameOverride(acct, &snap)
	// If quota is at/over limit, mark the snapshot as StatusLimited so the
	// TUI header shows "30 Days LIMIT" in red (like codex 5h/7d 100% does),
	// not "30 Days OK" in green. Both codex and muse have hit their limits
	// in the screenshot, so both should show LIMIT when weekly is 100% blocked.
	if isMuseQuotaLimited(&snap) {
		snap.Status = core.StatusLimited
	}
	return snap, nil
}

func readAllSessions(ctx context.Context, dirs []string) ([]museModelEntry, error) {
	var all []museModelEntry
	seen := make(map[string]struct{})
	for _, dir := range dirs {
		walkErr := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if filepath.Ext(path) != ".jsonl" {
				return nil
			}
			if isSubagentTranscript(path) {
				return nil
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			canonical := canonicalPath(path)
			if _, dup := seen[canonical]; dup {
				return nil
			}
			seen[canonical] = struct{}{}

			entries, perFileErr := readMuseSessionFile(path)
			if perFileErr != nil {
				return nil
			}
			all = append(all, entries...)
			return nil
		})
		if walkErr != nil {
			return all, walkErr
		}
	}
	// Deduplicate by stable event identity across files. The same
	// model_completed record can appear in multiple session files
	// (copied logs, feedback-session) and would otherwise be double-
	// counted. This matches Swift's dedup (which is heuristic) but uses
	// the stable record ID + stream + sequence when available.
	seenEntries := make(map[string]struct{}, len(all))
	deduped := make([]museModelEntry, 0, len(all))
	for _, e := range all {
		key := e.RecordID
		if key != "" {
			key = e.RecordID + "|" + e.StreamID + "|" + fmt.Sprintf("%d", e.Sequence)
		} else {
			// Fallback heuristic for records without stable ID (should be rare)
			key = fmt.Sprintf("%d-%s-%d-%d-%d-%d", e.Timestamp.UnixMicro(), e.Model, e.Input, e.CacheRead, e.Output, e.Reasoning)
		}
		if _, dup := seenEntries[key]; dup {
			continue
		}
		seenEntries[key] = struct{}{}
		deduped = append(deduped, e)
	}
	return deduped, nil
}

func canonicalPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		if abs, err := filepath.Abs(resolved); err == nil {
			return abs
		}
		return resolved
	}
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

// isSubagentTranscript reports whether path is a nested subagent transcript.
// Subagent steps are already mirrored into the parent session log, so
// counting both would double-count every delegated step.
func isSubagentTranscript(path string) bool {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for _, part := range parts {
		if part == "subagent" {
			return true
		}
	}
	return false
}

func populateSnapshot(ctx context.Context, snap *core.UsageSnapshot, entries []museModelEntry, now time.Time) {
	type modelTotals struct {
		input      int64
		output     int64
		reasoning  int64
		cacheRead  int64
		cacheWrite int64
		requests   int64
		cost       float64
		priced     bool
	}

	perModel := make(map[string]*modelTotals)
	sessions := make(map[string]struct{})
	unpriced := make(map[string]struct{})

	var (
		totalInput      int64
		totalOutput     int64
		totalReasoning  int64
		totalCacheRead  int64
		totalCacheWrite int64
		totalTokens     int64
		totalCost       float64
		todayCost       float64
	)

	today := now.UTC().Format("2006-01-02")
	cutoff7d := now.UTC().AddDate(0, 0, -7)
	var sessionsToday int64
	recentSessions := make(map[string]struct{})
	tokensByDay := make(map[string]float64)
	costByDay := make(map[string]float64)
	sessionsByDay := make(map[string]float64)
	sessionsSeenPerDay := make(map[string]map[string]struct{})

	for _, e := range entries {
		bucket, ok := perModel[e.Model]
		if !ok {
			bucket = &modelTotals{}
			perModel[e.Model] = bucket
		}
		bucket.input += e.Input
		bucket.output += e.Output
		bucket.reasoning += e.Reasoning
		bucket.cacheRead += e.CacheRead
		bucket.cacheWrite += e.CacheWrite
		bucket.requests++

		totalInput += e.Input
		totalOutput += e.Output
		totalReasoning += e.Reasoning
		totalCacheRead += e.CacheRead
		totalCacheWrite += e.CacheWrite
		totalTokens += e.TotalTokens

		entryCost, entryPriced := estimateEntryCost(ctx, e)
		if entryPriced {
			bucket.cost += entryCost
			bucket.priced = true
			totalCost += entryCost
		} else if e.TotalTokens > 0 {
			unpriced[e.Model] = struct{}{}
		}

		if e.SessionID != "" {
			sessions[e.SessionID] = struct{}{}
		}

		if e.Timestamp.IsZero() {
			continue
		}
		day := e.Timestamp.UTC().Format("2006-01-02")
		tokensByDay[day] += float64(e.TotalTokens)
		if entryPriced {
			costByDay[day] += entryCost
			if day == today {
				todayCost += entryCost
			}
		}
		seen, ok := sessionsSeenPerDay[day]
		if !ok {
			seen = make(map[string]struct{})
			sessionsSeenPerDay[day] = seen
		}
		if e.SessionID != "" {
			if _, dup := seen[e.SessionID]; !dup {
				seen[e.SessionID] = struct{}{}
				sessionsByDay[day]++
				if day == today {
					sessionsToday++
				}
			}
			if !e.Timestamp.Before(cutoff7d) {
				recentSessions[e.SessionID] = struct{}{}
			}
		}
	}

	setUsedMetric(snap, "total_sessions", float64(len(sessions)), "sessions", allTimeWindow)
	setUsedMetric(snap, "sessions_today", float64(sessionsToday), "sessions", "today")
	setUsedMetric(snap, "sessions_7d", float64(len(recentSessions)), "sessions", "7d")
	setUsedMetric(snap, "total_tokens", float64(totalTokens), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_input_tokens", float64(totalInput), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_output_tokens", float64(totalOutput), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_cache_read", float64(totalCacheRead), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_cache_write", float64(totalCacheWrite), "tokens", allTimeWindow)
	// window_* for the selected time window (30d/all-time) — use the same total
	// definition as total_tokens so the headline 26.6M/19.1M vs 307.7M mismatch
	// is fixed (MUSE-PARITY-DIAGNOSIS.md:25). Previously window_tokens was
	// billable-only (input+output) while total included cache reads.
	setUsedMetric(snap, "window_tokens", float64(totalTokens), "tokens", allTimeWindow)
	setUsedMetric(snap, "window_input_tokens", float64(totalInput), "tokens", allTimeWindow)
	setUsedMetric(snap, "window_output_tokens", float64(totalOutput), "tokens", allTimeWindow)
	if totalCost > 0 {
		v := totalCost
		snap.Metrics["total_cost_usd"] = core.Metric{Used: &v, Unit: "USD", Window: allTimeWindow}
		snap.Metrics["window_cost"] = core.Metric{Used: &v, Unit: "USD", Window: allTimeWindow}
	}
	if todayCost > 0 {
		v := todayCost
		snap.Metrics["today_cost"] = core.Metric{Used: &v, Unit: "USD", Window: "today"}
	}
	if len(unpriced) > 0 {
		names := make([]string, 0, len(unpriced))
		for name := range unpriced {
			names = append(names, name)
		}
		snap.SetAttribute("unpriced_models", strings.Join(names, ", "))
	}

	if len(sessionsByDay) > 0 {
		snap.DailySeries["sessions"] = core.SortedTimePoints(sessionsByDay)
	}
	if len(tokensByDay) > 0 {
		snap.DailySeries["tokens"] = core.SortedTimePoints(tokensByDay)
	}
	if len(costByDay) > 0 {
		snap.DailySeries["cost"] = core.SortedTimePoints(costByDay)
	}

	for model, bucket := range perModel {
		rec := core.ModelUsageRecord{
			RawModelID:      model,
			RawSource:       "jsonl",
			Window:          allTimeWindow,
			InputTokens:     core.Float64Ptr(float64(bucket.input)),
			OutputTokens:    core.Float64Ptr(float64(bucket.output)),
			ReasoningTokens: core.Float64Ptr(float64(bucket.reasoning)),
			CachedTokens:    core.Float64Ptr(float64(bucket.cacheRead)),
			TotalTokens: core.Float64Ptr(float64(bucket.input + bucket.output +
				bucket.reasoning + bucket.cacheRead + bucket.cacheWrite)),
			Requests: core.Float64Ptr(float64(bucket.requests)),
		}
		if bucket.priced {
			rec.CostUSD = core.Float64Ptr(bucket.cost)
		}
		snap.AppendModelUsage(rec)
	}
}

func buildStatusMessage(snap core.UsageSnapshot) string {
	parts := make([]string, 0, 3)
	if m, ok := snap.Metrics["total_sessions"]; ok && m.Used != nil && *m.Used > 0 {
		parts = append(parts, formatCount(*m.Used, "session"))
	}
	if m, ok := snap.Metrics["total_tokens"]; ok && m.Used != nil && *m.Used > 0 {
		parts = append(parts, shared.FormatTokenCount(int(*m.Used))+" tokens")
	}
	if m, ok := snap.Metrics["total_cost_usd"]; ok && m.Used != nil && *m.Used > 0 {
		parts = append(parts, formatCostUSD(*m.Used))
	}
	if len(parts) == 0 {
		return "OK"
	}
	return strings.Join(parts, ", ")
}

func setUsedMetric(snap *core.UsageSnapshot, key string, value float64, unit, window string) {
	if value <= 0 {
		return
	}
	v := value
	snap.Metrics[key] = core.Metric{
		Used:   &v,
		Unit:   unit,
		Window: window,
	}
}

func formatCount(v float64, noun string) string {
	if v == 1 {
		return "1 " + noun
	}
	return shared.FormatTokenCount(int(v)) + " " + noun + "s"
}

func formatCostUSD(v float64) string {
	if v >= 1 {
		return fmt.Sprintf("$%.2f", v)
	}
	return fmt.Sprintf("$%.4f", v)
}

func isMuseQuotaLimited(snap *core.UsageSnapshot) bool {
	if snap == nil {
		return false
	}
	for _, key := range []string{"muse.weekly", "muse.session", "muse.quota"} {
		if m, ok := snap.Metrics[key]; ok && m.Used != nil && m.Limit != nil && *m.Limit > 0 && *m.Used >= *m.Limit {
			return true
		}
		if m, ok := snap.Metrics[key]; ok && m.Used != nil && *m.Used >= 100 {
			return true
		}
	}
	if _, ok := snap.Diagnostics["muse_quota_blocked"]; ok {
		return true
	}
	return false
}

func populateToolMetrics(snap *core.UsageSnapshot, toolEntries []museToolEntry) {
	if len(toolEntries) == 0 {
		return
	}
	counts := make(map[string]int, len(toolEntries))
	for _, e := range toolEntries {
		// Normalize already done in museToolEntryFromRecord (shell->exec)
		name := strings.ToLower(strings.TrimSpace(e.Name))
		if name == "" {
			continue
		}
		counts[name]++
	}
	total := float64(len(toolEntries))
	setUsedMetric(snap, "tool_calls_total", total, "calls", allTimeWindow)
	// Per-tool breakdown, like codex's tool_<name> metrics
	for name, cnt := range counts {
		key := "tool_" + shared.SanitizeMetricName(name)
		setUsedMetric(snap, key, float64(cnt), "calls", allTimeWindow)
	}
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
