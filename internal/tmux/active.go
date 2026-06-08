package tmux

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/samber/lo"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers"
)

// LocalSourceProvider is an optional capability for providers whose primary
// data source is a local file/dir. The active-tool detection in this package
// probes for it via type assertion against providers.AllProviders(). Providers
// without local sources (openai, anthropic, openrouter direct-API, etc.) are
// skipped by the recency strategy and only matched via priority/pinned.
type LocalSourceProvider interface {
	core.UsageProvider
	LocalSourcePaths() []string
}

// processLister is the indirection used by the "process" strategy so tests can
// substitute a deterministic command list. The default implementation calls
// `ps` on Unix and returns empty on Windows.
type processLister func() ([]string, error)

// DefaultPriorityOrder is the fallback priority list used when settings.tmux
// does not specify one. It mirrors the design doc: Claude Code first since it
// is the most heavily-instrumented provider, followed by the rest of the
// per-tool local-file providers.
var DefaultPriorityOrder = []string{
	"claude_code",
	"cursor",
	"codex",
	"copilot",
	"gemini_cli",
	"opencode",
	"ollama",
}

// processNameMap maps lowercased process names to provider IDs. The match is
// substring-based (e.g. "claude-cli" or "Claude.app" both map to claude_code)
// since AI tools ship under many naming conventions.
var processNameMap = map[string]string{
	"claude":   "claude_code",
	"cursor":   "cursor",
	"codex":    "codex",
	"copilot":  "copilot",
	"gh":       "copilot", // gh-copilot subcommand
	"gemini":   "gemini_cli",
	"ollama":   "ollama",
	"opencode": "opencode",
}

// DetectOptions configures Detect. All fields are optional; the zero value
// performs the default detection (`recency,priority` over the default priority
// order with a 4h recency window and the on-disk cache enabled).
type DetectOptions struct {
	// Strategy is a comma-separated list of strategy names. Empty means
	// the default "recency,priority".
	Strategy string
	// PriorityOrder overrides DefaultPriorityOrder.
	PriorityOrder []string
	// RecencyWindow caps how stale a local source can be while still
	// counting as "active". Zero means 4h.
	RecencyWindow time.Duration
	// Pinned, when set, short-circuits detection. It is the value behind
	// `--provider` or `settings.tmux.provider`.
	Pinned string
	// Now lets tests inject a clock.
	Now time.Time
	// NoCache disables the on-disk cache.
	NoCache bool
	// CacheTTL overrides how long a cached detection is reused. Zero means
	// defaultCacheTTL.
	CacheTTL time.Duration
	// CachePath overrides the default `~/.cache/openusage/tmux-active.json`.
	// Mainly for tests.
	CachePath string
	// Providers overrides the provider list. Empty means
	// providers.AllProviders().
	Providers []core.UsageProvider
	// ProcessLister overrides the default ps-backed lister. Tests set this
	// to assert process-strategy behavior deterministically.
	ProcessLister processLister
}

// DetectResult is the outcome of one Detect call. Ordered holds every
// provider the strategy matched, in the order they would surface (most-active
// first). Primary is the first entry, or empty when nothing matched. Source
// records which strategy produced the primary match for debug/doctor.
type DetectResult struct {
	Primary string
	Ordered []string
	Source  string
}

const (
	defaultRecencyWindow = 4 * time.Hour
	// defaultCacheTTL is how long a detected active tool is reused before a
	// fresh disk scan. It is deliberately longer than a typical tmux
	// status-interval (5s) so the segment does not re-detect — and visibly
	// flip between tools — on every render. Pinning a provider bypasses
	// detection entirely. Override via DetectOptions.CacheTTL.
	defaultCacheTTL = 15 * time.Second
)

// Detect runs the configured strategies in order and returns the first
// non-empty match. The strategies share a single provider snapshot to avoid
// re-scanning the disk per strategy.
func Detect(opts DetectOptions) DetectResult {
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	if opts.RecencyWindow <= 0 {
		opts.RecencyWindow = defaultRecencyWindow
	}
	if len(opts.PriorityOrder) == 0 {
		opts.PriorityOrder = DefaultPriorityOrder
	}
	strategies := parseStrategies(opts.Strategy)
	if len(strategies) == 0 {
		strategies = []string{"recency", "priority"}
	}

	// Pinned short-circuits everything else (covered by the explicit "pinned"
	// strategy too, but a non-empty Pinned is treated as the user's intent
	// regardless of strategy list to mirror --provider flag semantics).
	if pinned := strings.TrimSpace(opts.Pinned); pinned != "" {
		return DetectResult{Primary: pinned, Ordered: []string{pinned}, Source: "pinned"}
	}

	// Try the cache first when the strategy list reads from disk. The cache
	// is a performance optimization for back-to-back tmux render calls and
	// is bypassed when the user passed --no-cache.
	ttl := opts.CacheTTL
	if ttl <= 0 {
		ttl = defaultCacheTTL
	}
	cacheKey := detectCacheKey(strategies, opts)
	if !opts.NoCache {
		if cached, ok := readCache(opts.CachePath, opts.Now, ttl, cacheKey); ok {
			return cached
		}
	}

	provs := opts.Providers
	if provs == nil {
		provs = providers.AllProviders()
	}
	localByID := localSourceIndex(provs)

	for _, strat := range strategies {
		switch strat {
		case "pinned":
			// already handled above; here only as a no-op entry so users can
			// keep "pinned" in their strategy list without breaking anything.
			continue
		case "recency":
			if res := detectRecency(localByID, opts); res.Primary != "" {
				writeCache(opts.CachePath, res, opts.Now, cacheKey)
				return res
			}
		case "process":
			if res := detectProcess(opts); res.Primary != "" {
				writeCache(opts.CachePath, res, opts.Now, cacheKey)
				return res
			}
		case "priority":
			if res := detectPriority(localByID, opts); res.Primary != "" {
				writeCache(opts.CachePath, res, opts.Now, cacheKey)
				return res
			}
		case "multi":
			if res := detectMulti(localByID, opts); res.Primary != "" {
				writeCache(opts.CachePath, res, opts.Now, cacheKey)
				return res
			}
		}
	}
	return DetectResult{}
}

// parseStrategies splits the comma-separated list into lowercase names,
// ignoring blanks. Unknown strategy names are kept so the Detect loop can
// no-op them (lets users freely add experimental names without errors).
func parseStrategies(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return lo.FilterMap(strings.Split(s, ","), func(p string, _ int) (string, bool) {
		name := strings.ToLower(strings.TrimSpace(p))
		return name, name != ""
	})
}

// localSourceIndex builds a map of provider ID to its LocalSourceProvider
// implementation, skipping providers that do not expose local files.
func localSourceIndex(provs []core.UsageProvider) map[string]LocalSourceProvider {
	out := map[string]LocalSourceProvider{}
	for _, p := range provs {
		lp, ok := p.(LocalSourceProvider)
		if !ok {
			continue
		}
		out[p.ID()] = lp
	}
	return out
}

// detectRecency returns the provider whose local source has the most recent
// mtime within the configured window. Returns empty when no provider's source
// is fresh enough.
func detectRecency(local map[string]LocalSourceProvider, opts DetectOptions) DetectResult {
	type stamped struct {
		id     string
		latest time.Time
	}
	cutoff := opts.Now.Add(-opts.RecencyWindow)
	hits := make([]stamped, 0, len(local))
	for id, p := range local {
		latest := newestMtime(p.LocalSourcePaths())
		if latest.IsZero() || latest.Before(cutoff) {
			continue
		}
		hits = append(hits, stamped{id: id, latest: latest})
	}
	if len(hits) == 0 {
		return DetectResult{}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].latest.After(hits[j].latest) })
	ordered := make([]string, 0, len(hits))
	for _, h := range hits {
		ordered = append(ordered, h.id)
	}
	return DetectResult{Primary: hits[0].id, Ordered: ordered, Source: "recency"}
}

// detectProcess runs the configured process lister and returns the first
// provider whose mapped process name appears in the output.
func detectProcess(opts DetectOptions) DetectResult {
	lister := opts.ProcessLister
	if lister == nil {
		lister = defaultProcessLister
	}
	procs, err := lister()
	if err != nil || len(procs) == 0 {
		return DetectResult{}
	}
	seen := map[string]bool{}
	ordered := make([]string, 0, len(procs))
	for _, raw := range procs {
		name := strings.ToLower(strings.TrimSpace(filepath.Base(raw)))
		if name == "" {
			continue
		}
		for key, providerID := range processNameMap {
			if strings.Contains(name, key) && !seen[providerID] {
				ordered = append(ordered, providerID)
				seen[providerID] = true
				break
			}
		}
	}
	if len(ordered) == 0 {
		return DetectResult{}
	}
	return DetectResult{Primary: ordered[0], Ordered: ordered, Source: "process"}
}

// detectPriority returns the first provider in priority order whose local
// source exists (regardless of recency). It is the conservative fallback so
// the status bar never appears blank for users who have at least one tool
// configured.
func detectPriority(local map[string]LocalSourceProvider, opts DetectOptions) DetectResult {
	for _, id := range opts.PriorityOrder {
		p, ok := local[id]
		if !ok {
			continue
		}
		if !anyExists(p.LocalSourcePaths()) {
			continue
		}
		return DetectResult{Primary: id, Ordered: []string{id}, Source: "priority"}
	}
	return DetectResult{}
}

// detectMulti returns every provider with any local-source activity in the
// recency window. Order matches recency strategy; primary is the most recent.
// Used to power the `active_tools` segment for users who multi-task across
// tools.
func detectMulti(local map[string]LocalSourceProvider, opts DetectOptions) DetectResult {
	res := detectRecency(local, opts)
	if res.Primary == "" {
		return DetectResult{}
	}
	res.Source = "multi"
	return res
}

// newestMtime returns the most recent mtime across paths. Missing paths are
// skipped; a directory is descended one level so providers that track a
// directory of session files still produce a useful mtime.
func newestMtime(paths []string) time.Time {
	var latest time.Time
	for _, p := range paths {
		if strings.TrimSpace(p) == "" {
			continue
		}
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		if info.IsDir() {
			latest = newer(latest, dirNewestMtime(p))
			continue
		}
		latest = newer(latest, info.ModTime())
	}
	return latest
}

// dirNewestMtime walks one level of a directory and returns the most recent
// child mtime. Skips dotfiles. Bounded to a single level so large session
// directories do not slow down the once-per-tick render.
func dirNewestMtime(dir string) time.Time {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return time.Time{}
	}
	var latest time.Time
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		latest = newer(latest, info.ModTime())
	}
	return latest
}

func newer(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// anyExists returns true if any of the paths exists. Used by detectPriority
// to filter out providers that have never been installed locally.
func anyExists(paths []string) bool {
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// defaultProcessLister calls `ps` and returns each comm field. Implemented for
// macOS and Linux only: tmux itself is Unix-only so this is acceptable.
func defaultProcessLister() ([]string, error) {
	if runtime.GOOS == "windows" {
		return nil, nil
	}
	args := []string{"-Ao", "comm="}
	if runtime.GOOS == "linux" {
		args = []string{"-eo", "comm="}
	}
	out, err := exec.Command("ps", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("tmux: listing processes: %w", err)
	}
	lines := strings.Split(string(out), "\n")
	procs := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			procs = append(procs, l)
		}
	}
	return procs, nil
}

// --- cache ------------------------------------------------------------------

type cacheEntry struct {
	Primary    string    `json:"primary"`
	Ordered    []string  `json:"ordered"`
	Source     string    `json:"source"`
	Key        string    `json:"key"`
	DetectedAt time.Time `json:"detected_at"`
}

// detectCacheKey identifies the detection inputs that change the result, so a
// cached entry produced for one configuration is not reused for another. Without
// this, switching --strategy (or priority order / recency window) within the
// cache TTL would return the previous configuration's answer.
func detectCacheKey(strategies []string, opts DetectOptions) string {
	return strings.Join(strategies, ",") + "|" +
		opts.RecencyWindow.String() + "|" +
		strings.Join(opts.PriorityOrder, ",")
}

func defaultCachePath() string {
	home, _ := os.UserHomeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".cache", "openusage", "tmux-active.json")
}

func readCache(path string, now time.Time, ttl time.Duration, key string) (DetectResult, bool) {
	if path == "" {
		path = defaultCachePath()
	}
	if path == "" {
		return DetectResult{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return DetectResult{}, false
	}
	var entry cacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return DetectResult{}, false
	}
	if entry.Key != key {
		return DetectResult{}, false
	}
	if now.Sub(entry.DetectedAt) > ttl {
		return DetectResult{}, false
	}
	return DetectResult{
		Primary: entry.Primary,
		Ordered: entry.Ordered,
		Source:  entry.Source,
	}, entry.Primary != ""
}

func writeCache(path string, res DetectResult, now time.Time, key string) {
	if path == "" {
		path = defaultCachePath()
	}
	if path == "" || res.Primary == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	entry := cacheEntry{
		Primary:    res.Primary,
		Ordered:    res.Ordered,
		Source:     res.Source,
		Key:        key,
		DetectedAt: now,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o600)
}
