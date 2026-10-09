package main

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

// newUpdateCmd builds the update command with scope, Git target, ref bump, and
// verbose output flags. Execution rejects organization scope and invalid target
// combinations, then returns any error from runUpdate.
func newUpdateCmd() *cobra.Command {
	var bump, verbose bool
	var scope, registry, ref, registryPath, kind string
	cmd := &cobra.Command{
		Use:   "update [resource ...]",
		Short: "Refresh pinned registries and sync enabled skills",
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
			return runUpdate(cmd.Context(), cmd.OutOrStdout(), bump, verbose, selected, targets)
		},
	}
	cmd.Flags().BoolVar(&bump, "bump", false, "Bump repo registry refs to newer matching tags")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "List organization-managed Marketplace pins")
	cmd.Flags().StringVar(&scope, "scope", "all", "Update scope: user, repo, local, or all")
	cmd.Flags().StringVar(&registry, "registry", "", "Target a configured Git registry URL")
	cmd.Flags().StringVar(&ref, "ref", "", "Narrow --registry to a configured Git ref")
	cmd.Flags().StringVar(&registryPath, "path", "", "Narrow --registry to a locked root path")
	cmd.Flags().StringVar(&kind, "kind", "", "Resource kind for direct Git targets")
	return cmd
}

// runUpdate refreshes dependency locks and Marketplace pins, syncs enabled
// resources, and writes the update report. selected controls Git upgrade and ref
// bump scopes; nil upgrades all writable scopes but bumps only repository refs.
// targets narrows Git updates and may have its ref advanced when bump rewrites a
// matching ref. verbose lists individual organization-managed pins in the report.
//
// It recovers pending dependency transactions and creates missing Marketplace
// locks before updating. Recovery, lock preparation, update, and output errors
// are propagated; an output error can occur after changes have been applied.
func runUpdate(ctx context.Context, out io.Writer, bump, verbose bool, selected map[lockfile.Scope]bool, targets *upgradeTargetSpec) error {
	repoRoot := config.FindRepoRoot()
	localDir := config.FindLocalConfigDir()
	ws, err := pipeline.WorkspaceForRoots(repoRoot, localDir)
	if err != nil {
		return err
	}
	if err := prepareDependencyTransactions(ctx, false); err != nil {
		return err
	}
	if _, err := bootstrapMissingMarketplaceLocks(ctx, ws.RepoRoot(), ws.LocalConfigDir(), selected, nil); err != nil {
		return err
	}
	targetResult := &upgradeTargetResult{}
	var warnings []error
	opts := pipeline.UpdateOptions{
		RepoRoot: repoRoot, Bump: bump, BumpScopes: selected, BumpTarget: updateBumpTarget(targets),
		DependencyReconcile: func(ctx context.Context, repoRoot, localDir string, bumps []pipeline.RefBump, edits []pipeline.StagedConfigEdit, lockEdits []pipeline.StagedLockEdit) (pipeline.DependencyLockView, error) {
			advanceUpgradeTargetRefs(targets, bumps)
			option, _, lease, err := prepareRuntimeLocks(ctx, repoRoot, localDir, lockMode{upgrade: true, selects: selected, upgradeTargets: targets, upgradeResult: targetResult, reconcileUnselected: true, stagedConfigEdits: edits, stagedLockEdits: lockEdits, warningSink: &warnings})
			return pipeline.DependencyLockView{CatalogOption: option, ExactLockLease: lease, Warnings: warnings}, err
		},
	}
	if localDir != "" && localDir != repoRoot {
		opts.LocalConfigDir = localDir
	}
	result, err := pipeline.Update(ctx, opts)
	if err != nil {
		return err
	}
	if err := writeGroupedWarnings(out, warnings, false); err != nil {
		return err
	}
	for _, notice := range targetResult.notices {
		if _, err := fmt.Fprintln(out, notice); err != nil {
			return err
		}
	}
	return writeUpdateOutput(out, result, bump, verbose)
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
