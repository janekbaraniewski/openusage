package main

import (
	"context"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/daemon"
	"github.com/janekbaraniewski/openusage/internal/tui"
)

type snapshotDispatcher struct {
	program *tea.Program
	nextID  atomic.Uint64
}

func (d *snapshotDispatcher) bind(program *tea.Program) {
	d.program = program
}

func (d *snapshotDispatcher) dispatch(frame daemon.SnapshotFrame) {
	d.send(frame, d.nextID.Add(1))
}

func (d *snapshotDispatcher) refresh(ctx context.Context, rt *daemon.ViewRuntime, window core.TimeWindow) {
	requestID := d.nextID.Add(1)
	go func() {
		frame := rt.ReadWithFallbackForWindow(ctx, window)
		d.send(frame, requestID)
	}()
}

func (d *snapshotDispatcher) send(frame daemon.SnapshotFrame, requestID uint64) {
	if d == nil || d.program == nil || len(frame.Snapshots) == 0 {
		return
	}
	d.program.Send(tui.SnapshotsMsg{
		Snapshots:  frame.Snapshots,
		TimeWindow: frame.TimeWindow,
		RequestID:  requestID,
	})
}
