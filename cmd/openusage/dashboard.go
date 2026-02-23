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
	"github.com/janekbaraniewski/openusage/internal/tui"
)

func runDashboard(cfg config.Config) {
	tui.SetThemeByName(cfg.Theme)

	cachedAccounts := core.MergeAccounts(cfg.Accounts, cfg.AutoDetectedAccounts)
	interval := time.Duration(cfg.UI.RefreshIntervalSeconds) * time.Second

	model := tui.NewModel(
		cfg.UI.WarnThreshold,
		cfg.UI.CritThreshold,
		cfg.Experimental.Analytics,
		cfg.Dashboard,
		cachedAccounts,
	)

	socketPath := daemon.ResolveSocketPath()
	verbose := os.Getenv("OPENUSAGE_DEBUG") != ""

	viewRuntime := daemon.NewViewRuntime(
		nil,
		socketPath,
		verbose,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var program *tea.Program

	model.SetOnAddAccount(func(acct core.AccountConfig) {
		if strings.TrimSpace(acct.ID) == "" || strings.TrimSpace(acct.Provider) == "" {
			return
		}
	})

	model.SetOnRefresh(func() {
		go func() {
			snaps := viewRuntime.ReadWithFallback(ctx)
			if len(snaps) > 0 && program != nil {
				program.Send(tui.SnapshotsMsg(snaps))
			}
		}()
	})

	model.SetOnInstallDaemon(func() error {
		if err := daemon.InstallService(strings.TrimSpace(socketPath)); err != nil {
			return err
		}
		viewRuntime.ResetEnsureThrottle()
		return nil
	})

	program = tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())

	daemon.StartBroadcaster(
		ctx,
		viewRuntime,
		interval,
		func(snaps map[string]core.UsageSnapshot) {
			program.Send(tui.SnapshotsMsg(snaps))
		},
		func(state daemon.DaemonState) {
			program.Send(mapDaemonState(state))
		},
	)

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

func mapDaemonState(s daemon.DaemonState) tui.DaemonStatusMsg {
	statusMap := map[daemon.DaemonStatus]tui.DaemonStatus{
		daemon.DaemonStatusUnknown:      tui.DaemonConnecting,
		daemon.DaemonStatusConnecting:   tui.DaemonConnecting,
		daemon.DaemonStatusNotInstalled: tui.DaemonNotInstalled,
		daemon.DaemonStatusStarting:     tui.DaemonStarting,
		daemon.DaemonStatusRunning:      tui.DaemonRunning,
		daemon.DaemonStatusOutdated:     tui.DaemonOutdated,
		daemon.DaemonStatusError:        tui.DaemonError,
	}
	tuiStatus, ok := statusMap[s.Status]
	if !ok {
		tuiStatus = tui.DaemonError
	}
	return tui.DaemonStatusMsg{
		Status:      tuiStatus,
		Message:     s.Message,
		InstallHint: s.InstallHint,
	}
}
