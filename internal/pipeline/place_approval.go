package pipeline

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
)

// PrepareDeployedPlaceApproval resolves approval for an already selected,
// deployed Git profile using cached bytes only. The target and state roots must
// already exist; this function does not fetch locks or profile artifacts.
func PrepareDeployedPlaceApproval(ctx context.Context, ws Workspace, targetID string) (targetprofile.ApprovalBinding, error) {
	if err := ws.validate(); err != nil {
		return targetprofile.ApprovalBinding{}, err
	}
	if !nameutil.IsTargetIdentifier(targetID) || target.IsValid(targetID) {
		return targetprofile.ApprovalBinding{}, fmt.Errorf("target.approval.target-invalid: %q is not a place target identifier", targetID)
	}
	environment, err := currentPlaceApprovalEnvironment()
	if err != nil {
		return targetprofile.ApprovalBinding{}, err
	}
	if err := dependencytxn.CheckPending(filepath.Join(environment.CanonicalStateRoot, "dependencies")); err != nil {
		return targetprofile.ApprovalBinding{}, err
	}
	intent := config.ReadIntent(ctx, config.LoadEffectiveOptions{
		RepoRoot: ws.RepoRoot(), LocalConfigDir: ws.LocalConfigDir(),
	})
	if len(intent.Errors) > 0 {
		return targetprofile.ApprovalBinding{}, errors.Join(intent.Errors...)
	}
	evidence, err := ReadDeployedLocks(ctx, intent)
	if err != nil {
		return targetprofile.ApprovalBinding{}, err
	}
	effective := config.Bind(intent, evidence)
	if len(effective.Errors) > 0 {
		return targetprofile.ApprovalBinding{}, errors.Join(effective.Errors...)
	}
	selected, err := effective.Merged.SelectedMarketplaceProfiles()
	if err != nil {
		return targetprofile.ApprovalBinding{}, err
	}
	identity, ok := selected[targetID]
	if !ok {
		return targetprofile.ApprovalBinding{}, fmt.Errorf("marketplace.profile.selection-invalid: target %q has no selected Marketplace profile", targetID)
	}
	raw, err := DeployedGitPlaceProfileReader(intent, evidence)(
		ctx, identity, resolver.MarketplaceResolveModeCachedNoFetch,
	)
	if err != nil {
		return targetprofile.ApprovalBinding{}, err
	}
	profile, err := targetprofile.Parse(raw, identity)
	if err != nil {
		return targetprofile.ApprovalBinding{}, err
	}
	return resolvePlaceApprovalBinding(profile, identity, environment)
}
