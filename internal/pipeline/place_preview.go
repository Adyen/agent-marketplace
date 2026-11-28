package pipeline

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/place"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
)

type PlaceImportOwnershipChange struct {
	DestinationPath string `json:"destination_path"`
	Action          string `json:"action"`
}

type PlaceImportPreview struct {
	Conflicts        []place.Refusal
	OwnershipChanges []PlaceImportOwnershipChange
}

func PreviewLocalPlaceImport(destination string, imported targetprofile.ImportedPackage) (preview PlaceImportPreview, retErr error) {
	if !filepath.IsAbs(destination) || filepath.Clean(destination) != destination {
		return preview, errors.New("destination must be an absolute clean path")
	}
	environment, err := currentPlaceApprovalEnvironment()
	if err != nil {
		return preview, err
	}
	stateDir, err := dependencytxn.StateDir()
	if err != nil {
		return preview, err
	}
	if filepath.Dir(stateDir) != environment.CanonicalStateRoot {
		return preview, errors.New("ownership state root differs from current environment")
	}
	if err := dependencytxn.CheckPending(stateDir); err != nil {
		return preview, err
	}
	admitted, _, err := targetprofile.ResolvePlaceRoot(
		imported.Profile, imported.Identity, placeProfileEnvironment(imported.Profile, environment),
	)
	if err != nil {
		return preview, err
	}
	root, err := openCanonicalPlaceSyncRoot(destination)
	if err != nil {
		return preview, fmt.Errorf("open target destination: %w", err)
	}
	defer func() { retErr = errors.Join(retErr, root.Close()) }()
	rootPath := root.Path()
	if rootPath != admitted {
		return preview, errors.New("destination is not the profile's admitted root")
	}

	ownerStore, err := ownership.Open(rootPath, ownership.WithStateRoot(environment.CanonicalStateRoot))
	if err != nil {
		return preview, fmt.Errorf("open target ownership state: %w", err)
	}
	if ownerStore.CanonicalRoot() != rootPath {
		return preview, errors.New("target destination is not canonical")
	}
	snapshot, err := ownerStore.SnapshotState()
	if err != nil {
		return preview, fmt.Errorf("read target ownership state: %w", err)
	}
	projection, err := ownerStore.PlaceFromSnapshot(snapshot)
	if err != nil {
		return preview, fmt.Errorf("read target ownership projection: %w", err)
	}
	projection = normalizePlaceProjection(projection)

	packageID := "local-import:" + imported.Kind + ":" + imported.Name
	matchingOwners := 0
	for _, pkg := range projection.Packages {
		if pkg.Kind == imported.Kind && pkg.Name == imported.Name {
			packageID = pkg.ID
			matchingOwners++
		}
	}
	if matchingOwners > 1 {
		return PlaceImportPreview{
			Conflicts: []place.Refusal{{
				PackageID: packageID, Path: imported.Name,
				Reason: "multiple owned packages share the imported identity",
			}},
			OwnershipChanges: []PlaceImportOwnershipChange{},
		}, nil
	}
	files := make([]place.File, 0, len(imported.Files))
	for _, file := range imported.Files {
		files = append(files, place.File{
			Path: file.DestinationPath, Digest: ownership.Digest(file.Bytes), Mode: file.Mode,
		})
	}
	desired := []place.Package{{
		ID: packageID, Revision: imported.Binding.PackageContentDigest, Files: files,
	}}
	paths := make(map[string]struct{}, len(files))
	for _, file := range files {
		paths[file.Path] = struct{}{}
	}
	installed := placeProjectionPackages(projection)
	for _, pkg := range installed {
		if pkg.ID != packageID {
			continue
		}
		for _, file := range pkg.Files {
			paths[file.Path] = struct{}{}
		}
	}
	observed, err := observePlacePaths(root, paths)
	if err != nil {
		return preview, fmt.Errorf("observe target destination: %w", err)
	}
	plan, err := place.BuildPlan(installed, desired, observed)
	if err != nil {
		return preview, fmt.Errorf("plan target destination: %w", err)
	}
	if len(plan.Refused) != 0 {
		return PlaceImportPreview{
			Conflicts:        plan.Refused,
			OwnershipChanges: []PlaceImportOwnershipChange{},
		}, nil
	}
	preview.OwnershipChanges = make([]PlaceImportOwnershipChange, 0, len(plan.Writes))
	for _, write := range plan.Writes {
		if write.Prior == nil {
			preview.OwnershipChanges = append(preview.OwnershipChanges, PlaceImportOwnershipChange{
				DestinationPath: write.File.Path, Action: "claim",
			})
		}
	}
	return preview, nil
}
