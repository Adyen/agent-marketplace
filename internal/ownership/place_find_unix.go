//go:build unix

package ownership

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"sort"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
)

const placeInstallationDirectoryLimit = 4096

// FindPlaceInstallation locates one validated recorded installation under stateRoot.
func FindPlaceInstallation(
	ctx context.Context,
	stateRoot string,
	match func(canonicalRoot, targetID string) bool,
) (string, string, error) {
	if !filepath.IsAbs(stateRoot) || filepath.Clean(stateRoot) != stateRoot || match == nil {
		return "", "", fmt.Errorf("ownership.place-installation-unavailable: invalid state root or matcher")
	}
	root, err := fsboundary.OpenDir(stateRoot)
	if err != nil {
		return "", "", fmt.Errorf("ownership.place-installation-unavailable: open state root: %w", err)
	}
	defer func() { _ = root.Close() }()
	if root.Path() != stateRoot {
		return "", "", fmt.Errorf("ownership.place-installation-unavailable: state root is not canonical")
	}
	ownershipDir, err := root.OpenPath(ownershipDirName)
	if errors.Is(err, fs.ErrNotExist) {
		return "", "", fmt.Errorf("ownership.place-installation-not-found: no place installations")
	}
	if err != nil {
		return "", "", fmt.Errorf("ownership.place-installation-unavailable: open ownership directory: %w", err)
	}
	defer func() { _ = ownershipDir.Close() }()
	native, err := ownershipDir.NativeRoot()
	if err != nil {
		return "", "", fmt.Errorf("ownership.place-installation-unavailable: pin ownership directory: %w", err)
	}
	defer func() { _ = native.Close() }()
	entriesFile, err := native.Open(".")
	if err != nil {
		return "", "", fmt.Errorf("ownership.place-installation-unavailable: list ownership directory: %w", err)
	}
	entries, readErr := entriesFile.ReadDir(placeInstallationDirectoryLimit + 1)
	closeErr := entriesFile.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return "", "", fmt.Errorf("ownership.place-installation-unavailable: list ownership directory: %w", readErr)
	}
	if closeErr != nil {
		return "", "", closeErr
	}
	if len(entries) > placeInstallationDirectoryLimit {
		return "", "", fmt.Errorf("ownership.place-installation-unavailable: too many ownership roots")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	type found struct{ root, target string }
	var matches []found
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return "", "", err
		}
		key := entry.Name()
		if !canonicalPlaceHash(key) {
			continue
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return "", "", fmt.Errorf("ownership.place-installation-unavailable: unsafe ownership root %s: %w", key, fsboundary.ErrUnsafePath)
		}
		if !entry.IsDir() {
			continue
		}
		candidate, err := ownershipDir.OpenPath(key)
		if err != nil {
			return "", "", fmt.Errorf("ownership.place-installation-unavailable: open ownership root %s: %w", key, err)
		}
		statePath := filepath.Join(candidate.Path(), stateFileName)
		data, _, err := candidate.ReadFile(stateFileName, dependencytxn.PlaceFileLimit)
		if errors.Is(err, fs.ErrNotExist) {
			_ = candidate.Close()
			continue
		}
		if err != nil {
			_ = candidate.Close()
			return "", "", fmt.Errorf("ownership.place-installation-unavailable: read ownership root %s: %w", key, err)
		}
		var record Record
		if err := jsonc.Unmarshal(statePath, data, &record); err != nil {
			_ = candidate.Close()
			return "", "", coded(diag.CodeOwnershipStateCorrupt, statePath, err)
		}
		if !filepath.IsAbs(record.CanonicalRoot) || filepath.Clean(record.CanonicalRoot) != record.CanonicalRoot ||
			RootKey(record.CanonicalRoot) != key {
			_ = candidate.Close()
			return "", "", fmt.Errorf("ownership.place-installation-unavailable: ownership root %s does not match its record", key)
		}
		if err := validateRecord(&record, record.CanonicalRoot, key); err != nil {
			_ = candidate.Close()
			return "", "", coded(diag.CodeOwnershipStateCorrupt, statePath, err)
		}
		targets := make(map[string]struct{})
		for _, pkg := range record.PlacePackages {
			targets[pkg.Binding.ProfileIdentity.TargetID] = struct{}{}
		}
		identity := candidate.Identity()
		if closeErr := candidate.Close(); closeErr != nil {
			return "", "", closeErr
		}
		current, err := ownershipDir.OpenPath(key)
		if err != nil || !identity.SameObject(current.Identity()) {
			if current != nil {
				_ = current.Close()
			}
			return "", "", fmt.Errorf("ownership.place-installation-unavailable: ownership root %s changed during scan", key)
		}
		if err := current.Close(); err != nil {
			return "", "", err
		}
		targetIDs := make([]string, 0, len(targets))
		for targetID := range targets {
			targetIDs = append(targetIDs, targetID)
		}
		sort.Strings(targetIDs)
		for _, targetID := range targetIDs {
			if match(record.CanonicalRoot, targetID) {
				matches = append(matches, found{record.CanonicalRoot, targetID})
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	if err := verifyPlaceFindDirectory(root, ownershipDir); err != nil {
		return "", "", err
	}
	if len(matches) == 0 {
		return "", "", fmt.Errorf("ownership.place-installation-not-found: no matching place installation")
	}
	if len(matches) != 1 {
		return "", "", fmt.Errorf("ownership.place-installation-ambiguous: multiple matching place installations")
	}
	return matches[0].root, matches[0].target, nil
}

func verifyPlaceFindDirectory(root, ownershipDir *fsboundary.Dir) error {
	currentRoot, err := fsboundary.OpenDir(root.Path())
	if err != nil {
		return fmt.Errorf("ownership.place-installation-unavailable: reopen state root: %w", err)
	}
	sameRoot := root.Identity().SameObject(currentRoot.Identity())
	currentOwnership, ownershipErr := currentRoot.OpenPath(ownershipDirName)
	if ownershipErr == nil {
		sameOwnership := ownershipDir.Identity().SameObject(currentOwnership.Identity())
		ownershipErr = currentOwnership.Close()
		if !sameOwnership && ownershipErr == nil {
			ownershipErr = fsboundary.ErrGenerationConflict
		}
	}
	closeErr := currentRoot.Close()
	if closeErr != nil {
		return closeErr
	}
	if !sameRoot || ownershipErr != nil {
		return fmt.Errorf("ownership.place-installation-unavailable: state directory changed during scan: %w", errors.Join(ownershipErr, fsboundary.ErrGenerationConflict))
	}
	return nil
}
