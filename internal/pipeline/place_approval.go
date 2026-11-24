package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	prepared, err := loadDeployedPlaceApproval(ctx, ws, targetID)
	if err != nil {
		return targetprofile.ApprovalBinding{}, err
	}
	return prepared.binding, nil
}

type deployedPlaceApproval struct {
	binding     targetprofile.ApprovalBinding
	environment targetprofile.ApprovalEnvironment
	raw         []byte
}

func loadDeployedPlaceApproval(ctx context.Context, ws Workspace, targetID string) (deployedPlaceApproval, error) {
	if err := ws.validate(); err != nil {
		return deployedPlaceApproval{}, err
	}
	if err := validatePlaceTargetID(targetID); err != nil {
		return deployedPlaceApproval{}, err
	}
	environment, err := currentPlaceApprovalEnvironment()
	if err != nil {
		return deployedPlaceApproval{}, err
	}
	if err := dependencytxn.CheckPending(filepath.Join(environment.CanonicalStateRoot, "dependencies")); err != nil {
		return deployedPlaceApproval{}, err
	}
	intent := config.ReadIntent(ctx, config.LoadEffectiveOptions{
		RepoRoot: ws.RepoRoot(), LocalConfigDir: ws.LocalConfigDir(),
	})
	if len(intent.Errors) > 0 {
		return deployedPlaceApproval{}, errors.Join(intent.Errors...)
	}
	evidence, err := ReadDeployedLocks(ctx, intent)
	if err != nil {
		return deployedPlaceApproval{}, err
	}
	effective := config.Bind(intent, evidence)
	if len(effective.Errors) > 0 {
		return deployedPlaceApproval{}, errors.Join(effective.Errors...)
	}
	profile, identity, raw, err := readSelectedDeployedPlaceProfile(ctx, intent, evidence, effective.Merged, targetID)
	if err != nil {
		return deployedPlaceApproval{}, err
	}
	binding, err := resolvePlaceApprovalBinding(profile, identity, environment)
	if err != nil {
		return deployedPlaceApproval{}, err
	}
	return deployedPlaceApproval{binding: binding, environment: environment, raw: raw}, nil
}

// PlaceInstallationID identifies a target root independently of profile and package revisions.
func PlaceInstallationID(binding targetprofile.ApprovalBinding) string {
	sum := sha256.Sum256([]byte("agent-marketplace/place-installation/v1\x00" +
		binding.Profile.TargetID + "\x00" + binding.CanonicalRoot + "\x00" + binding.CanonicalStateRoot))
	return hex.EncodeToString(sum[:])
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
) (targetprofile.Profile, targetprofile.Identity, []byte, error) {
	selected, err := merged.SelectedMarketplaceProfiles()
	if err != nil {
		return targetprofile.Profile{}, targetprofile.Identity{}, nil, err
	}
	identity, ok := selected[targetID]
	if !ok {
		return targetprofile.Profile{}, targetprofile.Identity{}, nil, fmt.Errorf("marketplace.profile.selection-invalid: target %q has no selected Marketplace profile", targetID)
	}
	raw, err := DeployedGitPlaceProfileReader(intent, evidence)(ctx, identity, resolver.MarketplaceResolveModeCachedNoFetch)
	if err != nil {
		return targetprofile.Profile{}, targetprofile.Identity{}, nil, err
	}
	profile, err := targetprofile.Parse(raw, identity)
	if err != nil {
		return targetprofile.Profile{}, targetprofile.Identity{}, nil, err
	}
	return profile, identity, raw, nil
}
