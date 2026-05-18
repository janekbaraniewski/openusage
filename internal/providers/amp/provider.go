package amp

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
)

// Provider implements core.UsageProvider for Amp. Amp is a local-file
// provider: it ships per-thread JSON snapshots and a credit ledger under
// the user's data directory. No network calls are made.
type Provider struct {
	providerbase.Base
}

// New returns a Provider configured with Amp's ProviderSpec.
func New() *Provider {
	return &Provider{
		Base: providerbase.New(core.ProviderSpec{
			ID: "amp",
			Info: core.ProviderInfo{
				Name:         "Amp",
				Capabilities: []string{"local_stats", "session_tracking", "model_tokens", "cost_estimation"},
				DocURL:       "https://ampcode.com/",
			},
			Auth: core.ProviderAuthSpec{
				Type: core.ProviderAuthTypeLocal,
			},
			Setup: core.ProviderSetupSpec{
				Quickstart: []string{
					"Install the Amp client and run at least one thread.",
					"OpenUsage reads thread JSON and the credit ledger from the per-user data directory; no API key is required.",
				},
			},
			Dashboard: dashboardWidget(),
		}),
	}
}

// DetailWidget renders the standard coding-tool detail layout. MCP usage is
// hidden because Amp does not surface MCP-tool counts in its local payloads.
func (p *Provider) DetailWidget() core.DetailWidget {
	return core.CodingToolDetailWidget(false)
}

// HasChanged is the ChangeDetector hook used by the daemon to skip cheap
// re-polls. We stat the threads directory and the ledger; if either has
// been touched after `since`, we re-fetch.
func (p *Provider) HasChanged(acct core.AccountConfig, since time.Time) (bool, error) {
	dataDir := resolveDataDir(ampDataDirOverride(acct))
	if dataDir == "" {
		return true, nil
	}
	threadsDir := resolveThreadsDir(acct, dataDir)
	ledgerPath := resolveLedgerPath(acct, dataDir)

	if mtime, ok := dirMTime(threadsDir); ok && mtime.After(since) {
		return true, nil
	}
	if mtime, ok := fileMTime(ledgerPath); ok && mtime.After(since) {
		return true, nil
	}
	return false, nil
}

// Fetch reads the Amp threads directory and ledger and produces a
// UsageSnapshot. The provider fails gracefully when Amp is not installed:
// it returns a snapshot with StatusUnknown and a human-readable message
// rather than an error, so the dashboard can render a "no data" tile.
func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
	snap := core.NewUsageSnapshot(p.ID(), acct.ID)
	snap.Status = core.StatusOK
	snap.DailySeries = make(map[string][]core.TimePoint)

	dataDir := resolveDataDir(ampDataDirOverride(acct))
	if dataDir == "" {
		snap.Status = core.StatusUnknown
		snap.Message = "Cannot determine Amp data directory"
		return snap, nil
	}
	snap.Raw["data_dir"] = dataDir

	threadsDir := resolveThreadsDir(acct, dataDir)
	ledgerPath := resolveLedgerPath(acct, dataDir)
	snap.Raw["threads_dir"] = threadsDir
	snap.Raw["ledger_path"] = ledgerPath

	if !dirExists(threadsDir) {
		snap.Status = core.StatusUnknown
		snap.Message = "No Amp threads directory found"
		return snap, nil
	}

	threadFiles, err := listThreadFiles(threadsDir)
	if err != nil {
		snap.Status = core.StatusError
		snap.Message = fmt.Sprintf("Amp: %v", err)
		return snap, nil
	}
	snap.Raw["thread_count"] = fmt.Sprintf("%d", len(threadFiles))

	if len(threadFiles) == 0 {
		snap.Status = core.StatusUnknown
		snap.Message = "No Amp thread files found"
		return snap, nil
	}

	ledger, skippedLines, err := loadLedgerRecords(ledgerPath)
	if err != nil {
		// A truly broken ledger should not block thread-only reporting.
		snap.SetDiagnostic("amp_ledger_error", err.Error())
	}
	if skippedLines > 0 {
		snap.SetDiagnostic("amp_ledger_skipped_lines", fmt.Sprintf("%d", skippedLines))
	}

	var allEvents []ampEvent
	var parseErrors int
	for _, path := range threadFiles {
		if err := ctx.Err(); err != nil {
			return snap, err
		}
		events, err := parseAmpThreadFile(path)
		if err != nil {
			parseErrors++
			continue
		}
		// Reconcile this thread's events against the global ledger map.
		// reconcileWithLedger does not duplicate ledger-only records — it
		// returns them once per call. We strip them here and re-add the
		// unmatched ones at the top level (after global dedup) to avoid
		// re-emitting the same ledger row N times across N thread files.
		reconciled := reconcileEventsOnly(events, ledger)
		allEvents = append(allEvents, reconciled...)
	}
	if parseErrors > 0 {
		snap.SetDiagnostic("amp_thread_parse_errors", fmt.Sprintf("%d", parseErrors))
	}

	// Append ledger-only entries (no matching thread message anywhere) so
	// they contribute to totals even when the thread JSON lags behind.
	matched := matchedMessageIDs(allEvents)
	for id, rec := range ledger {
		if _, ok := matched[id]; ok {
			continue
		}
		allEvents = append(allEvents, eventFromLedgerOnly(id, rec))
	}

	// Cross-file dedup: same message id observed in two files folds to a
	// single event with per-field max-merged tokens.
	deduped := dedupAndMerge(allEvents)
	sortEventsChronological(deduped)

	if len(deduped) == 0 {
		snap.Status = core.StatusUnknown
		snap.Message = "No Amp usage data found"
		return snap, nil
	}

	applyAggregates(&snap, deduped)
	snap.Message = "Amp local thread data"
	return snap, nil
}

// reconcileEventsOnly is a thin wrapper around reconcileWithLedger that
// drops the synthesised ledger-only events; the caller adds those exactly
// once at the end.
func reconcileEventsOnly(events []ampEvent, ledger map[string]ampLedgerRecord) []ampEvent {
	merged := reconcileWithLedger(events, ledger)
	out := merged[:0]
	for _, evt := range merged {
		if evt.Source == "ledger" {
			continue
		}
		out = append(out, evt)
	}
	return out
}

// matchedMessageIDs returns the set of message ids present in events.
// Used to identify which ledger rows have no matching thread message.
func matchedMessageIDs(events []ampEvent) map[string]struct{} {
	out := make(map[string]struct{}, len(events))
	for _, evt := range events {
		if evt.MessageID == "" {
			continue
		}
		out[evt.MessageID] = struct{}{}
	}
	return out
}

// applyAggregates walks the reconciled+deduped events and emits the snapshot's
// metrics, ModelUsage rows, and DailySeries time-series.
func applyAggregates(snap *core.UsageSnapshot, events []ampEvent) {
	now := time.Now()
	todayKey := now.Format("2006-01-02")

	var (
		totalCost      float64
		totalInput     int64
		totalOutput    int64
		totalCacheRead int64
		totalCacheWrt  int64
		todayCost      float64
		messagesToday  int64
		sessions       = make(map[string]struct{})

		dailyCost     = make(map[string]float64)
		dailyMessages = make(map[string]float64)

		perModel = make(map[string]*modelAccumulator)
	)

	for _, evt := range events {
		totalCost += evt.CreditCost
		totalInput += evt.Tokens.Input
		totalOutput += evt.Tokens.Output
		totalCacheRead += evt.Tokens.CacheRead
		totalCacheWrt += evt.Tokens.CacheWrite

		if evt.ThreadID != "" {
			sessions[evt.ThreadID] = struct{}{}
		}

		dateKey := ""
		if !evt.Timestamp.IsZero() {
			dateKey = evt.Timestamp.Format("2006-01-02")
			dailyCost[dateKey] += evt.CreditCost
			dailyMessages[dateKey] += 1
			if dateKey == todayKey {
				todayCost += evt.CreditCost
				messagesToday++
			}
		}

		modelKey := strings.TrimSpace(evt.Model)
		if modelKey == "" {
			modelKey = "unknown"
		}
		acc := perModel[modelKey]
		if acc == nil {
			acc = &modelAccumulator{RawModelID: modelKey}
			perModel[modelKey] = acc
		}
		acc.Input += evt.Tokens.Input
		acc.Output += evt.Tokens.Output
		acc.CacheRead += evt.Tokens.CacheRead
		acc.CacheWrite += evt.Tokens.CacheWrite
		acc.CostUSD += evt.CreditCost
		acc.Requests++
	}

	addMetric(snap, "total_cost", totalCost, "USD", "all-time")
	addMetric(snap, "today_cost", todayCost, "USD", "1d")
	addMetric(snap, "total_input_tokens", float64(totalInput), "tokens", "all-time")
	addMetric(snap, "total_output_tokens", float64(totalOutput), "tokens", "all-time")
	addMetric(snap, "total_cache_read_tokens", float64(totalCacheRead), "tokens", "all-time")
	addMetric(snap, "total_cache_write_tokens", float64(totalCacheWrt), "tokens", "all-time")
	addMetric(snap, "total_messages", float64(len(events)), "messages", "all-time")
	addMetric(snap, "messages_today", float64(messagesToday), "messages", "1d")
	addMetric(snap, "total_sessions", float64(len(sessions)), "sessions", "all-time")

	if len(dailyCost) > 0 {
		snap.DailySeries["cost"] = core.SortedTimePoints(dailyCost)
	}
	if len(dailyMessages) > 0 {
		snap.DailySeries["messages"] = core.SortedTimePoints(dailyMessages)
	}

	// Emit one ModelUsageRecord per model in deterministic order.
	modelKeys := make([]string, 0, len(perModel))
	for k := range perModel {
		modelKeys = append(modelKeys, k)
	}
	sort.Strings(modelKeys)
	for _, key := range modelKeys {
		acc := perModel[key]
		rec := core.ModelUsageRecord{
			RawModelID:   acc.RawModelID,
			RawSource:    "jsonl",
			Window:       "all-time",
			InputTokens:  core.Float64Ptr(float64(acc.Input)),
			OutputTokens: core.Float64Ptr(float64(acc.Output)),
			CachedTokens: core.Float64Ptr(float64(acc.CacheRead)),
			TotalTokens:  core.Float64Ptr(float64(acc.Input + acc.Output + acc.CacheRead + acc.CacheWrite)),
			CostUSD:      core.Float64Ptr(acc.CostUSD),
			Requests:     core.Float64Ptr(acc.Requests),
		}
		snap.AppendModelUsage(rec)
	}
}

type modelAccumulator struct {
	RawModelID string
	Input      int64
	Output     int64
	CacheRead  int64
	CacheWrite int64
	CostUSD    float64
	Requests   float64
}

func addMetric(snap *core.UsageSnapshot, key string, value float64, unit, window string) {
	v := value
	snap.Metrics[key] = core.Metric{Used: &v, Unit: unit, Window: window}
}

// ampDataDirOverride centralises the lookup of the user's chosen data dir.
// `AccountConfig.Binary` is repurposed as a data-dir override for local-file
// providers — claude_code does the same — but ProviderPaths and RuntimeHints
// take precedence so a more specific key still wins.
func ampDataDirOverride(acct core.AccountConfig) string {
	if v := strings.TrimSpace(acct.Path("data_dir", "")); v != "" {
		return v
	}
	if v := strings.TrimSpace(acct.Hint("data_dir", "")); v != "" {
		return v
	}
	if v := strings.TrimSpace(acct.Binary); v != "" {
		return v
	}
	return ""
}

func resolveThreadsDir(acct core.AccountConfig, dataDir string) string {
	if v := strings.TrimSpace(acct.Path("threads_dir", "")); v != "" {
		return v
	}
	if v := strings.TrimSpace(acct.Hint("threads_dir", "")); v != "" {
		return v
	}
	return filepath.Join(dataDir, "threads")
}

func resolveLedgerPath(acct core.AccountConfig, dataDir string) string {
	if v := strings.TrimSpace(acct.Path("ledger_path", "")); v != "" {
		return v
	}
	if v := strings.TrimSpace(acct.Hint("ledger_path", "")); v != "" {
		return v
	}
	return filepath.Join(dataDir, "ledger.jsonl")
}

// listThreadFiles returns a sorted slice of *.json files in dir. It silently
// returns nil when the directory is missing — callers above check existence
// upfront.
func listThreadFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("amp: reading threads dir: %w", err)
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".json") {
			continue
		}
		out = append(out, filepath.Join(dir, name))
	}
	sort.Strings(out)
	return out, nil
}

func dirExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func dirMTime(path string) (time.Time, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}, false
	}
	return info.ModTime(), true
}

func fileMTime(path string) (time.Time, bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return time.Time{}, false
	}
	return info.ModTime(), true
}
