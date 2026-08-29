package webserve

import (
	"context"
	"fmt"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/daemon"
	"github.com/janekbaraniewski/openusage/internal/export"
	"github.com/janekbaraniewski/openusage/internal/tui"
)

func (c *collector) fetchSnapshots(ctx context.Context) ([]core.UsageSnapshot, string, error) {
	if c.demo {
		return demoSnapshots(c.now()), "demo", nil
	}

	cfg := configOrDefault(c.opts)
	tw := core.ParseTimeWindow(cfg.Data.TimeWindow)
	projector := tui.NewWebProjectorFromConfig(cfg)

	rt := daemon.NewViewRuntime(nil, daemon.ResolveSocketPath(), core.DebugEnabled())
	rt.SetTimeWindow(tw)
	frame := rt.ReadWithFallbackForWindow(ctx, tw)
	if len(frame.Snapshots) > 0 {
		return projector.OrderSnapshots(frame.Snapshots), "daemon", nil
	}

	snaps, resolved, err := export.Collect(ctx, c.source)
	if err != nil {
		return nil, "", fmt.Errorf("serve: collecting snapshots: %w", err)
	}
	return projector.OrderSnapshots(tui.SnapshotsToMap(snaps)), string(resolved), nil
}
