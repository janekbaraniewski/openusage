package main

import (
	"context"
	"strings"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/daemon"
	"github.com/janekbaraniewski/openusage/internal/tui"
)

type snapshotDispatcher struct {
	program *tea.Program
	nextID  atomic.Uint64
	enrich  func(map[string]core.UsageSnapshot)
}

func (d *snapshotDispatcher) bind(program *tea.Program) {
	d.program = program
}

func (d *snapshotDispatcher) dispatch(frame daemon.SnapshotFrame) {
	d.send(frame, d.nextID.Add(1))
}

func (d *snapshotDispatcher) refresh(ctx context.Context, rt *daemon.ViewRuntime, req tui.RefreshRequest) uint64 {
	requestID := d.nextID.Add(1)
	go func() {
		frame := rt.ReadWithFallbackForWindow(ctx, req.TimeWindow)
		d.applyEnrich(frame.Snapshots, req.AccountID)
		d.send(frame, requestID)
	}()
	return requestID
}

func (d *snapshotDispatcher) applyEnrich(snaps map[string]core.UsageSnapshot, accountID string) {
	if d == nil || d.enrich == nil || len(snaps) == 0 {
		return
	}
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		d.enrich(snaps)
		return
	}
	subset := map[string]core.UsageSnapshot{}
	if snap, ok := snaps[accountID]; ok {
		subset[accountID] = snap
	}
	if len(subset) == 0 {
		return
	}
	d.enrich(subset)
	if snap, ok := subset[accountID]; ok {
		snaps[accountID] = snap
	}
}

func (d *snapshotDispatcher) send(frame daemon.SnapshotFrame, requestID uint64) {
	if d == nil || d.program == nil {
		return
	}
	if len(frame.Snapshots) == 0 {
		d.program.Send(tui.SnapshotsMsg{
			TimeWindow: frame.TimeWindow,
			RequestID:  requestID,
		})
		return
	}
	if d.enrich != nil {
		d.enrich(frame.Snapshots)
	}
	d.program.Send(tui.SnapshotsMsg{
		Snapshots:  frame.Snapshots,
		TimeWindow: frame.TimeWindow,
		RequestID:  requestID,
	})
}
