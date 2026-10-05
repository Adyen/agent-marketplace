package main

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lifecycle"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

func newUpdateCmd() *cobra.Command {
	var bump bool
	var scope, registry, ref, registryPath, kind string
	cmd := &cobra.Command{
		Use:   "update [resource ...]",
		Short: "Refresh pinned registries and sync enabled skills",
		RunE: func(cmd *cobra.Command, args []string) error {
			selected, explicitOrg, err := lifecycle.ParseLockSelection(scope, false)
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

func runUpdate(ctx context.Context, out io.Writer, bump bool, selected map[lockfile.Scope]bool, targets *lifecycle.UpgradeTargetSpec) error {
	repoRoot := config.FindRepoRoot()
	localDir := config.FindLocalConfigDir()
	if err := lifecycle.PrepareDependencyTransactions(ctx, false); err != nil {
		return err
	}
	if _, err := lifecycle.BootstrapMissingMarketplaceLocks(ctx, repoRoot, localDir, selected, nil); err != nil {
		return err
	}
	targetResult := &lifecycle.UpgradeTargetResult{}
	var warnings []error
	opts := pipeline.UpdateOptions{
		RepoRoot: repoRoot, Bump: bump, BumpScopes: selected, BumpTarget: updateBumpTarget(targets),
		DependencyReconcile: func(ctx context.Context, repoRoot, localDir string, bumps []pipeline.RefBump, edits []pipeline.StagedConfigEdit, lockEdits []pipeline.StagedLockEdit) (pipeline.DependencyLockView, error) {
			advanceUpgradeTargetRefs(targets, bumps)
			option, _, lease, err := lifecycle.PrepareRuntimeLocks(ctx, repoRoot, localDir, lifecycle.LockMode{Upgrade: true, Selects: selected, UpgradeTargets: targets, UpgradeResult: targetResult, ReconcileUnselected: true, StagedConfigEdits: edits, StagedLockEdits: lockEdits, WarningSink: &warnings})
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
	for _, notice := range targetResult.Notices {
		if _, err := fmt.Fprintln(out, notice); err != nil {
			return err
		}
	}
	return writeUpdateOutput(out, result, bump)
}

func updateBumpTarget(targets *lifecycle.UpgradeTargetSpec) *pipeline.UpdateTarget {
	if targets == nil || targets.Registry == "" {
		return nil
	}
	return &pipeline.UpdateTarget{Registry: targets.Registry, Ref: targets.Ref, Path: targets.Path, Kind: targets.Kind, Names: append([]string(nil), targets.Names...)}
}

func advanceUpgradeTargetRefs(targets *lifecycle.UpgradeTargetSpec, bumps []pipeline.RefBump) {
	if targets == nil || targets.Registry == "" || targets.Ref == "" {
		return
	}
	wanted, err := lifecycle.CanonicalRegistryURL(targets.Registry)
	if err != nil {
		return
	}
	for _, bump := range bumps {
		if bump.OldRef != targets.Ref {
			continue
		}
		bumpURL, err := lifecycle.CanonicalRegistryURL(bump.URL)
		if err == nil && bumpURL == wanted {
			targets.Ref = bump.NewRef
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
	for _, skipped := range result.SkippedBumps {
		if _, err := fmt.Fprintf(out, "Ref bump skipped for %s: %s -> %s is declared in an included config file; update it there\n", marketplace.QuoteTerminal(skipped.URL), marketplace.QuoteTerminal(skipped.OldRef), marketplace.QuoteTerminal(skipped.NewRef)); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(out, "Marketplace pins: updated %d, skipped %d\n", result.UpdatedMarketplacePins, result.SkippedMarketplacePins); err != nil {
		return err
	}
	for _, pin := range result.MarketplacePins {
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
