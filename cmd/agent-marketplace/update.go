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
	var bump bool
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
			return runUpdate(cmd.Context(), cmd.OutOrStdout(), bump, selected, targets)
		},
	}
	cmd.Flags().BoolVar(&bump, "bump", false, "Bump repo registry refs to newer matching tags")
	cmd.Flags().StringVar(&scope, "scope", "all", "Update scope: user, repo, local, or all")
	cmd.Flags().StringVar(&registry, "registry", "", "Target a configured Git registry URL")
	cmd.Flags().StringVar(&ref, "ref", "", "Narrow --registry to a configured Git ref")
	cmd.Flags().StringVar(&registryPath, "path", "", "Narrow --registry to a locked root path")
	cmd.Flags().StringVar(&kind, "kind", "", "Resource kind for direct Git targets")
	return cmd
}

func runUpdate(ctx context.Context, out io.Writer, bump bool, selected map[lockfile.Scope]bool, targets *upgradeTargetSpec) error {
	repoRoot := config.FindRepoRoot()
	localDir := config.FindLocalConfigDir()
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
	return writeUpdateOutput(out, result, bump)
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

func writeUpdateOutput(out io.Writer, result *pipeline.UpdateResult, bump bool) error {
	if bump {
		if _, err := fmt.Fprintf(out, "Updated %d registries, bumped %d refs\n", result.UpdatedRegistries, len(result.Bumps)); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintf(out, "Updated %d registries\n", result.UpdatedRegistries); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Marketplace pins: updated %d, skipped %d\n", result.UpdatedMarketplacePins, result.SkippedMarketplacePins); err != nil {
		return err
	}
	for _, pin := range result.MarketplacePins {
		identity := fmt.Sprintf("%s/%s/%s", marketplace.QuoteTerminal(pin.OldPin.RegistryID), marketplace.QuoteTerminal(pin.OldPin.Kind), marketplace.QuoteTerminal(pin.OldPin.Name))
		if pin.Updated {
			if _, err := fmt.Fprintf(out, "Marketplace %s %s: updated %s -> %s\n", marketplace.QuoteTerminal(pin.Entry.URL), identity, marketplace.QuoteTerminal(pin.OldPin.Digest), marketplace.QuoteTerminal(pin.NewPin.Digest)); err != nil {
				return err
			}
			continue
		}
		required := ""
		if pin.Required {
			required = " (required)"
		}
		if _, err := fmt.Fprintf(out, "Marketplace %s %s: skipped%s (%s)\n", marketplace.QuoteTerminal(pin.Entry.URL), identity, required, marketplace.QuoteTerminal(pin.Reason)); err != nil {
			return err
		}
	}
	return nil
}
