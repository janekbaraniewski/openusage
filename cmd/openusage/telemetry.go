package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/providers"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
	"github.com/janekbaraniewski/openusage/internal/telemetry"
)

func runTelemetryCLI(args []string) error {
	if len(args) == 0 {
		return runTelemetryCollect(nil)
	}

	switch args[0] {
	case "collect":
		return runTelemetryCollect(args[1:])
	case "hook":
		return runTelemetryHook(args[1:])
	case "daemon":
		return runTelemetryDaemon(args[1:])
	case "help", "-h", "--help":
		printTelemetryUsage()
		return nil
	default:
		return fmt.Errorf("unknown telemetry subcommand %q", args[0])
	}
}

func runTelemetryCollect(args []string) error {
	defaultDBPath, err := telemetry.DefaultDBPath()
	if err != nil {
		return fmt.Errorf("resolve telemetry db path: %w", err)
	}
	defaultSpoolDir, err := telemetry.DefaultSpoolDir()
	if err != nil {
		return fmt.Errorf("resolve telemetry spool dir: %w", err)
	}

	fs := flag.NewFlagSet("telemetry collect", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)

	dbPath := fs.String("db-path", defaultDBPath, "path to telemetry sqlite database")
	spoolDir := fs.String("spool-dir", defaultSpoolDir, "path to telemetry spool directory")

	codexSessions := fs.String("codex-sessions", "~/.codex/sessions", "Codex sessions directory")
	claudeProjects := fs.String("claude-projects", "~/.claude/projects", "Claude projects directory")
	claudeProjectsAlt := fs.String("claude-projects-alt", "~/.config/claude/projects", "Claude alt projects directory")
	opencodeEventsDirs := fs.String("opencode-events-dirs", "", "comma-separated OpenCode event directories")
	opencodeEventsFile := fs.String("opencode-events-file", "", "optional OpenCode event jsonl/ndjson file")
	opencodeDB := fs.String("opencode-db", "~/.local/share/opencode/opencode.db", "path to OpenCode sqlite database")

	maxFlush := fs.Int("max-flush", 0, "maximum spool records to ingest in this run (0 means no limit)")
	dryRun := fs.Bool("dry-run", false, "collect events but do not write spool/db")
	verbose := fs.Bool("verbose", false, "print per-collector details")

	if err := fs.Parse(args); err != nil {
		return err
	}

	collectors := []telemetry.Collector{}
	for _, source := range providers.AllTelemetrySources() {
		options := telemetryOptionsForSource(
			source.System(),
			*codexSessions,
			*claudeProjects,
			*claudeProjectsAlt,
			splitCSV(*opencodeEventsDirs),
			*opencodeEventsFile,
			*opencodeDB,
		)
		collectors = append(collectors, telemetry.NewSourceCollector(source, options, ""))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	totalCollected := 0
	collectedByCollector := make(map[string]int)
	warnings := make([]string, 0)

	var store *telemetry.Store
	var pipeline *telemetry.Pipeline

	if !*dryRun {
		store, err = telemetry.OpenStore(*dbPath)
		if err != nil {
			return fmt.Errorf("open telemetry store: %w", err)
		}
		defer store.Close()

		spool := telemetry.NewSpool(*spoolDir)
		pipeline = telemetry.NewPipeline(store, spool)
	}

	totalEnqueued := 0
	for _, collector := range collectors {
		reqs, err := collector.Collect(ctx)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", collector.Name(), err))
			continue
		}
		if *verbose {
			fmt.Printf("collector=%s collected=%d\n", collector.Name(), len(reqs))
		}
		totalCollected += len(reqs)
		collectedByCollector[collector.Name()] = len(reqs)

		if *dryRun || len(reqs) == 0 {
			continue
		}
		enqueued, enqueueErr := pipeline.EnqueueRequests(reqs)
		if enqueueErr != nil {
			return fmt.Errorf("enqueue %s events: %w", collector.Name(), enqueueErr)
		}
		totalEnqueued += enqueued
	}

	fmt.Printf("telemetry collected=%d codex=%d claude_code=%d opencode=%d\n",
		totalCollected,
		collectedByCollector["codex"],
		collectedByCollector["claude_code"],
		collectedByCollector["opencode"],
	)

	if *dryRun {
		fmt.Println("dry-run enabled: no spool/db writes performed")
		for _, warning := range warnings {
			fmt.Printf("warning: %s\n", warning)
		}
		return nil
	}

	flushResult, flushWarnings := flushInBatches(ctx, pipeline, *maxFlush)
	warnings = append(warnings, flushWarnings...)

	stats, statsErr := store.Stats(ctx)
	if statsErr != nil {
		return fmt.Errorf("load telemetry stats: %w", statsErr)
	}

	fmt.Printf("telemetry enqueued=%d processed=%d ingested=%d deduped=%d failed=%d\n",
		totalEnqueued,
		flushResult.Processed,
		flushResult.Ingested,
		flushResult.Deduped,
		flushResult.Failed,
	)
	fmt.Printf("telemetry db=%s raw_events=%d canonical_events=%d reconciliation_windows=%d\n",
		*dbPath,
		stats.RawEvents,
		stats.CanonicalEvents,
		stats.ReconciliationWindows,
	)

	for _, warning := range warnings {
		fmt.Printf("warning: %s\n", warning)
	}

	return nil
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func printTelemetryUsage() {
	fmt.Println("Usage:")
	fmt.Println("  openusage telemetry collect [flags]")
	fmt.Println("  openusage telemetry hook <source> [flags] < payload.json")
	fmt.Println("  openusage telemetry daemon [flags]")
	fmt.Println("  openusage telemetry daemon install [flags]")
	fmt.Println("  openusage telemetry daemon status [flags]")
	fmt.Println("  openusage telemetry daemon uninstall [flags]")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  openusage telemetry collect --verbose")
	fmt.Println("  openusage telemetry collect --dry-run")
	fmt.Println("  openusage telemetry daemon --verbose")
	fmt.Println("  openusage telemetry daemon install")
	fmt.Println("  openusage telemetry daemon status")
	fmt.Println("  openusage telemetry collect --opencode-events-file /tmp/opencode-events.jsonl")
	fmt.Println("  openusage telemetry collect --opencode-db ~/.local/share/opencode/opencode.db")
	fmt.Println("  openusage telemetry hook opencode < /tmp/opencode-hook-event.json")
	fmt.Println("  openusage telemetry hook codex < /tmp/codex-notify-payload.json")
	fmt.Println("  openusage telemetry hook claude_code < /tmp/claude-hook-payload.json")
}

func runTelemetryHook(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing hook source")
	}
	return runTelemetryHookSource(args[0], args[1:])
}

func runTelemetryHookSource(sourceName string, args []string) error {
	defaultSocketPath, err := telemetry.DefaultSocketPath()
	if err != nil {
		return fmt.Errorf("resolve telemetry daemon socket path: %w", err)
	}

	_, ok := providers.TelemetrySourceBySystem(sourceName)
	if !ok {
		return fmt.Errorf("unknown hook source %q", sourceName)
	}

	fs := flag.NewFlagSet("telemetry hook "+sourceName, flag.ContinueOnError)
	fs.SetOutput(os.Stdout)

	socketPath := fs.String("socket-path", defaultSocketPath, "path to telemetry daemon unix socket")
	accountID := fs.String("account-id", "", "optional logical account id override for ingested hook events")
	verbose := fs.Bool("verbose", false, "print detailed ingest summary")

	if err := fs.Parse(args); err != nil {
		return err
	}

	payload, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("read hook payload from stdin: %w", err)
	}
	if len(strings.TrimSpace(string(payload))) == 0 {
		return fmt.Errorf("stdin payload is empty")
	}

	client := newTelemetryDaemonClient(strings.TrimSpace(*socketPath))
	daemonCtx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	daemonResult, daemonErr := client.IngestHook(daemonCtx, sourceName, strings.TrimSpace(*accountID), payload)
	cancel()
	if daemonErr != nil {
		return fmt.Errorf("send hook payload to telemetry daemon: %w", daemonErr)
	}
	if *verbose {
		fmt.Printf("telemetry hook %s via daemon enqueued=%d processed=%d ingested=%d deduped=%d failed=%d\n",
			sourceName,
			daemonResult.Enqueued,
			daemonResult.Processed,
			daemonResult.Ingested,
			daemonResult.Deduped,
			daemonResult.Failed,
		)
		for _, warning := range daemonResult.Warnings {
			fmt.Printf("warning: %s\n", warning)
		}
	}

	return nil
}

func telemetryOptionsForSource(
	sourceSystem string,
	codexSessions string,
	claudeProjects string,
	claudeProjectsAlt string,
	opencodeEventsDirs []string,
	opencodeEventsFile string,
	opencodeDB string,
) shared.TelemetryCollectOptions {
	opts := shared.TelemetryCollectOptions{
		Paths:     map[string]string{},
		PathLists: map[string][]string{},
	}

	switch sourceSystem {
	case "codex":
		opts.Paths["sessions_dir"] = codexSessions
	case "claude_code":
		opts.Paths["projects_dir"] = claudeProjects
		opts.Paths["alt_projects_dir"] = claudeProjectsAlt
	case "opencode":
		opts.Paths["events_file"] = opencodeEventsFile
		opts.Paths["db_path"] = opencodeDB
		opts.PathLists["events_dirs"] = opencodeEventsDirs
	}
	return opts
}

func flushInBatches(ctx context.Context, pipeline *telemetry.Pipeline, maxTotal int) (telemetry.FlushResult, []string) {
	var (
		accum    telemetry.FlushResult
		warnings []string
	)

	remaining := maxTotal
	for {
		batchLimit := 10000
		if maxTotal > 0 {
			if remaining <= 0 {
				break
			}
			if remaining < batchLimit {
				batchLimit = remaining
			}
		}

		batch, err := pipeline.Flush(ctx, batchLimit)
		accum.Processed += batch.Processed
		accum.Ingested += batch.Ingested
		accum.Deduped += batch.Deduped
		accum.Failed += batch.Failed

		if err != nil {
			warnings = append(warnings, err.Error())
		}
		if maxTotal > 0 {
			remaining -= batch.Processed
		}

		// Stop when there is nothing left to process or no forward progress can be made.
		if batch.Processed == 0 || (batch.Ingested == 0 && batch.Deduped == 0) {
			break
		}
	}

	return accum, warnings
}
