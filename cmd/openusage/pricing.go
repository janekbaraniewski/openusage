package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/janekbaraniewski/openusage/internal/pricing"
)

func newPricingCommand() *cobra.Command {
	var (
		contextLen int
		jsonOutput bool
		timeout    time.Duration
	)
	cmd := &cobra.Command{
		Use:   "pricing <model>",
		Short: "Look up published per-million-token pricing for a model",
		Long: `pricing fetches model pricing from public sources (LiteLLM and OpenRouter),
caches the table on disk under the user cache directory, and prints the
resolved rates for the supplied model.

Examples:
  openusage pricing claude-3-5-sonnet
  openusage pricing gpt-4o --context 250000
  openusage pricing gemini-1.5-pro --json
`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
			defer cancel()

			resolver := pricing.DefaultResolver()
			p, err := resolver.Lookup(ctx, args[0], contextLen)
			if err != nil {
				return err
			}
			if jsonOutput {
				return writePricingJSON(cmd.OutOrStdout(), p)
			}
			return writePricingTable(cmd.OutOrStdout(), args[0], contextLen, p)
		},
	}
	cmd.Flags().IntVar(&contextLen, "context", 0, "Apply tiered pricing for this context length (input tokens)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Emit machine-readable JSON instead of a table")
	cmd.Flags().DurationVar(&timeout, "timeout", 15*time.Second, "Network timeout for fetching upstream pricing")
	return cmd
}

func writePricingJSON(w interface{ Write([]byte) (int, error) }, p *pricing.Price) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(p)
}

func writePricingTable(w interface{ Write([]byte) (int, error) }, query string, contextLen int, p *pricing.Price) error {
	out := os.Stdout
	if f, ok := w.(*os.File); ok {
		out = f
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "Query:\t%s\n", query)
	fmt.Fprintf(tw, "Resolved model:\t%s\n", p.ModelID)
	if p.Provider != "" {
		fmt.Fprintf(tw, "Provider:\t%s\n", p.Provider)
	}
	fmt.Fprintf(tw, "Source:\t%s\n", p.Source)
	if !p.LastUpdated.IsZero() {
		fmt.Fprintf(tw, "Last updated:\t%s\n", p.LastUpdated.Format(time.RFC3339))
	}
	if p.ContextWindow > 0 {
		fmt.Fprintf(tw, "Context window:\t%d tokens\n", p.ContextWindow)
	}
	if contextLen > 0 {
		fmt.Fprintf(tw, "Tier at context:\t%d tokens\n", contextLen)
	}
	fmt.Fprintln(tw, "----\tUSD per 1M tokens")
	fmt.Fprintf(tw, "Input:\t$%.4f\n", p.InputCostPerMillion)
	fmt.Fprintf(tw, "Output:\t$%.4f\n", p.OutputCostPerMillion)
	if p.CacheReadCostPerMillion > 0 {
		fmt.Fprintf(tw, "Cache read:\t$%.4f\n", p.CacheReadCostPerMillion)
	}
	if p.CacheWriteCostPerMillion > 0 {
		fmt.Fprintf(tw, "Cache write:\t$%.4f\n", p.CacheWriteCostPerMillion)
	}
	if p.ReasoningCostPerMillion > 0 {
		fmt.Fprintf(tw, "Reasoning:\t$%.4f\n", p.ReasoningCostPerMillion)
	}
	return tw.Flush()
}
