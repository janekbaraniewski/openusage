package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/detect"
	"github.com/janekbaraniewski/openusage/internal/telemetry"
	"github.com/janekbaraniewski/openusage/internal/tui"
)

func RunDashboard(cfg config.Config) {
	tui.SetThemeByName(cfg.Theme)

	allAccounts := resolveAccounts(&cfg)
	interval := time.Duration(cfg.UI.RefreshIntervalSeconds) * time.Second

	model := tui.NewModel(
		cfg.UI.WarnThreshold,
		cfg.UI.CritThreshold,
		cfg.Experimental.Analytics,
		cfg.Dashboard,
		allAccounts,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	viewRuntime := newDaemonViewRuntime(
		nil,
		resolveSocketPath(),
		os.Getenv("OPENUSAGE_DEBUG") != "",
		allAccounts,
		cfg.Telemetry.ProviderLinks,
	)

	var program *tea.Program

	model.SetOnAddAccount(func(acct core.AccountConfig) {
		if strings.TrimSpace(acct.ID) == "" || strings.TrimSpace(acct.Provider) == "" {
			return
		}
		exists := false
		for _, existing := range allAccounts {
			if strings.EqualFold(strings.TrimSpace(existing.ID), strings.TrimSpace(acct.ID)) {
				exists = true
				break
			}
		}
		if !exists {
			allAccounts = append(allAccounts, acct)
		}
		viewRuntime.setAccounts(allAccounts, cfg.Telemetry.ProviderLinks)
	})

	model.SetOnRefresh(func() {
		go func() {
			snaps := viewRuntime.readWithFallback(ctx)
			if len(snaps) > 0 && program != nil {
				program.Send(tui.SnapshotsMsg(snaps))
			}
		}()
	})

	program = tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	startDaemonViewBroadcaster(ctx, program, viewRuntime, interval)

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

func resolveAccounts(cfg *config.Config) []core.AccountConfig {
	allAccounts := core.MergeAccounts(cfg.Accounts, cfg.AutoDetectedAccounts)

	if cfg.AutoDetect {
		result := detect.AutoDetect()

		manualIDs := make(map[string]bool, len(cfg.Accounts))
		for _, acct := range cfg.Accounts {
			manualIDs[acct.ID] = true
		}
		var autoDetected []core.AccountConfig
		for _, acct := range result.Accounts {
			if !manualIDs[acct.ID] {
				autoDetected = append(autoDetected, acct)
			}
		}

		cfg.AutoDetectedAccounts = autoDetected
		if err := config.SaveAutoDetected(autoDetected); err != nil {
			log.Printf("Warning: could not persist auto-detected accounts: %v", err)
		}

		allAccounts = core.MergeAccounts(cfg.Accounts, cfg.AutoDetectedAccounts)

		if os.Getenv("OPENUSAGE_DEBUG") != "" {
			if len(result.Tools) > 0 || len(result.Accounts) > 0 {
				fmt.Fprint(os.Stderr, result.Summary())
				fmt.Fprintln(os.Stderr)
			}
		}
	}

	credResult := detect.Result{Accounts: allAccounts}
	detect.ApplyCredentials(&credResult)
	return credResult.Accounts
}

func resolveSocketPath() string {
	if value := strings.TrimSpace(os.Getenv("OPENUSAGE_TELEMETRY_SOCKET")); value != "" {
		return value
	}
	socketPath, err := telemetry.DefaultSocketPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving telemetry daemon socket path: %v\n", err)
		os.Exit(1)
	}
	return socketPath
}
