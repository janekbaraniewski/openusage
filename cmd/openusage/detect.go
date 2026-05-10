package main

import (
	"fmt"
	"io"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/detect"
	"github.com/janekbaraniewski/openusage/internal/providers"
)

// newDetectCommand returns the `openusage detect` cobra subcommand. It runs
// the full credential auto-detection pipeline (without persisting anything)
// and prints a human-readable report of:
//
//   - tools discovered on this workstation,
//   - accounts and where each credential was sourced from,
//   - providers we know how to handle but have no credential for yet.
//
// Tokens are masked. Use this command to debug "why doesn't openusage see
// my key?" before opening an issue.
func newDetectCommand() *cobra.Command {
	var showAll bool
	cmd := &cobra.Command{
		Use:   "detect",
		Short: "Run the credential auto-detection pipeline and print a report",
		Long: `Runs the same auto-detection logic openusage uses on startup and prints
what it found, including which file, env var, or keychain entry each
credential came from. Tokens are masked. Nothing is written to disk.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			result := detect.AutoDetect()
			detect.ApplyCredentials(&result)
			return printDetectReport(os.Stdout, result, showAll)
		},
	}
	cmd.Flags().BoolVar(&showAll, "all", false,
		"include providers with no credentials in the report")
	return cmd
}

func printDetectReport(out io.Writer, result detect.Result, showAll bool) error {
	// Tools section.
	fmt.Fprintln(out, "Tools detected:")
	if len(result.Tools) == 0 {
		fmt.Fprintln(out, "  (none)")
	} else {
		w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		for _, t := range result.Tools {
			fmt.Fprintf(w, "  %s\t%s\t%s\n", t.Name, t.Type, t.BinaryPath)
		}
		_ = w.Flush()
	}

	// Accounts section.
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Accounts detected:")
	if len(result.Accounts) == 0 {
		fmt.Fprintln(out, "  (none)")
	} else {
		// Sort by provider then account ID for stable output.
		sorted := append([]core.AccountConfig(nil), result.Accounts...)
		sort.Slice(sorted, func(i, j int) bool {
			if sorted[i].Provider != sorted[j].Provider {
				return sorted[i].Provider < sorted[j].Provider
			}
			return sorted[i].ID < sorted[j].ID
		})
		w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "  PROVIDER\tACCOUNT\tAUTH\tCREDENTIAL\tSOURCE")
		for _, a := range sorted {
			fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t%s\n",
				a.Provider, a.ID, displayAuth(a), displayCredential(a), displaySource(a))
		}
		_ = w.Flush()
	}

	// Coverage section.
	fmt.Fprintln(out)
	missing := providersWithoutAccount(result.Accounts)
	if len(missing) > 0 {
		fmt.Fprintln(out, "No credentials found for:")
		for _, p := range missing {
			fmt.Fprintf(out, "  - %s\n", p)
		}
	} else {
		fmt.Fprintln(out, "Every registered provider has at least one account.")
	}

	if showAll {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "All registered providers:")
		for _, p := range providers.AllProviders() {
			fmt.Fprintf(out, "  - %s\n", p.ID())
		}
	}
	return nil
}

// displayAuth returns the visible auth-mode label for a row.
func displayAuth(a core.AccountConfig) string {
	if a.Auth == "" {
		return "-"
	}
	return a.Auth
}

// displayCredential returns a one-word indicator of where the secret lives:
// a masked Token if we have one, the env-var name we'll resolve at fetch time,
// or "-" for accounts that don't carry a secret (CLI/local providers).
func displayCredential(a core.AccountConfig) string {
	if a.Token != "" {
		return detect.MaskKey(a.Token)
	}
	if a.APIKeyEnv != "" {
		if value := os.Getenv(a.APIKeyEnv); value != "" {
			return fmt.Sprintf("$%s=%s", a.APIKeyEnv, detect.MaskKey(value))
		}
		return fmt.Sprintf("$%s (unset)", a.APIKeyEnv)
	}
	return "-"
}

// displaySource returns the credential_source hint, falling back to "-".
func displaySource(a core.AccountConfig) string {
	if v := a.Hint("credential_source", ""); v != "" {
		return v
	}
	if a.APIKeyEnv != "" {
		return "env"
	}
	return "-"
}

// providersWithoutAccount returns the list of provider IDs registered in the
// global registry that have no detected account.
func providersWithoutAccount(accounts []core.AccountConfig) []string {
	have := make(map[string]struct{}, len(accounts))
	for _, a := range accounts {
		have[a.Provider] = struct{}{}
	}
	var missing []string
	for _, p := range providers.AllProviders() {
		if _, ok := have[p.ID()]; !ok {
			missing = append(missing, p.ID())
		}
	}
	sort.Strings(missing)
	return missing
}
