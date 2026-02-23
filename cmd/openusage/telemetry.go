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
	"github.com/janekbaraniewski/openusage/internal/telemetry"
)

func runTelemetryCLI(args []string) error {
	if len(args) == 0 {
		printTelemetryUsage()
		return nil
	}

	switch args[0] {
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

func printTelemetryUsage() {
	fmt.Println("Usage:")
	fmt.Println("  openusage telemetry hook <source> [flags] < payload.json")
	fmt.Println("  openusage telemetry daemon [flags]")
	fmt.Println("  openusage telemetry daemon install [flags]")
	fmt.Println("  openusage telemetry daemon status [flags]")
	fmt.Println("  openusage telemetry daemon uninstall [flags]")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  openusage telemetry daemon --verbose")
	fmt.Println("  openusage telemetry daemon install")
	fmt.Println("  openusage telemetry daemon status")
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
