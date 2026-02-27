package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers"
	"github.com/janekbaraniewski/openusage/internal/tui"
)

func main() {
	log.SetOutput(io.Discard)

	interval := 5 * time.Second
	accounts := buildDemoAccounts()
	demoProviders := buildDemoProviders(providers.AllProviders())

	providersByID := make(map[string]core.UsageProvider, len(demoProviders))
	for _, p := range demoProviders {
		providersByID[p.ID()] = p
	}

	model := tui.NewModel(
		0.20,
		0.05,
		false,
		config.DashboardConfig{},
		accounts,
		core.TimeWindow30d,
	)
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	refreshAll := func() {
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
			snaps[acct.ID] = snap
		}
		p.Send(tui.SnapshotsMsg(snaps))
	}

	go func() {
		refreshAll()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				refreshAll()
			}
		}
	}()

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		os.Exit(1)
	}
}
