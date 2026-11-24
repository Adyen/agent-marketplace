//go:build unix

package ownership

import (
	"encoding/hex"
	"fmt"
	"io/fs"
	"path"
	"regexp"
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
	Verification  *PlaceVerification    `json:"verification,omitempty"`
}

// PlaceVerification records an outcome for one exact installed package generation.
type PlaceVerification struct {
	Binding           targetprofile.Binding `json:"binding"`
	GenerationKey     string                `json:"generation_key"`
	Check             string                `json:"check"`
	Scope             string                `json:"scope"`
	HostVersion       string                `json:"host_version,omitempty"`
	HostVersionStatus string                `json:"host_version_status"`
	Outcome           string                `json:"outcome"`
	EvidenceSource    string                `json:"evidence_source"`
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

// PreparePlaceVerification prepares a CAS update for an installed place package.
func (s *Store) PreparePlaceVerification(
	snapshot StateSnapshot,
	packageID string,
	result PlaceVerification,
) (dependencytxn.DesiredFile, error) {
	projection, err := s.PlaceFromSnapshot(snapshot)
	if err != nil {
		return dependencytxn.DesiredFile{}, err
	}
	if snapshot.record == nil {
		return dependencytxn.DesiredFile{}, fmt.Errorf("%w: place verification requires an ownership record", ErrStateMissing)
	}
	for i := range projection.Packages {
		pkg := &projection.Packages[i]
		if pkg.ID != packageID {
			continue
		}
		if err := validatePlaceVerification(*pkg, result); err != nil {
			return dependencytxn.DesiredFile{}, err
		}
		if pkg.Verification != nil && *pkg.Verification == result {
			return dependencytxn.DesiredFile{}, nil
		}
		verification := result
		pkg.Verification = &verification
		return s.PreparePlace(snapshot, projection)
	}
	return dependencytxn.DesiredFile{}, fmt.Errorf("%w: place package is not installed", ErrUnsafeStateValue)
}

func clonePlacePackages(packages []PlacePackage) []PlacePackage {
	if packages == nil {
		return nil
	}
	cloned := make([]PlacePackage, len(packages))
	for i, pkg := range packages {
		pkg.Files = append([]PlaceFile(nil), pkg.Files...)
		pkg.Requires = append([]string(nil), pkg.Requires...)
		if pkg.Verification != nil {
			verification := *pkg.Verification
			pkg.Verification = &verification
		}
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
		if pkg.Verification != nil {
			if err := validatePlaceVerification(pkg, *pkg.Verification); err != nil {
				return invalid("package verification")
			}
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

var canonicalPlaceHostVersion = regexp.MustCompile(`^([0-9]{1,9}\.){2}[0-9]{1,9}$`)

func validatePlaceVerification(pkg PlacePackage, verification PlaceVerification) error {
	invalid := func(reason string) error {
		return fmt.Errorf("%w: invalid place verification: %s", ErrUnsafeStateValue, reason)
	}
	if verification.Binding != pkg.Binding || verification.GenerationKey != pkg.GenerationKey {
		return invalid("package binding or generation")
	}
	if !nameutil.IsTargetIdentifier(verification.Check) {
		return invalid("check target")
	}
	if verification.Scope != "eager-package-load" {
		return invalid("verification scope")
	}
	if verification.Outcome != "pass" && verification.Outcome != "fail" {
		return invalid("verification outcome")
	}
	if verification.EvidenceSource != "initial-load" && verification.EvidenceSource != "target-verify" {
		return invalid("verification evidence source")
	}
	switch verification.HostVersionStatus {
	case "unknown":
		if verification.HostVersion != "" {
			return invalid("unknown host version must be empty")
		}
	case "verified", "outside-verified-range":
		if !canonicalPlaceHostVersion.MatchString(verification.HostVersion) {
			return invalid("host version must be a bounded core version")
		}
		for _, component := range strings.Split(verification.HostVersion, ".") {
			if len(component) > 1 && component[0] == '0' {
				return invalid("host version must be normalized")
			}
		}
	default:
		return invalid("host version status")
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
