package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers"
	"github.com/janekbaraniewski/openusage/internal/tui"
)

func main() {
	log.SetOutput(io.Discard)

	cfg, err := parseDemoConfig(os.Args[1:])
	if err != nil {
		if err == flag.ErrHelp {
			fmt.Fprintf(os.Stderr, "Usage: %s [-interval 5s] [-loop]\n", os.Args[0])
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "demo config error: %v\n", err)
		os.Exit(2)
	}

	interval := cfg.interval
	accounts := buildDemoAccounts()
	scenario := newDemoScenario(time.Now(), cfg)
	demoProviders := buildDemoProviders(providers.AllProviders(), scenario)

	providersByID := make(map[string]core.UsageProvider, len(demoProviders))
	for _, p := range demoProviders {
		providersByID[p.ID()] = p
	}

	// Track the selected time window so refreshes re-scope the demo data to it.
	// Starts at 30d to match the model's initial window.
	var currentWindow atomic.Value
	currentWindow.Store(core.TimeWindow30d)

	model := tui.NewModel(
		0.20,
		0.05,
		false,
		config.DashboardConfig{},
		accounts,
		core.TimeWindow30d,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var snapshotRequestID atomic.Uint64

	var p *tea.Program
	refreshAll := func() {
		window := currentWindow.Load().(core.TimeWindow)
		snaps := make(map[string]core.UsageSnapshot, len(accounts))
		for _, acct := range accounts {
			provider, ok := providersByID[acct.Provider]
			if !ok {
				continue
			}
			fetchCtx, fetchCancel := context.WithTimeout(ctx, 5*time.Second)
			snap, err := provider.Fetch(fetchCtx, acct)
			fetchCancel()
			if err != nil {
				snap = core.UsageSnapshot{
					ProviderID: acct.Provider,
					AccountID:  acct.ID,
					Timestamp:  time.Now(),
					Status:     core.StatusError,
					Message:    err.Error(),
				}
			}
			snaps[acct.ID] = scopeSnapshotToWindow(snap, window)
		}
		p.Send(tui.SnapshotsMsg{
			Snapshots:  snaps,
			TimeWindow: window,
			RequestID:  snapshotRequestID.Add(1),
		})
	}

	// Pressing `w` cycles the window: record it and re-scope the data (async so
	// the TUI update loop never blocks on Fetch), which clears the refresh
	// spinner and updates the windowed metrics.
	model.SetOnTimeWindowChange(func(w core.TimeWindow) { currentWindow.Store(w) })
	model.SetOnRefresh(func(w core.TimeWindow) {
		currentWindow.Store(w)
		go refreshAll()
	})

	p = tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithFPS(30))

	go func() {
		refreshAll()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				scenario.Advance()
				refreshAll()
			}
		}
	}()

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		os.Exit(1)
	}
}
