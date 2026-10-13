package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/daemon"
	"github.com/janekbaraniewski/openusage/internal/detect"
	"github.com/janekbaraniewski/openusage/internal/integrations"
	"github.com/janekbaraniewski/openusage/internal/providers"
	"github.com/janekbaraniewski/openusage/internal/telemetry"
	"github.com/spf13/cobra"
)

func newTelemetryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "telemetry",
		Short: "Manage the telemetry daemon",
		Long:  "Commands for managing the telemetry daemon and sending hook payloads.",
	}

	cmd.AddCommand(newTelemetryHookCommand())
	cmd.AddCommand(newTelemetryDaemonCommand())

	return cmd
}

func newTelemetryHookCommand() *cobra.Command {
	var (
		socketPath string
		accountID  string
		dbPath     string
		spoolDir   string
		spoolOnly  bool
		verbose    bool
	)

	cmd := &cobra.Command{
		Use:   "hook <source> [payload]",
		Short: "Send a hook payload to the telemetry daemon via stdin or an argument",
		Long: strings.Join([]string{
			"Send a hook payload to the telemetry daemon.",
			"",
			"The payload is read from stdin by default. If a non-empty positional",
			"payload argument is provided after <source>, it is used instead of stdin.",
			"This supports tools (e.g. Codex's notify) that pass the event JSON as argv.",
		}, "\n"),
		Example: strings.Join([]string{
			"  openusage telemetry hook opencode < /tmp/opencode-hook-event.json",
			"  openusage telemetry hook codex < /tmp/codex-notify-payload.json",
			"  openusage telemetry hook claude_code < /tmp/claude-hook-payload.json",
			"  openusage telemetry hook codex '{\"type\":\"agent-turn-complete\"}'",
		}, "\n"),
		Args: cobra.RangeArgs(1, 2),
		RunE: func(_ *cobra.Command, args []string) error {
			sourceName := strings.TrimSpace(args[0])
			if _, ok := providers.TelemetrySourceBySystem(sourceName); !ok {
				var known []string
				for _, p := range providers.AllProviders() {
					if src, ok := p.(interface{ System() string }); ok {
						known = append(known, src.System())
					}
				}
				return fmt.Errorf("unknown telemetry source %q; known sources: %s", sourceName, strings.Join(known, ", "))
			}

			// Prefer a positional payload arg when provided and non-empty
			// (e.g. Codex's notify passes the event JSON as argv). Otherwise
			// read the payload from stdin exactly as before.
			var payload []byte
			if len(args) > 1 && strings.TrimSpace(args[1]) != "" {
				payload = []byte(args[1])
			} else {
				stdinPayload, err := io.ReadAll(os.Stdin)
				if err != nil {
					return fmt.Errorf("read hook payload from stdin: %w", err)
				}
				payload = stdinPayload
			}
			if len(strings.TrimSpace(string(payload))) == 0 {
				return fmt.Errorf("hook payload is empty")
			}

			client := daemon.NewClient(strings.TrimSpace(socketPath))
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			var daemonErr error
			if !spoolOnly {
				result, err := client.IngestHook(ctx, sourceName, strings.TrimSpace(accountID), payload)
				if err == nil {
					if verbose {
						fmt.Printf("telemetry hook %s via daemon enqueued=%d processed=%d ingested=%d deduped=%d failed=%d\n",
							sourceName,
							result.Enqueued,
							result.Processed,
							result.Ingested,
							result.Deduped,
							result.Failed,
						)
						for _, w := range result.Warnings {
							fmt.Printf("warning: %s\n", w)
						}
					}
					return nil
				}
				daemonErr = err
			}

			result, err := daemon.IngestHookLocally(
				ctx,
				sourceName,
				strings.TrimSpace(accountID),
				payload,
				strings.TrimSpace(dbPath),
				strings.TrimSpace(spoolDir),
				spoolOnly,
			)
			if err != nil {
				if daemonErr != nil {
					return fmt.Errorf("send hook payload to telemetry daemon: %w (local fallback failed: %v)", daemonErr, err)
				}
				return fmt.Errorf("ingest hook payload locally: %w", err)
			}

			if verbose {
				if daemonErr != nil && !spoolOnly {
					fmt.Printf("telemetry hook %s via local-fallback daemon_error=%v enqueued=%d processed=%d ingested=%d deduped=%d failed=%d\n",
						sourceName,
						daemonErr,
						result.Enqueued,
						result.Processed,
						result.Ingested,
						result.Deduped,
						result.Failed,
					)
				} else {
					fmt.Printf("telemetry hook %s via local-ingest enqueued=%d processed=%d ingested=%d deduped=%d failed=%d\n",
						sourceName,
						result.Enqueued,
						result.Processed,
						result.Ingested,
						result.Deduped,
						result.Failed,
					)
				}
				for _, w := range result.Warnings {
					fmt.Printf("warning: %s\n", w)
				}
			}
			return nil
		},
	}

	defaultSocketPath, _ := telemetry.DefaultSocketPath()
	defaultDBPath, _ := telemetry.DefaultDBPath()
	defaultSpoolDir, _ := telemetry.DefaultSpoolDir()
	cmd.Flags().StringVar(&socketPath, "socket-path", defaultSocketPath, "path to telemetry daemon unix socket")
	cmd.Flags().StringVar(&accountID, "account-id", "", "optional logical account id override for ingested hook events")
	cmd.Flags().StringVar(&dbPath, "db-path", defaultDBPath, "path to telemetry sqlite database (used by local fallback)")
	cmd.Flags().StringVar(&spoolDir, "spool-dir", defaultSpoolDir, "path to telemetry spool directory (used by local fallback)")
	cmd.Flags().BoolVar(&spoolOnly, "spool-only", false, "enqueue hook payload to local spool without immediate DB ingest")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "print detailed ingest summary")

	return cmd
}

func configuredClaudeCodeAccount(cfg config.Config) string {
	for _, accounts := range [][]core.AccountConfig{cfg.Accounts, cfg.AutoDetectedAccounts} {
		for _, account := range accounts {
			if strings.EqualFold(strings.TrimSpace(account.Provider), "claude_code") && strings.TrimSpace(account.ID) != "" {
				return strings.TrimSpace(account.ID)
			}
		}
	}
	return ""
}

func newTelemetryDaemonCommand() *cobra.Command {
	var (
		socketPath      string
		dbPath          string
		spoolDir        string
		interval        time.Duration
		collectInterval time.Duration
		pollInterval    time.Duration
		verbose         bool
	)

	runDaemon := func(_ *cobra.Command, _ []string) error {
		daemon.LoadServiceEnv()
		cfgFile, loadErr := config.Load()
		if loadErr != nil {
			log.Printf("warning: failed to load config, using defaults: %v", loadErr)
			cfgFile = config.DefaultConfig()
		}

		resolvedInterval := interval
		if resolvedInterval <= 0 {
			resolvedInterval = time.Duration(cfgFile.UI.RefreshIntervalSeconds) * time.Second
		}
		if resolvedInterval <= 0 {
			resolvedInterval = 30 * time.Second
		}

		resolvedCollect := collectInterval
		if resolvedCollect <= 0 {
			resolvedCollect = resolvedInterval
		}
		resolvedPoll := pollInterval
		if resolvedPoll <= 0 {
			resolvedPoll = resolvedInterval
		}

		// Check for actionable integrations and print advisory hints.
		detected := detect.AutoDetect()
		dirs := integrations.NewDefaultDirs()
		matches := integrations.MatchDetected(integrations.AllDefinitions(), detected, dirs)
		var actionableIDs []string
		for _, m := range matches {
			if m.Actionable {
				actionableIDs = append(actionableIDs, string(m.Definition.ID))
			}
		}
		if len(actionableIDs) > 0 {
			fmt.Fprintf(os.Stderr, "hint: detected tools with missing integrations: %s\n", strings.Join(actionableIDs, ", "))
			fmt.Fprintf(os.Stderr, "hint: run 'openusage integrations install <id>' to set up telemetry hooks\n")
		}

		return daemon.RunServer(daemon.Config{
			DBPath:            strings.TrimSpace(dbPath),
			SpoolDir:          strings.TrimSpace(spoolDir),
			SocketPath:        strings.TrimSpace(socketPath),
			CollectInterval:   resolvedCollect,
			PollInterval:      resolvedPoll,
			Verbose:           verbose,
			Export:            cfgFile.Export,
			RoutineListenAddr: strings.TrimSpace(os.Getenv("OPENUSAGE_ROUTINE_LISTEN_ADDR")),
			RoutineToken:      os.Getenv("OPENUSAGE_ROUTINE_TOKEN"),
			RoutineAccountID:  configuredClaudeCodeAccount(cfgFile),
		})
	}

	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Run the telemetry daemon server",
		Long:  "Start the telemetry daemon. Use subcommands to install, uninstall, or check status. Optional Claude Code Routine reports are accepted only when OPENUSAGE_ROUTINE_LISTEN_ADDR and OPENUSAGE_ROUTINE_TOKEN are set.",
		Example: strings.Join([]string{
			"  openusage telemetry daemon",
			"  openusage telemetry daemon run",
			"  openusage telemetry daemon --verbose",
			"  openusage telemetry daemon install",
			"  openusage telemetry daemon status",
			"  openusage telemetry daemon uninstall",
		}, "\n"),
		RunE: runDaemon,
	}

	defaultSocketPath, _ := telemetry.DefaultSocketPath()
	defaultDBPath, _ := telemetry.DefaultDBPath()
	defaultSpoolDir, _ := telemetry.DefaultSpoolDir()

	cmd.PersistentFlags().StringVar(&socketPath, "socket-path", defaultSocketPath, "path to telemetry daemon unix socket")
	addDaemonRunFlags(cmd, &dbPath, &spoolDir, &interval, &collectInterval, &pollInterval, &verbose, defaultDBPath, defaultSpoolDir)

	runCmd := newDaemonRunCommand(runDaemon)
	addDaemonRunFlags(runCmd, &dbPath, &spoolDir, &interval, &collectInterval, &pollInterval, &verbose, defaultDBPath, defaultSpoolDir)
	cmd.AddCommand(runCmd)
	cmd.AddCommand(newDaemonInstallCommand())
	cmd.AddCommand(newDaemonUninstallCommand())
	cmd.AddCommand(newDaemonStatusCommand())

	return cmd
}

func addDaemonRunFlags(
	cmd *cobra.Command,
	dbPath *string,
	spoolDir *string,
	interval *time.Duration,
	collectInterval *time.Duration,
	pollInterval *time.Duration,
	verbose *bool,
	defaultDBPath string,
	defaultSpoolDir string,
) {
	cmd.Flags().StringVar(dbPath, "db-path", defaultDBPath, "path to telemetry sqlite database")
	cmd.Flags().StringVar(spoolDir, "spool-dir", defaultSpoolDir, "path to telemetry spool directory")
	cmd.Flags().DurationVar(interval, "interval", 0, "default collector/poller interval (0 uses config or 30s)")
	cmd.Flags().DurationVar(collectInterval, "collect-interval", 0, "collector interval override (0 uses --interval)")
	cmd.Flags().DurationVar(pollInterval, "poll-interval", 0, "provider poll interval override (0 uses --interval)")
	cmd.Flags().BoolVar(verbose, "verbose", false, "enable daemon logs")
}

func newDaemonRunCommand(runE func(cmd *cobra.Command, args []string) error) *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Run the telemetry daemon server",
		RunE:  runE,
	}
}

func newDaemonInstallCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Install the telemetry daemon as a system service",
		RunE: func(cmd *cobra.Command, _ []string) error {
			socketPath, _ := cmd.Flags().GetString("socket-path")
			if err := daemon.InstallService(strings.TrimSpace(socketPath)); err != nil {
				return err
			}
			fmt.Println("telemetry daemon service installed")
			return nil
		},
	}
}

func newDaemonUninstallCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall the telemetry daemon system service",
		RunE: func(cmd *cobra.Command, _ []string) error {
			socketPath, _ := cmd.Flags().GetString("socket-path")
			if err := daemon.UninstallService(strings.TrimSpace(socketPath)); err != nil {
				return err
			}
			fmt.Println("telemetry daemon service uninstalled")
			return nil
		},
	}
}

func newDaemonStatusCommand() *cobra.Command {
	var details bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show telemetry daemon status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			socketPath, _ := cmd.Flags().GetString("socket-path")
			return daemon.ServiceStatus(cmd.Context(), strings.TrimSpace(socketPath), details)
		},
	}
	cmd.Flags().BoolVar(&details, "details", false, "include verbose startup diagnostics")
	return cmd
}
