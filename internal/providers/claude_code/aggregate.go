package claude_code

import (
	"path/filepath"
	"sort"
	"time"
)

// CostMode selects how a per-message cost is derived when aggregating
// conversation logs.
type CostMode string

const (
	// CostModeCalculate always recomputes cost from tokens at current model
	// rates. This is OpenUsage's historical behaviour and the default.
	CostModeCalculate CostMode = "calculate"
	// CostModeDisplay trusts the costUSD recorded in the JSONL entry and shows
	// 0 when it is absent.
	CostModeDisplay CostMode = "display"
	// CostModeAuto uses the recorded costUSD when present and otherwise
	// recomputes from tokens.
	CostModeAuto CostMode = "auto"
)

// ParseCostMode normalizes a user-supplied mode string. Unknown values fall
// back to CostModeCalculate.
func ParseCostMode(s string) CostMode {
	switch CostMode(s) {
	case CostModeDisplay:
		return CostModeDisplay
	case CostModeAuto:
		return CostModeAuto
	default:
		return CostModeCalculate
	}
}

// UsageStat is a single deduplicated assistant turn extracted from the Claude
// Code conversation logs, with its cost already resolved. It is the unit the
// headless report subcommands and the statusline aggregate over.
type UsageStat struct {
	Time        time.Time
	Model       string // raw model id as recorded in the JSONL
	Project     string // sanitized project/workspace label
	Session     string // session id
	SourcePath  string // originating JSONL file
	Input       int
	Output      int
	CacheRead   int
	CacheCreate int
	Reasoning   int
	Cost        float64
}

// TotalTokens returns the sum of every token bucket on the turn.
func (s UsageStat) TotalTokens() int {
	return s.Input + s.Output + s.CacheRead + s.CacheCreate + s.Reasoning
}

// AggregateOptions configures AggregateConversations.
type AggregateOptions struct {
	// ProjectsDir / AltProjectsDir override the conversation roots. When both
	// are empty the Claude Code defaults are used.
	ProjectsDir    string
	AltProjectsDir string
	// TranscriptPath, when set, restricts aggregation to a single JSONL file
	// (used by the statusline for the active session).
	TranscriptPath string
	// Mode selects the cost derivation strategy.
	Mode CostMode
	// Offline skips the network pricing lookup and uses the embedded
	// Anthropic family rates so the call returns instantly.
	Offline bool
	// ModifiedSince, when non-zero, skips JSONL files last modified before
	// it. Logs are append-only, so a file untouched since the cutoff holds no
	// event after it; callers that only need recent windows (statusline, tmux)
	// avoid re-parsing the whole multi-GB history on every render.
	ModifiedSince time.Time
	// KeepPath, when set, keeps a file regardless of ModifiedSince (e.g. the
	// active session's transcript and subagent logs, so session totals stay
	// exact).
	KeepPath func(path string) bool
}

// AggregateConversations parses the Claude Code conversation logs and returns
// one deduplicated UsageStat per assistant turn, sorted chronologically. It
// reuses the same parsing, streaming-merge and dedup logic as the live
// provider so costs stay consistent across the TUI, the daemon and the CLI.
func AggregateConversations(opts AggregateOptions) ([]UsageStat, error) {
	var paths []string
	if opts.TranscriptPath != "" {
		paths = []string{opts.TranscriptPath}
	} else {
		primary, alt := opts.ProjectsDir, opts.AltProjectsDir
		if primary == "" && alt == "" {
			primary, alt = DefaultTelemetryProjectsDirs()
		}
		fileInfos, err := collectJSONLFilesWithStatAcross(primary, alt)
		if err != nil {
			return nil, err
		}
		paths = make([]string, 0, len(fileInfos))
		for p, info := range fileInfos {
			if !opts.ModifiedSince.IsZero() && info.ModTime().Before(opts.ModifiedSince) &&
				(opts.KeepPath == nil || !opts.KeepPath(p)) {
				continue
			}
			paths = append(paths, p)
		}
		sort.Strings(paths)
	}

	var records []conversationRecord
	for _, p := range paths {
		records = append(records, parseConversationRecords(p)...)
	}
	sort.SliceStable(records, func(i, j int) bool {
		return records[i].timestamp.Before(records[j].timestamp)
	})

	seen := make(map[string]bool, len(records))
	out := make([]UsageStat, 0, len(records))
	for _, r := range records {
		if r.usage == nil {
			continue
		}
		// Claude Code records local/interrupted turns with the "<synthetic>"
		// model and zero usage. They are not real API calls, so skip them to
		// keep model breakdowns clean.
		if r.model == "<synthetic>" {
			continue
		}
		if key := conversationUsageDedupKey(r); key != "" {
			if seen[key] {
				continue
			}
			seen[key] = true
		}
		out = append(out, UsageStat{
			Time:        r.timestamp,
			Model:       r.model,
			Project:     conversationProjectLabel(r.cwd, r.sourcePath),
			Session:     r.sessionID,
			SourcePath:  r.sourcePath,
			Input:       r.usage.InputTokens,
			Output:      r.usage.OutputTokens,
			CacheRead:   r.usage.CacheReadInputTokens,
			CacheCreate: r.usage.CacheCreationInputTokens,
			Reasoning:   r.usage.ReasoningTokens,
			Cost:        costForRecord(r, opts.Mode, opts.Offline),
		})
	}
	return out, nil
}

// costForRecord resolves a turn's cost according to the selected mode.
func costForRecord(rec conversationRecord, mode CostMode, offline bool) float64 {
	switch mode {
	case CostModeDisplay:
		if rec.costUSD != nil {
			return *rec.costUSD
		}
		return 0
	case CostModeAuto:
		if rec.costUSD != nil {
			return *rec.costUSD
		}
	}
	if offline {
		return estimateCostOffline(rec.model, rec.usage)
	}
	return estimateCost(rec.model, rec.usage)
}

// estimateCostOffline computes a turn cost using only the embedded Anthropic
// family rates, with no network lookup. It is used for the statusline (which
// must respond instantly) and for --offline report runs.
func estimateCostOffline(model string, u *jsonlUsage) float64 {
	if u == nil {
		return 0
	}
	p := findPricing(model)
	cost := float64(u.InputTokens) * p.InputPerMillion / 1_000_000
	cost += float64(u.OutputTokens) * p.OutputPerMillion / 1_000_000
	cost += float64(u.CacheReadInputTokens) * p.CacheReadPerMillion / 1_000_000
	cost += float64(u.CacheCreationInputTokens) * p.CacheCreatePerMillion / 1_000_000
	return cost
}

// conversationProjectLabel derives a stable, sanitized project label from the
// recorded working directory, falling back to the JSONL file's parent dir.
func conversationProjectLabel(cwd, sourcePath string) string {
	if cwd != "" {
		base := filepath.Base(cwd)
		if base != "" && base != "." && base != string(filepath.Separator) {
			return sanitizeModelName(base)
		}
		return sanitizeModelName(cwd)
	}
	dir := filepath.Base(filepath.Dir(sourcePath))
	if dir == "" || dir == "." {
		return "unknown"
	}
	return sanitizeModelName(dir)
}
