package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/skill"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

type listRow struct {
	Registry string
	Kind     string
	Name     string
	Tags     string
	Status   string
}

type loadedCatalog = catalog.LoadedCatalog

func newListCmd() *cobra.Command {
	var kindFilter string
	var fromURL string
	var fromRef string
	var fromPath string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List resources available from configured registries",
		Args: func(cmd *cobra.Command, args []string) error {
			fromURL = strings.TrimSpace(fromURL)
			fromRef = strings.TrimSpace(fromRef)
			fromPath = strings.TrimSpace(fromPath)
			if len(args) > 0 {
				return fmt.Errorf("list does not accept positional arguments")
			}
			if fromRef != "" && fromURL == "" {
				return fmt.Errorf("--ref requires --from")
			}
			if fromPath != "" && fromURL == "" {
				return fmt.Errorf("--path requires --from")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			errOut := cmd.ErrOrStderr()
			if fromURL != "" {
				return runListFrom(cmd.Context(), cmd.OutOrStdout(), errOut, fromURL, fromRef, fromPath, kindFilter)
			}
			return runList(cmd.Context(), cmd.OutOrStdout(), errOut, kindFilter)
		},
	}

	cmd.Flags().StringVar(&kindFilter, "kind", "", "Filter resources by kind: skill, command, subagent, hook, mcp, or collection")
	cmd.Flags().StringVar(&fromURL, "from", "", "List resources from an ad-hoc registry URL (git URL or GitLab/GitHub web URL)")
	cmd.Flags().StringVar(&fromRef, "ref", "", "Git ref (branch/tag/sha) for --from registry; requires --from")
	cmd.Flags().StringVar(&fromPath, "path", "", "Subdirectory within the --from registry; requires --from")

	return cmd
}

func runList(ctx context.Context, out io.Writer, errOut io.Writer, kindFilter string) error {
	kind, hasKind, err := parseOptionalKind(kindFilter)
	if err != nil {
		return err
	}

	repoRoot := config.FindRepoRoot()
	result := pipeline.Resolve(ctx, pipeline.ResolveOptions{RepoRoot: repoRoot})

	if len(result.TargetHarnesses) == 0 {
		if _, err := fmt.Fprintf(errOut, "No machine-wide target harness set configured; add `harnesses` to your user config.\n"); err != nil {
			return err
		}
	}

	for _, w := range result.Warnings {
		if _, werr := fmt.Fprintf(errOut, "warning: %v\n", w); werr != nil {
			return werr
		}
		var r resolver.Remediable
		if errors.As(w, &r) {
			if _, werr := fmt.Fprintf(errOut, "  -> %s\n", r.Remediation()); werr != nil {
				return werr
			}
		}
	}

	for _, err := range result.ResolveErrors {
		if resolver.IsWarning(err) || resolver.IsSoftSelectedNotFound(err) || resolver.IsSoftSelectedMalformed(err) {
			if _, werr := fmt.Fprintf(errOut, "warning: %v\n", err); werr != nil {
				return werr
			}
			if r := warningRemediation(err); r != nil {
				if _, werr := fmt.Fprintf(errOut, "  → %s\n", r.Remediation()); werr != nil {
					return werr
				}
			}
		}
	}
	for _, err := range result.CatalogErrors {
		if catalog.IsOverlapWarning(err) {
			if _, werr := fmt.Fprintf(errOut, "warning: %v\n", err); werr != nil {
				return werr
			}
		}
	}

	if len(result.Errors) > 0 {
		return errors.Join(result.Errors...)
	}

	rows := buildListRows(result.LoadedCfg, result.CatalogLoad.Catalogs, result.Resolved)
	if hasKind {
		rows = filterListRowsByKind(rows, skill.KindLabel(kind))
	}
	if err := writeListTable(out, rows); err != nil {
		return err
	}

	// Use the resolved scope policy for dry-run drift detection.
	modes, err := config.ResolveMaterializationModes(result.GlobalCfg, result.RepoCfg, result.LocalCfg)
	if err != nil {
		return err
	}
	dryRunReport, dryRunErr := materialize.Run(context.Background(), repoRoot, result.Resolved, materialize.WithDryRun(), materialize.WithMaterializationModes(modes))
	if dryRunErr != nil {
		// A failed drift probe must not hide the listing itself, but file mode
		// returns manifest and containment problems as hard errors that would
		// otherwise vanish here.
		slog.Warn("list drift check failed", "error", dryRunErr)
		if _, err := fmt.Fprintf(errOut, "warning: could not check sync state: %v\n", dryRunErr); err != nil {
			return err
		}
	}
	if dryRunReport != nil && !dryRunReport.InSync() {
		pending := dryRunReport.TotalWritten() + dryRunReport.TotalRemoved()
		if _, err := fmt.Fprintf(out, "%d resources out of sync — run 'agent-marketplace sync'\n", pending); err != nil {
			return err
		}
	}

	return nil
}

// runListFrom lists resources available at an ad-hoc registry URL without
// writing to any config. This is a browse-before-you-install feature.
func runListFrom(ctx context.Context, out io.Writer, errOut io.Writer, fromURL, fromRef, fromPath, kindFilter string) error {
	kind, hasKind, err := parseOptionalKind(kindFilter)
	if err != nil {
		return err
	}

	// Resolve web URLs into git URL + ref + path. Explicit flags win.
	fromURL, fromRef, fromPath, _ = pipeline.ResolveFromURL(fromURL, fromRef, fromPath)

	// Validate the ad-hoc URL against the effective allowlist before any
	// network access. LoadEffective also validates the existing configured
	// layers and returns their resolved allowlist.
	ec := config.LoadEffective(ctx, config.LoadEffectiveOptions{})
	config.LogManagedDiagnostics(ec.Diagnostics)
	for _, w := range ec.Warnings {
		fmt.Fprintf(errOut, "warning: %v\n", w) //nolint:errcheck // best-effort warning on stderr
	}
	syntheticEntry := config.RegistryEntry{URL: fromURL, Ref: fromRef, Path: fromPath}
	if err := config.ValidateRegistryURLs([]config.RegistryEntry{syntheticEntry}, ec.Allowed); err != nil {
		return err
	}

	// Build a synthetic config with just the --from entry to load catalogs.
	syntheticCfg := &config.Config{
		APIVersion: config.APIVersionV2,
		Registries: []config.RegistryEntry{syntheticEntry},
	}

	repoRoot := config.FindRepoRoot()
	result := catalog.LoadRegistriesWithContext(ctx, syntheticCfg, repoRoot, catalog.LoadModeRefresh)
	// Separate overlap warnings (soft) from hard catalog errors.
	var hardCatalogErrs []error
	for _, err := range result.Errors {
		if catalog.IsOverlapWarning(err) {
			fmt.Fprintf(errOut, "warning: %v\n", err) //nolint:errcheck // best-effort warning on stderr
			continue
		}
		hardCatalogErrs = append(hardCatalogErrs, err)
	}
	if len(hardCatalogErrs) > 0 {
		return errors.Join(hardCatalogErrs...)
	}

	displayName := registryDisplayName(fromURL)

	rows := make([]listRow, 0)
	for _, catalogForEntry := range result.Catalogs {
		for _, discovered := range catalogForEntry.Entries {
			if hasKind && discovered.Kind != kind {
				continue
			}
			rows = append(rows, listRow{
				Registry: displayName,
				Kind:     skill.KindLabel(discovered.Kind),
				Name:     discovered.Name,
				Tags:     strings.Join(discovered.Tags, ","),
				Status:   "available",
			})
		}
		for _, malformed := range catalogForEntry.Malformed {
			rows = append(rows, listRow{
				Registry: displayName,
				Kind:     "skill",
				Name:     malformed.DirName,
				Tags:     "",
				Status:   fmt.Sprintf("malformed: %v", malformed.Error),
			})
		}
	}

	if hasKind {
		rows = filterListRowsByKind(rows, skill.KindLabel(kind))
	}
	if len(rows) == 0 {
		if hasKind {
			_, err := fmt.Fprintf(out, "No %ss found at %s\n", skill.KindLabel(kind), fromURL)
			return err
		}
		_, err := fmt.Fprintf(out, "No resources found at %s\n", fromURL)
		return err
	}

	return writeListTable(out, rows)
}

func buildListRows(cfg *config.Config, catalogs map[string]loadedCatalog, resolved []resolver.ResolvedArtifact) []listRow {
	enabled := make(map[string]struct{}, len(resolved))
	for _, resolvedSkill := range resolved {
		enabled[resolvedSkill.CatalogKey+"\x00"+skill.ResourceKey(resolvedSkill.Kind, resolvedSkill.Name)] = struct{}{}
	}

	// Collapse registry entries that normalize to the same (URL, ref, path) — e.g.
	// the global config referring to "<repo>.git" and the repo config
	// referring to "<repo>" both resolve to the same upstream registry.
	// Without this merge, every skill shows up twice in the list output
	// (once per entry). Required status wins over non-required when merging.
	// Path is included so entries with different subdirectories stay separate.
	type mergedRegistry struct {
		displayName string
		cacheKey    string
		required    bool
	}
	mergedEntries := make([]mergedRegistry, 0, len(cfg.Registries))
	byNormalizedKey := make(map[string]int, len(cfg.Registries))

	repoRoot := config.FindRepoRoot()

	for _, entry := range cfg.Registries {
		var normalizedKey string
		if entry.IsLocal() {
			localPath, _ := entry.ResolveLocalPath(repoRoot)
			normalizedKey = "local\x00" + entry.BundlePath(localPath)
		} else {
			cleanPath := filepath.Clean(strings.TrimSpace(entry.Path))
			if cleanPath == "." {
				cleanPath = ""
			}
			normalizedKey = config.NormalizeGitURL(entry.URL) + "\x00" + entry.Ref + "\x00" + cleanPath
		}

		if idx, ok := byNormalizedKey[normalizedKey]; ok {
			if entry.Required {
				mergedEntries[idx].required = true
			}
			continue
		}

		var catalogKey string
		if entry.IsLocal() {
			localPath, _ := entry.ResolveLocalPath(repoRoot)
			catalogKey = entry.BundlePath(localPath)
		} else {
			catalogKey = cache.CatalogKeyForEntry(entry.URL, entry.Ref, entry.Path)
		}

		byNormalizedKey[normalizedKey] = len(mergedEntries)
		mergedEntries = append(mergedEntries, mergedRegistry{
			displayName: registryDisplayName(entry.URL),
			cacheKey:    catalogKey,
			required:    entry.Required,
		})
	}

	rows := make([]listRow, 0)
	emittedNormalizedKeys := make(map[string]struct{}, len(mergedEntries))
	for _, entry := range cfg.Registries {
		var normalizedKey string
		var entryCatalogKey string
		if entry.IsLocal() {
			localPath, _ := entry.ResolveLocalPath(repoRoot)
			normalizedKey = "local\x00" + entry.BundlePath(localPath)
			entryCatalogKey = entry.BundlePath(localPath)
		} else {
			cleanPath := filepath.Clean(strings.TrimSpace(entry.Path))
			if cleanPath == "." {
				cleanPath = ""
			}
			normalizedKey = config.NormalizeGitURL(entry.URL) + "\x00" + entry.Ref + "\x00" + cleanPath
			entryCatalogKey = cache.CatalogKeyForEntry(entry.URL, entry.Ref, entry.Path)
		}

		if _, emitted := emittedNormalizedKeys[normalizedKey]; emitted {
			continue
		}
		emittedNormalizedKeys[normalizedKey] = struct{}{}

		idx := byNormalizedKey[normalizedKey]
		merged := mergedEntries[idx]
		catalog := catalogs[merged.cacheKey]
		for _, discovered := range catalog.Entries {
			status := "available"
			if merged.required {
				status = "required (org)"
			} else if _, ok := enabled[entryCatalogKey+"\x00"+skill.ResourceKey(discovered.Kind, discovered.Name)]; ok {
				status = "enabled"
			}

			rows = append(rows, listRow{
				Registry: merged.displayName,
				Kind:     skill.KindLabel(discovered.Kind),
				Name:     discovered.Name,
				Tags:     strings.Join(discovered.Tags, ","),
				Status:   status,
			})
		}

		for _, malformed := range catalog.Malformed {
			rows = append(rows, listRow{
				Registry: merged.displayName,
				Kind:     "skill",
				Name:     malformed.DirName,
				Tags:     "",
				Status:   fmt.Sprintf("malformed: %v", malformed.Error),
			})
		}
	}

	slices.SortFunc(rows, func(a, b listRow) int {
		if a.Registry != b.Registry {
			return strings.Compare(a.Registry, b.Registry)
		}
		if a.Kind != b.Kind {
			return strings.Compare(a.Kind, b.Kind)
		}
		if a.Name != b.Name {
			return strings.Compare(a.Name, b.Name)
		}
		if a.Tags != b.Tags {
			return strings.Compare(a.Tags, b.Tags)
		}
		return strings.Compare(a.Status, b.Status)
	})

	return rows
}

func writeListTable(out io.Writer, rows []listRow) error {
	writer := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(writer, "REGISTRY\tKIND\tNAME\tTAGS (metadata)\tSTATUS"); err != nil {
		return err
	}

	for _, row := range rows {
		if _, err := fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\n", row.Registry, row.Kind, row.Name, row.Tags, row.Status); err != nil {
			return err
		}
	}

	return writer.Flush()
}

func parseOptionalKind(value string) (ir.ResourceKind, bool, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false, nil
	}
	kind, ok := skill.ParseKind(value)
	if !ok {
		return 0, false, fmt.Errorf("unknown kind %q: must be one of skill, command, subagent, hook, mcp, collection", value)
	}
	return kind, true, nil
}

func filterListRowsByKind(rows []listRow, kind string) []listRow {
	filtered := rows[:0]
	for _, row := range rows {
		if row.Kind == kind {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func registryDisplayName(url string) string {
	trimmed := strings.TrimSuffix(strings.TrimRight(url, "/"), ".git")
	lastSeparator := strings.LastIndexAny(trimmed, "/:")
	if lastSeparator == -1 {
		return trimmed
	}

	return trimmed[lastSeparator+1:]
}
