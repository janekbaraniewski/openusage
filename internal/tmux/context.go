package tmux

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/ccevents"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/export"
	"github.com/janekbaraniewski/openusage/internal/providers/claude_code"
	"github.com/janekbaraniewski/openusage/internal/report"
)

// Context is the rendering input handed to the formatter. It bundles a
// resolved active snapshot, the active billing block (when available), and the
// pre-resolved theme color refs. It is constructed once per render and is
// otherwise pure data so the formatter remains side-effect-free.
type Context struct {
	// Provider is the resolved provider ID, e.g. "claude_code".
	Provider string
	// Account is the resolved account ID within Provider.
	Account string
	// Snapshot is the primary snapshot for Provider/Account (or zero value).
	Snapshot core.UsageSnapshot
	// AllSnapshots holds every snapshot returned by Collect, so the multi-tool
	// segment can iterate other active providers without re-fetching.
	AllSnapshots []core.UsageSnapshot
	// Block holds the currently-active billing block (Claude Code only).
	Block     report.Row
	HaveBlock bool
	// Synthetic holds derived values keyed by `_`-prefixed names from the
	// alias map (e.g. "_block_burn_rate"). Populated by BuildContext.
	Synthetic map[string]string
	// ThemeRefs is the resolved `$name` -> emit-mode-correct color string
	// table used by `#[fg=$name]` passthrough.
	ThemeRefs map[string]string
	// Theme is the raw palette, kept around for any caller that needs to
	// resolve a color outside of `#[...]`.
	Theme ThemeColors
	// Variables is the user-defined templates map from settings.tmux.variables.
	Variables map[string]string
	// Segments is the user-defined named-segments map. Keys take precedence
	// over the built-in segments table when both define the same name.
	Segments map[string]string
	// ColorRules carries threshold-coloring overrides keyed by variable name.
	ColorRules map[string]ColorRule
	// Now is the reference time for any time-relative formatting.
	Now time.Time
	// ColorMode and Glyphs are the resolved emission preferences. The
	// formatter passes them to color/glyph helpers.
	ColorMode ColorMode
	Glyphs    GlyphTier
}

// BuildOptions configures BuildContext. Source and Provider mirror the user
// flags; Now defaults to time.Now() when zero so tests can inject a clock.
type BuildOptions struct {
	Source     export.Source
	Provider   string
	Theme      ThemeColors
	ColorMode  ColorMode
	Glyphs     GlyphTier
	Variables  map[string]string
	Segments   map[string]string
	ColorRules map[string]ColorRule
	Now        time.Time
	// OfflineClaudePricing forces the embedded Claude Code pricing table.
	// Default true: tmux renders should be fast and offline-capable.
	OfflineClaudePricing bool
}

// BuildContext is the single I/O point during rendering: it talks to the
// export collector (daemon or direct) and, for Claude Code, parses local
// conversation logs to derive synthetic block/context fields. Every later
// formatter call is pure.
func BuildContext(ctx context.Context, opts BuildOptions) (Context, error) {
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	if opts.ColorMode == "" {
		opts.ColorMode = ColorModeTruecolor
	}
	if opts.Glyphs == "" {
		opts.Glyphs = GlyphTierUnicode
	}

	snaps, _, err := export.Collect(ctx, opts.Source)
	if err != nil {
		// Don't fail the render: emit an empty context so the caller can
		// degrade gracefully (e.g., render "?" placeholders).
		snaps = nil
	}

	c := Context{
		AllSnapshots: snaps,
		Synthetic:    map[string]string{},
		ThemeRefs:    ThemeRefs(opts.Theme, opts.ColorMode),
		Theme:        opts.Theme,
		Variables:    opts.Variables,
		Segments:     opts.Segments,
		ColorRules:   opts.ColorRules,
		Now:          opts.Now,
		ColorMode:    opts.ColorMode,
		Glyphs:       opts.Glyphs,
	}

	// Resolve the active snapshot. If the user pinned a provider, pick the
	// first snapshot matching that ID; otherwise pick the first non-empty
	// snapshot in the collected slice. Active-tool *detection* (recency,
	// process, etc.) is wired in phase 2; this is the pinned fallback.
	pinned := strings.ToLower(strings.TrimSpace(opts.Provider))
	if pinned != "" {
		for _, s := range snaps {
			if strings.EqualFold(s.ProviderID, pinned) {
				c.Snapshot = s
				c.Provider = s.ProviderID
				c.Account = s.AccountID
				break
			}
		}
	}
	if c.Provider == "" && len(snaps) > 0 {
		c.Snapshot = snaps[0]
		c.Provider = snaps[0].ProviderID
		c.Account = snaps[0].AccountID
	}

	// Derive Claude Code synthetics (block + context window) from the local
	// conversation log. Failures are non-fatal: the formatter falls back to
	// the snapshot Metrics map when the synthetic key is missing.
	if c.Provider == "claude_code" {
		populateClaudeCodeSynthetics(&c, opts)
	}

	return c, nil
}

// populateClaudeCodeSynthetics computes the active billing block (cost,
// remaining, burn rate, projection) and the most recent context-window
// percentage from local conversation logs. Errors are intentionally swallowed
// because the tmux render must never block tmux: if the log is missing or
// malformed we leave the synthetics empty and let downstream `{?cond:...}`
// suppress the affected segments.
func populateClaudeCodeSynthetics(c *Context, opts BuildOptions) {
	mode := claude_code.CostModeAuto
	if opts.OfflineClaudePricing {
		mode = claude_code.CostModeCalculate
	}
	events, err := ccevents.Conversations(mode, opts.OfflineClaudePricing)
	if err != nil || len(events) == 0 {
		return
	}
	rep := report.Build(events, report.Options{Kind: report.KindBlocks, Now: opts.Now})
	if active, ok := rep.ActiveBlock(); ok {
		c.Block = active
		c.HaveBlock = true
		c.Synthetic["_block_remaining"] = fmtDurationDefault(active.TimeRemaining)
		c.Synthetic["_block_burn_rate"] = fmtMoneyDefault(active.BurnRateUSDPerHour)
		c.Synthetic["_block_projection"] = fmtMoneyDefault(active.ProjectedCost)
	}

	// Context window: take the last event's session and sum tokens.
	last := events[len(events)-1]
	contextTok := last.Input + last.CacheRead + last.CacheCreate
	if contextTok > 0 {
		window := contextWindowFor(last.Model, contextTok)
		c.Synthetic["_context_tokens"] = fmt.Sprintf("%d", contextTok)
		if window > 0 {
			pct := float64(contextTok) / float64(window) * 100
			if pct > 100 {
				pct = 100
			}
			c.Synthetic["_context_pct"] = fmt.Sprintf("%.0f", pct)
		}
	}
}

// contextWindowFor returns a conservative context-window guess for a Claude
// model ID. If the observed token count already exceeds the guess, callers
// can scale up; we mirror the statusline's heuristic so the percentages match.
func contextWindowFor(model string, observed int) int {
	id := strings.ToLower(model)
	if strings.Contains(id, "1m") || observed > 200_000 {
		return 1_000_000
	}
	return 200_000
}

func fmtDurationDefault(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh%02dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

func fmtMoneyDefault(v float64) string {
	if v <= 0 {
		return ""
	}
	return fmt.Sprintf("$%.2f", v)
}
