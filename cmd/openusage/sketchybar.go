package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/spf13/cobra"

	"github.com/janekbaraniewski/openusage/internal/sketchybar"
)

func newSketchybarCommand(configs ...config.Config) *cobra.Command {
	cfg := config.DefaultConfig()
	switch {
	case len(configs) > 0:
		cfg = configs[0]
	default:
		// Loaded here rather than in main: the trigger names are flag
		// defaults, so they must be known at construction time, and an
		// unreadable config must not stop unrelated subcommands from
		// running. A failed load simply leaves the built-in defaults.
		if loaded, err := config.Load(); err == nil {
			cfg = loaded
		}
	}
	opts := sketchybar.InstallOptions{
		UsageTrigger:    cfg.SketchyBar.UsageTrigger,
		SwitcherTrigger: cfg.SketchyBar.SwitcherTrigger,
	}

	cmd := &cobra.Command{
		Use:   "sketchybar",
		Short: "Install or print the OpenUsage SketchyBar integration",
		Long: `Wire the active-provider bar item, detail popup, and provider switcher
into sketchybarrc. Scripts are installed under ~/.local/share/openusage and
the managed block never writes into ~/.config/sketchybar/plugins.

With no subcommand, print the complete copy-pasteable managed block. Use
` + "`openusage sketchybar install --write`" + ` to apply it automatically.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			_, err := sketchybar.Install(os.Stdout, opts)
			return err
		},
	}

	fl := cmd.PersistentFlags()
	fl.StringVar(&opts.Preset, "preset", sketchybar.DefaultPreset, "embedded SketchyBar preset")
	fl.StringVar(&opts.Binary, "binary", "", "override the openusage binary path used by generated scripts")
	fl.StringVar(&opts.ConfigPath, "config", "", "override the sketchybarrc path")
	fl.StringVar(&opts.DataDir, "data-dir", "", "override the neutral generated-script directory")
	fl.StringVar(&opts.UsageTrigger, "usage-trigger", opts.UsageTrigger, "gesture that opens the usage popup (click|hover)")
	fl.StringVar(&opts.SwitcherTrigger, "switcher-trigger", opts.SwitcherTrigger, "gesture that opens the provider picker (click|hover)")
	fl.BoolVar(&opts.Write, "write", false, "apply the managed block and generated scripts")

	install := &cobra.Command{
		Use:   "install",
		Short: "Install the managed block and generated scripts",
		Long: `Write the managed block into sketchybarrc and generate the scripts.

Requires --write. Without it the block is printed so it can be reviewed
before anything on disk is touched; sketchybarrc is a file people hand-edit,
and an install subcommand should not rewrite it as a side effect of being
run without arguments.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			_, err := sketchybar.Install(os.Stdout, opts)
			return err
		},
	}
	uninstall := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the managed block from sketchybarrc",
		RunE: func(_ *cobra.Command, _ []string) error {
			return sketchybar.Uninstall(os.Stdout, opts.ConfigPath)
		},
	}
	doctor := &cobra.Command{
		Use:   "doctor",
		Short: "Check SketchyBar config, scripts, and executables",
		RunE: func(_ *cobra.Command, _ []string) error {
			return sketchybar.Doctor(os.Stdout, sketchybar.DoctorOptions{
				ConfigPath:      opts.ConfigPath,
				DataDir:         opts.DataDir,
				Binary:          opts.Binary,
				UsageTrigger:    opts.UsageTrigger,
				SwitcherTrigger: opts.SwitcherTrigger,
			})
		},
	}
	presets := newSketchybarPresetsCommand()
	cmd.AddCommand(install, uninstall, doctor, presets)
	return cmd
}

func newSketchybarPresetsCommand() *cobra.Command {
	var show string
	cmd := &cobra.Command{
		Use:   "presets",
		Short: "List the built-in SketchyBar presets",
		RunE: func(_ *cobra.Command, _ []string) error {
			if strings.TrimSpace(show) != "" {
				preset, err := sketchybar.SamplePreset(show)
				if err != nil {
					return err
				}
				return json.NewEncoder(os.Stdout).Encode(preset)
			}
			fmt.Fprintf(os.Stdout, "%-24s %-8s %s\n", "NAME", "INTERVAL", "DESCRIPTION")
			for _, preset := range sketchybar.Presets() {
				fmt.Fprintf(os.Stdout, "%-24s %-8ds %s\n", preset.Name, preset.UpdateFreq, preset.Description)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&show, "show", "", "dump a single preset as JSON")
	return cmd
}
