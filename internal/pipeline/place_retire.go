package pipeline

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/place"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
)

type placeRetirementPlan struct {
	Projection ownership.PlaceProjection
	Prunes     []dependencytxn.PlacePrune
	Ceded      []string
	// Unproven paths must block committing Projection and preserve their files.
	Unproven []string
}

func planPlaceRetirement(
	ctx context.Context,
	root *fsboundary.Dir,
	installed ownership.PlaceProjection,
	targetID string,
) (placeRetirementPlan, error) {
	if root == nil {
		return placeRetirementPlan{}, fmt.Errorf("target.place.retirement-root-invalid: target root is required")
	}
	if err := validatePlaceTargetID(targetID); err != nil {
		return placeRetirementPlan{}, err
	}
	if err := ctx.Err(); err != nil {
		return placeRetirementPlan{}, err
	}
	found := false
	for _, pkg := range installed.Packages {
		if pkg.Binding.ProfileIdentity.TargetID == targetID {
			found = true
			break
		}
	}
	if !found {
		return placeRetirementPlan{}, fmt.Errorf("target.place.not-installed: target %s has no installed packages", targetID)
	}
	return planPlacePackageRemoval(ctx, root, installed, func(pkg ownership.PlacePackage) bool {
		return pkg.Binding.ProfileIdentity.TargetID == targetID
	})
}

// planPlaceDeselection plans removal only for one exact profile revision. The
// caller must prove selectedPackageIDs is the complete policy-admitted selection
// and that local approval is valid; it must not pass failed or filtered acquisition.
func planPlaceDeselection(
	ctx context.Context,
	root *fsboundary.Dir,
	installed ownership.PlaceProjection,
	profile targetprofile.Identity,
	selectedPackageIDs map[string]struct{},
) (placeRetirementPlan, error) {
	if err := profile.Validate(); err != nil {
		return placeRetirementPlan{}, fmt.Errorf("target.place.profile-invalid: %w", err)
	}
	return planPlacePackageRemoval(ctx, root, installed, func(pkg ownership.PlacePackage) bool {
		if pkg.Binding.ProfileIdentity != profile {
			return false
		}
		_, selected := selectedPackageIDs[pkg.ID]
		return !selected
	})
}

func planPlaceDeselectedDirectories(
	root *fsboundary.Dir,
	recorded []dependencytxn.PlaceDirectory,
	live []ownership.PlacePackage,
	removed []dependencytxn.PlacePrune,
	ceded []string,
) ([]dependencytxn.PlacePrune, error) {
	if root == nil {
		return nil, fmt.Errorf("target.place.retirement-root-invalid: target root is required")
	}
	candidates := make(map[string]bool)
	protected := make(map[string]bool)
	addParents := func(paths map[string]bool, file string) {
		for parent := path.Dir(file); parent != "."; parent = path.Dir(parent) {
			paths[parent] = true
		}
	}
	for _, prune := range removed {
		addParents(candidates, prune.Path)
	}
	for _, file := range ceded {
		addParents(candidates, file)
	}
	for _, pkg := range live {
		for _, file := range pkg.Files {
			addParents(protected, file.Path)
		}
	}

	directoryPrunes := make([]dependencytxn.PlacePrune, 0, len(recorded))
	for _, directory := range recorded {
		if !candidates[directory.Path] || protected[directory.Path] {
			continue
		}
		actual, err := root.OpenPath(filepath.FromSlash(directory.Path))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("inspect recorded place directory %s: %w", directory.Path, err)
		}
		actualIdentity := actual.Identity()
		if err := actual.Close(); err != nil {
			return nil, fmt.Errorf("close recorded place directory %s: %w", directory.Path, err)
		}
		if !actualIdentity.SameObject(directory.Identity) {
			continue
		}

		parentPath := path.Dir(directory.Path)
		parentIdentity := root.Identity()
		if parentPath != "." {
			parent, err := root.OpenPath(filepath.FromSlash(parentPath))
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("inspect parent of recorded place directory %s: %w", directory.Path, err)
			}
			parentIdentity = parent.Identity()
			if err := parent.Close(); err != nil {
				return nil, fmt.Errorf("close parent of recorded place directory %s: %w", directory.Path, err)
			}
		}
		directoryIdentity := directory.Identity
		directoryPrunes = append(directoryPrunes, dependencytxn.PlacePrune{
			Path: directory.Path, Parent: parentIdentity, Directory: &directoryIdentity,
		})
	}
	sort.Slice(directoryPrunes, func(i, j int) bool {
		depthI := strings.Count(directoryPrunes[i].Path, "/")
		depthJ := strings.Count(directoryPrunes[j].Path, "/")
		if depthI != depthJ {
			return depthI > depthJ
		}
		return directoryPrunes[i].Path < directoryPrunes[j].Path
	})
	return directoryPrunes, nil
}

func planPlacePackageRemoval(
	ctx context.Context,
	root *fsboundary.Dir,
	installed ownership.PlaceProjection,
	removePackage func(ownership.PlacePackage) bool,
) (placeRetirementPlan, error) {
	if root == nil {
		return placeRetirementPlan{}, fmt.Errorf("target.place.retirement-root-invalid: target root is required")
	}
	if removePackage == nil {
		return placeRetirementPlan{}, fmt.Errorf("target.place.removal-invalid: package selector is required")
	}
	if err := ctx.Err(); err != nil {
		return placeRetirementPlan{}, err
	}
	retiring := make([]ownership.PlacePackage, 0)
	next := ownership.PlaceProjection{
		Packages:    make([]ownership.PlacePackage, 0, len(installed.Packages)),
		Directories: append([]dependencytxn.PlaceDirectory(nil), installed.Directories...),
	}
	retiredIDs := make(map[string]struct{})
	for _, pkg := range installed.Packages {
		if removePackage(pkg) {
			retiring = append(retiring, pkg)
			retiredIDs[pkg.ID] = struct{}{}
			continue
		}
		next.Packages = append(next.Packages, clonePlaceRetirementPackage(pkg))
	}
	if len(retiring) == 0 {
		result := placeRetirementPlan{Projection: next}
		if err := revalidatePlacePackageRemovalRoot(ctx, root); err != nil {
			return placeRetirementPlan{}, err
		}
		return result, nil
	}
	sort.Slice(retiring, func(i, j int) bool { return retiring[i].ID < retiring[j].ID })
	for _, pkg := range next.Packages {
		for _, dependencyID := range pkg.Requires {
			if _, retiresDependency := retiredIDs[dependencyID]; retiresDependency {
				return placeRetirementPlan{}, fmt.Errorf(
					"target.place.retirement-dependency: surviving package %s requires retiring package %s",
					pkg.ID, dependencyID,
				)
			}
		}
	}

	retiringProjection := ownership.PlaceProjection{Packages: retiring}
	installedPackages := placeProjectionPackages(retiringProjection)
	observed := make(map[string]place.Observation)
	provenParents := make(map[string]fsboundary.Identity)
	var unproven []string
	ownedFiles := make(map[string]ownership.PlaceFile)
	for _, pkg := range retiring {
		for _, file := range pkg.Files {
			ownedFiles[file.Path] = file
		}
	}
	directories := make(map[string]fsboundary.Identity, len(installed.Directories))
	for _, directory := range installed.Directories {
		directories[directory.Path] = directory.Identity
	}
	for _, pkg := range installedPackages {
		for _, file := range pkg.Files {
			if err := ctx.Err(); err != nil {
				return placeRetirementPlan{}, err
			}
			ownedFile, ok := ownedFiles[file.Path]
			if !ok {
				return placeRetirementPlan{}, fmt.Errorf("place retirement plan references unknown file %s", file.Path)
			}
			parent, recordedParent, proven, err := openPlaceRetirementParent(root, directories, ownedFile)
			if err != nil {
				return placeRetirementPlan{}, err
			}
			if !proven {
				unproven = append(unproven, file.Path)
				continue
			}
			provenParents[file.Path] = recordedParent
			data, identity, readErr := parent.ReadFile(path.Base(file.Path), dependencytxn.PlaceFileLimit)
			closeErr := parent.Close()
			if errors.Is(readErr, fsboundary.ErrGenerationConflict) {
				return placeRetirementPlan{}, errors.Join(
					fmt.Errorf("observe retirement path %s: %w", file.Path, readErr), closeErr,
				)
			}
			if readErr != nil && !placeRetirementCedeReadError(readErr) {
				return placeRetirementPlan{}, errors.Join(
					fmt.Errorf("observe retirement path %s: %w", file.Path, readErr), closeErr,
				)
			}
			if closeErr != nil {
				return placeRetirementPlan{}, fmt.Errorf("close retirement parent for %s: %w", file.Path, closeErr)
			}
			if readErr != nil {
				continue
			}
			observed[file.Path] = place.Observation{Mode: identity.Mode, Digest: ownership.Digest(data)}
		}
	}
	planned, err := place.BuildPlan(installedPackages, nil, observed)
	if err != nil {
		return placeRetirementPlan{}, fmt.Errorf("plan installed place retirement: %w", err)
	}
	if len(planned.Refused) > 0 {
		return placeRetirementPlan{}, fmt.Errorf("plan installed place retirement: package paths have conflicting ownership")
	}
	result := placeRetirementPlan{Projection: next, Unproven: unproven}
	for _, ceded := range planned.Ceded {
		if _, proven := provenParents[ceded.File.Path]; proven {
			result.Ceded = append(result.Ceded, ceded.File.Path)
		}
	}
	for _, removal := range planned.Removes {
		if err := ctx.Err(); err != nil {
			return placeRetirementPlan{}, err
		}
		recordedParent, proven := provenParents[removal.File.Path]
		if !proven {
			return placeRetirementPlan{}, fmt.Errorf(
				"place retirement plan has no proven parent for removal %s", removal.File.Path,
			)
		}
		result.Prunes = append(result.Prunes, dependencytxn.PlacePrune{
			Path: removal.File.Path, Parent: recordedParent,
			Expected: dependencytxn.Generation{
				Exists: true, Digest: strings.TrimPrefix(removal.File.Digest, "sha256:"),
			},
			Mode: removal.File.Mode,
		})
	}
	if err := ctx.Err(); err != nil {
		return placeRetirementPlan{}, err
	}
	if err := revalidatePlacePackageRemovalRoot(ctx, root); err != nil {
		return placeRetirementPlan{}, err
	}
	sort.Strings(result.Ceded)
	sort.Strings(result.Unproven)
	return result, nil
}

func clonePlaceRetirementPackage(pkg ownership.PlacePackage) ownership.PlacePackage {
	pkg.Files = append([]ownership.PlaceFile(nil), pkg.Files...)
	for i := range pkg.Files {
		if pkg.Files[i].Parent != nil {
			parent := *pkg.Files[i].Parent
			pkg.Files[i].Parent = &parent
		}
	}
	pkg.Requires = append([]string(nil), pkg.Requires...)
	if pkg.Verification != nil {
		verification := *pkg.Verification
		pkg.Verification = &verification
	}
	return pkg
}

func revalidatePlacePackageRemovalRoot(ctx context.Context, root *fsboundary.Dir) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	currentRoot, err := openCanonicalPlaceSyncRoot(root.Path())
	if err != nil {
		return fmt.Errorf("reopen place root after retirement planning: %w", err)
	}
	sameRoot := root.Identity().SameObject(currentRoot.Identity())
	closeErr := currentRoot.Close()
	if closeErr != nil {
		return fmt.Errorf("close revalidated place root: %w", closeErr)
	}
	if !sameRoot {
		return fmt.Errorf("target.place.retirement-root-changed: place root changed during removal planning")
	}
	return nil
}

func openPlaceRetirementParent(
	root *fsboundary.Dir,
	directories map[string]fsboundary.Identity,
	file ownership.PlaceFile,
) (*fsboundary.Dir, fsboundary.Identity, bool, error) {
	parentPath := path.Dir(file.Path)
	var recordedParent fsboundary.Identity
	if file.Parent != nil {
		recordedParent = *file.Parent
	} else {
		if parentPath == "." {
			return nil, fsboundary.Identity{}, false, nil
		}
		var recorded bool
		recordedParent, recorded = directories[parentPath]
		if !recorded {
			return nil, fsboundary.Identity{}, false, nil
		}
	}
	if recordedParent.Inode == 0 || !recordedParent.Mode.IsDir() {
		return nil, fsboundary.Identity{}, false, nil
	}
	parent, err := root.OpenPath(filepath.FromSlash(parentPath))
	if err != nil {
		return nil, fsboundary.Identity{}, false, nil
	}
	if !parent.Identity().SameObject(recordedParent) {
		if err := parent.Close(); err != nil {
			return nil, fsboundary.Identity{}, false, fmt.Errorf("close unproven retirement parent %s: %w", parentPath, err)
		}
		return nil, fsboundary.Identity{}, false, nil
	}
	return parent, recordedParent, true, nil
}

func placeRetirementCedeReadError(err error) bool {
	return errors.Is(err, fs.ErrNotExist) ||
		errors.Is(err, fsboundary.ErrUnsafePath) ||
		errors.Is(err, fsboundary.ErrUnsafeFile) ||
		errors.Is(err, fsboundary.ErrTooLarge)
}
