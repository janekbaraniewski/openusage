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
	"github.com/janekbaraniewski/openusage/internal/appupdate"
	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/daemon"
	"github.com/janekbaraniewski/openusage/internal/tui"
	"github.com/janekbaraniewski/openusage/internal/version"
)

func runDashboard(cfg config.Config) {
	verbose := os.Getenv("OPENUSAGE_DEBUG") != ""

	if err := tui.LoadThemes(config.ConfigDir()); err != nil && verbose {
		log.Printf("theme load: %v", err)
	}
	tui.SetThemeByName(cfg.Theme)

	cachedAccounts := core.MergeAccounts(cfg.Accounts, cfg.AutoDetectedAccounts)
	interval := time.Duration(cfg.UI.RefreshIntervalSeconds) * time.Second

	timeWindow := core.ParseTimeWindow(cfg.Data.TimeWindow)

	model := tui.NewModel(
		cfg.UI.WarnThreshold,
		cfg.UI.CritThreshold,
		cfg.Experimental.Analytics,
		cfg.Dashboard,
		cachedAccounts,
		timeWindow,
	)

	socketPath := daemon.ResolveSocketPath()

	viewRuntime := daemon.NewViewRuntime(
		nil,
		socketPath,
		verbose,
	)
	viewRuntime.SetTimeWindow(string(timeWindow))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var program *tea.Program

	model.SetOnAddAccount(func(acct core.AccountConfig) {
		if strings.TrimSpace(acct.ID) == "" || strings.TrimSpace(acct.Provider) == "" {
			return
		}

		cfgNow, err := config.Load()
		if err != nil {
			log.Printf("add account: load config failed: %v", err)
			cfgNow = config.DefaultConfig()
		}

		accountID := strings.TrimSpace(acct.ID)
		providerID := strings.TrimSpace(acct.Provider)
		authType := strings.TrimSpace(acct.Auth)

		found := false
		for i := range cfgNow.Accounts {
			if strings.TrimSpace(cfgNow.Accounts[i].ID) != accountID {
				continue
			}
			found = true
			if strings.TrimSpace(cfgNow.Accounts[i].Provider) == "" {
				cfgNow.Accounts[i].Provider = providerID
			}
			if strings.TrimSpace(cfgNow.Accounts[i].Auth) == "" {
				cfgNow.Accounts[i].Auth = authType
			}
			break
		}
		if !found {
			cfgNow.Accounts = append(cfgNow.Accounts, core.AccountConfig{
				ID:       accountID,
				Provider: providerID,
				Auth:     authType,
			})
		}

		if err := config.Save(cfgNow); err != nil {
			log.Printf("add account: save config failed: %v", err)
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

	model.SetOnTimeWindowChange(func(tw string) {
		viewRuntime.SetTimeWindow(tw)
	})

	model.SetOnInstallDaemon(func() error {
		if err := daemon.InstallService(strings.TrimSpace(socketPath)); err != nil {
			return err
		}
		viewRuntime.ResetEnsureThrottle()
		return nil
	})

	program = tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())

	go func() {
		runStartupUpdateCheck(
			ctx,
			strings.TrimSpace(version.Version),
			1200*time.Millisecond,
			verbose,
			appupdate.Check,
			func(msg tui.AppUpdateMsg) {
				if program == nil {
					return
				}
				program.Send(msg)
			},
		)
	}()

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

type appUpdateCheckFunc func(context.Context, appupdate.CheckOptions) (appupdate.Result, error)

func runStartupUpdateCheck(
	ctx context.Context,
	currentVersion string,
	timeout time.Duration,
	debug bool,
	checkFn appUpdateCheckFunc,
	sendFn func(tui.AppUpdateMsg),
) {
	if checkFn == nil || sendFn == nil {
		return
	}

	result, err := checkFn(ctx, appupdate.CheckOptions{
		CurrentVersion: strings.TrimSpace(currentVersion),
		Timeout:        timeout,
	})
	if err != nil {
		if debug {
			log.Printf("app update check failed: %v", err)
		}
		return
	}
	if !result.UpdateAvailable {
		return
	}

	sendFn(tui.AppUpdateMsg{
		CurrentVersion: result.CurrentVersion,
		LatestVersion:  result.LatestVersion,
		UpgradeHint:    result.UpgradeHint,
	})
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
