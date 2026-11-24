package pipeline

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
)

// PlaceRetirementPreview is the safe, read-only summary shown before retirement.
type PlaceRetirementPreview struct {
	InstallationID string   `json:"installation_id"`
	TargetID       string   `json:"target_id"`
	CanonicalRoot  string   `json:"canonical_root"`
	StateRoot      string   `json:"state_root"`
	Remove         []string `json:"remove"`
	Ceded          []string `json:"ceded"`
}

// PlaceRetirementResult reports files actually removed and ceded by retirement.
type PlaceRetirementResult struct {
	Removed []string
	Ceded   []string
}

// PreparedPlaceRetirement pins the retirement context until confirmation.
type PreparedPlaceRetirement struct {
	preview       PlaceRetirementPreview
	stateDir      string
	stateIdentity fsboundary.Identity
	plan          dependencytxn.PlacePlan
}

// Preview returns a copy of the retirement summary.
func (prepared PreparedPlaceRetirement) Preview() PlaceRetirementPreview {
	preview := prepared.preview
	preview.Remove = append([]string(nil), preview.Remove...)
	preview.Ceded = append([]string(nil), preview.Ceded...)
	return preview
}

func validPlaceInstallationID(value string) bool {
	return len(value) == 64 && nameutil.IsCanonicalSHA256Digest("sha256:"+value)
}

// PreparePlaceRetirement plans retirement from the current XDG ownership store.
func PreparePlaceRetirement(ctx context.Context, installationID string) (PreparedPlaceRetirement, error) {
	if !validPlaceInstallationID(installationID) {
		return PreparedPlaceRetirement{}, fmt.Errorf("target.place.installation-unavailable: invalid installation ID")
	}
	stateDir, err := dependencytxn.StateDir()
	if err != nil {
		return PreparedPlaceRetirement{}, err
	}
	stateRoot := filepath.Dir(stateDir)
	if err := dependencytxn.CheckPending(stateDir); err != nil {
		return PreparedPlaceRetirement{}, err
	}
	stateRootPin, err := openCanonicalPlaceSyncRoot(stateRoot)
	if err != nil {
		return PreparedPlaceRetirement{}, fmt.Errorf("target.place.installation-unavailable: open state root: %w", err)
	}
	defer func() { _ = stateRootPin.Close() }()
	stateIdentity := stateRootPin.Identity()
	rootPath, targetID, err := ownership.FindPlaceInstallation(ctx, stateRoot, func(root, target string) bool {
		return placeInstallationID(target, root, stateRoot) == installationID
	})
	if err != nil {
		return PreparedPlaceRetirement{}, err
	}
	root, err := openCanonicalPlaceSyncRoot(rootPath)
	if err != nil {
		return PreparedPlaceRetirement{}, fmt.Errorf("target.place.installation-unavailable: open recorded root: %w", err)
	}
	defer func() { _ = root.Close() }()
	owner, err := ownership.Open(root.Path(), ownership.WithStateRoot(stateRoot))
	if err != nil {
		return PreparedPlaceRetirement{}, err
	}
	snapshot, err := owner.SnapshotState()
	if err != nil {
		return PreparedPlaceRetirement{}, err
	}
	projection, err := owner.PlaceFromSnapshot(snapshot)
	if err != nil {
		return PreparedPlaceRetirement{}, err
	}
	if placeInstallationID(targetID, root.Path(), stateRoot) != installationID {
		return PreparedPlaceRetirement{}, fmt.Errorf("target.place.installation-unavailable: installation changed during lookup")
	}
	planned, err := planPlaceRetirement(ctx, root, projection, targetID)
	if err != nil {
		return PreparedPlaceRetirement{}, err
	}
	if len(planned.Unproven) != 0 {
		return PreparedPlaceRetirement{}, fmt.Errorf("target.place.retirement-unproven: cannot retire files without parent proof: %v", planned.Unproven)
	}
	desired, err := owner.PreparePlace(snapshot, planned.Projection)
	if err != nil {
		return PreparedPlaceRetirement{}, err
	}
	remove := make([]string, 0, len(planned.Prunes))
	for _, prune := range planned.Prunes {
		remove = append(remove, prune.Path)
	}
	sort.Strings(remove)
	preview := PlaceRetirementPreview{
		InstallationID: installationID, TargetID: targetID,
		CanonicalRoot: root.Path(), StateRoot: stateRoot,
		Remove: remove, Ceded: append([]string(nil), planned.Ceded...),
	}
	plan := dependencytxn.PlacePlan{
		Root: root.Path(), RootIdentity: root.Identity(),
		Ownership: desired, Prunes: append([]dependencytxn.PlacePrune(nil), planned.Prunes...),
	}
	stateRootNow, err := openCanonicalPlaceSyncRoot(stateRoot)
	if err != nil {
		return PreparedPlaceRetirement{}, fmt.Errorf("target.place.installation-unavailable: reopen state root: %w", err)
	}
	stateRootSame := stateIdentity.SameObject(stateRootNow.Identity())
	closeErr := stateRootNow.Close()
	if closeErr != nil {
		return PreparedPlaceRetirement{}, closeErr
	}
	if !stateRootSame {
		return PreparedPlaceRetirement{}, fmt.Errorf("target.place.retirement-context-changed: state root changed during preparation")
	}
	return PreparedPlaceRetirement{
		preview: preview, stateDir: stateDir, stateIdentity: stateIdentity, plan: plan,
	}, nil
}

// CommitPreparedPlaceRetirement revalidates the confirmed context before pruning.
func CommitPreparedPlaceRetirement(ctx context.Context, prepared PreparedPlaceRetirement) (PlaceRetirementResult, error) {
	current, err := PreparePlaceRetirement(ctx, prepared.preview.InstallationID)
	if err != nil {
		return PlaceRetirementResult{}, err
	}
	if current.stateDir != prepared.stateDir ||
		!current.stateIdentity.SameObject(prepared.stateIdentity) ||
		current.plan.Root != prepared.plan.Root ||
		!current.plan.RootIdentity.SameObject(prepared.plan.RootIdentity) ||
		!reflect.DeepEqual(current.preview, prepared.preview) ||
		current.plan.Ownership.Path != prepared.plan.Ownership.Path ||
		current.plan.Ownership.Expected != prepared.plan.Ownership.Expected ||
		current.plan.Ownership.ExpectedMode != prepared.plan.Ownership.ExpectedMode ||
		current.plan.Ownership.Mode != prepared.plan.Ownership.Mode ||
		!reflect.DeepEqual(current.plan.Prunes, prepared.plan.Prunes) {
		return PlaceRetirementResult{}, fmt.Errorf("target.place.retirement-context-changed: recorded installation changed during confirmation")
	}
	if err := ctx.Err(); err != nil {
		return PlaceRetirementResult{}, err
	}
	committed, err := dependencytxn.CommitPlace(ctx, current.stateDir, current.plan)
	if err != nil {
		return PlaceRetirementResult{}, err
	}
	pruneSet := make(map[string]struct{}, len(current.plan.Prunes))
	for _, prune := range current.plan.Prunes {
		pruneSet[prune.Path] = struct{}{}
	}
	removedSet := make(map[string]struct{})
	for _, path := range committed.Changed {
		if _, ok := pruneSet[path]; ok {
			removedSet[path] = struct{}{}
		}
	}
	cededSet := make(map[string]struct{})
	for _, path := range current.preview.Ceded {
		cededSet[path] = struct{}{}
	}
	for _, path := range committed.Retained {
		cededSet[path] = struct{}{}
	}
	result := PlaceRetirementResult{}
	for path := range removedSet {
		result.Removed = append(result.Removed, path)
	}
	for path := range cededSet {
		result.Ceded = append(result.Ceded, path)
	}
	sort.Strings(result.Removed)
	sort.Strings(result.Ceded)
	return result, nil
}
