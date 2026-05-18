package main

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/janekbaraniewski/openusage/internal/export"
)

// newExportCommand wires the `openusage export` subcommand. It collects the
// current usage snapshots from the running telemetry daemon (when reachable)
// or from a one-shot direct provider poll, and serializes them into a
// versioned JSON envelope.
//
// Schema notes: the on-disk format is `schema_version="1"`. Token-bearing
// fields are excluded by their `json:"-"` tag, and the provider Raw map is
// also stripped because provider probes sometimes drop credential hints
// there.
func newExportCommand() *cobra.Command {
	var (
		outputFlag string
		formatFlag string
		sourceFlag string
	)

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export current usage snapshots to a file or stdout",
		Long: `Collect the current usage snapshots from all configured providers and write them
to a file or stdout as a versioned JSON envelope.

By default the command prefers the running telemetry daemon when available
(quiet, cache-backed) and falls back to a one-shot direct provider poll when
the daemon is not running. Use --source to force a specific path.

API keys and tokens are never written to the output file. The envelope
contains schema_version, generated_at, openusage_version, source, and the
collected snapshots.`,
		Example: strings.Join([]string{
			"  openusage export --output ~/usage.json",
			"  openusage export --output - --format json",
			"  openusage export --output /tmp/usage.csv --format csv",
			"  openusage export --output ~/usage.json --source direct",
		}, "\n"),
		RunE: func(_ *cobra.Command, _ []string) error {
			opts := export.Options{
				Output: strings.TrimSpace(outputFlag),
				Format: export.Format(strings.ToLower(strings.TrimSpace(formatFlag))),
				Source: export.Source(strings.ToLower(strings.TrimSpace(sourceFlag))),
			}
			return export.Run(opts)
		},
	}

	cmd.Flags().StringVarP(&outputFlag, "output", "o", "",
		"output file path; use '-' for stdout (required)")
	cmd.Flags().StringVar(&formatFlag, "format", string(export.FormatJSON),
		"output format: json (default) or csv")
	cmd.Flags().StringVar(&sourceFlag, "source", string(export.SourceAuto),
		"collection source: auto (default), direct, or daemon")
	_ = cmd.MarkFlagRequired("output")

	return cmd
}
