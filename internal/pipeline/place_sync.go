package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
)

func syncDeployedPlace(ctx context.Context, resolved ResolveResult, offline bool) (PlaceSyncResult, error) {
	if resolved.placeProfileReader == nil {
		return PlaceSyncResult{}, fmt.Errorf("deployed place profile reader is unavailable")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return PlaceSyncResult{}, fmt.Errorf("resolve home for place sync: %w", err)
	}
	platform := runtime.GOOS
	if platform == "darwin" {
		platform = "macos"
	}
	if platform != "macos" && platform != "linux" {
		return PlaceSyncResult{}, fmt.Errorf("target.approval.unsupported-platform: %q", platform)
	}
	xdgConfig := os.Getenv("XDG_CONFIG_HOME")
	if !filepath.IsAbs(xdgConfig) {
		xdgConfig = filepath.Join(home, ".config")
	}
	stateDir, err := dependencytxn.StateDir()
	if err != nil {
		return PlaceSyncResult{}, fmt.Errorf("resolve state for place sync: %w", err)
	}
	mode := resolver.MarketplaceResolveModeRefresh
	if offline {
		mode = resolver.MarketplaceResolveModeCachedNoFetch
	}
	cfg := resolved.placeConfig
	if cfg == nil {
		cfg = resolved.MergedCfg
	}
	return SyncPlaceUpserts(
		ctx, cfg, resolved.placeTargets,
		targetprofile.ApprovalEnvironment{
			Platform: platform, Home: home, XDGConfig: xdgConfig,
			CanonicalStateRoot: filepath.Dir(stateDir),
		},
		resolved.placeProfileReader, marketplace.NewResolver(), mode,
	)
}

// PlaceSyncResult contains profiles awaiting approval and the independently
// committed result for each target root.
type PlaceSyncResult struct {
	Pending   []targetprofile.Identity
	Committed map[string]dependencytxn.PlaceResult
}

type placeSyncGroup struct {
	identity targetprofile.Identity
	binding  targetprofile.ApprovalBinding
	profile  []byte
	packages []marketplace.ResolvedPlacePackage
}

// SyncPlaceUpserts admits exact approved profiles, resolves their locked
// packages, and commits each target root independently. A later group failure
// does not roll back earlier root commits.
func SyncPlaceUpserts(
	ctx context.Context,
	cfg *config.Config,
	enabled []string,
	environment targetprofile.ApprovalEnvironment,
	readProfile PlaceProfileReader,
	packages *marketplace.Resolver,
	mode resolver.MarketplaceResolveMode,
) (result PlaceSyncResult, retErr error) {
	slog.Debug("place sync started", "enabled_targets", len(enabled))
	defer func() {
		if retErr != nil {
			slog.Warn("place sync failed", "committed_targets", len(result.Committed), "error", retErr)
		}
	}()
	admission, err := PreparePlaceAdmission(ctx, cfg, enabled, environment, readProfile, packages, mode)
	if err != nil {
		return PlaceSyncResult{}, err
	}
	result = PlaceSyncResult{
		Pending:   append([]targetprofile.Identity(nil), admission.Pending...),
		Committed: make(map[string]dependencytxn.PlaceResult),
	}

	groupsByIdentity := make(map[targetprofile.Identity]*placeSyncGroup)
	for _, pkg := range admission.Packages {
		identity := pkg.Pin.LockHostPackageIdentity.ProfileIdentity
		approval, approved := admission.Approvals[identity]
		profile, profileLoaded := admission.profiles[identity]
		if !approved || !profileLoaded || len(profile) == 0 || approval.Profile != identity {
			return result, fmt.Errorf("target %s package is missing its admitted profile or approval", identity.TargetID)
		}
		group := groupsByIdentity[identity]
		if group == nil {
			group = &placeSyncGroup{
				identity: identity, binding: approval,
				profile: profile,
			}
			groupsByIdentity[identity] = group
		}
		group.packages = append(group.packages, pkg)
	}

	groups := make([]*placeSyncGroup, 0, len(groupsByIdentity))
	for _, group := range groupsByIdentity {
		groups = append(groups, group)
	}
	// SelectedMarketplaceProfiles guarantees one exact identity per target ID.
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].identity.TargetID < groups[j].identity.TargetID
	})
	for i := range groups {
		for j := i + 1; j < len(groups); j++ {
			overlap, err := placeSyncRootsOverlap(groups[i].binding.CanonicalRoot, groups[j].binding.CanonicalRoot)
			if err != nil {
				return result, fmt.Errorf(
					"check target roots for %s and %s: %w",
					groups[i].identity.TargetID, groups[j].identity.TargetID, err,
				)
			}
			if overlap {
				return result, fmt.Errorf(
					"target roots for %s and %s overlap",
					groups[i].identity.TargetID, groups[j].identity.TargetID,
				)
			}
		}
	}

	for _, group := range groups {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		committed, err := CommitPlaceUpserts(ctx, group.binding, group.profile, environment, group.packages)
		if err != nil {
			return result, fmt.Errorf("commit place upserts for target %s: %w", group.identity.TargetID, err)
		}
		result.Committed[group.identity.TargetID] = committed
	}
	slog.Info("place sync completed", "pending_profiles", len(result.Pending), "committed_targets", len(result.Committed))
	return result, nil
}

func placeSyncRootsOverlap(left, right string) (overlap bool, retErr error) {
	if placeSyncRootWithin(left, right) || placeSyncRootWithin(right, left) {
		return true, nil
	}
	leftRoot, err := openCanonicalPlaceSyncRoot(left)
	if err != nil {
		return false, fmt.Errorf("open target root %q: %w", left, err)
	}
	defer func() { retErr = errors.Join(retErr, leftRoot.Close()) }()
	rightRoot, err := openCanonicalPlaceSyncRoot(right)
	if err != nil {
		return false, fmt.Errorf("open target root %q: %w", right, err)
	}
	defer func() { retErr = errors.Join(retErr, rightRoot.Close()) }()
	leftIdentity, rightIdentity := leftRoot.Identity(), rightRoot.Identity()
	if leftIdentity.SameObject(rightIdentity) {
		return true, nil
	}
	overlap, err = placeSyncAncestorHasIdentity(right, leftIdentity)
	if err != nil || overlap {
		return overlap, err
	}
	return placeSyncAncestorHasIdentity(left, rightIdentity)
}

func openCanonicalPlaceSyncRoot(rootPath string) (*fsboundary.Dir, error) {
	root, err := fsboundary.OpenDir(rootPath)
	if err != nil {
		return nil, err
	}
	if root.Path() != rootPath {
		return nil, errors.Join(fmt.Errorf("root path %q is not canonical", rootPath), root.Close())
	}
	return root, nil
}

func placeSyncAncestorHasIdentity(candidate string, identity fsboundary.Identity) (bool, error) {
	for ancestor := filepath.Dir(candidate); ancestor != candidate; {
		root, err := openCanonicalPlaceSyncRoot(ancestor)
		if err != nil {
			return false, fmt.Errorf("open target root ancestor %q: %w", ancestor, err)
		}
		matches := identity.SameObject(root.Identity())
		if err := root.Close(); err != nil {
			return false, fmt.Errorf("close target root ancestor %q: %w", ancestor, err)
		}
		if matches {
			return true, nil
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			break
		}
		ancestor = parent
	}
	return false, nil
}

func placeSyncRootWithin(parent, candidate string) bool {
	relative, err := filepath.Rel(parent, candidate)
	return err == nil && (relative == "." ||
		relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative))
}
