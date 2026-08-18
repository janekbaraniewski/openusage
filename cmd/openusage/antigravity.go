package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/providers/antigravity"
	"github.com/spf13/cobra"
)

func newAntigravityCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "antigravity",
		Short: "Antigravity CLI integration commands",
	}

	var stateFile string
	statusline := &cobra.Command{
		Use:   "statusline",
		Short: "Capture and render Antigravity's status-line JSON",
		Long: `Read the JSON Antigravity sends to a statusLine command, save the latest
payload for OpenUsage, and print a compact one-line summary. The command does
not read or store credentials.`,
		Example: strings.Join([]string{
			`  agy --output-format json | openusage antigravity statusline`,
			`  cat statusline.json | openusage antigravity statusline --state-file /tmp/agy.json`,
		}, "\n"),
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				fmt.Fprintln(os.Stdout, "AGY")
				return fmt.Errorf("read Antigravity status-line input: %w", err)
			}
			line, captureErr := antigravity.CaptureStatusLine(data, stateFile)
			fmt.Fprintln(os.Stdout, line)
			return captureErr
		},
	}
	statusline.Flags().StringVar(&stateFile, "state-file", "", "override the OpenUsage Antigravity state file")
	cmd.AddCommand(statusline)
	return cmd
}
