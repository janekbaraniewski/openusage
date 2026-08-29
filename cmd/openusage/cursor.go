package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/providers/cursor"
	"github.com/spf13/cobra"
)

func newCursorCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cursor",
		Short: "Cursor CLI integration commands",
	}

	var stateFile string
	statusline := &cobra.Command{
		Use:   "statusline",
		Short: "Capture and render Cursor's status-line JSON",
		Long: `Read the JSON Cursor sends to a statusLine command, save the latest
payload for OpenUsage, and print a compact one-line summary. The command does
not read or store credentials.`,
		Example: strings.Join([]string{
			`  cat statusline.json | openusage cursor statusline`,
			`  cat statusline.json | openusage cursor statusline --state-file /tmp/cursor.json`,
		}, "\n"),
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				fmt.Fprintln(os.Stdout, "Cursor")
				return fmt.Errorf("read Cursor status-line input: %w", err)
			}
			line, captureErr := cursor.CaptureStatusLine(data, stateFile)
			fmt.Fprintln(os.Stdout, line)
			return captureErr
		},
	}
	statusline.Flags().StringVar(&stateFile, "state-file", "", "override the OpenUsage Cursor state file")
	cmd.AddCommand(statusline)
	return cmd
}
