package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/detect"
	"github.com/janekbaraniewski/openusage/internal/integrations"
	"github.com/spf13/cobra"
)

func newIntegrationsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "integrations",
		Aliases: []string{"int"},
		Short:   "Manage tool integrations (hooks, plugins)",
		Long:    "List, install, upgrade, and uninstall integrations for supported AI coding tools.",
	}

	cmd.AddCommand(newIntegrationsListCommand())
	cmd.AddCommand(newIntegrationsInstallCommand())
	cmd.AddCommand(newIntegrationsUninstallCommand())
	cmd.AddCommand(newIntegrationsUpgradeCommand())

	return cmd
}

func newIntegrationsListCommand() *cobra.Command {
	var showAll bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List integration statuses",
		RunE: func(_ *cobra.Command, _ []string) error {
			dirs := integrations.NewDefaultDirs()
			defs := integrations.AllDefinitions()
			detected := detect.AutoDetect()
			matches := integrations.MatchDetected(defs, detected, dirs)

			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tNAME\tSTATE\tVERSION\tSUMMARY")
			for _, m := range matches {
				if !showAll && !m.Actionable && m.Status.State == "missing" {
					continue
				}
				ver := m.Status.InstalledVersion
				if ver == "" {
					ver = "-"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
					m.Definition.ID,
					m.Definition.Name,
					m.Status.State,
					ver,
					m.Status.Summary,
				)
			}
			return w.Flush()
		},
	}

	cmd.Flags().BoolVarP(&showAll, "all", "a", false, "show all integrations, including undetected ones")
	return cmd
}

func newIntegrationsInstallCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "install <id>",
		Short: "Install an integration",
		Long:  "Install a hook or plugin for a supported tool. Use 'integrations list --all' to see available IDs.",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			id := integrations.ID(args[0])
			def, ok := integrations.DefinitionByID(id)
			if !ok {
				return fmt.Errorf("unknown integration %q; run 'openusage integrations list --all' to see options", id)
			}

			dirs := integrations.NewDefaultDirs()
			result, err := integrations.Install(def, dirs)
			if err != nil {
				return fmt.Errorf("install %s: %w", id, err)
			}

			if err := config.SaveIntegrationState(string(id), config.IntegrationState{
				Installed: true,
				Version:   result.InstalledVer,
			}); err != nil {
				fmt.Fprintf(os.Stderr, "warning: integration installed but failed to save state: %v\n", err)
			}

			fmt.Printf("%s %s (version %s)\n", def.Name, result.Action, result.InstalledVer)
			fmt.Printf("  template: %s\n", result.TemplateFile)
			fmt.Printf("  config:   %s\n", result.ConfigFile)
			return nil
		},
	}
}

func newIntegrationsUninstallCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall <id>",
		Short: "Uninstall an integration",
		Long:  "Remove a hook or plugin and unregister it from the target tool's config.",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			id := integrations.ID(args[0])
			def, ok := integrations.DefinitionByID(id)
			if !ok {
				return fmt.Errorf("unknown integration %q", id)
			}

			dirs := integrations.NewDefaultDirs()
			if err := integrations.Uninstall(def, dirs); err != nil {
				return fmt.Errorf("uninstall %s: %w", id, err)
			}

			if err := config.SaveIntegrationState(string(id), config.IntegrationState{
				Installed: false,
			}); err != nil {
				fmt.Fprintf(os.Stderr, "warning: integration uninstalled but failed to save state: %v\n", err)
			}

			fmt.Printf("%s uninstalled\n", def.Name)
			return nil
		},
	}
}

func newIntegrationsUpgradeCommand() *cobra.Command {
	var upgradeAll bool

	cmd := &cobra.Command{
		Use:   "upgrade [id]",
		Short: "Upgrade an integration to the latest embedded version",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			dirs := integrations.NewDefaultDirs()

			if upgradeAll {
				return upgradeAllIntegrations(dirs)
			}

			if len(args) == 0 {
				return fmt.Errorf("provide an integration id or use --all")
			}

			id := integrations.ID(args[0])
			def, ok := integrations.DefinitionByID(id)
			if !ok {
				return fmt.Errorf("unknown integration %q", id)
			}

			result, err := integrations.Upgrade(def, dirs)
			if err != nil {
				return fmt.Errorf("upgrade %s: %w", id, err)
			}

			if err := config.SaveIntegrationState(string(id), config.IntegrationState{
				Installed: true,
				Version:   result.InstalledVer,
			}); err != nil {
				fmt.Fprintf(os.Stderr, "warning: integration upgraded but failed to save state: %v\n", err)
			}

			fmt.Printf("%s upgraded (%s -> %s)\n", def.Name, result.PreviousVer, result.InstalledVer)
			return nil
		},
	}

	cmd.Flags().BoolVar(&upgradeAll, "all", false, "upgrade all installed integrations")
	return cmd
}

func upgradeAllIntegrations(dirs integrations.Dirs) error {
	defs := integrations.AllDefinitions()
	upgraded := 0
	for _, def := range defs {
		st := def.Detector(dirs)
		if !st.NeedsUpgrade {
			continue
		}
		result, err := integrations.Upgrade(def, dirs)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error upgrading %s: %v\n", def.ID, err)
			continue
		}
		if err := config.SaveIntegrationState(string(def.ID), config.IntegrationState{
			Installed: true,
			Version:   result.InstalledVer,
		}); err != nil {
			fmt.Fprintf(os.Stderr, "warning: %s upgraded but failed to save state: %v\n", def.ID, err)
		}
		fmt.Printf("%s upgraded (%s -> %s)\n", def.Name, result.PreviousVer, result.InstalledVer)
		upgraded++
	}
	if upgraded == 0 {
		fmt.Println("all integrations are up to date")
	}
	return nil
}
