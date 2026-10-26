package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

// newUpdateCmd builds the update command with scope, Git target, ref bump,
// preview, and verbose output flags. Execution rejects organization scope and
// invalid target combinations, then returns any error from runUpdate.
func newUpdateCmd() *cobra.Command {
	var bump, verbose, dryRun, includeDiff bool
	var scope, registry, ref, registryPath, kind string
	cmd := &cobra.Command{
		Use:   "update [resource ...]",
		Short: "Refresh pinned registries and sync enabled skills",
		Long: "Advance exact Git acquisition locks and Marketplace pins to newer revisions and sync enabled resources.\n" +
			"\nUse --dry-run to preview proposed dependency and resource changes without changing locks, configuration, or installed resources.\n" +
			"Use --include-diff to include normalized review diffs of changed resources. Explicit diff output can disclose sensitive values authored in resources; it is not a secret scanner or a security finding.",
		RunE: func(cmd *cobra.Command, args []string) error {
			selected, explicitOrg, err := parseLockSelection(scope, false)
			if err != nil {
				return err
			}
			if explicitOrg {
				return fmt.Errorf("update does not support organization scope")
			}
			targets, err := parseUpgradeTargets(true, args, registry, ref, registryPath, kind)
			if err != nil {
				return err
			}
			return runUpdate(cmd.Context(), cmd.OutOrStdout(), bump, verbose, dryRun, includeDiff, selected, targets)
		},
	}
	cmd.Flags().BoolVar(&bump, "bump", false, "Bump repo registry refs to newer matching tags")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "List organization-managed Marketplace pins")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview proposed updates without changing locks, configuration, or installed resources")
	cmd.Flags().BoolVar(&includeDiff, "include-diff", false, "Include normalized resource content diffs in the report")
	cmd.Flags().StringVar(&scope, "scope", "all", "Update scope: user, repo, local, or all")
	cmd.Flags().StringVar(&registry, "registry", "", "Target a configured Git registry URL")
	cmd.Flags().StringVar(&ref, "ref", "", "Narrow --registry to a configured Git ref")
	cmd.Flags().StringVar(&registryPath, "path", "", "Narrow --registry to a locked root path")
	cmd.Flags().StringVar(&kind, "kind", "", "Resource kind for direct Git targets")
	return cmd
}

// runUpdate refreshes dependency locks and Marketplace pins, syncs enabled
// resources, and writes the update report. selected controls Git upgrade and
// ref bump scopes; nil upgrades all writable scopes but bumps only repository
// refs. targets narrows Git updates and may have its ref advanced when bump
// rewrites a matching ref. verbose lists individual organization-managed pins
// in the report. dryRun previews the prepared candidates without publishing
// configuration, locks, or materialized resources; includeDiff adds the
// normalized resource content diffs of the prepared candidates to the report.
//
// An applying update recovers pending dependency transactions and creates
// missing Marketplace locks before updating; a preview instead refuses a
// pending journal without replay and prepares missing acquisition evidence in
// memory. Recovery, lock preparation, update, and output errors are
// propagated; an output error can occur after changes have been applied.
func runUpdate(ctx context.Context, out io.Writer, bump, verbose, dryRun, includeDiff bool, selected map[lockfile.Scope]bool, targets *upgradeTargetSpec) error {
	repoRoot := config.FindRepoRoot()
	localDir := config.FindLocalConfigDir()
	ws, err := pipeline.WorkspaceForRoots(repoRoot, localDir)
	if err != nil {
		return err
	}
	var preseed []pipeline.StagedLockEdit
	if dryRun {
		// A preview checks for pending dependency work without replay and
		// prepares the missing Marketplace evidence an update would bootstrap
		// as in-memory staged documents instead of committing it.
		if err := prepareDependencyTransactions(ctx, true); err != nil {
			return err
		}
		preseed, err = prepareMissingMarketplaceLockEdits(ctx, ws.RepoRoot(), ws.LocalConfigDir(), selected, nil)
		if err != nil {
			return err
		}
	} else {
		if err := prepareDependencyTransactions(ctx, false); err != nil {
			return err
		}
		if _, err := bootstrapMissingMarketplaceLocks(ctx, ws.RepoRoot(), ws.LocalConfigDir(), selected, nil); err != nil {
			return err
		}
	}
	targetResult := &upgradeTargetResult{}
	var warnings []error
	var preview *updatePreview
	var previewErr error
	previewHook := func(ctx context.Context, view preparedUpdateView) error {
		preview, previewErr = buildUpdatePreview(ctx, view, includeDiff)
		return previewErr
	}
	reconcileMode := lockMode{upgrade: true, selects: selected, upgradeTargets: targets, upgradeResult: targetResult, reconcileUnselected: true, stagedConfigEdits: nil, stagedLockEdits: nil, warningSink: &warnings, selectionGuard: true}
	if dryRun || includeDiff {
		reconcileMode.updatePreview = previewHook
	}
	if dryRun {
		reconcileMode.previewNoCommit = true
	}
	opts := pipeline.UpdateOptions{
		RepoRoot: repoRoot, Bump: bump, BumpScopes: selected, BumpTarget: updateBumpTarget(targets),
		DryRun:           dryRun,
		PreseedLockEdits: preseed,
		DependencyReconcile: func(ctx context.Context, repoRoot, localDir string, bumps []pipeline.RefBump, edits []pipeline.StagedConfigEdit, lockEdits []pipeline.StagedLockEdit) (pipeline.DependencyLockView, error) {
			advanceUpgradeTargetRefs(targets, bumps)
			mode := reconcileMode
			if mode.updatePreview != nil {
				mode.updatePreview = func(ctx context.Context, view preparedUpdateView) error {
					view.refBumps = bumps
					return previewHook(ctx, view)
				}
			}
			mode.stagedConfigEdits = edits
			mode.stagedLockEdits = lockEdits
			option, _, lease, err := prepareRuntimeLocks(ctx, repoRoot, localDir, mode)
			return pipeline.DependencyLockView{CatalogOption: option, ExactLockLease: lease, Warnings: warnings}, err
		},
	}
	if localDir != "" && localDir != repoRoot {
		opts.LocalConfigDir = localDir
	}
	result, updateErr := pipeline.Update(ctx, opts)
	if updateErr != nil && result == nil {
		var committed *pipeline.DependencyCommittedSyncError
		if errors.As(updateErr, &committed) {
			// Publication succeeded but harness synchronization did not
			// complete: report the phase boundary without claiming full
			// application or a rollback.
			if err := writeGroupedWarnings(out, warnings, false); err != nil {
				return err
			}
			if _, err := fmt.Fprintln(out, "Dependency updates were published, but harness synchronization did not complete; installed resources may be out of sync. Run `agent-marketplace sync` or repeat `agent-marketplace update` to complete materialization."); err != nil {
				return err
			}
			if includeDiff && preview != nil {
				if err := writeUpdatePreviewOutput(out, preview, nil, bump, verbose, true, true); err != nil {
					return err
				}
			}
			return updateErr
		}
		if refused, ok := errors.AsType[*updateSelectionRefusedError](updateErr); ok {
			return refused
		}
		return updateErr
	}
	if err := writeHookChanges(out, result.Report, false); err != nil {
		return errors.Join(updateErr, err)
	}
	if err := writeGroupedWarnings(out, dedupIssues(append(warnings, result.Warnings...)), false); err != nil {
		return errors.Join(updateErr, err)
	}
	for _, notice := range targetResult.notices {
		if _, err := fmt.Fprintln(out, notice); err != nil {
			return err
		}
	}
	if dryRun || includeDiff {
		if err := writeUpdatePreviewOutput(out, preview, result, bump, verbose, !dryRun, includeDiff); err != nil {
			return err
		}
	} else if err := writeUpdateOutput(out, result, bump, verbose); err != nil {
		return err
	}
	return updateErr
}

func updateBumpTarget(targets *upgradeTargetSpec) *pipeline.UpdateTarget {
	if targets == nil || targets.registry == "" {
		return nil
	}
	return &pipeline.UpdateTarget{Registry: targets.registry, Ref: targets.ref, Path: targets.path, Kind: targets.kind, Names: append([]string(nil), targets.names...)}
}

func advanceUpgradeTargetRefs(targets *upgradeTargetSpec, bumps []pipeline.RefBump) {
	if targets == nil || targets.registry == "" || targets.ref == "" {
		return
	}
	wanted, err := canonicalRegistryURL(targets.registry)
	if err != nil {
		return
	}
	for _, bump := range bumps {
		if bump.OldRef != targets.ref {
			continue
		}
		bumpURL, err := canonicalRegistryURL(bump.URL)
		if err == nil && bumpURL == wanted {
			targets.ref = bump.NewRef
			return
		}
	}
}

// writeUpdateOutput writes registry counts and Marketplace pin decisions from a
// non-nil result. bump includes the ref bump count; verbose lists unchanged
// organization-managed pins, which otherwise appear only in the summary.
// Archive evidence refreshes are reported separately from content digest changes.
// It returns the first output error, possibly after writing a partial report.
func writeUpdateOutput(out io.Writer, result *pipeline.UpdateResult, bump, verbose bool) error {
	if bump {
		if _, err := fmt.Fprintf(out, "Configured registry entries: %d, refs bumped: %d\n", result.UpdatedRegistries, len(result.Bumps)); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintf(out, "Configured registry entries: %d\n", result.UpdatedRegistries); err != nil {
		return err
	}
	for _, skipped := range result.SkippedBumps {
		if _, err := fmt.Fprintf(out, "Ref bump skipped for %s: %s -> %s is declared in an included config file; update it there\n", marketplace.QuoteTerminal(skipped.URL), marketplace.QuoteTerminal(skipped.OldRef), marketplace.QuoteTerminal(skipped.NewRef)); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(out, "Marketplace pins updated: %d\n", result.UpdatedMarketplacePins); err != nil {
		return err
	}
	if err := writeMarketplaceSkipSummary(out, result, verbose); err != nil {
		return err
	}
	for _, pin := range result.MarketplacePins {
		if pin.Source == config.Organization && !pin.Updated && !verbose {
			continue
		}
		identity := fmt.Sprintf("%s/%s/%s", marketplace.QuoteTerminal(pin.OldPin.RegistryID), marketplace.QuoteTerminal(pin.OldPin.Kind), marketplace.QuoteTerminal(pin.OldPin.Name))
		if pin.Updated {
			if pin.NewPin.Digest == pin.OldPin.Digest && pin.OldPin.LockArchiveDigest != "" && pin.ArchiveDigest != "" && pin.ArchiveDigest != pin.OldPin.LockArchiveDigest {
				if err := writeGroupedWarnings(out, []error{&resolver.ArchiveEvidenceRefreshedWarning{
					MarketplaceURL:   pin.Entry.URL,
					InstanceID:       pin.Entry.InstanceID,
					RegistryID:       pin.OldPin.RegistryID,
					Kind:             pin.OldPin.Kind,
					Name:             pin.OldPin.Name,
					OldArchiveDigest: pin.OldPin.LockArchiveDigest,
					NewArchiveDigest: pin.ArchiveDigest,
				}}, false); err != nil {
					return err
				}
				if _, err := fmt.Fprintf(out, "Marketplace %s %s: %s\n", marketplace.QuoteTerminal(pin.Entry.URL), identity, terminalSafeText(pin.Reason)); err != nil {
					return err
				}
				continue
			}
			if _, err := fmt.Fprintf(out, "Marketplace %s %s: updated %s -> %s\n", marketplace.QuoteTerminal(pin.Entry.URL), identity, marketplace.QuoteTerminal(pin.OldPin.Digest), marketplace.QuoteTerminal(pin.NewPin.Digest)); err != nil {
				return err
			}
			continue
		}
		if pin.Source == config.Organization {
			if _, err := fmt.Fprintf(out, "Marketplace %s %s: not checked (organization-managed)\n", marketplace.QuoteTerminal(pin.Entry.URL), identity); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintf(out, "Marketplace %s %s: skipped (%s)\n", marketplace.QuoteTerminal(pin.Entry.URL), identity, marketplace.QuoteTerminal(pin.Reason)); err != nil {
			return err
		}
	}
	return nil
}

func writeMarketplaceSkipSummary(out io.Writer, result *pipeline.UpdateResult, verbose bool) error {
	organizationPins := 0
	for _, pin := range result.MarketplacePins {
		if pin.Source == config.Organization && pin.Skipped {
			organizationPins++
		}
	}
	if otherSkipped := result.SkippedMarketplacePins - organizationPins; otherSkipped > 0 {
		if _, err := fmt.Fprintf(out, "Other Marketplace pins skipped: %d\n", otherSkipped); err != nil {
			return err
		}
	}
	if organizationPins > 0 {
		if _, err := fmt.Fprintf(out, "Organization-managed Marketplace pins: %d (not checked for newer revisions; pins unchanged)\n", organizationPins); err != nil {
			return err
		}
		if !verbose {
			if _, err := fmt.Fprintln(out, "Use `agent-marketplace update --verbose` to list organization-managed pins."); err != nil {
				return err
			}
		}
	}
	return nil
}
