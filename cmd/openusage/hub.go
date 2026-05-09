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

// hubRuntime bundles the store + server + listen addr resolved for a hub run.
type hubRuntime struct {
	addr      string
	staleFor  time.Duration
	store     *hub.Store
	server    *hub.Server
	authToken string
}

// resolveHubRuntime normalises config values (applying defaults) and constructs
// the store+server pair. Called by both the interactive TUI and headless paths.
func resolveHubRuntime(cfg config.Config) hubRuntime {
	addr := strings.TrimSpace(cfg.Hub.ListenAddr)
	if addr == "" {
		addr = ":9190"
	}
	stale := time.Duration(cfg.Hub.StaleTimeoutSeconds) * time.Second
	if stale <= 0 {
		stale = 300 * time.Second
	}
	store := hub.NewStore(stale)
	server := hub.NewServerWithAuth(addr, store, cfg.Hub.AuthToken)
	return hubRuntime{
		addr:      addr,
		staleFor:  stale,
		store:     store,
		server:    server,
		authToken: cfg.Hub.AuthToken,
	}
}

func newHubCommand() *cobra.Command {
	var listenAddr string
	var headless bool

	cmd := &cobra.Command{
		Use:   "hub",
		Short: "Run a hub that aggregates usage snapshots from multiple machines",
		Long: strings.Join([]string{
			"Start the OpenUsage hub server. Worker machines push snapshots here; the TUI shows an aggregated view.",
			"",
			"Security: by default the hub has NO authentication and is intended for trusted LAN use.",
			"To require a Bearer token, set `hub.auth_token` in settings.json or export OPENUSAGE_HUB_TOKEN.",
			"Do not expose the hub to untrusted networks without enabling auth.",
		}, "\n"),
		Example: strings.Join([]string{
			"  openusage hub",
			"  openusage hub --listen :9190",
			"  openusage hub --headless",
			"  OPENUSAGE_HUB_TOKEN=s3cret openusage hub --headless",
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
			if headless {
				runHubHeadless(cfg)
			} else {
				runHub(cfg)
			}
		},
	}

	cmd.Flags().StringVar(&listenAddr, "listen", "", "TCP address to listen on (overrides hub.listen_addr in config)")
	cmd.Flags().BoolVar(&headless, "headless", false, "Run without TUI (HTTP server only; suitable for containers)")
	return cmd
}

func runHub(cfg config.Config) {
	verbose := os.Getenv("OPENUSAGE_DEBUG") != ""

	if err := tui.LoadThemes(config.ConfigDir()); err != nil && verbose {
		log.Printf("theme load: %v", err)
	}
	tui.SetThemeByName(cfg.Theme)

	rt := resolveHubRuntime(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := rt.server.ListenAndServe(ctx); err != nil && ctx.Err() == nil {
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
				snaps := rt.store.Snapshots()
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

func runHubHeadless(cfg config.Config) {
	rt := resolveHubRuntime(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	authLabel := "disabled"
	if rt.server.AuthEnabled() {
		authLabel = "bearer-token"
	}
	log.Printf("hub listening on %s (headless, auth=%s)", rt.addr, authLabel)
	if err := rt.server.ListenAndServe(ctx); err != nil && ctx.Err() == nil {
		log.Fatalf("hub server error: %v", err)
	}
}
