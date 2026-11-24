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

	retiring := make([]ownership.PlacePackage, 0)
	next := ownership.PlaceProjection{
		Packages:    make([]ownership.PlacePackage, 0, len(installed.Packages)),
		Directories: append([]dependencytxn.PlaceDirectory(nil), installed.Directories...),
	}
	retiredIDs := make(map[string]struct{})
	for _, pkg := range installed.Packages {
		if pkg.Binding.ProfileIdentity.TargetID == targetID {
			retiring = append(retiring, pkg)
			retiredIDs[pkg.ID] = struct{}{}
			continue
		}
		pkg.Files = append([]ownership.PlaceFile(nil), pkg.Files...)
		pkg.Requires = append([]string(nil), pkg.Requires...)
		if pkg.Verification != nil {
			verification := *pkg.Verification
			pkg.Verification = &verification
		}
		next.Packages = append(next.Packages, pkg)
	}
	if len(retiring) == 0 {
		return placeRetirementPlan{}, fmt.Errorf("target.place.not-installed: target %s has no installed packages", targetID)
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
	currentRoot, err := openCanonicalPlaceSyncRoot(root.Path())
	if err != nil {
		return placeRetirementPlan{}, fmt.Errorf("reopen place root after retirement planning: %w", err)
	}
	sameRoot := root.Identity().SameObject(currentRoot.Identity())
	closeErr := currentRoot.Close()
	if closeErr != nil {
		return placeRetirementPlan{}, fmt.Errorf("close revalidated place root: %w", closeErr)
	}
	if !sameRoot {
		return placeRetirementPlan{}, fmt.Errorf("target.place.retirement-root-changed: place root changed during retirement planning")
	}
	sort.Strings(result.Ceded)
	sort.Strings(result.Unproven)
	return result, nil
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
