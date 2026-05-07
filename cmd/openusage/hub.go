package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/daemon"
	"github.com/janekbaraniewski/openusage/internal/hub"
	"github.com/janekbaraniewski/openusage/internal/tui"
	"github.com/spf13/cobra"
)

func newHubCommand() *cobra.Command {
	var listenAddr string

	cmd := &cobra.Command{
		Use:   "hub",
		Short: "Run a hub that aggregates usage snapshots from multiple machines",
		Long:  "Start the OpenUsage hub server. Worker machines push snapshots here; the TUI shows an aggregated view.",
		Example: strings.Join([]string{
			"  openusage hub",
			"  openusage hub --listen :9190",
		}, "\n"),
		Run: func(_ *cobra.Command, _ []string) {
			cfg, err := config.Load()
			if err != nil {
				log.Printf("warning: config load failed, using defaults: %v", err)
				cfg = config.DefaultConfig()
			}
			if strings.TrimSpace(listenAddr) != "" {
				cfg.Hub.ListenAddr = strings.TrimSpace(listenAddr)
			}
			runHub(cfg)
		},
	}

	cmd.Flags().StringVar(&listenAddr, "listen", "", "TCP address to listen on (overrides hub.listen_addr in config)")
	return cmd
}

func runHub(cfg config.Config) {
	verbose := os.Getenv("OPENUSAGE_DEBUG") != ""

	if err := tui.LoadThemes(config.ConfigDir()); err != nil && verbose {
		log.Printf("theme load: %v", err)
	}
	tui.SetThemeByName(cfg.Theme)

	addr := cfg.Hub.ListenAddr
	if strings.TrimSpace(addr) == "" {
		addr = ":9190"
	}
	stale := time.Duration(cfg.Hub.StaleTimeoutSeconds) * time.Second
	if stale <= 0 {
		stale = 300 * time.Second
	}

	store := hub.NewStore(stale)
	server := hub.NewServer(addr, store)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := server.ListenAndServe(ctx); err != nil && ctx.Err() == nil {
			log.Printf("hub server error: %v", err)
		}
	}()

	timeWindow := core.ParseTimeWindow(cfg.Data.TimeWindow)

	model := tui.NewModel(
		cfg.UI.WarnThreshold,
		cfg.UI.CritThreshold,
		cfg.Experimental.Analytics,
		cfg.Dashboard,
		nil,
		timeWindow,
	)

	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithFPS(30))
	dispatcher := &snapshotDispatcher{}
	dispatcher.bind(program)

	go func() {
		// Tell the TUI the hub server is running — suppresses the "Connecting to
		// background helper" splash screen that shows when daemon.status=Connecting.
		program.Send(tui.DaemonStatusMsg{Status: tui.DaemonRunning})

		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				snaps := store.Snapshots()
				if len(snaps) == 0 {
					continue
				}
				dispatcher.dispatch(daemon.SnapshotFrame{Snapshots: snaps, TimeWindow: timeWindow})
			}
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
		program.Quit()
	}()

	if _, err := program.Run(); err != nil {
		log.SetOutput(os.Stderr)
		log.Fatalf("TUI error: %v", err)
	}
}
