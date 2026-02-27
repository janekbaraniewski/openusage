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
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
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

			result, err := ingestHookLocally(
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

func ingestHookLocally(
	ctx context.Context,
	sourceName string,
	accountID string,
	payload []byte,
	dbPath string,
	spoolDir string,
	spoolOnly bool,
) (daemon.HookResponse, error) {
	source, ok := providers.TelemetrySourceBySystem(sourceName)
	if !ok {
		return daemon.HookResponse{}, fmt.Errorf("unknown hook source %q", sourceName)
	}
	reqs, err := telemetry.ParseSourceHookPayload(source, payload, shared.TelemetryCollectOptions{}, accountID)
	if err != nil {
		return daemon.HookResponse{}, fmt.Errorf("parse hook payload: %w", err)
	}
	resp := daemon.HookResponse{
		Source:   sourceName,
		Enqueued: len(reqs),
	}
	if len(reqs) == 0 {
		return resp, nil
	}

	if strings.TrimSpace(dbPath) == "" {
		resolved, resolveErr := telemetry.DefaultDBPath()
		if resolveErr != nil {
			return daemon.HookResponse{}, fmt.Errorf("resolve telemetry db path: %w", resolveErr)
		}
		dbPath = resolved
	}
	if strings.TrimSpace(spoolDir) == "" {
		resolved, resolveErr := telemetry.DefaultSpoolDir()
		if resolveErr != nil {
			return daemon.HookResponse{}, fmt.Errorf("resolve telemetry spool dir: %w", resolveErr)
		}
		spoolDir = resolved
	}

	store, err := telemetry.OpenStore(dbPath)
	if err != nil {
		return daemon.HookResponse{}, fmt.Errorf("open telemetry store: %w", err)
	}
	defer store.Close()

	pipeline := telemetry.NewPipeline(store, telemetry.NewSpool(spoolDir))
	if spoolOnly {
		enqueued, enqueueErr := pipeline.EnqueueRequests(reqs)
		if enqueueErr != nil {
			return daemon.HookResponse{}, fmt.Errorf("enqueue to telemetry spool: %w", enqueueErr)
		}
		resp.Enqueued = enqueued
		return resp, nil
	}

	retries := make([]telemetry.IngestRequest, 0, len(reqs))
	var firstIngestErr error
	for _, req := range reqs {
		resp.Processed++
		result, ingestErr := store.Ingest(ctx, req)
		if ingestErr != nil {
			if firstIngestErr == nil {
				firstIngestErr = ingestErr
			}
			retries = append(retries, req)
			continue
		}
		if result.Deduped {
			resp.Deduped++
		} else {
			resp.Ingested++
		}
	}

	if len(retries) == 0 {
		return resp, nil
	}
	if firstIngestErr != nil {
		resp.Warnings = append(resp.Warnings, fmt.Sprintf("direct ingest failed for %d event(s): %v", len(retries), firstIngestErr))
	}

	enqueued, enqueueErr := pipeline.EnqueueRequests(retries)
	if enqueueErr != nil {
		resp.Failed += len(retries)
		resp.Warnings = append(resp.Warnings, fmt.Sprintf("retry enqueue failed: %v", enqueueErr))
		return resp, nil
	}
	flush, warnings := daemon.FlushInBatches(ctx, pipeline, enqueued)
	resp.Processed += flush.Processed
	resp.Ingested += flush.Ingested
	resp.Deduped += flush.Deduped
	resp.Failed += flush.Failed
	resp.Warnings = append(resp.Warnings, warnings...)

	if remaining := len(retries) - flush.Processed; remaining > 0 {
		resp.Warnings = append(resp.Warnings, fmt.Sprintf("%d event(s) remain queued in spool", remaining))
	}
	return resp, nil
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
