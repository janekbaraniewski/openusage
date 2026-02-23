package main

import (
	"fmt"
	"io"
	"log"
	"os"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/spf13/cobra"
)

func main() {
	if os.Getenv("OPENUSAGE_DEBUG") != "" {
		log.SetOutput(os.Stderr)
	} else {
		log.SetOutput(io.Discard)
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		fmt.Fprintf(os.Stderr, "Config path: %s\n", config.ConfigPath())
		os.Exit(1)
	}

	root := cobra.Command{
		Use:   "openusage",
		Short: "OpenUsage is a terminal dashboard for monitoring AI coding tool usage and spend.",
		Run: func(_ *cobra.Command, _ []string) {
			RunDashboard(cfg)
		},
	}

	root.AddCommand(NewTelemetryCommand())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
