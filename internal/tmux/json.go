package tmux

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/report"
)

// JSONOutput is the structured payload emitted by `openusage tmux --json`.
// It mirrors the rendering context so downstream callers (scripts, polybar,
// xbar, custom tmux wrappers) can re-render without re-running the
// formatter.
type JSONOutput struct {
	Provider  string             `json:"provider,omitempty"`
	Account   string             `json:"account,omitempty"`
	Rendered  string             `json:"rendered"`
	Snapshot  core.UsageSnapshot `json:"snapshot,omitempty"`
	Block     *report.Row        `json:"block,omitempty"`
	Synthetic map[string]string  `json:"synthetic,omitempty"`
	Detected  *DetectResult      `json:"detected,omitempty"`
	Now       time.Time          `json:"now"`
}

// MarshalJSON emits the structured payload to out. Errors wrap with the
// `tmux:` prefix so the caller can surface them consistently.
func WriteJSON(out io.Writer, payload JSONOutput) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		return fmt.Errorf("tmux: encoding json output: %w", err)
	}
	return nil
}

// BuildJSON assembles the JSONOutput from a Context and a rendered string.
// It is a small convenience so cmd/openusage/tmux.go does not have to know
// the shape of the payload.
func BuildJSON(ctx Context, rendered string, detected *DetectResult) JSONOutput {
	out := JSONOutput{
		Provider:  ctx.Provider,
		Account:   ctx.Account,
		Rendered:  rendered,
		Snapshot:  ctx.Snapshot,
		Synthetic: ctx.Synthetic,
		Now:       ctx.Now,
		Detected:  detected,
	}
	if ctx.HaveBlock {
		block := ctx.Block
		out.Block = &block
	}
	return out
}
