package targetprofile

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
	"gopkg.in/yaml.v3"
)

const packageSidecarLimit int64 = 1 << 20

const (
	packageInvalidCode            = "marketplace.package.invalid"
	packageNameInvalidCode        = "marketplace.package.name-invalid"
	packageSidecarInvalidCode     = "marketplace.package.sidecar-invalid"
	packageAuthorityForbiddenCode = "marketplace.package.authority-forbidden"
	packageLimitsExceededCode     = "marketplace.package.limits-exceeded"
	packageDestinationInvalidCode = "marketplace.package.destination-invalid"
	packagePayloadInvalidCode     = "marketplace.package.payload-invalid"
	packageModeInvalidCode        = "marketplace.package.file-mode-invalid"
	packageClaimMismatchCode      = "marketplace.package.claim-mismatch"
)

// PackageLimits bounds package file counts and bytes read.
type PackageLimits struct {
	FileCount             int
	FileBytes, TotalBytes int64
}

// PackageMetadata contains descriptive package setup metadata.
type PackageMetadata struct {
	Requires    []yaml.Node `yaml:"requires"`
	ManualSetup []string    `yaml:"manual_setup"`
}

// LoadedPackage contains a package verified against an exact profile identity.
type LoadedPackage struct {
	Binding  Binding
	Files    []PackageFile
	Metadata PackageMetadata
}

type packageSidecar struct {
	TargetID      string               `yaml:"target_id"`
	Kind          string               `yaml:"kind"`
	Name          string               `yaml:"name"`
	Files         []packageFileMapping `yaml:"files"`
	DisplayFiles  []string             `yaml:"display_files"`
	ContentDigest *string              `yaml:"content_digest"`
	MappingDigest *string              `yaml:"mapping_digest"`
	Metadata      PackageMetadata      `yaml:"metadata"`
	Provenance    map[string]yaml.Node `yaml:"provenance"`
}

type packageFileMapping struct {
	SourcePath      string `yaml:"source_path"`
	DestinationPath string `yaml:"destination_path"`
}

// LoadPackage reads one exact-profile scoped package without writing or executing it.
func LoadPackage(registry *fsboundary.Dir, kind, name string, profileBytes []byte, profileIdentity Identity, limits PackageLimits) (LoadedPackage, error) {
	if registry == nil || limits.FileCount <= 0 || limits.FileBytes <= 0 || limits.TotalBytes <= 0 {
		return LoadedPackage{}, fmt.Errorf("%s: registry and positive limits are required", packageInvalidCode)
	}
	targetID, localKind, scoped := nameutil.SplitPlaceKind(kind)
	if !scoped {
		return LoadedPackage{}, fmt.Errorf("%s: kind must be target-scoped", packageInvalidCode)
	}
	if !nameutil.IsPlacePackageName(name) {
		return LoadedPackage{}, fmt.Errorf("%s: invalid package name", packageNameInvalidCode)
	}
	if targetID != profileIdentity.TargetID {
		return LoadedPackage{}, fmt.Errorf("%s: kind does not match profile identity", packageInvalidCode)
	}
	profile, err := Parse(profileBytes, profileIdentity)
	if err != nil {
		return LoadedPackage{}, err
	}
	pkg, err := registry.OpenPath(path.Join("packages", targetID, localKind, name))
	if err != nil {
		return LoadedPackage{}, fmt.Errorf("%s: open exact package: %w", packageInvalidCode, err)
	}
	defer func() { _ = pkg.Close() }()
	raw, _, err := pkg.ReadFile("agent-marketplace.yaml", min(limits.FileBytes, packageSidecarLimit))
	if err != nil {
		return LoadedPackage{}, packageReadError(packageSidecarInvalidCode, err)
	}
	sidecar, err := decodePackageSidecar(raw)
	if err != nil {
		return LoadedPackage{}, err
	}
	if sidecar.TargetID != targetID || sidecar.Kind != kind || sidecar.Name != name {
		return LoadedPackage{}, fmt.Errorf("%s: sidecar identity does not match request", packageSidecarInvalidCode)
	}
	if len(sidecar.Files) == 0 || len(sidecar.Files) > limits.FileCount {
		return LoadedPackage{}, fmt.Errorf("%s: package file count is empty or exceeds limit", packageLimitsExceededCode)
	}
	mapped := make(map[string]string, len(sidecar.Files))
	files := make([]PackageFile, 0, len(sidecar.Files))
	for _, file := range sidecar.Files {
		if !validProfileRelativePath(file.SourcePath) {
			return LoadedPackage{}, fmt.Errorf("%s: invalid mapped path", packageSidecarInvalidCode)
		}
		if _, ok := mapped[file.SourcePath]; ok {
			return LoadedPackage{}, fmt.Errorf("%s: duplicate source mapping", packageSidecarInvalidCode)
		}
		mapped[file.SourcePath] = file.DestinationPath
		files = append(files, PackageFile{SourcePath: file.SourcePath, DestinationPath: file.DestinationPath})
	}
	if err := ValidatePackageDestinations(profile, kind, files); err != nil {
		return LoadedPackage{}, err
	}
	if err := validatePackageDisplayFiles(sidecar.DisplayFiles); err != nil {
		return LoadedPackage{}, err
	}
	if len(files)+len(sidecar.DisplayFiles) > limits.FileCount {
		return LoadedPackage{}, fmt.Errorf("%s: package and display files exceed count limit", packageLimitsExceededCode)
	}
	remaining := limits.TotalBytes
	for _, display := range sidecar.DisplayFiles {
		if _, _, err := readPackageFile(pkg, display, limits, &remaining); err != nil {
			return LoadedPackage{}, packageReadError(packageSidecarInvalidCode, err)
		}
	}
	payload, err := pkg.OpenPath("payload")
	if err != nil {
		return LoadedPackage{}, fmt.Errorf("%s: open payload tree: %w", packagePayloadInvalidCode, err)
	}
	defer func() { _ = payload.Close() }()
	files, err = readPackagePayload(payload, mapped, limits, &remaining)
	if err != nil {
		return LoadedPackage{}, err
	}
	if len(mapped) != 0 {
		return LoadedPackage{}, fmt.Errorf("%s: mapped payload file is missing", packagePayloadInvalidCode)
	}
	binding, err := BuildBinding(profileIdentity, files)
	if err != nil {
		return LoadedPackage{}, err
	}
	if sidecar.ContentDigest != nil && *sidecar.ContentDigest != binding.PackageContentDigest ||
		sidecar.MappingDigest != nil && *sidecar.MappingDigest != binding.DeliveryMappingDigest {
		return LoadedPackage{}, fmt.Errorf("%s: claimed digest does not match package", packageClaimMismatchCode)
	}
	return LoadedPackage{Binding: binding, Files: files, Metadata: sidecar.Metadata}, nil
}

// ValidatePackageDestinations checks scoped package destinations against the profile.
func ValidatePackageDestinations(profile Profile, scopedKind string, files []PackageFile) error {
	targetID, localKind, ok := nameutil.SplitPlaceKind(scopedKind)
	if !ok || profile.ID != targetID {
		return fmt.Errorf("%s: scoped kind does not match profile", packageDestinationInvalidCode)
	}
	kind, ok := profile.Kinds[localKind]
	if !ok || kind.Deliver != "place" {
		return fmt.Errorf("%s: profile kind is not place-delivered", packageDestinationInvalidCode)
	}
	seen := make(map[string]struct{}, len(files))
	for _, file := range files {
		dst := file.DestinationPath
		if !validProfileRelativePath(dst) {
			return fmt.Errorf("%s: invalid destination path", packageDestinationInvalidCode)
		}
		if _, ok := seen[dst]; ok {
			return fmt.Errorf("%s: duplicate destination path", packageDestinationInvalidCode)
		}
		seen[dst] = struct{}{}
		if !slices.Contains(kind.SupportFiles, dst) && !slices.ContainsFunc(kind.Into, func(prefix string) bool {
			return strings.HasPrefix(dst, prefix)
		}) {
			return fmt.Errorf("%s: destination is outside profile allowlist", packageDestinationInvalidCode)
		}
	}
	return nil
}

func decodePackageSidecar(data []byte) (packageSidecar, error) {
	var document yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&document); err != nil {
		return packageSidecar{}, fmt.Errorf("%s: decode sidecar: %w", packageSidecarInvalidCode, err)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return packageSidecar{}, fmt.Errorf("%s: sidecar must contain one YAML document", packageSidecarInvalidCode)
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return packageSidecar{}, fmt.Errorf("%s: sidecar must be one mapping document", packageSidecarInvalidCode)
	}
	root := document.Content[0]
	if err := rejectPackageAuthorityKeys(root); err != nil {
		return packageSidecar{}, err
	}
	if err := inspectProfileYAML(root); err != nil {
		return packageSidecar{}, fmt.Errorf("%s: %w", packageSidecarInvalidCode, err)
	}
	if err := validatePackageSidecarScalars(root); err != nil {
		return packageSidecar{}, err
	}
	strict := yaml.NewDecoder(bytes.NewReader(data))
	strict.KnownFields(true)
	var sidecar packageSidecar
	if err := strict.Decode(&sidecar); err != nil {
		return packageSidecar{}, fmt.Errorf("%s: decode strict sidecar: %w", packageSidecarInvalidCode, err)
	}
	return sidecar, nil
}

func validatePackageSidecarScalars(root *yaml.Node) error {
	for _, field := range []string{"target_id", "kind", "name"} {
		if !isYAMLStringScalar(mappingValue(root, field)) {
			return fmt.Errorf("%s: %s must be a string scalar", packageSidecarInvalidCode, field)
		}
	}
	files := mappingValue(root, "files")
	if files == nil || files.Kind != yaml.SequenceNode || len(files.Content) == 0 {
		return fmt.Errorf("%s: files must be a non-empty sequence", packageSidecarInvalidCode)
	}
	for _, file := range files.Content {
		if file.Kind != yaml.MappingNode || !isYAMLStringScalar(mappingValue(file, "source_path")) ||
			!isYAMLStringScalar(mappingValue(file, "destination_path")) {
			return fmt.Errorf("%s: file paths must be string scalars", packageSidecarInvalidCode)
		}
	}
	for _, field := range []string{"content_digest", "mapping_digest"} {
		if value := mappingValue(root, field); value != nil && !isYAMLStringScalar(value) {
			return fmt.Errorf("%s: %s must be a string scalar", packageSidecarInvalidCode, field)
		}
	}
	for _, field := range []struct {
		name string
		node *yaml.Node
	}{
		{"display_files", mappingValue(root, "display_files")},
		{"manual_setup", mappingValue(mappingValue(root, "metadata"), "manual_setup")},
	} {
		if field.node == nil {
			continue
		}
		if field.node.Kind != yaml.SequenceNode {
			return fmt.Errorf("%s: %s must be a sequence", packageSidecarInvalidCode, field.name)
		}
		if err := validateYAMLStringSequence(field.node); err != nil {
			return fmt.Errorf("%s: %w", packageSidecarInvalidCode, err)
		}
	}
	if node := mappingValue(root, "metadata"); node != nil && node.Kind != yaml.MappingNode {
		return fmt.Errorf("%s: metadata must be a mapping", packageSidecarInvalidCode)
	}
	if node := mappingValue(root, "provenance"); node != nil && node.Kind != yaml.MappingNode {
		return fmt.Errorf("%s: provenance must be a mapping", packageSidecarInvalidCode)
	}
	return nil
}

func rejectPackageAuthorityKeys(node *yaml.Node) error {
	if node.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(node.Content); i += 2 {
			switch node.Content[i].Value {
			case "host_package_identity", "assessment", "assessment_digest", "profile_identity", "profile_digest":
				return fmt.Errorf("%s: sidecar contains reserved authority key", packageAuthorityForbiddenCode)
			}
			if err := rejectPackageAuthorityKeys(node.Content[i+1]); err != nil {
				return err
			}
		}
		return nil
	}
	for _, child := range node.Content {
		if err := rejectPackageAuthorityKeys(child); err != nil {
			return err
		}
	}
	return nil
}

func validatePackageDisplayFiles(files []string) error {
	seen := make(map[string]struct{}, len(files))
	for _, file := range files {
		if !validProfileRelativePath(file) || file == "agent-marketplace.yaml" || file == "payload" || strings.HasPrefix(file, "payload/") {
			return fmt.Errorf("%s: invalid display path", packageSidecarInvalidCode)
		}
		if _, ok := seen[file]; ok {
			return fmt.Errorf("%s: duplicate display path", packageSidecarInvalidCode)
		}
		seen[file] = struct{}{}
	}
	return nil
}

func readPackagePayload(payload *fsboundary.Dir, mapped map[string]string, limits PackageLimits, remaining *int64) ([]PackageFile, error) {
	native, err := payload.NativeRoot()
	if err != nil {
		return nil, fmt.Errorf("%s: capture payload tree: %w", packagePayloadInvalidCode, err)
	}
	defer func() { _ = native.Close() }()
	var files []PackageFile
	err = fs.WalkDir(native.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("%s: walk payload tree: %w", packagePayloadInvalidCode, walkErr)
		}
		if name == "." {
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%s: symlink in payload tree", packagePayloadInvalidCode)
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("%s: non-regular payload leaf", packagePayloadInvalidCode)
		}
		destination, ok := mapped[name]
		if !ok {
			return fmt.Errorf("%s: unmapped payload file %q", packagePayloadInvalidCode, name)
		}
		data, identity, err := readPackageFile(payload, name, limits, remaining)
		if err != nil {
			return packageReadError(packagePayloadInvalidCode, err)
		}
		mode := identity.Mode & (fs.ModePerm | fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky)
		if mode != 0o644 && mode != 0o755 {
			return fmt.Errorf("%s: unsupported payload file mode", packageModeInvalidCode)
		}
		files = append(files, PackageFile{SourcePath: name, DestinationPath: destination, Mode: mode, Bytes: data})
		delete(mapped, name)
		return nil
	})
	return files, err
}

func readPackageFile(dir *fsboundary.Dir, name string, limits PackageLimits, remaining *int64) ([]byte, fsboundary.Identity, error) {
	data, identity, err := dir.ReadFile(name, min(limits.FileBytes, *remaining))
	if err == nil {
		*remaining -= int64(len(data))
	}
	return data, identity, err
}

func packageReadError(code string, err error) error {
	if errors.Is(err, fsboundary.ErrTooLarge) {
		return fmt.Errorf("%s: %w", packageLimitsExceededCode, err)
	}
	return fmt.Errorf("%s: %w", code, err)
}
