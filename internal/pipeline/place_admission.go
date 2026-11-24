package pipeline

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
)

// PlaceProfileReader returns exact profile bytes for the selected identity.
// Implementations must not fetch or fall back to another source in
// CachedNoFetch mode.
type PlaceProfileReader func(ctx context.Context, selected targetprofile.Identity, mode resolver.MarketplaceResolveMode) ([]byte, error)

// PlaceAdmission contains approved bindings, profiles awaiting consent, and
// packages admitted against those bindings.
type PlaceAdmission struct {
	Approvals map[targetprofile.Identity]targetprofile.ApprovalBinding
	Pending   []targetprofile.Identity
	Packages  []marketplace.ResolvedPlacePackage
	profiles  map[targetprofile.Identity][]byte
}

// PreparePlaceAdmission validates approved place profiles before resolving
// their locked packages. It does not create receipts, execute host commands,
// or authorize later writes; a transaction must re-resolve and recheck
// approval before publishing.
func PreparePlaceAdmission(
	ctx context.Context,
	cfg *config.Config,
	enabled []string,
	environment targetprofile.ApprovalEnvironment,
	readProfile PlaceProfileReader,
	packages *marketplace.Resolver,
	mode resolver.MarketplaceResolveMode,
) (PlaceAdmission, error) {
	return preparePlaceAdmission(ctx, cfg, enabled, environment, readProfile, packages, mode, false)
}

func preparePlaceAdmission(
	ctx context.Context,
	cfg *config.Config,
	enabled []string,
	environment targetprofile.ApprovalEnvironment,
	readProfile PlaceProfileReader,
	packages *marketplace.Resolver,
	mode resolver.MarketplaceResolveMode,
	includeEmpty bool,
) (PlaceAdmission, error) {
	if err := ctx.Err(); err != nil {
		return PlaceAdmission{}, err
	}
	if mode != resolver.MarketplaceResolveModeRefresh && mode != resolver.MarketplaceResolveModeCachedNoFetch {
		return PlaceAdmission{}, fmt.Errorf("unsupported Marketplace place admission mode %d", mode)
	}
	selected, err := cfg.SelectedMarketplaceProfiles()
	if err != nil {
		return PlaceAdmission{}, err
	}
	if len(selected) == 0 {
		return PlaceAdmission{}, nil
	}
	placeTargets := selectedPlaceTargetIDs(cfg)
	if len(placeTargets) == 0 && !includeEmpty {
		return PlaceAdmission{}, nil
	}
	var allowed []string
	if cfg.AllowedMarketplaces != nil {
		allowed = *cfg.AllowedMarketplaces
	}
	if err := config.ValidateMarketplaceURLs(cfg.Marketplaces, allowed); err != nil {
		return PlaceAdmission{}, fmt.Errorf("validate Marketplace endpoints for place admission: %w", err)
	}

	admission := PlaceAdmission{
		Approvals: make(map[targetprofile.Identity]targetprofile.ApprovalBinding),
	}
	approvedProfiles := make(map[targetprofile.Identity][]byte)
	seen := make(map[targetprofile.Identity]struct{})
	for _, targetID := range enabled {
		if err := ctx.Err(); err != nil {
			return PlaceAdmission{}, err
		}
		if !includeEmpty {
			if _, ok := placeTargets[targetID]; !ok {
				continue
			}
		}
		identity, ok := selected[targetID]
		if !ok {
			continue
		}
		if _, duplicate := seen[identity]; duplicate {
			continue
		}
		seen[identity] = struct{}{}
		if readProfile == nil {
			return PlaceAdmission{}, fmt.Errorf("target %s profile reader is required", targetID)
		}
		raw, err := readProfile(ctx, identity, mode)
		if err != nil {
			return PlaceAdmission{}, fmt.Errorf("target %s read exact profile: %w", targetID, err)
		}
		profile, err := targetprofile.Parse(raw, identity)
		if err != nil {
			return PlaceAdmission{}, fmt.Errorf("target %s profile validation: %w", targetID, err)
		}
		binding, err := resolvePlaceApprovalBinding(profile, identity, environment)
		if err != nil {
			return PlaceAdmission{}, fmt.Errorf("target %s approval binding: %w", targetID, err)
		}
		if err := targetprofile.RequireApproval(binding); errors.Is(err, targetprofile.ErrApprovalRequired) {
			admission.Pending = append(admission.Pending, identity)
			continue
		} else if err != nil {
			return PlaceAdmission{}, fmt.Errorf("target %s approval: %w", targetID, err)
		}
		admission.Approvals[identity] = binding
		approvedProfiles[identity] = append([]byte(nil), raw...)
	}
	admission.profiles = approvedProfiles
	if len(approvedProfiles) == 0 {
		return admission, nil
	}
	if packages == nil {
		return PlaceAdmission{}, fmt.Errorf("approved place profiles require a Marketplace resolver")
	}
	resolved, err := packages.ResolvePlacePackages(ctx, cfg, approvedProfiles, mode)
	if err != nil {
		return PlaceAdmission{}, fmt.Errorf("resolve approved place packages: %w", err)
	}
	admission.Packages = resolved
	return admission, nil
}

func resolvePlaceApprovalBinding(profile targetprofile.Profile, identity targetprofile.Identity, environment targetprofile.ApprovalEnvironment) (targetprofile.ApprovalBinding, error) {
	return targetprofile.ResolveApprovalBinding(profile, identity, placeProfileEnvironment(profile, environment))
}

func placeProfileEnvironment(profile targetprofile.Profile, environment targetprofile.ApprovalEnvironment) targetprofile.ApprovalEnvironment {
	profileEnvironment := environment
	profileEnvironment.Override = nil
	if key := profile.Roots.User.Override; key != "" {
		if value, exists := os.LookupEnv(key); exists {
			profileEnvironment.Override = &value
		}
	}
	return profileEnvironment
}

func selectedPlaceTargetIDs(cfg *config.Config) map[string]struct{} {
	targets := make(map[string]struct{})
	if cfg == nil {
		return targets
	}
	for _, entry := range cfg.Marketplaces {
		for _, pin := range entry.Resources {
			if cfg.MarketplacePinExcluded(entry, pin) {
				continue
			}
			targetID, _, scoped := nameutil.SplitPlaceKind(pin.Kind)
			if scoped {
				targets[targetID] = struct{}{}
			}
		}
	}
	return targets
}
