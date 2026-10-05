package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lifecycle"
)

func newLockCmd() *cobra.Command {
	var scope, registry, ref, registryPath, kind string
	var check, upgrade bool
	cmd := &cobra.Command{
		Use:   "lock [resource ...]",
		Short: "Resolve and verify exact Git dependency locks",
		RunE: func(cmd *cobra.Command, args []string) error {
			selected, explicitOrg, err := lifecycle.ParseLockSelection(scope, check)
			if err != nil {
				return err
			}
			targets, err := parseUpgradeTargets(upgrade, args, registry, ref, registryPath, kind)
			if err != nil {
				return err
			}
			result := &lifecycle.UpgradeTargetResult{}
			var warnings []error
			repoRoot := config.FindRepoRoot()
			_, changed, err := lifecycle.PrepareExactLocks(cmd.Context(), repoRoot, config.FindLocalConfigDir(), lifecycle.LockMode{
				Upgrade: upgrade, Check: check, Selects: selected, ExplicitOrganization: explicitOrg, UpgradeTargets: targets, UpgradeResult: result, WarningSink: &warnings,
			})
			if err != nil {
				return err
			}
			if check && changed > 0 {
				if result.CheckUpgradeErr != nil {
					return result.CheckUpgradeErr
				}
				return &lifecycle.LockLifecycleError{Code: "lock.change-required", Err: errors.New("lock files would change")}
			}
			if err := writeGroupedWarnings(cmd.OutOrStdout(), warnings, false); err != nil {
				return err
			}
			for _, notice := range result.Notices {
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), notice); err != nil {
					return err
				}
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Locks: %d changed\n", changed)
			return err
		},
	}
	cmd.Flags().StringVar(&scope, "scope", "all", "Lock scope: user, repo, local, all, or organization")
	cmd.Flags().BoolVar(&check, "check", false, "Check whether selected locks would change without writing")
	cmd.Flags().BoolVar(&upgrade, "upgrade", false, "Refresh selected locks to current Git refs")
	cmd.Flags().StringVar(&registry, "registry", "", "Target a configured Git registry URL")
	cmd.Flags().StringVar(&ref, "ref", "", "Narrow --registry to a configured Git ref")
	cmd.Flags().StringVar(&registryPath, "path", "", "Narrow --registry to a locked root path")
	cmd.Flags().StringVar(&kind, "kind", "", "Resource kind for direct git targets")
	return cmd
}

func parseUpgradeTargets(upgrade bool, args []string, registry, ref, registryPath, kind string) (*lifecycle.UpgradeTargetSpec, error) {
	hasGit := registry != "" || ref != "" || registryPath != "" || kind != ""
	if !upgrade && (len(args) > 0 || hasGit) {
		return nil, errors.New("upgrade targets require --upgrade")
	}
	if registry == "" && (len(args) > 0 || ref != "" || registryPath != "" || kind != "") {
		return nil, errors.New("resource targets, --ref, --path, and --kind require --registry")
	}
	if len(args) > 0 && kind == "" {
		return nil, errors.New("named --registry targets require --kind")
	}
	return &lifecycle.UpgradeTargetSpec{Registry: registry, Ref: ref, Path: registryPath, Kind: kind, Names: append([]string(nil), args...)}, nil
}
