package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/skill"
)

type statusRow struct {
	Kind     string
	Name     string
	Harness  string
	Target   string
	Registry string
	Ref      string
	Cache    string
	Source   config.Source
	Via      string
}

type orphanedSymlinkRow struct {
	Name   string
	Target string
}

type excludedRow struct {
	Name     string
	Registry string
	Ref      string
}

type localRegistryRow struct {
	URL  string
	Ref  string
	Path string
}

type collisionRow struct {
	Name    string
	Details string
}

type errorRow struct {
	Message string
}

type requirementRow struct {
	Resource    string
	Requirement string
	Remediation string
}

type statusView struct {
	diag                config.ManagedInstallationDiagnostics
	warnings            []error
	statusRows          []statusRow
	locallyExcluded     []excludedRow
	localRegistries     []localRegistryRow
	orphaned            []orphanedSymlinkRow
	collisions          []collisionRow
	requirements        []requirementRow
	errorRows           []errorRow
	dryRunReport        *materialize.Report
	targetHarnesses     []string
	targetSource        config.Source
	harnessTargets      []harnessTargetRow
	lockMetadata        []pipeline.LockMetadata
	marketplaceStatuses []marketplace.PinStatus
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the current sync state for enabled skills",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(cmd.Context(), cmd.OutOrStdout(), time.Now())
		},
	}
}

var runStatusMaterialize = materialize.Run

func runStatus(ctx context.Context, out io.Writer, now time.Time) error {
	if err := prepareDependencyTransactions(ctx, true); err != nil {
		return err
	}
	repoRoot := config.FindRepoRoot()

	ws, err := pipeline.NewWorkspace(repoRoot, config.FindLocalConfigDir())
	if err != nil {
		return err
	}
	// Use the resolved scope policy for dry-run drift detection. status exists
	// to explain misconfiguration, so an invalid mode or a failed drift probe
	// is reported as an error row rather than suppressing every other
	// diagnostic. File mode returns manifest and containment problems here as
	// hard errors.
	inspection, releaseExactLease := pipeline.Inspect(ctx, pipeline.InspectOptions{
		Resolve: pipeline.ResolveOptions{
			Workspace:          ws,
			CatalogMode:        catalog.LoadModeInspect,
			ReadOnlyExactLocks: true,
			// Marketplace selections resolve into artifacts from the verified
			// local cache, so the desired set matches sync --check without
			// downloading artifacts or writing files.
			MarketplaceArtifactsFromCache: true,
		},
		DryRun:                         runStatusMaterialize,
		SkipMaterializeOnResolveErrors: true,
	})
	defer releaseExactLease()
	result := inspection.ResolveResult
	dryRunReport := inspection.Report

	catalogEntries := make(map[string][]catalog.Entry, len(result.CatalogLoad.Catalogs))
	for k, v := range result.CatalogLoad.Catalogs {
		catalogEntries[k] = v.Entries
	}

	cacheFreshness := make(map[string]string)
	if result.LoadedCfg != nil {
		for _, entry := range result.LoadedCfg.Registries {
			catalogKey := entry.CatalogKey
			if entry.IsLocal() {
				cacheFreshness[catalogKey] = "local"
			} else {
				cacheFreshness[catalogKey] = describeCacheFreshness(catalogKey, now)
			}
		}
	}
	if inspection.MaterializeErr != nil {
		slog.Warn("status drift check failed", "error", inspection.MaterializeErr)
	}

	// Build orphaned rows from files that would be removed.
	orphaned := buildOrphanedRows(dryRunReport)

	var locallyExcluded []excludedRow
	var localRegistries []localRegistryRow
	var secondResolveRealErrs []error
	localCfg := result.LocalCfg
	if localCfg != nil {
		anyExcludes := len(localCfg.Skills.Exclude) > 0 ||
			len(localCfg.Commands.Exclude) > 0 ||
			len(localCfg.Subagents.Exclude) > 0 ||
			len(localCfg.Hooks.Exclude) > 0 ||
			len(localCfg.MCP.Exclude) > 0 ||
			len(localCfg.Collections.Exclude) > 0
		if anyExcludes && result.LoadedCfg != nil {
			// Re-resolve keeping local Skills but dropping Excludes to compute the diff.
			localNoExclude := &config.LocalConfig{
				Skills:      config.KindSelector{Include: localCfg.Skills.Include},
				Commands:    config.KindSelector{Include: localCfg.Commands.Include},
				Subagents:   config.KindSelector{Include: localCfg.Subagents.Include},
				Hooks:       config.KindSelector{Include: localCfg.Hooks.Include},
				MCP:         config.KindSelector{Include: localCfg.MCP.Include},
				Collections: config.KindSelector{Include: localCfg.Collections.Include},
			}
			secondResolveOpts := []resolver.ResolveOption{
				resolver.WithContext(ctx),
				resolver.WithResources(result.CatalogLoad.ResourceCatalogs),
				resolver.WithMalformed(catalog.MalformedByKey(result.CatalogLoad.Catalogs)),
				resolver.WithTargetHarnesses(result.TargetHarnesses),
			}
			// The second resolve keeps the Marketplace view consistent with the
			// primary one: when the pipeline resolved Marketplace artifacts
			// from cache, the excluded diff must see the same artifacts. The
			// metadata-only inspection remains the fallback for callers that
			// did not opt in.
			if result.MarketplaceArtifactResolver != nil {
				secondResolveOpts = append(secondResolveOpts, resolver.WithMarketplaceResolver(result.MarketplaceArtifactResolver))
			} else if result.MarketplaceInspection != nil {
				secondResolveOpts = append(secondResolveOpts, resolver.WithMarketplaceResolver(*result.MarketplaceInspection))
			}
			resolvedWithoutExcludes, secondResolveErrs := resolver.Resolve(result.LoadedCfg, catalogEntries, localNoExclude, secondResolveOpts...)
			for _, e := range secondResolveErrs {
				if !resolver.IsWarning(e) && !resolver.IsSoftSelectedNotFound(e) && !resolver.IsSoftSelectedMalformed(e) &&
					!pipeline.IsMarketplaceCacheMetadataMissingWarning(e) {
					secondResolveRealErrs = append(secondResolveRealErrs, e)
				}
			}
			locallyExcluded = buildExcludedRows(resolvedWithoutExcludes, result.Resolved)
		}

		if len(localCfg.Registries) > 0 {
			localRegistries = make([]localRegistryRow, 0, len(localCfg.Registries))
			for _, entry := range localCfg.Registries {
				localRegistries = append(localRegistries, localRegistryRow{
					URL:  entry.URL,
					Ref:  entry.Ref,
					Path: entry.Path,
				})
			}
			slices.SortFunc(localRegistries, func(a, b localRegistryRow) int {
				if c := cmp.Compare(a.URL, b.URL); c != 0 {
					return c
				}
				if c := cmp.Compare(a.Ref, b.Ref); c != 0 {
					return c
				}
				return cmp.Compare(a.Path, b.Path)
			})
		}
	}

	statusRows := buildStatusRows(repoRoot, result.Resolved, cacheFreshness, dryRunReport)
	applyExactLockFreshness(statusRows, result.LockMetadata)
	// A Marketplace pin that failed to resolve is already reported as a hard
	// error naming the pin. Its dependents must not also be told to select a
	// resource that is selected; the resolution failure is the actionable fact.
	pendingMarketplacePins := pipeline.MarketplacePendingPins(result.ResolveErrors)
	requirementRows, resolveErrs := splitRequirementWarnings(
		pipeline.SuppressUnmetMarketplaceRequirements(result.ResolveErrors, pendingMarketplacePins))
	collisionRows := splitResolveErrors(resolveErrs)

	// Convert overlap warnings to collision rows so they appear in the
	// NAME COLLISIONS section, not ERRORS. Hard catalog errors are already
	// captured in result.Errors after pipeline.Resolve filters out overlap
	// warnings, so we do not need to duplicate them here.
	for _, err := range result.CatalogErrors {
		if catalog.IsOverlapWarning(err) {
			collisionRows = append(collisionRows, collisionRow{
				Name:    "overlap",
				Details: err.Error(),
			})
		}
	}

	inspectionErrors := make([]error, 0, len(inspection.Issues))
	for _, issue := range inspection.Issues {
		if issue.Severity != pipeline.IssueError {
			continue
		}
		if issue.Category == pipeline.IssueMaterialization && pipeline.IsPromotedDiagnostic(issue.Err) {
			continue
		}
		inspectionErrors = append(inspectionErrors, issue.Err)
	}
	errorRows := buildErrorRows(inspectionErrors, secondResolveRealErrs)

	targetHarnesses := make([]string, len(result.TargetHarnesses))
	for i, t := range result.TargetHarnesses {
		targetHarnesses[i] = string(t)
	}
	slices.Sort(targetHarnesses)
	harnessTargets, err := buildHarnessTargetRows(repoRoot, targetHarnesses)
	if err != nil {
		return fmt.Errorf("resolve harness target roots: %w", err)
	}

	var statusWarnings []error
	for _, issue := range inspection.Issues {
		if issue.Severity == pipeline.IssueWarning {
			statusWarnings = append(statusWarnings, issue.Err)
		}
	}

	if err := writeStatusSections(out, statusView{
		diag:                result.Diagnostics,
		warnings:            statusWarnings,
		statusRows:          statusRows,
		locallyExcluded:     locallyExcluded,
		localRegistries:     localRegistries,
		orphaned:            orphaned,
		collisions:          collisionRows,
		requirements:        requirementRows,
		errorRows:           errorRows,
		dryRunReport:        dryRunReport,
		targetHarnesses:     targetHarnesses,
		targetSource:        result.TargetHarnessSource,
		harnessTargets:      harnessTargets,
		lockMetadata:        result.LockMetadata,
		marketplaceStatuses: result.MarketplaceStatuses,
	}); err != nil {
		return err
	}

	var failureErrs []error
	for _, issue := range inspection.Issues {
		if issue.Severity == pipeline.IssueError {
			failureErrs = append(failureErrs, issue.Err)
		}
	}
	return errors.Join(failureErrs...)
}

func splitRequirementWarnings(errs []error) ([]requirementRow, []error) {
	rows := make([]requirementRow, 0)
	other := make([]error, 0, len(errs))
	for _, err := range errs {
		var warning *resolver.UnmetRequirementWarning
		if !errors.As(err, &warning) {
			other = append(other, err)
			continue
		}
		rows = append(rows, requirementRow{
			Resource:    fmt.Sprintf("%s:%s", skill.KindLabel(warning.ResourceKind), warning.ResourceName),
			Requirement: fmt.Sprintf("%s:%s", skill.KindLabel(warning.Requirement.Kind), warning.Requirement.Name),
			Remediation: warning.Remediation(),
		})
	}
	slices.SortFunc(rows, func(a, b requirementRow) int {
		if result := cmp.Compare(a.Resource, b.Resource); result != 0 {
			return result
		}
		return cmp.Compare(a.Requirement, b.Requirement)
	})
	return rows, other
}

// describeCacheFreshness returns a human-readable string describing
// how fresh the cache directory is.
func describeCacheFreshness(catalogKey string, now time.Time) string {
	// Strip the synthetic ":HASH" suffix for path-specific entries so
	// we stat the real cache directory on disk.
	cachePath := cache.BaseCachePath(catalogKey)
	info, err := os.Stat(cachePath)
	if err != nil || !info.IsDir() {
		return "missing"
	}

	if now.Sub(info.ModTime()) > staleCacheThreshold {
		return "stale"
	}

	return fmt.Sprintf("last fetched: %s", info.ModTime().UTC().Format(time.RFC3339))
}

const staleCacheThreshold = 24 * time.Hour

func applyExactLockFreshness(rows []statusRow, metadata []pipeline.LockMetadata) {
	for i := range rows {
		rowURL, rowErr := canonicalRegistryURL(rows[i].Registry)
		for _, locked := range metadata {
			lockedURL, lockedErr := canonicalRegistryURL(locked.URL)
			if rows[i].Source == locked.Source && rows[i].Ref == locked.Ref && rowErr == nil && lockedErr == nil && rowURL == lockedURL {
				rows[i].Cache = "locked: " + locked.Commit
				break
			}
		}
	}
}

func buildStatusRows(repoRoot string, resolved []resolver.ResolvedArtifact, cacheFreshness map[string]string, report *materialize.Report) []statusRow {
	blocked := make(map[materialize.BlockedResource]bool)
	if report != nil {
		for _, resource := range report.BlockedResources {
			blocked[resource] = true
		}
	}
	// A diagnostic without a blocked identity may arise later, during writing
	// or aggregate reconciliation. Attribute it only when exactly one resolved
	// row matches its name and (if provided) harness target. Ambiguous errors
	// remain visible in ERRORS, without accusing unrelated rows.
	fallback := make(map[int]bool)
	if report != nil {
		for _, d := range report.Diags {
			if !d.IsError() || d.Resource == "" {
				continue
			}
			known := false
			for _, identity := range report.BlockedResources {
				if identity.Name == d.Resource && (d.Target == "" || string(identity.Target) == d.Target) {
					known = true
					break
				}
			}
			if known {
				continue
			}
			candidate := -1
			for i, artifact := range resolved {
				if artifact.Name != d.Resource && artifact.InstalledName != d.Resource {
					continue
				}
				if d.Target != "" && !slices.Contains(statusHarnesses(artifact), hcc.Target(d.Target)) {
					continue
				}
				if candidate >= 0 {
					candidate = -1
					break
				}
				candidate = i
			}
			if candidate >= 0 {
				fallback[candidate] = true
			}
		}
	}

	rows := make([]statusRow, 0, len(resolved))

	for i, resolvedArtifact := range resolved {
		cacheKey := resolvedArtifact.CatalogKey
		if cacheKey == "" {
			cacheKey = cache.RegistryCachePath(resolvedArtifact.RegistryURL, resolvedArtifact.Ref)
		}

		target := resolvedArtifact.Path
		name := resolvedArtifact.Name
		if resolvedArtifact.IsMarketplace && resolvedArtifact.InstalledName != "" {
			name = resolvedArtifact.InstalledName
		}
		if fallback[i] || slices.ContainsFunc(statusHarnesses(resolvedArtifact), func(target hcc.Target) bool {
			return blocked[materialize.BlockedResource{Kind: resolvedArtifact.Kind, Name: name, Target: target, MachineScope: resolvedArtifact.Source.IsMachineScope()}]
		}) {
			target = "error: not materialized"
		}

		rows = append(rows, statusRow{
			Kind:     skill.KindLabel(resolvedArtifact.Kind),
			Name:     resolvedArtifact.Name,
			Harness:  harnessLabel(resolvedArtifact.Harnesses),
			Target:   target,
			Registry: resolvedArtifact.RegistryURL,
			Ref:      resolvedArtifact.Ref,
			Cache:    cacheFreshness[cacheKey],
			Source:   resolvedArtifact.Source,
			Via:      resolvedArtifact.Via,
		})
	}

	slices.SortFunc(rows, func(a, b statusRow) int {
		if kindCmp := cmp.Compare(a.Kind, b.Kind); kindCmp != 0 {
			return kindCmp
		}
		if nameCmp := cmp.Compare(a.Name, b.Name); nameCmp != 0 {
			return nameCmp
		}
		if registryCmp := cmp.Compare(a.Registry, b.Registry); registryCmp != 0 {
			return registryCmp
		}
		return cmp.Compare(a.Ref, b.Ref)
	})

	return rows
}

func statusHarnesses(artifact resolver.ResolvedArtifact) []hcc.Target {
	if artifact.Harnesses == nil {
		return []hcc.Target{hcc.TargetClaude}
	}
	return artifact.Harnesses
}

func buildOrphanedRows(report *materialize.Report) []orphanedSymlinkRow {
	if report == nil {
		return nil
	}

	rows := make([]orphanedSymlinkRow, 0)
	for _, tr := range report.TargetReports() {
		for _, removed := range tr.Removed {
			rows = append(rows, orphanedSymlinkRow{
				Name:   artifactName(removed),
				Target: filepath.Join(tr.OutDir, removed),
			})
		}
	}

	slices.SortFunc(rows, func(a, b orphanedSymlinkRow) int {
		return cmp.Compare(a.Name, b.Name)
	})

	return rows
}

func artifactName(path string) string {
	parts := strings.Split(filepath.ToSlash(path), "/")
	if len(parts) >= 3 && parts[0] == "skills" {
		return parts[1]
	}
	return filepath.Base(path)
}

func splitResolveErrors(errs []error) []collisionRow {
	collisions := make([]collisionRow, 0)
	softErrors := make([]error, 0)

	for _, err := range errs {
		// Checked before the generic warning branch: resolver.IsWarning also
		// reports true for an ambiguity, and the generic branch would then
		// render the whole message as the row name instead of the member name.
		if resolver.IsCollectionMemberAmbiguity(err) {
			// A path-less collection member provided by several configured
			// locations of one repository is reported, not fatal.
			collisions = append(collisions, collisionRow{
				Name:    collectionMemberAmbiguityName(err),
				Details: err.Error(),
			})
			continue
		}
		if resolver.IsSoftSelectedNotFound(err) || resolver.IsWarning(err) {
			// Non-required "selected but not found", incompatible organization
			// defaults, and other resolve warnings are displayed in the
			// NAME COLLISIONS section rather than ERRORS.
			softErrors = append(softErrors, err)
			continue
		}
		var nce *resolver.NameCollisionError
		if !errors.As(err, &nce) {
			// Other hard resolve errors are reported through result.Errors.
			continue
		}

		collisions = append(collisions, collisionRow{
			Name:    nce.Name,
			Details: err.Error(),
		})
	}

	slices.SortFunc(collisions, func(a, b collisionRow) int {
		return cmp.Compare(a.Name, b.Name)
	})

	// Merge soft errors into collision rows so they appear in the WARNINGS section.
	for _, err := range softErrors {
		collisions = append(collisions, collisionRow{
			Name:    selectedNotFoundName(err),
			Details: selectedNotFoundDetails(err),
		})
	}

	return collisions
}

// collectionMemberAmbiguityName returns the resource name to show in the
// WARNINGS table for an ambiguous path-less collection member.
func collectionMemberAmbiguityName(err error) string {
	var ambiguous *resolver.AmbiguousCollectionMemberWarning
	if errors.As(err, &ambiguous) {
		return ambiguous.Name
	}
	return err.Error()
}

func selectedNotFoundName(err error) string {
	var snf *resolver.SelectedNotFoundError
	if errors.As(err, &snf) {
		return snf.Name
	}
	return err.Error()
}

// selectedNotFoundDetails returns a detail string for a SelectedNotFoundError,
// including the collection provenance when available.
func selectedNotFoundDetails(err error) string {
	var snf *resolver.SelectedNotFoundError
	if !errors.As(err, &snf) {
		return err.Error()
	}
	if snf.Via != "" {
		return fmt.Sprintf("%s (referenced by %s)", snf.Error(), snf.Via)
	}
	return snf.Error()
}

func buildErrorRows(groups ...[]error) []errorRow {
	rows := make([]errorRow, 0)
	for _, group := range groups {
		for _, err := range group {
			if err == nil {
				continue
			}

			rows = append(rows, errorRow{Message: err.Error()})
		}
	}

	slices.SortFunc(rows, func(a, b errorRow) int {
		return cmp.Compare(a.Message, b.Message)
	})

	return rows
}

func buildExcludedRows(withoutExcludes, withExcludes []resolver.ResolvedArtifact) []excludedRow {
	included := make(map[string]struct{}, len(withExcludes))
	for _, s := range withExcludes {
		included[resolver.ViaMapKey(s.Kind, s.Name)] = struct{}{}
	}

	var rows []excludedRow
	for _, s := range withoutExcludes {
		if _, ok := included[resolver.ViaMapKey(s.Kind, s.Name)]; !ok {
			rows = append(rows, excludedRow{
				Name:     s.Name,
				Registry: s.RegistryURL,
				Ref:      s.Ref,
			})
		}
	}

	slices.SortFunc(rows, func(a, b excludedRow) int {
		if cmp := strings.Compare(a.Name, b.Name); cmp != 0 {
			return cmp
		}
		if cmp := strings.Compare(a.Registry, b.Registry); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.Ref, b.Ref)
	})

	return rows
}

// harnessTargetRow describes one selected harness target: what it can do and
// where it writes.
//
// Partitioned targets use named root summaries so status never invents a
// primary root for a layout that has none.
type harnessTargetRow struct {
	Target       string
	Capabilities string
	RepoRoot     string
	MachineRoot  string
}

// buildHarnessTargetRows renders the effective harness list from the
// authoritative target registry. An unknown harness name is shown rather than
// hidden: status exists to explain misconfiguration.
func buildHarnessTargetRows(repoRoot string, harnesses []string) ([]harnessTargetRow, error) {
	if len(harnesses) == 0 {
		return nil, nil
	}
	legacyRoots := harness.MakeInstallRoots()
	seen := make(map[string]bool, len(harnesses))
	rows := make([]harnessTargetRow, 0, len(harnesses))
	for _, name := range harnesses {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		row := harnessTargetRow{Target: name, Capabilities: targetCapabilities(name), RepoRoot: "-", MachineRoot: "-"}
		if definition, ok := targetmeta.Lookup(name); ok {
			row.Target = targetDisplayName(name)
			if definition.PartitionedRoots || len(harness.NativeAggregateInstalls(hcc.Target(name))) > 0 {
				repoSummary, machineSummary, err := harnessRootSummaries(hcc.Target(name), repoRoot)
				if err != nil {
					return nil, err
				}
				row.RepoRoot = repoSummary
				row.MachineRoot = machineSummary
			} else if root, exists := legacyRoots[hcc.Target(name)]; exists {
				row.RepoRoot = root.For(false, repoRoot)
				row.MachineRoot = root.For(true, repoRoot)
			}
		}
		rows = append(rows, row)
	}
	slices.SortFunc(rows, func(a, b harnessTargetRow) int { return cmp.Compare(a.Target, b.Target) })
	return rows, nil
}

func harnessRootSummaries(target hcc.Target, repoRoot string) (string, string, error) {
	summaries := make([]string, 0, 2)
	for _, machine := range []bool{false, true} {
		destinations, err := harness.ResolveDestinations(target, machine, repoRoot)
		if err != nil {
			if !errors.Is(err, fsboundary.ErrUnsafePath) && !errors.Is(err, fsboundary.ErrUnsafeFile) {
				return "", "", err
			}
			summaries = append(summaries, fmt.Sprintf("unresolvable: %v", err))
		} else {
			summaries = append(summaries, destinationSummary(destinations))
		}
	}
	return summaries[0], summaries[1], nil
}

func destinationSummary(destinations []harness.Destination) string {
	if len(destinations) == 1 && destinations[0].ID == "default" {
		return destinations[0].Root
	}
	parts := make([]string, 0, len(destinations))
	for _, destination := range destinations {
		root := destination.Root
		if destination.AggregateFile != "" {
			root = filepath.Join(root, destination.AggregateFile)
		}
		parts = append(parts, destination.ID+"="+root)
	}
	return strings.Join(parts, ", ")
}

func harnessTargetRowsToCells(rows []harnessTargetRow) [][]string {
	cells := make([][]string, 0, len(rows))
	for _, row := range rows {
		cells = append(cells, []string{row.Target, row.Capabilities, row.RepoRoot, row.MachineRoot})
	}
	return cells
}

func writeStatusSections(out io.Writer, view statusView) error {
	diag := view.diag
	warnings := view.warnings
	statusRows := view.statusRows
	locallyExcluded := view.locallyExcluded
	localRegistries := view.localRegistries
	orphaned := view.orphaned
	collisions := view.collisions
	requirements := view.requirements
	errorRows := slices.Clone(view.errorRows)
	dryRunReport := view.dryRunReport
	if dryRunReport != nil {
		for _, diagnostic := range dryRunReport.Diags {
			if diagnostic.IsError() {
				errorRows = append(errorRows, errorRow{Message: pipeline.PromoteDiagnostic(diagnostic).Error()})
			}
		}
		slices.SortFunc(errorRows, func(a, b errorRow) int { return cmp.Compare(a.Message, b.Message) })
	}
	targetHarnesses := view.targetHarnesses
	targetSource := view.targetSource
	harnessTargets := view.harnessTargets
	lockMetadata := view.lockMetadata
	marketplaceStatuses := view.marketplaceStatuses

	if diag.Managed() {
		if _, err := fmt.Fprintln(out, "MANAGED INSTALLATION"); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(out, "true"); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "path: %s\n", diag.Path); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "validation: %s\n", diag.Validation); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(out); err != nil {
			return err
		}
	}

	displayTargets := make([]string, len(targetHarnesses))
	for i, target := range targetHarnesses {
		displayTargets[i] = targetDisplayName(target)
	}
	targetHarnessStr := strings.Join(displayTargets, ", ")
	if targetHarnessStr == "" {
		targetHarnessStr = "none configured"
	}
	if err := writeStatusTable(out, "TARGET HARNESS SET", []string{"TARGET HARNESSES", "SOURCE"}, [][]string{{targetHarnessStr, targetSource.String()}}); err != nil {
		return err
	}
	if len(targetHarnesses) == 0 {
		if _, err := fmt.Fprintln(out, "  -> add harnesses to ~/.agents/config.yaml"); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(out); err != nil {
			return err
		}
	}

	if len(lockMetadata) > 0 {
		if err := writeStatusTable(out, "EXACT REGISTRY LOCKS", []string{"SOURCE", "REGISTRY", "REF", "COMMIT", "SNAPSHOT"}, lockMetadataRowsToCells(lockMetadata)); err != nil {
			return err
		}
	}

	if err := writeStatusTable(out, "SYNCED RESOURCES", []string{"KIND", "NAME", "HARNESS", "SOURCE", "VIA", "TARGET", "REGISTRY", "REF", "CACHE"}, statusRowsToCells(statusRows)); err != nil {
		return err
	}

	if len(marketplaceStatuses) > 0 {
		if err := writeStatusTable(out, "MARKETPLACE PINS", marketplaceStatusHeaders(), marketplaceStatusRowsToCells(marketplaceStatuses)); err != nil {
			return err
		}
	}

	if len(harnessTargets) > 0 {
		if err := writeStatusTable(out, "HARNESS TARGETS", []string{"TARGET", "CAPABILITIES", "REPO ROOT", "MACHINE ROOT"}, harnessTargetRowsToCells(harnessTargets)); err != nil {
			return err
		}
	}

	if len(locallyExcluded) > 0 {
		if err := writeStatusTable(out, "LOCALLY EXCLUDED", []string{"NAME", "REGISTRY", "REF"}, excludedRowsToCells(locallyExcluded)); err != nil {
			return err
		}
	}

	if len(localRegistries) > 0 {
		if err := writeStatusTable(out, "LOCAL REGISTRIES", []string{"URL", "REF", "PATH"}, localRegistryRowsToCells(localRegistries)); err != nil {
			return err
		}
	}

	if len(orphaned) > 0 {
		if err := writeStatusTable(out, "ORPHANED FILES", []string{"NAME", "TARGET"}, orphanedRowsToCells(orphaned)); err != nil {
			return err
		}
	}

	if len(collisions) > 0 {
		if err := writeStatusTable(out, "NAME COLLISIONS", []string{"NAME", "DETAILS"}, collisionRowsToCells(collisions)); err != nil {
			return err
		}
	}

	if len(requirements) > 0 {
		if err := writeStatusTable(out, "UNMET REQUIREMENTS", []string{"RESOURCE", "REQUIRES", "FIX"}, requirementRowsToCells(requirements)); err != nil {
			return err
		}
	}

	if len(warnings) > 0 {
		if err := writeStatusWarnings(out, warnings); err != nil {
			return err
		}
	}

	// DRIFT section
	if _, err := fmt.Fprintln(out, "DRIFT"); err != nil {
		return err
	}
	if dryRunReport == nil {
		if _, err := fmt.Fprintln(out, "Unknown (exact registry view incomplete)"); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(out); err != nil {
			return err
		}
	} else {
		if dryRunReport.InSync() {
			if _, err := fmt.Fprintln(out, "In sync"); err != nil {
				return err
			}
		} else {
			parts := []string{}
			if dryRunReport.TotalWritten() > 0 {
				parts = append(parts, fmt.Sprintf("%d file(s) to write", dryRunReport.TotalWritten()))
			}
			if dryRunReport.TotalRemoved() > 0 {
				parts = append(parts, fmt.Sprintf("%d file(s) to remove", dryRunReport.TotalRemoved()))
			}
			if _, err := fmt.Fprintln(out, strings.Join(parts, ", ")); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(out); err != nil {
			return err
		}
	}

	if len(errorRows) > 0 {
		if err := writeStatusTable(out, "ERRORS", []string{"ERROR"}, errorRowsToCells(errorRows)); err != nil {
			return err
		}
	}

	return nil
}

func writeStatusWarnings(out io.Writer, warnings []error) error {
	type dedupKeyer interface{ DedupKey() string }
	seen := make(map[string]struct{})

	if _, err := fmt.Fprintln(out, "WARNINGS"); err != nil {
		return err
	}
	for _, w := range warnings {
		key := w.Error()
		var dk dedupKeyer
		if errors.As(w, &dk) {
			key = dk.DedupKey()
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		if _, err := fmt.Fprintf(out, "warning: %s\n", terminalSafeText(w.Error())); err != nil {
			return err
		}
		var r resolver.Remediable
		if errors.As(w, &r) {
			if _, err := fmt.Fprintf(out, "  -> %s\n", terminalSafeText(r.Remediation())); err != nil {
				return err
			}
		}
	}
	_, err := fmt.Fprintln(out)
	return err
}

func writeStatusTable(out io.Writer, title string, header []string, rows [][]string) error {
	if _, err := fmt.Fprintln(out, title); err != nil {
		return err
	}

	writer := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(writer, strings.Join(header, "\t")); err != nil {
		return err
	}

	for _, row := range rows {
		cells := make([]string, len(row))
		for i, cell := range row {
			cells[i] = terminalSafeText(cell)
		}
		if _, err := fmt.Fprintln(writer, strings.Join(cells, "\t")); err != nil {
			return err
		}
	}

	if err := writer.Flush(); err != nil {
		return err
	}

	_, err := fmt.Fprintln(out)
	return err
}

func lockMetadataRowsToCells(rows []pipeline.LockMetadata) [][]string {
	cells := make([][]string, 0, len(rows))
	for _, row := range rows {
		snapshot := "missing"
		if row.SnapshotAvailable {
			snapshot = "available"
		}
		cells = append(cells, []string{row.Source.String(), row.URL, row.Ref, row.Commit, snapshot})
	}
	return cells
}

func statusRowsToCells(rows []statusRow) [][]string {
	cells := make([][]string, 0, len(rows))
	for _, row := range rows {
		via := row.Via
		if via == "" {
			via = "-"
		}
		cells = append(cells, []string{row.Kind, row.Name, row.Harness, row.Source.String(), via, row.Target, row.Registry, row.Ref, row.Cache})
	}

	return cells
}

func harnessLabel(targets []hcc.Target) string {
	if targets == nil {
		return string(hcc.TargetClaude)
	}
	if len(targets) == 0 {
		return "-"
	}
	labels := make([]string, 0, len(targets))
	for _, target := range targets {
		labels = append(labels, targetDisplayName(string(target)))
	}
	slices.Sort(labels)
	return strings.Join(labels, ",")
}

func targetDisplayName(name string) string {
	if name != string(targetmeta.TargetCodex) {
		return name
	}
	definition, ok := targetmeta.Lookup(name)
	if !ok {
		return name
	}
	return definition.Label
}

func orphanedRowsToCells(rows []orphanedSymlinkRow) [][]string {
	cells := make([][]string, 0, len(rows))
	for _, row := range rows {
		cells = append(cells, []string{row.Name, row.Target})
	}

	return cells
}

func collisionRowsToCells(rows []collisionRow) [][]string {
	cells := make([][]string, 0, len(rows))
	for _, row := range rows {
		cells = append(cells, []string{row.Name, row.Details})
	}

	return cells
}

func requirementRowsToCells(rows []requirementRow) [][]string {
	cells := make([][]string, 0, len(rows))
	for _, row := range rows {
		cells = append(cells, []string{row.Resource, row.Requirement, row.Remediation})
	}
	return cells
}

func errorRowsToCells(rows []errorRow) [][]string {
	cells := make([][]string, 0, len(rows))
	for _, row := range rows {
		cells = append(cells, []string{row.Message})
	}

	return cells
}

func excludedRowsToCells(rows []excludedRow) [][]string {
	cells := make([][]string, 0, len(rows))
	for _, row := range rows {
		cells = append(cells, []string{row.Name, row.Registry, row.Ref})
	}

	return cells
}

func localRegistryRowsToCells(rows []localRegistryRow) [][]string {
	cells := make([][]string, 0, len(rows))
	for _, row := range rows {
		path := row.Path
		if path == "" {
			path = "-"
		}
		cells = append(cells, []string{row.URL, row.Ref, path})
	}

	return cells
}
