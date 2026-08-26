package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
)

func newSearchCmd() *cobra.Command {
	var marketplaceFlag string
	var kindFilter string
	var output string
	var limit int
	var offset int

	cmd := &cobra.Command{
		Use:   "search [query]",
		Short: "Search resources across configured Marketplace endpoints",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSearchContext(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args, marketplaceFlag, kindFilter, output, limit, offset)
		},
	}

	cmd.Flags().StringVar(&marketplaceFlag, "marketplace", "", "Marketplace alias, URL, or 'all' (default: all configured)")
	cmd.Flags().StringVar(&kindFilter, "kind", "", "Filter by resource kind: skill, command, subagent, hook, mcp, or collection")
	cmd.Flags().StringVar(&output, "output", "table", "Output format: table or json")
	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum results to return (server default 100)")
	cmd.Flags().IntVar(&offset, "offset", 0, "Result offset for pagination")

	return cmd
}

func runSearchContext(ctx context.Context, out io.Writer, errOut io.Writer, args []string, marketplaceFlag, kindFilter, output string, limit, offset int) error {
	query := strings.Join(args, " ")

	ec := config.LoadEffective(ctx, config.LoadEffectiveOptions{})
	config.LogManagedDiagnostics(ec.Diagnostics)
	for _, w := range ec.Warnings {
		slog.Warn("config warning during search", "warning", w)
		fmt.Fprintf(errOut, "warning: %v\n", w) //nolint:errcheck // best-effort stderr warning
	}
	if len(ec.Errors) > 0 {
		return errors.Join(ec.Errors...)
	}

	kind, hasKind, err := parseOptionalKind(kindFilter)
	if err != nil {
		return err
	}
	apiKind := ""
	if hasKind {
		apiKind = mpapi.FromIR(kind)
	}

	switch output {
	case "table", "json":
	default:
		return fmt.Errorf("invalid output %q: must be 'table' or 'json'", output)
	}
	if limit < 0 {
		return fmt.Errorf("invalid limit %d", limit)
	}
	if offset < 0 {
		return fmt.Errorf("invalid offset %d", offset)
	}

	results, endpointErrs, err := marketplace.FederatedSearch(ctx, ec, query, marketplace.SearchOpts{
		Marketplace: marketplaceFlag,
		Kind:        apiKind,
		Limit:       limit,
		Offset:      offset,
		UserAgent:   "agent-marketplace/" + version,
	})
	if err != nil {
		return err
	}

	explicitSingle := marketplaceFlag != "" && marketplaceFlag != "all"
	if explicitSingle && len(endpointErrs) > 0 {
		// A sole requested endpoint failure is surfaced directly.
		return endpointErrs[0]
	}

	if len(endpointErrs) > 0 {
		for _, e := range endpointErrs {
			slog.Warn("marketplace search endpoint failed", "marketplace", e.Marketplace, "url", e.URL, "error", e.Err)
			fmt.Fprintf(errOut, "warning: %s: %v\n", e.Marketplace, e.Err) //nolint:errcheck
		}
	}

	if err := writeSearchOutput(out, output, results, endpointErrs); err != nil {
		return err
	}

	if len(results) == 0 && len(endpointErrs) > 0 {
		errs := make([]error, len(endpointErrs))
		for i, e := range endpointErrs {
			errs[i] = e
		}
		return errors.Join(errs...)
	}
	return nil
}

func writeSearchOutput(out io.Writer, output string, results []marketplace.SearchResult, endpointErrs []marketplace.EndpointError) error {
	switch output {
	case "json":
		return writeSearchJSON(out, results, endpointErrs)
	default:
		return writeSearchTable(out, results)
	}
}

func writeSearchJSON(out io.Writer, results []marketplace.SearchResult, errs []marketplace.EndpointError) error {
	type jsonError struct {
		Marketplace string `json:"marketplace"`
		URL         string `json:"url"`
		Error       string `json:"error"`
	}
	type payload struct {
		Results []marketplace.SearchResult `json:"results"`
		Errors  []jsonError                `json:"errors,omitempty"`
	}
	p := payload{Results: results}
	for _, e := range errs {
		p.Errors = append(p.Errors, jsonError{
			Marketplace: e.Marketplace,
			URL:         e.URL,
			Error:       e.Err.Error(),
		})
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(p)
}

func writeSearchTable(out io.Writer, results []marketplace.SearchResult) error {
	if len(results) == 0 {
		_, err := fmt.Fprintln(out, "No resources found.")
		return err
	}

	writer := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(writer, "MARKETPLACE\tSOURCE\tKIND\tNAME\tSCORE\tDESCRIPTION"); err != nil {
		return err
	}
	for _, r := range results {
		description := r.Description
		if description == "" && len(r.Metadata) > 0 {
			description = firstMetadataValue(r.Metadata, "license", "tags")
		}
		if _, err := fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%.2g\t%s\n",
			marketplace.QuoteTerminal(r.Marketplace),
			marketplace.QuoteTerminal(r.SourceSlug),
			marketplace.QuoteTerminal(r.Kind),
			marketplace.QuoteTerminal(r.Name),
			r.Score,
			marketplace.QuoteTerminal(description),
		); err != nil {
			return err
		}
	}
	return writer.Flush()
}

func firstMetadataValue(m map[string]string, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != "" {
			return v
		}
	}
	return ""
}
