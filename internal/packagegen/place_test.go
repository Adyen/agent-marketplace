package packagegen

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
)

// spec: target-profile-place/generations-stay-referenced-while-needed
func TestPlaceGenerationCollection(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_STATE_HOME", filepath.Join(base, "state"))
	t.Setenv("HOME", base)

	root := filepath.Join(base, "target")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	profile := targetprofile.Identity{
		TargetID: "herdr-app", SourceID: "builtin:agent-marketplace",
		RevisionDigest: "sha256:" + strings.Repeat("a", 64),
	}
	firstFile := targetprofile.PackageFile{
		SourcePath: "source/first.json", DestinationPath: "mods/first.json",
		Mode: 0o644, Bytes: []byte(`{"package":"first"}`),
	}
	firstInput := placeGenerationInput(t, profile, firstFile)
	first, err := PlanPlace(firstInput)
	if err != nil {
		t.Fatal(err)
	}
	stateRoot, err := StateRoot()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(stateRoot, "packages")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("planning created package storage: %v", err)
	}
	if first.DataPath != "" || first.Path != filepath.Join(stateRoot, "packages", first.Key) ||
		len(first.Key) != 64 || strings.Trim(first.Key, "0123456789abcdef") != "" {
		t.Fatalf("place generation = %+v", first)
	}

	publish := func(input PlaceInput) (Generation, error) {
		var generation Generation
		err := WithPackagesLock(context.Background(), func() error {
			var err error
			generation, err = PublishPlaceUnderPackagesLock(input)
			return err
		})
		return generation, err
	}
	if got, err := publish(firstInput); err != nil || got != first {
		t.Fatalf("publish first place generation = %+v, %v; want %+v", got, err, first)
	}
	if got, err := os.ReadFile(filepath.Join(first.Path, firstFile.SourcePath)); err != nil || !reflect.DeepEqual(got, firstFile.Bytes) {
		t.Fatalf("generation did not retain its source path bytes: %q / %v", got, err)
	}
	if _, err := os.Lstat(filepath.Join(first.Path, firstFile.DestinationPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("generation stored a destination path: %v", err)
	}
	if got, err := publish(firstInput); err != nil || got != first {
		t.Fatalf("reuse first place generation = %+v, %v; want %+v", got, err, first)
	}
	extraFile := filepath.Join(first.Path, "unexpected.txt")
	if err := os.WriteFile(extraFile, []byte("unexpected"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := publish(firstInput); err == nil {
		t.Fatal("place generation with an extra file was reused")
	}
	if err := os.Remove(extraFile); err != nil {
		t.Fatal(err)
	}
	directoryLink := filepath.Join(first.Path, "unexpected-directory")
	if err := os.Symlink(base, directoryLink); err != nil {
		t.Fatal(err)
	}
	if _, err := publish(firstInput); err == nil {
		t.Fatal("place generation with a symlink directory was reused")
	}
	if err := os.Remove(directoryLink); err != nil {
		t.Fatal(err)
	}
	packagesAlias := filepath.Join(base, "packages-alias")
	if err := os.Symlink(filepath.Join(stateRoot, "packages"), packagesAlias); err != nil {
		t.Fatal(err)
	}
	if err := verifyPlaceGeneration(filepath.Join(packagesAlias, first.Key), []targetprofile.PackageFile{firstFile}); err == nil {
		t.Fatal("place generation with an aliased root was reused")
	}

	generationFile := filepath.Join(first.Path, firstFile.SourcePath)
	if err := os.Chmod(generationFile, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := publish(firstInput); err == nil {
		t.Fatal("place generation with edited mode was reused")
	}
	if err := os.Chmod(generationFile, firstFile.Mode); err != nil {
		t.Fatal(err)
	}
	mismatched := firstInput
	mismatched.Binding.DeliveryMappingDigest = "sha256:" + strings.Repeat("b", 64)
	if _, err := PlanPlace(mismatched); err == nil {
		t.Fatal("place plan accepted a binding that did not match its files")
	}

	firstPackage := placeOwnershipPackage(firstInput, first)
	store, err := ownership.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.SnapshotState()
	if err != nil {
		t.Fatal(err)
	}
	ownershipFile, err := store.PreparePlace(snapshot, ownership.PlaceProjection{Packages: []ownership.PlacePackage{firstPackage}})
	if err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(stateRoot, "dependencies")
	rootIdentity := placeRootIdentity(t, root)
	_, err = dependencytxn.CommitPlace(context.Background(), stateDir, dependencytxn.PlacePlan{
		Root: store.CanonicalRoot(), RootIdentity: rootIdentity, Ownership: ownershipFile,
	}, dependencytxn.WithFsyncDir(func(string) error {
		return errors.New("interrupted before ownership publication")
	}))
	if err == nil {
		t.Fatal("interrupted ownership-only place commit succeeded")
	}
	if _, err := os.Lstat(ownershipFile.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("interrupted ownership-only commit published ownership: %v", err)
	}
	if entries, err := os.ReadDir(stateDir); err != nil || len(entries) != 1 {
		t.Fatalf("interrupted commit journal entries = %v / %v", entries, err)
	}
	pending, err := dependencytxn.ReadPendingPlaceGenerationKeys(stateDir)
	if err != nil || !reflect.DeepEqual(pending, []string{first.Key}) {
		t.Fatalf("pending place generation keys = %v / %v", pending, err)
	}
	if removed, err := GarbageCollect(false); err != nil || len(removed) != 0 {
		t.Fatalf("collection during interrupted ownership commit = %v / %v", removed, err)
	}
	if _, err := os.Stat(first.Path); err != nil {
		t.Fatalf("pending ownership did not retain generation: %v", err)
	}
	if err := dependencytxn.Recover(context.Background(), stateDir); err != nil {
		t.Fatalf("clear interrupted ownership-only intent: %v", err)
	}
	if entries, err := os.ReadDir(stateDir); err != nil || len(entries) != 0 {
		t.Fatalf("recovered journal entries = %v / %v", entries, err)
	}

	// Once a target write has happened, recovery finishes ownership publication
	// instead of clearing the intent. Complete that handoff inside GC's snapshot.
	interrupted := errors.New("stop after target write")
	_, err = dependencytxn.CommitPlace(t.Context(), stateDir, dependencytxn.PlacePlan{
		Root: store.CanonicalRoot(), RootIdentity: rootIdentity, Ownership: ownershipFile,
		Directories: []string{filepath.Dir(firstFile.DestinationPath)},
		Writes: []dependencytxn.PlaceWrite{{
			Path: firstFile.DestinationPath, GenerationKey: first.Key, SourcePath: firstFile.SourcePath,
			Digest: dependencytxn.Digest(firstFile.Bytes), Mode: firstFile.Mode, Size: int64(len(firstFile.Bytes)),
			ParentCreated: true,
		}},
	}, dependencytxn.WithFaultHook(func(p dependencytxn.FaultPoint) error {
		if p.Stage == dependencytxn.FaultPlaceAfterPublish {
			return interrupted
		}
		return nil
	}))
	if !errors.Is(err, interrupted) {
		t.Fatalf("interrupt place transaction: %v", err)
	}
	if removed := collectAcrossRecovery(t, stateDir); len(removed) != 0 {
		t.Fatalf("collection across place recovery removed %v", removed)
	}
	if keys, err := ownership.ReadCommittedPackageGenerationKeys(); err != nil || !reflect.DeepEqual(keys, []string{first.Key}) {
		t.Fatalf("recovered place references = %v / %v", keys, err)
	}
	secondFile := targetprofile.PackageFile{
		SourcePath: "source/second.json", DestinationPath: "mods/second.json",
		Mode: 0o644, Bytes: []byte(`{"package":"second"}`),
	}
	secondInput := placeGenerationInput(t, profile, secondFile)
	second, err := publish(secondInput)
	if err != nil {
		t.Fatalf("publish second place generation: %v", err)
	}
	secondPackage := placeOwnershipPackage(secondInput, second)
	commitPlaceProjection(t, store, stateDir, rootIdentity, ownership.PlaceProjection{
		Packages: []ownership.PlacePackage{firstPackage, secondPackage},
	})
	if removed, err := GarbageCollect(false); err != nil || len(removed) != 0 {
		t.Fatalf("collection of committed place references = %v / %v", removed, err)
	}
	commitPlaceProjection(t, store, stateDir, rootIdentity, ownership.PlaceProjection{
		Packages: []ownership.PlacePackage{secondPackage},
	})
	if removed, err := GarbageCollect(false); err != nil || !reflect.DeepEqual(removed, []string{first.Path}) {
		t.Fatalf("collection after deselection = %v / %v; want only %s", removed, err, first.Path)
	}
	if _, err := os.Stat(second.Path); err != nil {
		t.Fatalf("collection removed a still-referenced generation: %v", err)
	}
}

func placeGenerationInput(t *testing.T, profile targetprofile.Identity, file targetprofile.PackageFile) PlaceInput {
	t.Helper()
	binding, err := targetprofile.BuildBinding(profile, []targetprofile.PackageFile{file})
	if err != nil {
		t.Fatal(err)
	}
	return PlaceInput{Binding: binding, Files: []targetprofile.PackageFile{file}}
}

func placeOwnershipPackage(input PlaceInput, generation Generation) ownership.PlacePackage {
	file := input.Files[0]
	return ownership.PlacePackage{
		ID: generation.Key, Kind: input.Binding.ProfileIdentity.TargetID + "/mod",
		Name:    strings.TrimSuffix(filepath.Base(file.SourcePath), filepath.Ext(file.SourcePath)),
		Binding: input.Binding, GenerationKey: generation.Key,
		Files: []ownership.PlaceFile{{Path: file.DestinationPath, Digest: ownership.Digest(file.Bytes), Mode: file.Mode}},
	}
}

func placeRootIdentity(t *testing.T, root string) fsboundary.Identity {
	t.Helper()
	dir, err := fsboundary.OpenDir(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dir.Close() }()
	return dir.Identity()
}

func commitPlaceProjection(t *testing.T, store *ownership.Store, stateDir string, rootIdentity fsboundary.Identity, projection ownership.PlaceProjection) {
	t.Helper()
	snapshot, err := store.SnapshotState()
	if err != nil {
		t.Fatal(err)
	}
	file, err := store.PreparePlace(snapshot, projection)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dependencytxn.CommitPlace(context.Background(), stateDir, dependencytxn.PlacePlan{
		Root: store.CanonicalRoot(), RootIdentity: rootIdentity, Ownership: file,
	}); err != nil {
		t.Fatal(err)
	}
}
