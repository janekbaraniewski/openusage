package tmux

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// segmentFunc renders one named segment against the renderer's context. The
// signature takes the active renderer (not just the Context) so segments can
// reuse the variable-resolution chain.
type segmentFunc func(r *renderer) string

// builtinSegments returns the registry of pre-baked segment names. It is a
// function (rather than a package-level var) because the segment bodies call
// renderer methods that reference back into the registry. Go's
// initialization-cycle check fires on var-to-method cycles even though the
// values would resolve fine at call time, so the indirection is required.
func builtinSegments() map[string]segmentFunc {
	return map[string]segmentFunc{
		"cost":         segCost,
		"block":        segBlock,
		"burn":         segBurn,
		"tool":         segTool,
		"model":        segModel,
		"tokens":       segTokens,
		"context":      segContext,
		"daily":        segDaily,
		"active_tools": segActiveTools,
	}
}

// SegmentNames returns the list of built-in segment names. Used by the
// `openusage tmux variables` command.
func SegmentNames() []string {
	segs := builtinSegments()
	out := make([]string, 0, len(segs))
	for k := range segs {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func segCost(r *renderer) string {
	v, ok := r.resolve("today_cost")
	if !ok || v == "" {
		return ""
	}
	return modShort(v)
}

func segBlock(r *renderer) string {
	cost, _ := r.resolve("block_cost")
	if cost == "" {
		return ""
	}
	remaining, _ := r.resolve("block_remaining")
	if remaining == "" {
		return modShort(cost) + " block"
	}
	return fmt.Sprintf("%s block (%s)", modShort(cost), remaining)
}

func segBurn(r *renderer) string {
	v, _ := r.resolve("burn_rate")
	if v == "" {
		return ""
	}
	return modMoney(v, "2") + "/hr"
}

func segTool(r *renderer) string {
	return r.ctx.Provider
}

func segModel(r *renderer) string {
	if v, ok := r.ctx.Snapshot.Attributes["model"]; ok && v != "" {
		return v
	}
	return ""
}

func segTokens(r *renderer) string {
	v, _ := r.resolve("context_tokens")
	if v == "" {
		return ""
	}
	return modTokens(v)
}

func segContext(r *renderer) string {
	pct, _ := r.resolve("context_pct")
	tok, _ := r.resolve("context_tokens")
	switch {
	case tok != "" && pct != "":
		return fmt.Sprintf("%s (%s%%)", modTokens(tok), pct)
	case tok != "":
		return modTokens(tok)
	case pct != "":
		return pct + "%"
	}
	return ""
}

func segDaily(r *renderer) string {
	v, _ := r.resolve("today_cost")
	if v == "" {
		return ""
	}
	return modShort(v) + " today"
}

// segActiveTools lists the active providers from AllSnapshots, joined by " | ".
// It expects the multi-tool detection (phase 2) to have ordered AllSnapshots
// by recency; until then the rendering still works against the raw collector
// order which is good enough as a placeholder.
func segActiveTools(r *renderer) string {
	if len(r.ctx.AllSnapshots) == 0 {
		return ""
	}
	parts := make([]string, 0, len(r.ctx.AllSnapshots))
	now := r.ctx.Now
	if now.IsZero() {
		now = time.Now()
	}
	for _, snap := range r.ctx.AllSnapshots {
		if snap.ProviderID == "" {
			continue
		}
		parts = append(parts, snap.ProviderID)
	}
	return strings.Join(parts, " | ")
}
