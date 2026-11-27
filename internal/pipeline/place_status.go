package pipeline

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
)

type PlaceStatusRow struct {
	TargetID          string
	InstallationID    string
	Kind              string
	Name              string
	Verification      string
	HostVersion       string
	HostVersionStatus string
	Drift             []string
}

// InspectDeployedPlaceStatus reads installed place ownership and file state
// for the currently enabled, deployed Git-backed profiles. It never runs host
// commands or changes ownership state.
func InspectDeployedPlaceStatus(ctx context.Context, resolved ResolveResult) ([]PlaceStatusRow, []error) {
	targets := append([]string(nil), resolved.placeTargets...)
	if len(targets) == 0 || len(resolved.ConfigErrors) > 0 || resolved.placeConfig == nil {
		return nil, nil
	}
	sort.Strings(targets)
	warn := func(targetID string, err error) error {
		return fmt.Errorf("target.place.ownership-unavailable: target %s: %w", targetID, err)
	}
	allTargetsWarn := func(err error) []error {
		warnings := make([]error, 0, len(targets))
		for _, targetID := range targets {
			warnings = append(warnings, warn(targetID, err))
		}
		return warnings
	}
	if resolved.placeProfileReader == nil {
		return nil, allTargetsWarn(errors.New("deployed profile reader is unavailable"))
	}
	environment, err := currentPlaceApprovalEnvironment()
	if err != nil {
		return nil, allTargetsWarn(err)
	}
	stateDir, err := dependencytxn.StateDir()
	if err != nil {
		return nil, allTargetsWarn(err)
	}
	if filepath.Dir(stateDir) != environment.CanonicalStateRoot {
		return nil, allTargetsWarn(errors.New("ownership state root differs from current environment"))
	}
	if err := dependencytxn.CheckPending(stateDir); err != nil {
		return nil, allTargetsWarn(err)
	}
	selected, err := resolved.placeConfig.SelectedMarketplaceProfiles()
	if err != nil {
		return nil, allTargetsWarn(err)
	}

	var rows []PlaceStatusRow
	var warnings []error
	for _, targetID := range targets {
		if err := ctx.Err(); err != nil {
			warnings = append(warnings, warn(targetID, err))
			break
		}
		identity, ok := selected[targetID]
		if !ok {
			warnings = append(warnings, warn(targetID, errors.New("no selected deployed profile identity")))
			continue
		}
		raw, err := resolved.placeProfileReader(ctx, identity, resolver.MarketplaceResolveModeCachedNoFetch)
		if err != nil {
			warnings = append(warnings, warn(targetID, err))
			continue
		}
		profile, err := targetprofile.Parse(raw, identity)
		if err != nil {
			warnings = append(warnings, warn(targetID, err))
			continue
		}
		root, _, err := targetprofile.ResolvePlaceRoot(
			profile, identity, placeProfileEnvironment(profile, environment),
		)
		if err != nil {
			warnings = append(warnings, warn(targetID, err))
			continue
		}
		targetRows, err := inspectDeployedPlaceStatusTarget(
			ctx, targetID, identity, profile, root, environment.CanonicalStateRoot,
		)
		if err != nil {
			warnings = append(warnings, warn(targetID, err))
			continue
		}
		rows = append(rows, targetRows...)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].TargetID != rows[j].TargetID {
			return rows[i].TargetID < rows[j].TargetID
		}
		if rows[i].Kind != rows[j].Kind {
			return rows[i].Kind < rows[j].Kind
		}
		return rows[i].Name < rows[j].Name
	})
	return rows, warnings
}

func inspectDeployedPlaceStatusTarget(
	ctx context.Context,
	targetID string,
	identity targetprofile.Identity,
	profile targetprofile.Profile,
	rootPath, stateRoot string,
) (rows []PlaceStatusRow, retErr error) {
	root, err := openCanonicalPlaceSyncRoot(rootPath)
	if err != nil {
		return nil, err
	}
	rootIdentity := root.Identity()
	defer func() { retErr = errors.Join(retErr, root.Close()) }()
	owner, err := ownership.Open(root.Path(), ownership.WithStateRoot(stateRoot))
	if err != nil {
		return nil, err
	}
	if owner.CanonicalRoot() != rootPath {
		return nil, fmt.Errorf("ownership root differs from current profile root")
	}
	snapshot, err := owner.SnapshotState()
	if err != nil {
		return nil, err
	}
	if !snapshot.HasRecord() {
		return nil, errors.New("no ownership record exists for the current target root")
	}
	projection, err := owner.PlaceFromSnapshot(snapshot)
	if err != nil {
		return nil, err
	}
	for _, pkg := range projection.Packages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if pkg.Binding.ProfileIdentity.TargetID != targetID {
			continue
		}
		drift, err := placeOwnedFileDrift(ctx, root, pkg.Files)
		if err != nil {
			return nil, err
		}
		row := PlaceStatusRow{
			TargetID:       targetID,
			InstallationID: placeInstallationID(targetID, rootPath, stateRoot),
			Kind:           pkg.Kind, Name: pkg.Name, Verification: "unknown", Drift: drift,
		}
		if verification := pkg.Verification; verification != nil {
			switch verification.Outcome {
			case "pass":
				row.Verification = "passed"
			case "fail":
				row.Verification = "failed"
			}
			row.HostVersion = verification.HostVersion
			row.HostVersionStatus = verification.HostVersionStatus
		} else if pkg.Binding.ProfileIdentity == identity {
			_, localKind, scoped := nameutil.SplitPlaceKind(pkg.Kind)
			kind, ok := profile.Kinds[localKind]
			if scoped && ok && kind.Verify != "" {
				row.Verification = "verification-pending"
			} else {
				row.Verification = "not-configured"
			}
		}
		rows = append(rows, row)
	}
	currentRoot, err := openCanonicalPlaceSyncRoot(rootPath)
	if err != nil {
		return nil, fmt.Errorf("reopen current target root: %w", err)
	}
	sameRoot := rootIdentity.SameObject(currentRoot.Identity())
	closeErr := currentRoot.Close()
	if closeErr != nil {
		return nil, fmt.Errorf("close revalidated target root: %w", closeErr)
	}
	if !sameRoot {
		return nil, errors.New("current target root changed while reading ownership")
	}
	return rows, nil
}
