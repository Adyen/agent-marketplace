package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Adyen/agent-marketplace/internal/cache"
	pkgcatalog "github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/skill"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func newSyncCmd() *cobra.Command {
	var checkOnly bool
	var noBackup bool
	var strict bool
	var pruneStale bool
	var verbose bool
	var offline bool
	var locked bool
	var allowRevoked []string
	var policy resolver.MarketplaceRevocationPolicy

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync enabled skills into local skill directories",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if offline && pruneStale {
				return fmt.Errorf("--prune-stale cannot be used with --offline: cached snapshots must never drive destructive stale-selection pruning")
			}
			if locked && pruneStale {
				return fmt.Errorf("--prune-stale cannot be used with --locked: locked mode forbids config writes")
			}
			parsedPolicy, err := marketplace.NewRevocationOverridePolicy(allowRevoked)
			if err != nil {
				return fmt.Errorf("invalid --allow-revoked value: %w", err)
			}
			policy = parsedPolicy
			if checkOnly && len(allowRevoked) > 0 {
				return fmt.Errorf("--allow-revoked cannot be used with --check: revoked overrides are execution-only and checks must remain unhealthy")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			locked = locked || checkOnly || offline
			if checkOnly {
				return runSyncCheck(cmd.Context(), cmd.OutOrStdout(), strict, offline, locked)
			}
			return runSyncWithLocks(cmd.Context(), cmd.OutOrStdout(), noBackup, strict, pruneStale, verbose, offline, lockRunOptions{locked: locked, marketplacePolicy: policy})
		},
	}

	cmd.Flags().BoolVar(&checkOnly, "check", false, "Check drift without reconciling; exits 1 if out of sync")
	cmd.Flags().BoolVar(&noBackup, "no-backup", false, "Skip pre-sync backup of machine-scope harness directories")
	cmd.Flags().BoolVar(&strict, "strict", false, "Fail on unresolved selections ('selected but not found', 'found but failed to load') and on unmet prerequisites (exit 1)")
	cmd.Flags().BoolVar(&pruneStale, "prune-stale", false, "Auto-remove stale skill selections from config after successful sync")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Print per-file detail for each target and list malformed resources")
	cmd.Flags().BoolVar(&offline, "offline", false, "Use only trusted local registry and Marketplace caches; never fetch or contact remote endpoints")
	cmd.Flags().BoolVar(&locked, "locked", false, "Require fresh lock files and never update config or locks")
	cmd.Flags().StringArrayVar(&allowRevoked, "allow-revoked", nil, "Allow one exact revoked Marketplace pin for this invocation: marketplace/source/kind/name@sha256:<64 lowercase hex>")

	return cmd
}

type lockRunOptions struct {
	locked            bool
	marketplacePolicy resolver.MarketplaceRevocationPolicy
}

var errStrictUnmetPrerequisites = errors.New("unmet prerequisites reported as warnings; select the prerequisite resources or run without --strict")

// discoverSyncWorkspace is the single Workspace discovery point for sync and
// sync-check. It runs before lock preparation so one validated value carries
// the repository, local-config, and materialization roots through locks,
// effective config, stale-selection inspection, dry-run materialization, and
// real materialization. It is a package var so tests can observe ordering and
// reuse.
var discoverSyncWorkspace = pipeline.DiscoverWorkspace
var ensureGlobalConfigForSync = config.EnsureGlobalConfig

var runSyncCheckMaterialize = materialize.Run

func runSyncWithLocks(ctx context.Context, out io.Writer, noBackup bool, strict bool, pruneStale bool, verbose bool, offline bool, lockOpts ...lockRunOptions) error {
	locked := len(lockOpts) > 0 && lockOpts[0].locked
	var marketplacePolicy resolver.MarketplaceRevocationPolicy
	if len(lockOpts) > 0 {
		marketplacePolicy = lockOpts[0].marketplacePolicy
	}
	ws, err := discoverSyncWorkspace()
	if err != nil {
		return err
	}
	if !offline && !locked {
		if err := ensureGlobalConfigForSync(); err != nil {
			slog.Warn("failed to create global config skeleton", "error", err)
		}
	}

	repoRoot := ws.RepoRoot()
	localDir := ws.LocalConfigDir()
	var lockWarnings []error
	exactOption, _, exactLease, err := prepareRuntimeLocks(ctx, repoRoot, localDir, lockMode{locked: locked || offline, offline: offline, warningSink: &lockWarnings, marketplacePolicy: marketplacePolicy})
	if err != nil {
		style := newSyncOutputStyle(out)
		if summaryErr := writeSyncSummary(out, style, 0, 0, 0, 1); summaryErr != nil {
			return errors.Join(err, summaryErr)
		}
		if pruneStale {
			labels := configuredSelectionLabels(ctx, repoRoot, localDir)
			kept := "configured selections"
			if len(labels) > 0 {
				kept = fmt.Sprintf("%d configured selection(s): %s", len(labels), strings.Join(labels, ", "))
			}
			if _, outputErr := fmt.Fprintf(out, "prune-stale skipped: the catalog could not be trusted this run (1 catalog/resolve error(s)), so %s were kept\n", kept); outputErr != nil {
				return errors.Join(err, outputErr)
			}
		}
		return err
	}
	resolveOpts := pipeline.ResolveOptions{Workspace: ws, NoBackup: noBackup, Offline: offline, CatalogOption: []pkgcatalog.LoadOption{exactOption}, ExactLockLease: exactLease, SkipInventory: pruneStale, MarketplaceRevocationPolicy: marketplacePolicy, MarketplaceLockEvidenceStrict: locked, MarketplaceReportArchiveEvidenceDrift: !locked && !offline}
	syncResult := pipeline.Sync(ctx, resolveOpts)
	syncResult.Warnings = append(lockWarnings, syncResult.Warnings...)
	errs := syncResult.Errors
	malformedCount := syncResult.CatalogLoad.MalformedCount
	mergedCfg := syncResult.MergedCfg
	resolvedCount := len(syncResult.Resolved)
	report := syncResult.Report

	var persistedRefreshes map[archiveRefreshKey]bool
	if !locked && !offline && len(errs) == 0 {
		var refreshErr error
		persistedRefreshes, refreshErr = refreshDriftedMarketplaceLocks(ctx, repoRoot, localDir, mergedCfg, syncResult.Warnings)
		if refreshErr != nil {
			errs = append(errs, fmt.Errorf("refresh Marketplace lock evidence: %w", refreshErr))
		}
	}
	syncResult.Warnings = stripUnpersistedRefreshWarnings(syncResult.Warnings, persistedRefreshes)

	// Collect stale selections before strict mode reclassifies them.
	// Only collect direct selections (not collection-sourced) for pruning.
	// Collection-sourced stale skills are left as warnings with enriched
	// remediation hints that point at the collection definition.
	var staleSelections []*resolver.SelectedNotFoundError
	if pruneStale {
		for _, w := range syncResult.Warnings {
			var snf *resolver.SelectedNotFoundError
			if errors.As(w, &snf) && !snf.Required && snf.Via == "" {
				staleSelections = append(staleSelections, snf)
			}
		}
		for _, e := range syncResult.ResolveErrors {
			var snf *resolver.SelectedNotFoundError
			if errors.As(e, &snf) && !snf.Required && snf.Via == "" {
				staleSelections = append(staleSelections, snf)
			}
		}
	}

	// In strict mode, re-elevate soft SelectedNotFound warnings to hard errors.
	// Present-but-malformed selections elevate too: they are equally unresolved,
	// and a strict caller (CI) wants the run to fail either way.
	unmetRequirements := false
	if strict {
		strictErrors := make([]error, 0, len(syncResult.Warnings))
		strictWarnings := make([]error, 0, len(syncResult.Warnings))
		for _, w := range syncResult.Warnings {
			if resolver.IsUnmetRequirement(w) {
				unmetRequirements = true
			}
			if resolver.IsSoftSelectedNotFound(w) || resolver.IsSoftSelectedMalformed(w) {
				strictErrors = append(strictErrors, w)
			} else {
				strictWarnings = append(strictWarnings, w)
			}
		}
		errs = append(errs, strictErrors...)
		syncResult.Warnings = strictWarnings
	}

	// Errors get the same dedup treatment warnings have always had. A single
	// failing resource can be reported once per harness target, and before bead
	// agent-marketplace-oc2r those repeats printed verbatim. dedupIssues keys on
	// DedupKey() where available, which for a promoted materialize diagnostic
	// INCLUDES the resource and its source location, so repeats collapse while
	// the same message from different resources remains a distinct counted issue.
	// Presentation grouping happens later and never changes this count.
	errs = dedupIssues(errs)
	if unmetRequirements {
		errs = append(errs, errStrictUnmetPrerequisites)
	}

	totalWritten := 0
	totalRemoved := 0
	if report != nil {
		totalWritten = report.TotalWritten()
		totalRemoved = report.TotalRemoved()
	}

	style := newSyncOutputStyle(out)
	if err := writeSyncSummary(out, style, totalWritten, totalRemoved, malformedCount, len(errs)); err != nil {
		return err
	}

	// Show which output directories were written to so users know where files
	// landed. Default output lists each directory with its write/remove counts;
	// verbose output lists every affected file under each directory.
	if report != nil && (totalWritten > 0 || totalRemoved > 0) {
		if verbose {
			if err := printOutputDirDetail(out, report, false); err != nil {
				return err
			}
		} else if dirs := activeOutputDirs(report); len(dirs) > 0 {
			if _, err := fmt.Fprintf(out, "  → %s\n", strings.Join(dirs, ", ")); err != nil {
				return err
			}
		}
	}

	// Under verbose, list malformed resources with their path and parse error
	// so the user can fix them without re-running `status`.
	//
	// Default output suppresses per-resource parse details on purpose
	// (TestSyncCommandIncludesMalformedSkillsInSummary pins that suppression),
	// but a count with no way to act on it is what let a dropped resource look
	// like a deletion for so long — so name the command that explains it.
	if malformedCount > 0 {
		if _, err := fmt.Fprintln(out); err != nil {
			return err
		}
		if verbose {
			if err := printMalformedDetail(out, syncResult.CatalogLoad.Malformed); err != nil {
				return err
			}
		} else if err := printMalformedHint(out, malformedCount, style.warningPrefix()); err != nil {
			return err
		}
	}

	// Print actionable hint when no skills are selected but registries are configured
	if resolvedCount == 0 && mergedCfg != nil && len(mergedCfg.Registries) > 0 {
		if _, err := fmt.Fprintln(out); err != nil {
			return err
		}
		configSources := describeConfigSources(syncResult.OrgCfg, syncResult.GlobalCfg, syncResult.RepoCfg)
		if _, err := fmt.Fprintf(out, "No skills selected from %s. Add skills or tags to your config, or set required: true on a registry.\n", configSources); err != nil {
			return err
		}
	}

	if len(syncResult.Warnings) > 0 {
		if _, err := fmt.Fprintln(out); err != nil {
			return err
		}
		if err := writeSyncWarnings(out, syncResult.Warnings, verbose, style.warningPrefix()); err != nil {
			return err
		}
	}

	// Prune stale selections once the CATALOG is trustworthy — not once the run
	// is error-free. Removing a selection because a resource looks absent is
	// only safe if we actually read every registry: a catalog-load or resolve
	// failure can make a live resource look gone, so those still block pruning
	// entirely. A materialization failure cannot: it happens after the catalog
	// has been read and says nothing about what the registries contain.
	//
	// Bead agent-marketplace-tbjf: this used to gate on len(errs) == 0, so a
	// beta tester's three unrelated Devin compile errors silently disabled the
	// pruning of three selections that were verifiably gone upstream — and
	// printed nothing about it. One bug disabled the remediation for another.
	//
	// errs (which includes the compile failures and, under --strict, promoted
	// stale-selection warnings) still drives the exit code below: pruning
	// proceeding does not make the run successful.
	if pruneStale && len(staleSelections) > 0 {
		pruned, err := prunePhase(ctx, out, repoRoot, &syncResult, staleSelections)
		if err != nil {
			slog.Error("sync stale-prune failed", "repo_root", repoRoot, "strict", strict, "error", err)
			allErrs := append([]error{}, errs...)
			allErrs = append(allErrs, err)
			if remediationErr := printRemediations(out, errs); remediationErr != nil {
				allErrs = append(allErrs, remediationErr)
			}
			return errors.Join(allErrs...)
		}
		if pruned > 0 {
			slog.Info("sync stale selections pruned; starting re-sync",
				"repo_root", repoRoot,
				"count", pruned,
				"strict", strict,
			)
			// This pass's errors are about to be thrown away: the recursive
			// call below returns ITS error, and the framework only ever prints
			// the returned one. The summary line above already told the user
			// "Errors N", so without naming them here the run reports a count
			// of errors it never names — and when the re-sync is clean it
			// exits 0 having printed "Errors 1" with nothing to act on.
			// Pinned by TestSyncPruneStaleNamesTheErrorsItCounted.
			if err := printSupersededErrors(out, errs); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(out, "Pruned %d stale selection(s); re-syncing...\n", pruned); err != nil {
				return err
			}
			// The re-run is passed pruneStale=false, which is the whole
			// termination argument: the only call site that can recurse is
			// guarded by pruneStale, so at most ONE prune round can happen per
			// invocation no matter how many errors survive the re-sync. Pinned
			// by TestSyncPruneStaleReSyncTerminatesWhenErrorPersists.
			//
			// strict IS forwarded: the re-sync's exit code becomes the run's
			// exit code, so dropping it would let `--strict --prune-stale`
			// exit 0 while stale selections the prune deliberately cannot
			// remove (collection-sourced, or a failed config edit) are still
			// in the config — silently discarding the promotion the user
			// asked for. Pinned by TestSyncPruneStaleStrictSurvivesTheReSync.
			return runSyncWithLocks(ctx, out, noBackup, strict, false, verbose, offline, lockRunOptions{locked: locked, marketplacePolicy: marketplacePolicy})
		}
	}

	// A prune-capable first pass suppresses its provisional inventory. If no
	// mutation triggered the recursive authoritative sync, publish this result
	// as the command's single final generation now.
	if pruneStale {
		pipeline.RecordResolvedInventory(ctx, syncResult.ResolveResult)
	}

	if len(errs) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(out); err != nil {
		return err
	}
	return newSyncCommandError(errs, true, "error:")
}

func configuredSelectionLabels(ctx context.Context, repoRoot, localDir string) []string {
	intent := config.ReadIntent(ctx, config.LoadEffectiveOptions{
		RepoRoot: repoRoot, LocalConfigDir: localDir,
	})
	seen := make(map[string]struct{})
	for _, source := range []config.Source{config.Organization, config.UserPersonal, config.RepoCommitted, config.RepoPersonal} {
		layer := intent.Layers[source]
		if !layer.Present() || layer.Err() != nil {
			continue
		}
		for _, selection := range layer.ExplicitSelections() {
			seen[fmt.Sprintf("%s %q", skill.KindLabel(selection.Kind), selection.Name)] = struct{}{}
		}
	}
	labels := make([]string, 0, len(seen))
	for label := range seen {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	return labels
}

// dedupIssues drops later occurrences of the same underlying problem while
// preserving order.
//
// Both the warning and the error path use this one helper: warnings had it and
// errors did not, which is half of the bug in bead agent-marketplace-oc2r. The
// implementation lives in internal/pipeline, beside the DedupKey() it consumes,
// so the TUI collapses repeats identically instead of growing a second copy.
func dedupIssues(issues []error) []error { return pipeline.DedupIssues(issues) }

// remediationHint returns the user-facing remediation an error carries, or "".
// A promoted materialize diagnostic satisfies resolver.Remediable with
// diag.Diagnostic.Hint, so compiler-authored remediation is rendered by the
// same code path as resolver remediation instead of a parallel one.
func remediationHint(issue error) string {
	var remediable resolver.Remediable
	if !errors.As(issue, &remediable) {
		return ""
	}
	return remediable.Remediation()
}

// printRemediation renders one error's remediation hint, if it carries one, in
// the single "  → <hint>" style every surface uses. Used where the hint follows
// the line it belongs to, so repetition is attributable and stays.
func printRemediation(out io.Writer, issue error) error {
	hint := remediationHint(issue)
	if hint == "" {
		return nil
	}
	_, err := fmt.Fprintf(out, "  → %s\n", hint)
	return err
}

// printRemediations renders the remediation hints of a set of errors, printing
// identical advice once. Unlike the warning loop, the error messages themselves
// are printed afterwards by the CLI framework, so a hint here is not attached to
// a line: repeating the same sentence once per failing resource would add no
// attribution and no information, while the distinct error lines below it
// already name every resource that failed.
func printRemediations(out io.Writer, issues []error) error {
	printed := make(map[string]struct{}, len(issues))
	for _, issue := range issues {
		hint := remediationHint(issue)
		if hint == "" {
			continue
		}
		if _, dup := printed[hint]; dup {
			continue
		}
		printed[hint] = struct{}{}
		if _, err := fmt.Fprintf(out, "  → %s\n", hint); err != nil {
			return err
		}
	}
	return nil
}

func runSyncCheck(ctx context.Context, out io.Writer, strict bool, offline ...bool) error {
	isOffline := len(offline) > 0 && offline[0]
	ws, err := discoverSyncWorkspace()
	if err != nil {
		return err
	}
	repoRoot := ws.RepoRoot()
	localDir := ws.LocalConfigDir()
	var lockWarnings []error
	configLoadMode := pipeline.ConfigLoadStrict
	if !isOffline {
		configLoadMode = pipeline.ConfigLoadCached
	}
	exactOption, _, exactLease, err := prepareRuntimeLocks(ctx, repoRoot, localDir, lockMode{
		locked: true, offline: isOffline, check: true,
		allowTransientOrganization: !isOffline,
		warningSink:                &lockWarnings,
	})
	if err != nil {
		return err
	}
	// Resolve and dry-run materialization consume paths inside immutable exact
	// snapshots. Hold the lease across both operations and mark the context so
	// snapshot opens do not attempt a nested namespace acquisition.
	releaseExactLocks, err := exactLease(ctx)
	if err != nil {
		return fmt.Errorf("acquire and revalidate exact locks before sync check: %w", err)
	}
	defer releaseExactLocks()
	ctx = cache.ContextWithExactGitNamespaceLease(ctx)

	resolveOpts := pipeline.ResolveOptions{
		Workspace: ws, CatalogMode: pkgcatalog.LoadModeRefresh, Offline: isOffline,
		CatalogOption: []pkgcatalog.LoadOption{exactOption}, ExactLockLease: exactLease,
		ConfigLoadMode: configLoadMode, MarketplaceLockEvidenceStrict: true,
	}
	resolveOpts.RequireTargetSet = true
	// Use materialize in dry-run mode to check for drift; the Workspace derives
	// the same materialization root the real sync would write beneath.
	inspection, inspectRelease := pipeline.Inspect(ctx, pipeline.InspectOptions{
		Resolve: resolveOpts,
		DryRun:  runSyncCheckMaterialize,
	})
	defer inspectRelease()
	resolvedResult := inspection.ResolveResult
	report := inspection.Report
	matErr := inspection.MaterializeErr

	// Surface every warning the run produced, not just the ineffective-subdir
	// ones. --check used to recompute AncestorMaterializedExcludeWarnings and
	// print only those, which was both a duplicate (Resolve already puts them
	// in Warnings) and a silent drop of everything else: catalog overlaps, soft
	// selected-not-found, soft malformed selections and materialize warning
	// diagnostics never reached the user. A drift check that hides why output is
	// partial is worse than no check. Deduplicated exactly as `sync` does, so
	// one diagnostic affecting several harness targets prints once.
	//
	// Warnings do not affect drift detection or the --check exit code.
	// Concat keeps the separately collected lock and inspection warnings
	// independent before deduplication.
	var inspectionWarnings []error
	for _, issue := range inspection.Issues {
		if issue.Severity == pipeline.IssueWarning {
			inspectionWarnings = append(inspectionWarnings, issue.Err)
		}
	}
	allWarnings := slices.Concat(lockWarnings, inspectionWarnings)
	for _, warning := range dedupIssues(allWarnings) {
		if _, err := fmt.Fprintf(out, "warning: %v\n", warning); err != nil {
			return err
		}
		if err := printRemediation(out, warning); err != nil {
			return err
		}
	}

	var requirementWarnings []error
	for _, resolveErr := range resolvedResult.ResolveErrors {
		if resolver.IsUnmetRequirement(resolveErr) {
			requirementWarnings = append(requirementWarnings, resolveErr)
		}
	}
	if err := writeGroupedWarnings(out, requirementWarnings, false); err != nil {
		return err
	}
	checkErrs := slices.Clone(resolvedResult.Errors)
	if matErr != nil {
		checkErrs = append(checkErrs, matErr)
	}
	errorRows := buildErrorRows(checkErrs)

	summaryErr := writeSyncCheckSummary(out, report, errorRows, checkErrs)
	if strict && len(requirementWarnings) > 0 {
		return errors.Join(summaryErr, errStrictUnmetPrerequisites)
	}
	return summaryErr
}

// writeSyncCheckSummary renders the `sync --check` verdict and returns the
// error that becomes the command's non-zero exit.
//
// Report.InSync() is false for TWO independent reasons: pending file changes
// (missing/orphaned artifacts) and error-severity diagnostics. Only the first
// contributes to pendingCount, so an error-only report used to print
// "sync: 0 out of sync" followed by "0 skills out of sync" — a message
// indistinguishable from the zero-value-severity bug this command was just
// fixed for, and one that never named the failure. The two reasons are now
// reported separately, and an error diagnostic is always printed with its
// stable code.
//
// configErrs are the underlying resolve/config errors behind errorRows. They
// are returned verbatim for the configuration case rather than collapsed to a
// count, because that error is the only thing a caller (and `sync --check` in
// CI) sees: "1 configuration error(s)" would hide "no machine-wide target
// harness set configured", which is both the most common cause and the one with
// an actionable remedy.
func writeSyncCheckSummary(out io.Writer, report *materialize.Report, errorRows []errorRow, configErrs []error) error {
	inSync := report != nil && report.InSync()
	if inSync && len(errorRows) == 0 {
		return nil
	}

	pendingCount := 0
	var errorDiags []diag.Diagnostic
	if report != nil {
		pendingCount = report.TotalWritten() + report.TotalRemoved()
		for _, d := range report.Diags {
			if d.IsError() {
				errorDiags = append(errorDiags, d)
			}
		}
	}

	if pendingCount > 0 {
		if _, err := fmt.Fprintf(out, "sync: %d out of sync", pendingCount); err != nil {
			return err
		}

		parts := []string{}
		if report.TotalWritten() > 0 {
			parts = append(parts, fmt.Sprintf("%d missing", report.TotalWritten()))
		}
		if report.TotalRemoved() > 0 {
			parts = append(parts, fmt.Sprintf("%d orphaned", report.TotalRemoved()))
		}

		if len(parts) > 0 {
			if _, err := fmt.Fprintf(out, " (%s)", strings.Join(parts, ", ")); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(out); err != nil {
			return err
		}
		if err := printOutputDirDetail(out, report, true); err != nil {
			return err
		}
	}

	for _, d := range errorDiags {
		label := d.Code
		if d.Resource != "" {
			label = fmt.Sprintf("%s (%s)", d.Code, d.Resource)
		}
		if _, err := fmt.Fprintf(out, "error: %s: %s\n", label, d.Message); err != nil {
			return err
		}
	}

	for _, row := range errorRows {
		if _, err := fmt.Fprintf(out, "error: %s\n", row.Message); err != nil {
			return err
		}
	}

	switch {
	case pendingCount > 0:
		return fmt.Errorf("%d skills out of sync", pendingCount)
	case len(errorDiags) > 0:
		return fmt.Errorf("%d materialization error(s)", len(errorDiags))
	case len(configErrs) > 0:
		return errors.Join(configErrs...)
	default:
		return fmt.Errorf("%d configuration error(s)", len(errorRows))
	}
}

// activeOutputDirs returns a deduplicated, sorted list of output directories
// that had files written or removed during a sync, with the home directory
// abbreviated to "~" and per-directory write/remove counts appended.
func activeOutputDirs(report *materialize.Report) []string {
	type counts struct{ written, removed int }
	homeDir, _ := os.UserHomeDir()
	agg := make(map[string]*counts)
	var order []string
	for _, tr := range report.TargetReports() {
		if len(tr.Written) == 0 && len(tr.Removed) == 0 {
			continue
		}
		dir := abbreviateHome(tr.OutDir, homeDir)
		c, ok := agg[dir]
		if !ok {
			c = &counts{}
			agg[dir] = c
			order = append(order, dir)
		}
		c.written += len(tr.Written)
		c.removed += len(tr.Removed)
	}
	sort.Strings(order)
	dirs := make([]string, 0, len(order))
	for _, dir := range order {
		c := agg[dir]
		var parts []string
		if c.written > 0 {
			parts = append(parts, fmt.Sprintf("%d written", c.written))
		}
		if c.removed > 0 {
			parts = append(parts, fmt.Sprintf("%d removed", c.removed))
		}
		if len(parts) > 0 {
			dirs = append(dirs, fmt.Sprintf("%s (%s)", dir, strings.Join(parts, ", ")))
		} else {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

// printOutputDirDetail lists every written and removed file under each affected
// output directory. Check mode includes destination IDs so split-root targets
// identify both the affected named root and artifact.
func printOutputDirDetail(out io.Writer, report *materialize.Report, nameRoots bool) error {
	homeDir, _ := os.UserHomeDir()
	type entry struct {
		written []string
		removed []string
	}
	agg := make(map[string]*entry)
	var order []string
	for _, tr := range report.TargetReports() {
		if len(tr.Written) == 0 && len(tr.Removed) == 0 {
			continue
		}
		dir := abbreviateHome(tr.OutDir, homeDir)
		if nameRoots && tr.RootID != "" {
			dir = fmt.Sprintf("%s (%s)", tr.RootID, dir)
		}
		e, ok := agg[dir]
		if !ok {
			e = &entry{}
			agg[dir] = e
			order = append(order, dir)
		}
		e.written = append(e.written, tr.Written...)
		e.removed = append(e.removed, tr.Removed...)
	}
	sort.Strings(order)
	for _, dir := range order {
		e := agg[dir]
		if _, err := fmt.Fprintf(out, "  → %s\n", dir); err != nil {
			return err
		}
		sort.Strings(e.written)
		sort.Strings(e.removed)
		for _, w := range e.written {
			if _, err := fmt.Fprintf(out, "      + %s\n", w); err != nil {
				return err
			}
		}
		for _, r := range e.removed {
			if _, err := fmt.Fprintf(out, "      - %s\n", r); err != nil {
				return err
			}
		}
	}
	return nil
}

// printMalformedDetail lists malformed resources with their source path and
// parse error, used for verbose sync output.
func printMalformedDetail(out io.Writer, malformed []skill.MalformedSkill) error {
	if _, err := fmt.Fprintf(out, "malformed (%d):\n", len(malformed)); err != nil {
		return err
	}
	for _, m := range malformed {
		if _, err := fmt.Fprintf(out, "  - %s (%s): %v\n", m.DirName, m.Path, m.Error); err != nil {
			return err
		}
	}
	return nil
}

// printMalformedHint points a default-output user at the command that explains
// a non-zero malformed count. Malformed resources are dropped from the catalog,
// so the count is the only signal that a selection may be unresolvable for a
// reason the tool already knows precisely.
func printMalformedHint(out io.Writer, malformedCount int, prefix string) error {
	subject := "resource was"
	if malformedCount != 1 {
		subject = "resources were"
	}
	if _, err := fmt.Fprintf(out, "%s %d %s dropped from the catalog after failing validation\n", prefix, malformedCount, subject); err != nil {
		return err
	}
	_, err := fmt.Fprintln(out, "  → agent-marketplace sync --verbose (or agent-marketplace status)")
	return err
}

// abbreviateHome replaces a leading home directory prefix with "~".
func abbreviateHome(dir, homeDir string) string {
	if homeDir != "" && strings.HasPrefix(dir, homeDir+string(filepath.Separator)) {
		return "~" + dir[len(homeDir):]
	}
	return dir
}

func describeConfigSources(orgCfg, globalCfg, repoCfg *config.Config) string {
	var sources []string
	if orgCfg != nil {
		sources = append(sources, config.OrganizationConfigPath())
	}
	if globalCfg != nil {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			sources = append(sources, "global config")
		} else {
			sources = append(sources, filepath.Join(homeDir, ".agents", "config.yaml"))
		}
	}
	if repoCfg != nil {
		sources = append(sources, filepath.Join(config.FindRepoRoot(), ".agents", "config.yaml"))
	}
	if len(sources) == 0 {
		return "config"
	}
	return strings.Join(sources, " and ")
}

// prunedStale records a resource that was pruned from the config.
type prunedStale struct {
	Kind ir.ResourceKind
	Name string
}

// dedupStaleSelections drops repeats of the same kind+name selection while
// preserving order. One stale selection can be reported once per registry
// entry, and both the prune path and the skipped-prune message must count and
// name it once — so they share this helper rather than each keeping a map.
func dedupStaleSelections(stale []*resolver.SelectedNotFoundError) []*resolver.SelectedNotFoundError {
	unique := make([]*resolver.SelectedNotFoundError, 0, len(stale))
	seen := make(map[string]struct{}, len(stale))
	for _, snf := range stale {
		key := skill.ResourceKey(snf.Kind, snf.Name)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, snf)
	}
	return unique
}

// prunePhase runs the --prune-stale decision for a run that HAS stale
// selections: it either prunes them (reporting each removal) or explains why it
// refused. It returns how many selections were actually removed from the
// config; a non-zero count is what makes the caller re-sync.
//
// The decision reads pipeline's error CATEGORIES, never the flat Errors list:
// an untrustworthy catalog blocks pruning entirely, a materialization failure
// does not (bead agent-marketplace-tbjf).
//
// The "Pruned N ...; re-syncing..." line is deliberately NOT printed here: the
// caller has to name this pass's errors between the per-selection lines and
// the re-sync announcement, so it owns that line.
func prunePhase(ctx context.Context, out io.Writer, repoRoot string, syncResult *pipeline.SyncResult, stale []*resolver.SelectedNotFoundError) (int, error) {
	if !syncResult.CatalogTrusted() {
		slog.Warn("prune-stale skipped: catalog not trustworthy",
			"stale_selections", len(stale),
			"catalog_trust_errors", len(syncResult.CatalogTrustErrors))
		return 0, printPruneSkipped(out, stale, syncResult.CatalogTrustErrors)
	}

	slog.Info("pruning stale selections",
		"stale_selections", len(stale),
		"materialize_errors", len(syncResult.MaterializeErrors))
	pruned, pruneErrs, fatal := pruneStaleSelections(ctx, repoRoot, stale)
	for _, p := range pruned {
		if _, err := fmt.Fprintf(out, "pruned stale %s %q from config\n", skill.KindLabel(p.Kind), p.Name); err != nil {
			return 0, err
		}
	}
	for _, e := range pruneErrs {
		if _, err := fmt.Fprintf(out, "warning: prune failed: %v\n", e); err != nil {
			return 0, err
		}
	}
	return len(pruned), fatal
}

// printSupersededErrors names the errors THIS pass counted, on the one path
// where they would otherwise never be printed: a successful prune returns the
// recursive re-sync's error, and the CLI framework only renders that one.
//
// Bead agent-marketplace-tbjf review: without this the run prints
// "Installed 1, Removed 0, Malformed 0, Errors 1", prunes, re-syncs clean and
// exits 0 — reporting a count of errors it never named and giving the user
// nothing to act on. Worse, a genuine failure that does not reproduce on the
// second pass would vanish entirely behind a zero exit code.
//
// They are labelled "error (superseded by re-sync)" rather than plain "error"
// because the run's exit code comes from the re-sync, not from these.
func printSupersededErrors(out io.Writer, errs []error) error {
	for _, e := range errs {
		if _, err := fmt.Fprintf(out, "error (superseded by re-sync): %v\n", e); err != nil {
			return err
		}
		if err := printRemediation(out, e); err != nil {
			return err
		}
	}
	return nil
}

// printPruneSkipped explains a --prune-stale that found stale selections and
// deliberately removed none of them, because this run could not read every
// registry. Without it a skipped prune is indistinguishable from one that found
// nothing to do, which is the second half of bead agent-marketplace-tbjf: the
// beta tester saw no output at all and concluded the flag was broken.
//
// It names what was KEPT (nothing was touched) and why, and the trailing hint
// points at the errors the CLI framework prints immediately below.
func printPruneSkipped(out io.Writer, stale []*resolver.SelectedNotFoundError, trustErrs []error) error {
	kept := dedupStaleSelections(stale)
	labels := make([]string, 0, len(kept))
	for _, snf := range kept {
		labels = append(labels, fmt.Sprintf("%s %q", skill.KindLabel(snf.Kind), snf.Name))
	}
	if _, err := fmt.Fprintf(out,
		"prune-stale skipped: the catalog could not be trusted this run (%d catalog/resolve error(s)), so %d stale selection(s) were kept: %s\n",
		len(trustErrs), len(kept), strings.Join(labels, ", ")); err != nil {
		return err
	}
	_, err := fmt.Fprintln(out,
		"  → a resource can look absent when its registry fails to load; fix the errors reported by this run, then re-run 'agent-marketplace sync --prune-stale'")
	return err
}

// pruneStaleSelections disables each stale (non-required, selected-but-not-found,
// direct-config-sourced) resource in the repo config and returns the list of
// successfully pruned entries along with any errors encountered.
//
// Only direct selections (Via == "") are pruned. Collection-sourced stale
// skills are left as warnings with enriched remediation hints.
func pruneStaleSelections(ctx context.Context, repoRoot string, stale []*resolver.SelectedNotFoundError) ([]prunedStale, []error, error) {
	configPath := config.ProjectConfigPath(repoRoot)

	var pruned []prunedStale
	var errs []error

	fatal := pipeline.MutateLayer(ctx, pipeline.LayerMutationTarget{
		RepoRoot:       repoRoot,
		LocalConfigDir: config.FindLocalConfigDir(),
		ConfigPath:     configPath,
		Source:         config.RepoCommitted,
		Reconcile:      reconcileCommandDependencies,
	}, func(m *pipeline.LayerMutation) error {
		for _, snf := range dedupStaleSelections(stale) {
			label := skill.KindLabel(snf.Kind)
			locator := config.ResourceLocator{Kind: snf.Kind, Name: snf.Name}
			topLevel, entry, inEntry, siteErr := selectionSites(configPath, snf)
			if siteErr != nil {
				errs = append(errs, fmt.Errorf("prune %s %q: %w", label, snf.Name, siteErr))
				continue
			}
			inherited, inhErr := m.UnboundIncludeInherited(locator)
			if inhErr != nil {
				errs = append(errs, fmt.Errorf("prune %s %q: %w", label, snf.Name, inhErr))
				continue
			}
			if inherited {
				errs = append(errs, fmt.Errorf("prune %s %q: config.edit.include-owned: selection is declared in an included fragment", label, snf.Name))
				continue
			}
			removed := false
			if inEntry {
				binding, found, bindErr := m.LookupRegistry(entry)
				if bindErr != nil {
					errs = append(errs, fmt.Errorf("prune %s %q: %w", label, snf.Name, bindErr))
					continue
				}
				if found {
					if err := m.Apply(binding, config.SelectionChange{Operation: config.SelectionRemoveInclude, Locators: []config.ResourceLocator{locator}}); err != nil {
						errs = append(errs, fmt.Errorf("prune %s %q: %w", label, snf.Name, err))
						continue
					}
					removed = true
				}
			} else if strings.TrimSpace(snf.URL) != "" {
				// The root file does not select this resource under a registry
				// entry, so there is nothing here to remove: selectionSites
				// would have reported inEntry. The edit is still attempted so
				// that a selection owned by an included fragment is refused by
				// name instead of being reported as undeclared.
				binding, found, bindErr := m.LookupRegistry(config.RegistryEntry{URL: snf.URL, Ref: snf.Ref, Path: snf.Path})
				if bindErr != nil {
					errs = append(errs, fmt.Errorf("prune %s %q: %w", label, snf.Name, bindErr))
					continue
				}
				if found {
					if err := m.Apply(binding, config.SelectionChange{Operation: config.SelectionRemoveInclude, Locators: []config.ResourceLocator{locator}}); err != nil {
						errs = append(errs, fmt.Errorf("prune %s %q: %w", label, snf.Name, err))
						continue
					}
				}
			}
			if topLevel {
				if err := m.RemoveUnboundInclude(locator); err != nil {
					errs = append(errs, fmt.Errorf("prune %s %q: %w", label, snf.Name, err))
					continue
				}
				removed = true
			}
			if !removed {
				errs = append(errs, fmt.Errorf("prune %s %q: selection is not declared in %s", label, snf.Name, configPath))
				continue
			}
			pruned = append(pruned, prunedStale{Kind: snf.Kind, Name: snf.Name})
		}
		return nil
	})
	if fatal != nil {
		return nil, nil, fatal
	}
	return pruned, errs, nil
}

// selectionSites reports where a direct selection is actually written in the
// repo config ROOT file: in the TOP-LEVEL per-kind include list, in the
// include list of a registry entry matching the blamed URL, or both. The
// registry-scoped editors can only see the second, so the prune has to ask
// before deciding which edit to make.
func selectionSites(configPath string, snf *resolver.SelectedNotFoundError) (topLevel bool, entry config.RegistryEntry, inEntry bool, err error) {
	cfg, err := config.LoadRootConfigFromPath(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, config.RegistryEntry{}, false, nil
		}
		return false, config.RegistryEntry{}, false, err
	}
	if cfg == nil {
		return false, config.RegistryEntry{}, false, nil
	}
	for _, candidate := range cfg.SelectorForKind(snf.Kind).Include {
		if strings.TrimSpace(candidate) == snf.Name {
			topLevel = true
			break
		}
	}
	wantURL := config.NormalizeGitURL(strings.TrimSpace(snf.URL))
	for _, candidate := range cfg.Registries {
		if config.NormalizeGitURL(strings.TrimSpace(candidate.URL)) != wantURL {
			continue
		}
		if p := strings.TrimSpace(snf.Path); p != "" && config.NormalizeRegistryPath(candidate.Path) != config.NormalizeRegistryPath(p) {
			continue
		}
		if r := strings.TrimSpace(snf.Ref); r != "" && strings.TrimSpace(candidate.Ref) != r {
			continue
		}
		for _, selected := range candidate.SelectorForKind(snf.Kind).Include {
			if strings.TrimSpace(selected) == snf.Name {
				return topLevel, candidate, true, nil
			}
		}
	}
	return topLevel, config.RegistryEntry{}, false, nil
}
