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
)

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

func runUpdate(ctx context.Context, out io.Writer, bump, verbose bool, selected map[lockfile.Scope]bool, targets *upgradeTargetSpec) error {
	ctx = pipeline.WithTransientOrganizationMarketplaceLockCache(ctx)
	repoRoot := config.FindRepoRoot()
	localDir := config.FindLocalConfigDir()
	if err := prepareDependencyTransactions(ctx, false); err != nil {
		return err
	}
	if _, err := bootstrapMissingMarketplaceLocks(ctx, repoRoot, localDir, selected, nil); err != nil {
		return err
	}
	targetResult := &upgradeTargetResult{}
	var warnings []error
	opts := pipeline.UpdateOptions{
		RepoRoot: repoRoot, Bump: bump, BumpScopes: selected, BumpTarget: updateBumpTarget(targets),
		DependencyReconcile: func(ctx context.Context, repoRoot, localDir string, bumps []pipeline.RefBump, edits []pipeline.StagedConfigEdit) (pipeline.DependencyLockView, error) {
			advanceUpgradeTargetRefs(targets, bumps)
			option, _, lease, err := prepareRuntimeLocks(ctx, repoRoot, localDir, lockMode{upgrade: true, selects: selected, upgradeTargets: targets, upgradeResult: targetResult, reconcileUnselected: true, stagedConfigEdits: edits, warningSink: &warnings})
			return pipeline.DependencyLockView{CatalogOption: option, ExactLockLease: lease, Warnings: warnings}, err
		},
	}
	if localDir != "" && localDir != repoRoot {
		opts.LocalConfigDir = localDir
		opts.MaterializeRoot = localDir
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

func writeUpdateOutput(out io.Writer, result *pipeline.UpdateResult, bump, verbose bool) error {
	if bump {
		if _, err := fmt.Fprintf(out, "Configured registry entries: %d, refs bumped: %d\n", result.UpdatedRegistries, len(result.Bumps)); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintf(out, "Configured registry entries: %d\n", result.UpdatedRegistries); err != nil {
		return err
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
		if _, err := fmt.Fprintln(out, "Ask your organization config owner to update these pins, then run `agent-marketplace sync`."); err != nil {
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
