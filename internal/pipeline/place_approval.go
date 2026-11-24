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
	if err := validatePlaceTargetID(targetID); err != nil {
		return targetprofile.ApprovalBinding{}, err
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
	profile, identity, err := readSelectedDeployedPlaceProfile(ctx, intent, evidence, effective.Merged, targetID)
	if err != nil {
		return targetprofile.ApprovalBinding{}, err
	}
	return resolvePlaceApprovalBinding(profile, identity, environment)
}

func validatePlaceTargetID(targetID string) error {
	if !nameutil.IsTargetIdentifier(targetID) || target.IsValid(targetID) {
		return fmt.Errorf("target.approval.target-invalid: %q is not a place target identifier", targetID)
	}
	return nil
}

func readSelectedDeployedPlaceProfile(
	ctx context.Context,
	intent config.Intent,
	evidence config.Evidence,
	merged *config.Config,
	targetID string,
) (targetprofile.Profile, targetprofile.Identity, error) {
	selected, err := merged.SelectedMarketplaceProfiles()
	if err != nil {
		return targetprofile.Profile{}, targetprofile.Identity{}, err
	}
	identity, ok := selected[targetID]
	if !ok {
		return targetprofile.Profile{}, targetprofile.Identity{}, fmt.Errorf("marketplace.profile.selection-invalid: target %q has no selected Marketplace profile", targetID)
	}
	raw, err := DeployedGitPlaceProfileReader(intent, evidence)(ctx, identity, resolver.MarketplaceResolveModeCachedNoFetch)
	if err != nil {
		return targetprofile.Profile{}, targetprofile.Identity{}, err
	}
	profile, err := targetprofile.Parse(raw, identity)
	if err != nil {
		return targetprofile.Profile{}, targetprofile.Identity{}, err
	}
	return profile, identity, nil
}
