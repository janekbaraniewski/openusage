package main

import (
	"context"
	"fmt"
	"log"
	"net"
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
//
// Auth-token resolution happens here so rt.authToken is the single source of
// truth: explicit config wins, falling back to OPENUSAGE_HUB_TOKEN when config
// is empty. Without this, only the server saw the env var and the headless
// log line would falsely report auth=disabled.
func resolveHubRuntime(cfg config.Config) hubRuntime {
	addr := strings.TrimSpace(cfg.Hub.ListenAddr)
	if addr == "" {
		addr = ":9190"
	}
	stale := time.Duration(cfg.Hub.StaleTimeoutSeconds) * time.Second
	if stale <= 0 {
		stale = 300 * time.Second
	}
	authToken := strings.TrimSpace(cfg.Hub.AuthToken)
	if authToken == "" {
		authToken = strings.TrimSpace(os.Getenv(envHubToken))
	}
	store := hub.NewStore(stale)
	server := hub.NewServerWithAuth(addr, store, authToken)
	return hubRuntime{
		addr:      addr,
		staleFor:  stale,
		store:     store,
		server:    server,
		authToken: authToken,
	}
}

func newHubCommand() *cobra.Command {
	var listenAddr string
	var headless bool
	var allowPublic bool

	cmd := &cobra.Command{
		Use:   "hub",
		Short: "Run a hub that aggregates usage snapshots from multiple machines",
		Long: strings.Join([]string{
			"Start the OpenUsage hub server. Worker machines push snapshots here; the TUI shows an aggregated view.",
			"",
			"Security: by default the hub has NO authentication. Without an auth token, the hub refuses to bind to a",
			"non-loopback interface unless you pass --allow-public to explicitly opt in.",
			"To require a Bearer token, export OPENUSAGE_HUB_TOKEN.",
			"Do not expose the hub to untrusted networks without enabling auth.",
		}, "\n"),
		Example: strings.Join([]string{
			"  openusage hub                                        # TUI on 127.0.0.1:9190",
			"  openusage hub --listen :9190 --allow-public          # bind 0.0.0.0 without auth (trusted LAN only)",
			"  OPENUSAGE_HUB_TOKEN=s3cret openusage hub --headless  # bind 0.0.0.0 with Bearer auth",
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
				runHubHeadless(cfg, allowPublic)
			} else {
				runHub(cfg, allowPublic)
			}
		},
	}

	cmd.Flags().StringVar(&listenAddr, "listen", "", "TCP address to listen on (overrides hub.listen_addr in config)")
	cmd.Flags().BoolVar(&headless, "headless", false, "Run without TUI (HTTP server only; suitable for containers)")
	cmd.Flags().BoolVar(&allowPublic, "allow-public", false, "Allow binding to a non-loopback interface without auth_token (footgun-prevention; off by default)")
	return cmd
}

// validateHubExposure refuses to start the hub when it would bind to a
// non-loopback interface with no Bearer auth configured, unless the operator
// explicitly opts in with --allow-public. This catches the common footgun of
// `openusage hub --listen :9190` on a host reachable from the public internet
// without a token. Returns nil when the configuration is safe.
func validateHubExposure(addr, authToken string, allowPublic bool) error {
	if authToken != "" {
		return nil // explicit auth → safe regardless of bind
	}
	if allowPublic {
		return nil // explicit opt-in → operator accepts the risk
	}
	if isLoopbackAddr(addr) {
		return nil // bound to loopback → not externally reachable
	}
	return fmt.Errorf(
		"hub: refusing to listen on %q without auth_token.\n"+
			"  Choose one:\n"+
			"    1. export OPENUSAGE_HUB_TOKEN=<secret> to enable Bearer auth, OR\n"+
			"    2. bind to loopback only:  --listen 127.0.0.1:9190, OR\n"+
			"    3. pass --allow-public if you have a network-level firewall in place",
		addr,
	)
}

// isLoopbackAddr reports whether addr binds only to a loopback interface.
// Accepts ":port" (empty host = all interfaces, NOT loopback), "host:port",
// and bare "host". Hostnames other than "localhost" are treated conservatively
// as non-loopback because we can't resolve them deterministically at startup.
func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		// Could be a bare host or a malformed addr. ":port" form (e.g. ":9190")
		// trips SplitHostPort because it returns host="" → we treat that as
		// all-interfaces.
		if addr == "" || strings.HasPrefix(addr, ":") {
			return false
		}
		host = addr
	}
	if host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		// Unresolvable hostname — be conservative.
		return false
	}
	return ip.IsLoopback()
}

func runHub(cfg config.Config, allowPublic bool) {
	verbose := os.Getenv("OPENUSAGE_DEBUG") != ""

	if err := tui.LoadThemes(config.ConfigDir()); err != nil && verbose {
		log.Printf("theme load: %v", err)
	}
	tui.SetThemeByName(cfg.Theme)

	rt := resolveHubRuntime(cfg)
	if err := validateHubExposure(rt.addr, rt.authToken, allowPublic); err != nil {
		log.Fatalf("%v", err)
	}

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
			}
			// Re-check after ticker fires: select picks randomly when both
			// ctx.Done() and ticker.C are ready, so we could otherwise dispatch
			// to a program that has already been Quit().
			if ctx.Err() != nil {
				return
			}
			snaps := rt.store.Snapshots()
			if len(snaps) == 0 {
				continue
			}
			dispatcher.dispatch(daemon.SnapshotFrame{Snapshots: snaps, TimeWindow: timeWindow})
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

func runHubHeadless(cfg config.Config, allowPublic bool) {
	rt := resolveHubRuntime(cfg)
	if err := validateHubExposure(rt.addr, rt.authToken, allowPublic); err != nil {
		log.Fatalf("%v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	authLabel := "disabled"
	if rt.authToken != "" {
		authLabel = "bearer-token"
	}
	log.Printf("hub listening on %s (headless, auth=%s)", rt.addr, authLabel)
	if err := rt.server.ListenAndServe(ctx); err != nil && ctx.Err() == nil {
		log.Fatalf("hub server error: %v", err)
	}
}
