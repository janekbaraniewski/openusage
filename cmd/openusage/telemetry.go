package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/config"
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
		verbose    bool
	)

	cmd := &cobra.Command{
		Use:   "hook <source>",
		Short: "Send a hook payload to the telemetry daemon via stdin",
		Example: strings.Join([]string{
			"  openusage telemetry hook opencode < /tmp/opencode-hook-event.json",
			"  openusage telemetry hook codex < /tmp/codex-notify-payload.json",
			"  openusage telemetry hook claude_code < /tmp/claude-hook-payload.json",
		}, "\n"),
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			sourceName := args[0]
			if _, ok := providers.TelemetrySourceBySystem(sourceName); !ok {
				return fmt.Errorf("unknown hook source %q", sourceName)
			}

			payload, err := io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("read hook payload from stdin: %w", err)
			}
			if len(strings.TrimSpace(string(payload))) == 0 {
				return fmt.Errorf("stdin payload is empty")
			}

			client := daemon.NewClient(strings.TrimSpace(socketPath))
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()

			result, err := client.IngestHook(ctx, sourceName, strings.TrimSpace(accountID), payload)
			if err != nil {
				return fmt.Errorf("send hook payload to telemetry daemon: %w", err)
			}
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
		},
	}

	defaultSocketPath, _ := telemetry.DefaultSocketPath()
	cmd.Flags().StringVar(&socketPath, "socket-path", defaultSocketPath, "path to telemetry daemon unix socket")
	cmd.Flags().StringVar(&accountID, "account-id", "", "optional logical account id override for ingested hook events")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "print detailed ingest summary")

	return cmd
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

	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Run the telemetry daemon server",
		Long:  "Start the telemetry daemon. Use subcommands to install, uninstall, or check status.",
		Example: strings.Join([]string{
			"  openusage telemetry daemon",
			"  openusage telemetry daemon --verbose",
			"  openusage telemetry daemon install",
			"  openusage telemetry daemon status",
			"  openusage telemetry daemon uninstall",
		}, "\n"),
		RunE: func(_ *cobra.Command, _ []string) error {
			cfgFile, loadErr := config.Load()
			if loadErr != nil {
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
				DBPath:          strings.TrimSpace(dbPath),
				SpoolDir:        strings.TrimSpace(spoolDir),
				SocketPath:      strings.TrimSpace(socketPath),
				CollectInterval: resolvedCollect,
				PollInterval:    resolvedPoll,
				Verbose:         verbose,
			})
		},
	}

	defaultSocketPath, _ := telemetry.DefaultSocketPath()
	defaultDBPath, _ := telemetry.DefaultDBPath()
	defaultSpoolDir, _ := telemetry.DefaultSpoolDir()

	cmd.PersistentFlags().StringVar(&socketPath, "socket-path", defaultSocketPath, "path to telemetry daemon unix socket")
	cmd.Flags().StringVar(&dbPath, "db-path", defaultDBPath, "path to telemetry sqlite database")
	cmd.Flags().StringVar(&spoolDir, "spool-dir", defaultSpoolDir, "path to telemetry spool directory")
	cmd.Flags().DurationVar(&interval, "interval", 0, "default collector/poller interval (0 uses config or 30s)")
	cmd.Flags().DurationVar(&collectInterval, "collect-interval", 0, "collector interval override (0 uses --interval)")
	cmd.Flags().DurationVar(&pollInterval, "poll-interval", 0, "provider poll interval override (0 uses --interval)")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "enable daemon logs")

	cmd.AddCommand(newDaemonInstallCommand())
	cmd.AddCommand(newDaemonUninstallCommand())
	cmd.AddCommand(newDaemonStatusCommand())

	return cmd
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
	return &cobra.Command{
		Use:   "status",
		Short: "Show telemetry daemon status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			socketPath, _ := cmd.Flags().GetString("socket-path")
			return daemon.ServiceStatus(strings.TrimSpace(socketPath))
		},
	}
}
