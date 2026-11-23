//go:build unix

package ownership

import (
	"encoding/hex"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
)

type PlaceFile struct {
	Path   string      `json:"path"`
	Digest string      `json:"digest"`
	Mode   fs.FileMode `json:"mode"`
}

type PlacePackage struct {
	ID            string                `json:"id"`
	Kind          string                `json:"kind"`
	Name          string                `json:"name"`
	Binding       targetprofile.Binding `json:"binding"`
	GenerationKey string                `json:"generation_key"`
	Files         []PlaceFile           `json:"files"`
	Requires      []string              `json:"requires,omitempty"`
}

type PlaceProjection struct {
	Packages    []PlacePackage
	Directories []dependencytxn.PlaceDirectory
}

func (s *Store) PlaceFromSnapshot(snapshot StateSnapshot) (PlaceProjection, error) {
	if snapshot.rootKey != s.rootKey {
		return PlaceProjection{}, ErrStateRootMismatch
	}
	record := cloneRecord(snapshot.record)
	if record == nil {
		return PlaceProjection{}, nil
	}
	return PlaceProjection{Packages: record.PlacePackages, Directories: record.PlaceDirectories}, nil
}

func (s *Store) PreparePlace(snapshot StateSnapshot, next PlaceProjection) (dependencytxn.DesiredFile, error) {
	if snapshot.rootKey != s.rootKey {
		return dependencytxn.DesiredFile{}, ErrStateRootMismatch
	}
	if err := validatePlaceProjection(next); err != nil {
		return dependencytxn.DesiredFile{}, err
	}
	current := &Record{
		Version: stateVersion, CanonicalRoot: s.canonicalRoot, RootKey: s.rootKey,
		Files: map[string]FileState{},
	}
	baseGeneration := uint64(0)
	if snapshot.record != nil {
		current = cloneRecord(snapshot.record)
		baseGeneration = current.Generation
	}
	current.Generation = baseGeneration + 1
	current.UpdatedAt = s.now().UTC().Format(time.RFC3339Nano)
	current.PlacePackages = clonePlacePackages(next.Packages)
	current.PlaceDirectories = append([]dependencytxn.PlaceDirectory(nil), next.Directories...)
	if err := validateRecord(current, s.canonicalRoot, s.rootKey); err != nil {
		return dependencytxn.DesiredFile{}, coded(diag.CodeOwnershipStateUnsafeValue, "", err)
	}
	return s.desiredStateFile(snapshot, current)
}

func clonePlacePackages(packages []PlacePackage) []PlacePackage {
	if packages == nil {
		return nil
	}
	cloned := make([]PlacePackage, len(packages))
	for i, pkg := range packages {
		pkg.Files = append([]PlaceFile(nil), pkg.Files...)
		pkg.Requires = append([]string(nil), pkg.Requires...)
		cloned[i] = pkg
	}
	return cloned
}

func validatePlaceProjection(projection PlaceProjection) error {
	invalid := func(reason string) error {
		return fmt.Errorf("%w: invalid place projection: %s", ErrUnsafeStateValue, reason)
	}
	packageIDs := make(map[string]struct{}, len(projection.Packages))
	filePaths := make(map[string]struct{})
	for _, pkg := range projection.Packages {
		if !canonicalPlaceHash(pkg.ID) || !canonicalPlaceHash(pkg.GenerationKey) {
			return invalid("package identity or generation key")
		}
		if !nameutil.IsPlacePackageName(pkg.Name) || !digestPattern.MatchString(pkg.Binding.PackageContentDigest) {
			return invalid("package name or content digest")
		}
		if err := pkg.Binding.ValidateFor(pkg.Kind, pkg.Binding.PackageContentDigest); err != nil {
			return invalid("package profile binding")
		}
		if _, duplicate := packageIDs[pkg.ID]; duplicate {
			return invalid("duplicate package identity")
		}
		packageIDs[pkg.ID] = struct{}{}
		if len(pkg.Files) == 0 {
			return invalid("package has no files")
		}
		for _, file := range pkg.Files {
			if !validPlaceStatePath(file.Path) || !digestPattern.MatchString(file.Digest) ||
				(file.Mode != 0o644 && file.Mode != 0o755) {
				return invalid("package file")
			}
			if _, duplicate := filePaths[file.Path]; duplicate {
				return invalid("duplicate package file path")
			}
			filePaths[file.Path] = struct{}{}
		}
		requirements := make(map[string]struct{}, len(pkg.Requires))
		for _, required := range pkg.Requires {
			if !canonicalPlaceHash(required) || required == pkg.ID {
				return invalid("package dependency identity")
			}
			if _, duplicate := requirements[required]; duplicate {
				return invalid("duplicate package dependency")
			}
			requirements[required] = struct{}{}
		}
	}
	directoryPaths := make(map[string]struct{}, len(projection.Directories))
	for _, directory := range projection.Directories {
		if !validPlaceStatePath(directory.Path) || directory.Identity.Inode == 0 || !directory.Identity.Mode.IsDir() {
			return invalid("created directory")
		}
		if _, duplicate := directoryPaths[directory.Path]; duplicate {
			return invalid("duplicate created directory")
		}
		directoryPaths[directory.Path] = struct{}{}
	}
	return nil
}

func canonicalPlaceHash(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validPlaceStatePath(value string) bool {
	return value != "" && value != "." && utf8.ValidString(value) && !strings.Contains(value, "\\") &&
		path.Clean(value) == value && fs.ValidPath(value) &&
		!strings.ContainsFunc(value, unicode.IsControl)
}
