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
	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
)

func syncDeployedPlace(ctx context.Context, resolved ResolveResult, offline bool, repoRoot string) (PlaceSyncResult, error) {
	if resolved.placeProfileReader == nil {
		return PlaceSyncResult{}, fmt.Errorf("deployed place profile reader is unavailable")
	}
	environment, err := currentPlaceApprovalEnvironment()
	if err != nil {
		return PlaceSyncResult{}, err
	}
	mode := resolver.MarketplaceResolveModeRefresh
	if offline {
		mode = resolver.MarketplaceResolveModeCachedNoFetch
	}
	cfg := resolved.placeConfig
	if cfg == nil {
		cfg = resolved.MergedCfg
	}
	reads := deployedPlacePruneReads(resolved, cfg)
	preapproval := deployedPlacePreapproval(resolved.placePreapprovals, resolved.placeSelectionReads)
	return syncPlaceUpserts(ctx, cfg, resolved.placeTargets, environment, resolved.placeProfileReader, marketplace.NewResolver(), mode, repoRoot, reads, preapproval)
}

func deployedPlacePreapproval(grants []config.PlacePreapproval, reads []dependencytxn.ReadAssertion) func(targetprofile.ApprovalBinding) error {
	if len(grants) == 0 || len(reads) == 0 {
		return nil
	}
	return func(binding targetprofile.ApprovalBinding) error {
		for _, grant := range grants {
			if !grant.Matches(binding) {
				continue
			}
			if err := dependencytxn.ValidateReadAssertions(reads); err != nil {
				return err
			}
			return targetprofile.RecordOrganizationApproval(binding)
		}
		return targetprofile.ErrApprovalRequired
	}
}

func deployedPlacePruneReads(resolved ResolveResult, cfg *config.Config) []dependencytxn.ReadAssertion {
	if len(resolved.placeTargets) == 0 || resolved.placeProfileReader == nil ||
		len(resolved.placeSelectionReads) == 0 || cfg == nil || len(cfg.MarketplaceExclusions) != 0 {
		return nil
	}
	return append([]dependencytxn.ReadAssertion(nil), resolved.placeSelectionReads...)
}

func currentPlaceApprovalEnvironment() (targetprofile.ApprovalEnvironment, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return targetprofile.ApprovalEnvironment{}, fmt.Errorf("resolve home for place approval: %w", err)
	}
	platform := runtime.GOOS
	if platform == "darwin" {
		platform = "macos"
	}
	if platform != "macos" && platform != "linux" {
		return targetprofile.ApprovalEnvironment{}, fmt.Errorf("target.approval.unsupported-platform: %q", platform)
	}
	xdgConfig := os.Getenv("XDG_CONFIG_HOME")
	if !filepath.IsAbs(xdgConfig) {
		xdgConfig = filepath.Join(home, ".config")
	}
	stateDir, err := dependencytxn.StateDir()
	if err != nil {
		return targetprofile.ApprovalEnvironment{}, fmt.Errorf("resolve state for place approval: %w", err)
	}
	return targetprofile.ApprovalEnvironment{
		Platform: platform, Home: home, XDGConfig: xdgConfig,
		CanonicalStateRoot: filepath.Dir(stateDir),
	}, nil
}

// PlaceSyncResult contains pending profiles, independently committed roots,
// ceded files, and warnings from post-commit checks.
type PlaceSyncResult struct {
	Pending              []targetprofile.Identity
	Committed            map[string]dependencytxn.PlaceResult
	Ceded                map[string][]string
	VerificationWarnings []error
}

// PlaceCheckResult reports pending approval and target IDs whose place plan
// would change their installed projection.
type PlaceCheckResult struct {
	Pending []targetprofile.Identity
	Drift   []string
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
	return syncPlaceUpserts(ctx, cfg, enabled, environment, readProfile, packages, mode, "", nil, nil)
}

func syncPlaceUpserts(
	ctx context.Context,
	cfg *config.Config,
	enabled []string,
	environment targetprofile.ApprovalEnvironment,
	readProfile PlaceProfileReader,
	packages *marketplace.Resolver,
	mode resolver.MarketplaceResolveMode,
	repoRoot string,
	reads []dependencytxn.ReadAssertion,
	preapproval func(targetprofile.ApprovalBinding) error,
) (result PlaceSyncResult, retErr error) {
	slog.Debug("place sync started", "enabled_targets", len(enabled))
	defer func() {
		if retErr != nil {
			slog.Warn("place sync failed", "committed_targets", len(result.Committed), "error", retErr)
		}
	}()
	pruneDeselected := len(reads) > 0
	admission, err := preparePlaceAdmission(ctx, cfg, enabled, environment, readProfile, packages, mode, pruneDeselected, preapproval)
	if err != nil {
		return PlaceSyncResult{}, err
	}
	result = PlaceSyncResult{
		Pending:   append([]targetprofile.Identity(nil), admission.Pending...),
		Committed: make(map[string]dependencytxn.PlaceResult),
	}

	groups, err := buildPlaceSyncGroups(admission, pruneDeselected)
	if err != nil {
		return result, err
	}
	for _, group := range groups {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		committed, ceded, err := commitPlaceChanges(
			ctx, group.binding, group.profile, environment, group.packages, pruneDeselected, reads...,
		)
		if err != nil {
			return result, fmt.Errorf("commit place upserts for target %s: %w", group.identity.TargetID, err)
		}
		result.Committed[group.identity.TargetID] = committed
		if len(ceded) > 0 {
			if result.Ceded == nil {
				result.Ceded = make(map[string][]string)
			}
			result.Ceded[group.identity.TargetID] = append([]string(nil), ceded...)
		}
	}
	if repoRoot != "" {
		result.VerificationWarnings, err = runPlaceInitialLoadChecks(ctx, groups, environment, repoRoot)
		if err != nil {
			return result, err
		}
	}
	slog.Info("place sync completed", "pending_profiles", len(result.Pending), "committed_targets", len(result.Committed))
	return result, nil
}

func runPlaceInitialLoadChecks(
	ctx context.Context,
	groups []*placeSyncGroup,
	environment targetprofile.ApprovalEnvironment,
	repoRoot string,
) ([]error, error) {
	var warnings []error
	for _, group := range groups {
		if err := ctx.Err(); err != nil {
			return warnings, err
		}
		profile, err := targetprofile.Parse(group.profile, group.identity)
		if err != nil {
			warnings = append(warnings, fmt.Errorf("target %s initial-load profile: %w", group.identity.TargetID, err))
			continue
		}
		packages := append([]marketplace.ResolvedPlacePackage(nil), group.packages...)
		sort.SliceStable(packages, func(i, j int) bool {
			left, right := packages[i], packages[j]
			if left.Pin.Kind != right.Pin.Kind {
				return left.Pin.Kind < right.Pin.Kind
			}
			if left.Pin.Name != right.Pin.Name {
				return left.Pin.Name < right.Pin.Name
			}
			if left.Pin.RegistryID != right.Pin.RegistryID {
				return left.Pin.RegistryID < right.Pin.RegistryID
			}
			return left.Entry.InstanceID < right.Entry.InstanceID
		})
		for _, resolved := range packages {
			if err := ctx.Err(); err != nil {
				return warnings, err
			}
			targetID, localKind, scoped := nameutil.SplitPlaceKind(resolved.Pin.Kind)
			if !scoped || targetID != group.identity.TargetID {
				warnings = append(warnings, fmt.Errorf(
					"target.place.initial-load: target %s package %s has an invalid scoped kind",
					group.identity.TargetID, resolved.Pin.Name,
				))
				continue
			}
			kind, ok := profile.Kinds[localKind]
			if !ok || kind.Verify == "" {
				continue
			}
			prepared, err := preparePlaceVerification(
				ctx, group.binding, group.profile, environment, resolved.Pin.Kind, resolved.Pin.Name,
			)
			if err != nil {
				if ctx.Err() != nil {
					return warnings, ctx.Err()
				}
				warnings = append(warnings, fmt.Errorf(
					"target.place.initial-load: target %s package %s/%s: %w",
					group.identity.TargetID, resolved.Pin.Kind, resolved.Pin.Name, err,
				))
				continue
			}
			expected, err := planPlaceUpsert(
				group.binding, prepared.profile, filepath.Dir(prepared.stateDir), resolved,
			)
			if err != nil {
				warnings = append(warnings, fmt.Errorf(
					"target.place.initial-load: target %s package %s/%s does not match admitted input: %w",
					group.identity.TargetID, resolved.Pin.Kind, resolved.Pin.Name, err,
				))
				continue
			}
			if prepared.pkg.ID != expected.owner.ID ||
				prepared.pkg.Binding != expected.owner.Binding ||
				prepared.pkg.GenerationKey != expected.owner.GenerationKey {
				warnings = append(warnings, fmt.Errorf(
					"target.place.initial-load: target %s package %s/%s does not match admitted installation",
					group.identity.TargetID, resolved.Pin.Kind, resolved.Pin.Name,
				))
				continue
			}
			if verification := prepared.pkg.Verification; verification != nil &&
				verification.Outcome == "pass" && verification.Check == prepared.checkName &&
				verification.Scope == prepared.scope {
				continue
			}
			_, err = runPreparedPlaceVerification(
				ctx, prepared, group.profile, environment, repoRoot,
				resolved.Pin.Kind, resolved.Pin.Name, "initial-load",
			)
			if ctx.Err() != nil {
				return warnings, ctx.Err()
			}
			if err != nil {
				warnings = append(warnings, fmt.Errorf(
					"target.place.initial-load: target %s package %s/%s: %w",
					group.identity.TargetID, resolved.Pin.Kind, resolved.Pin.Name, err,
				))
			}
		}
	}
	return warnings, nil
}

func buildPlaceSyncGroups(admission PlaceAdmission, includeEmpty bool) ([]*placeSyncGroup, error) {
	groupsByIdentity := make(map[targetprofile.Identity]*placeSyncGroup)
	if includeEmpty {
		for identity, approval := range admission.Approvals {
			profile, profileLoaded := admission.profiles[identity]
			if !profileLoaded || len(profile) == 0 || approval.Profile != identity {
				return nil, fmt.Errorf("target %s approval is missing its admitted profile", identity.TargetID)
			}
			groupsByIdentity[identity] = &placeSyncGroup{
				identity: identity, binding: approval, profile: profile,
			}
		}
	}
	for _, pkg := range admission.Packages {
		identity := pkg.Pin.LockHostPackageIdentity.ProfileIdentity
		approval, approved := admission.Approvals[identity]
		profile, profileLoaded := admission.profiles[identity]
		if !approved || !profileLoaded || len(profile) == 0 || approval.Profile != identity {
			return nil, fmt.Errorf("target %s package is missing its admitted profile or approval", identity.TargetID)
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
				return nil, fmt.Errorf(
					"check target roots for %s and %s: %w",
					groups[i].identity.TargetID, groups[j].identity.TargetID, err,
				)
			}
			if overlap {
				return nil, fmt.Errorf(
					"target roots for %s and %s overlap",
					groups[i].identity.TargetID, groups[j].identity.TargetID,
				)
			}
		}
	}
	return groups, nil
}

// CheckDeployedPlace prepares the same deployed place inputs as sync but never
// publishes package generations or target files.
func CheckDeployedPlace(ctx context.Context, resolved ResolveResult) (result PlaceCheckResult, retErr error) {
	slog.Debug("deployed place check started", "enabled_targets", len(resolved.placeTargets))
	defer func() {
		slog.Debug("deployed place check completed",
			"pending_profiles", len(result.Pending), "drift_targets", len(result.Drift),
			"error", retErr != nil,
		)
	}()
	if len(resolved.placeTargets) == 0 {
		return result, nil
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if len(resolved.Errors) > 0 {
		return result, fmt.Errorf("resolve errors block deployed place check: %w", errors.Join(resolved.Errors...))
	}
	if resolved.placeProfileReader == nil {
		return result, fmt.Errorf("deployed place profile reader is unavailable")
	}
	environment, err := currentPlaceApprovalEnvironment()
	if err != nil {
		return result, err
	}
	cfg := resolved.placeConfig
	if cfg == nil {
		cfg = resolved.MergedCfg
	}
	if cfg == nil {
		return result, fmt.Errorf("deployed place config is unavailable")
	}
	reads := deployedPlacePruneReads(resolved, cfg)
	pruneDeselected := len(reads) > 0
	if pruneDeselected {
		if err := dependencytxn.ValidateReadAssertions(reads); err != nil {
			return result, err
		}
	}
	admission, err := preparePlaceAdmission(
		ctx, cfg, resolved.placeTargets, environment, resolved.placeProfileReader,
		marketplace.NewResolver(), resolver.MarketplaceResolveModeCachedNoFetch, pruneDeselected, nil,
	)
	if err != nil {
		return result, fmt.Errorf("prepare deployed place check: %w", err)
	}
	result.Pending = append([]targetprofile.Identity(nil), admission.Pending...)
	sort.Slice(result.Pending, func(i, j int) bool {
		return result.Pending[i].TargetID < result.Pending[j].TargetID
	})
	groups, err := buildPlaceSyncGroups(admission, pruneDeselected)
	if err != nil {
		return result, err
	}
	for _, group := range groups {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		prepared, err := preparePlaceChanges(
			ctx, group.binding, group.profile, environment, group.packages, pruneDeselected,
		)
		if err != nil {
			return result, fmt.Errorf("check place upserts for target %s: %w", group.identity.TargetID, err)
		}
		if prepared.changed {
			result.Drift = append(result.Drift, group.identity.TargetID)
		}
	}
	if pruneDeselected {
		if err := dependencytxn.ValidateReadAssertions(reads); err != nil {
			return result, err
		}
	}
	sort.Strings(result.Drift)
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
