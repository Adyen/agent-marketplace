package targetprofile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
	"gopkg.in/yaml.v3"
)

const importManifestVersion = "agent-marketplace.io/target-import/v1"

type ImportError struct {
	Code        string
	Path        string
	LiteralPath bool
	err         error
}

func (e *ImportError) Error() string {
	if e.err == nil {
		return fmt.Sprintf("%s: %s", e.Code, e.Path)
	}
	return fmt.Sprintf("%s: %s: %v", e.Code, e.Path, e.err)
}

func (e *ImportError) Unwrap() error { return e.err }

type ImportedPackage struct {
	Identity      Identity
	Profile       Profile
	TargetID      string
	Kind          string
	Name          string
	Files         []PackageFile
	DisplayFiles  []string
	Metadata      PackageMetadata
	ExcludedFiles []ImportExcludedFile
	Binding       Binding
}

type ImportExcludedFile struct {
	SourcePath string `json:"source_path"`
	Reason     string `json:"reason"`
}

type importManifest struct {
	APIVersion   string          `yaml:"apiVersion"`
	TargetID     string          `yaml:"target_id"`
	Kind         string          `yaml:"kind"`
	Name         string          `yaml:"name"`
	Mappings     []ImportMapping `yaml:"mappings"`
	DisplayFiles []string        `yaml:"display_files"`
	Metadata     PackageMetadata `yaml:"metadata"`
}

type ImportMapping struct {
	SourcePath      string `yaml:"source_path" json:"source_path"`
	DestinationPath string `yaml:"destination_path" json:"destination_path"`
}

func ImportLocalPackage(profileBytes []byte, sourcePath string, manifestBytes []byte, limits PackageLimits) (ImportedPackage, error) {
	if limits.FileCount <= 0 || limits.FileBytes <= 0 || limits.TotalBytes <= 0 {
		return ImportedPackage{}, importFailure("target.import.invalid-limits", "manifest", errors.New("positive package limits are required"))
	}
	if int64(len(profileBytes)) > MaxProfileBytes {
		return ImportedPackage{}, importFailure("target.import.profile-invalid", "profile", errors.New("profile exceeds its byte limit"))
	}
	if int64(len(manifestBytes)) > packageSidecarLimit || int64(len(manifestBytes)) > limits.FileBytes {
		return ImportedPackage{}, importFailure("target.import.manifest-invalid", "manifest", errors.New("manifest exceeds its byte limit"))
	}
	manifest, err := decodeImportManifest(manifestBytes)
	if err != nil {
		return ImportedPackage{}, err
	}
	if !nameutil.IsTargetIdentifier(manifest.TargetID) {
		return ImportedPackage{}, importFailure("target.import.identity-invalid", "manifest.target_id", nil)
	}
	if !nameutil.IsPlacePackageName(manifest.Name) {
		return ImportedPackage{}, importFailure("target.import.identity-invalid", "manifest.name", nil)
	}
	profileID, err := profileTargetID(profileBytes)
	if err != nil {
		return ImportedPackage{}, importFailure("target.import.profile-invalid", "profile", err)
	}
	digest := sha256.Sum256(profileBytes)
	identity := Identity{
		TargetID: profileID, SourceID: "builtin:agent-marketplace",
		RevisionDigest: "sha256:" + hex.EncodeToString(digest[:]),
	}
	profile, err := Parse(profileBytes, identity)
	if err != nil {
		return ImportedPackage{}, importFailure("target.import.profile-invalid", "profile", err)
	}
	if profile.ID != manifest.TargetID {
		return ImportedPackage{}, importFailure("target.import.identity-invalid", "manifest.target_id", nil)
	}
	localTarget, localKind, scoped := nameutil.SplitPlaceKind(manifest.Kind)
	if !scoped || localTarget != manifest.TargetID {
		return ImportedPackage{}, importFailure("target.import.identity-invalid", "manifest.kind", nil)
	}
	if slices.Contains(profile.Host.Shipped[localKind], manifest.Name) {
		return ImportedPackage{}, importFailure("target.import.shipped-name", "manifest.name", fmt.Errorf("package %q is shipped by the target", manifest.Name))
	}
	if len(manifest.Mappings) == 0 || len(manifest.Mappings)+len(manifest.DisplayFiles) > limits.FileCount {
		return ImportedPackage{}, importFailure("target.import.manifest-invalid", "manifest.mappings", errors.New("mappings must be non-empty and within the package file limit"))
	}
	if err := validateImportMetadata(manifest.Metadata); err != nil {
		return ImportedPackage{}, err
	}

	files := make([]PackageFile, 0, len(manifest.Mappings))
	sources, destinations := make(map[string]struct{}, len(manifest.Mappings)), make(map[string]struct{}, len(manifest.Mappings))
	for _, mapping := range manifest.Mappings {
		if !validImportPath(mapping.SourcePath) {
			return ImportedPackage{}, importPathFailure("target.import.path-invalid", mapping.SourcePath, nil)
		}
		if !validImportPath(mapping.DestinationPath) {
			return ImportedPackage{}, importPathFailure("target.import.path-invalid", mapping.DestinationPath, nil)
		}
		if _, exists := sources[mapping.SourcePath]; exists {
			return ImportedPackage{}, importPathFailure("target.import.duplicate-mapping", mapping.SourcePath, nil)
		}
		if _, exists := destinations[mapping.DestinationPath]; exists {
			return ImportedPackage{}, importPathFailure("target.import.duplicate-mapping", mapping.DestinationPath, nil)
		}
		sources[mapping.SourcePath] = struct{}{}
		destinations[mapping.DestinationPath] = struct{}{}
		files = append(files, PackageFile{
			SourcePath: mapping.SourcePath, DestinationPath: mapping.DestinationPath,
		})
	}
	displaySet := make(map[string]struct{}, len(manifest.DisplayFiles))
	for _, display := range manifest.DisplayFiles {
		_, duplicate := displaySet[display]
		if !validImportPath(display) || duplicate {
			return ImportedPackage{}, importFailure(
				"target.import.manifest-invalid", "manifest.display_files",
				errors.New("display paths must be unique and source-relative"),
			)
		}
		displaySet[display] = struct{}{}
		if _, selected := sources[display]; selected {
			return ImportedPackage{}, importPathFailure("target.import.path-invalid", display, errors.New("a source path cannot be both payload and display"))
		}
	}
	if err := ValidatePackageDestinations(profile, manifest.Kind, nil); err != nil {
		return ImportedPackage{}, importFailure("target.import.destination-invalid", "manifest.kind", err)
	}
	for _, file := range files {
		if err := ValidatePackageDestinations(profile, manifest.Kind, []PackageFile{file}); err != nil {
			return ImportedPackage{}, importPathFailure("target.import.destination-invalid", file.DestinationPath, err)
		}
	}

	source, err := fsboundary.OpenDir(sourcePath)
	if err != nil {
		return ImportedPackage{}, importPathFailure("target.import.source-invalid", sourcePath, err)
	}
	defer func() { _ = source.Close() }()
	sourceFiles, err := listImportSourceFiles(source)
	if err != nil {
		return ImportedPackage{}, importPathFailure("target.import.source-invalid", sourcePath, err)
	}
	packageIdentity := manifest.Kind + "/" + manifest.Name
	remaining := limits.TotalBytes
	for i := range files {
		fileBytes, fileIdentity, err := readPackageFile(source, files[i].SourcePath, limits, &remaining)
		if err != nil {
			return ImportedPackage{}, importPathFailure(
				"target.import.mapped-file-invalid", files[i].SourcePath,
				fmt.Errorf("package %q: %w", packageIdentity, err),
			)
		}
		mode := fileIdentity.Mode & (fs.ModePerm | fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky)
		if mode != 0o644 && mode != 0o755 {
			return ImportedPackage{}, importPathFailure("target.import.file-mode-invalid", files[i].SourcePath, nil)
		}
		files[i].Mode = mode
		files[i].Bytes = fileBytes
	}
	for _, display := range manifest.DisplayFiles {
		_, _, err := readPackageFile(source, display, limits, &remaining)
		if err != nil {
			return ImportedPackage{}, importPathFailure("target.import.display-file-invalid", display, err)
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].SourcePath < files[j].SourcePath })
	excluded := importExclusions(sourceFiles, sources, displaySet)
	binding, err := BuildBinding(identity, files)
	if err != nil {
		return ImportedPackage{}, importFailure("target.import.binding-invalid", "manifest.mappings", err)
	}
	return ImportedPackage{
		Identity: identity, Profile: profile,
		TargetID: manifest.TargetID, Kind: manifest.Kind, Name: manifest.Name,
		Files: files, DisplayFiles: append([]string(nil), manifest.DisplayFiles...),
		Metadata: manifest.Metadata, ExcludedFiles: excluded, Binding: binding,
	}, nil
}

func importExclusions(inventory []string, selected, displays map[string]struct{}) []ImportExcludedFile {
	excluded := make([]ImportExcludedFile, 0, len(inventory)+len(displays))
	for display := range displays {
		if _, isSelected := selected[display]; !isSelected {
			excluded = append(excluded, ImportExcludedFile{SourcePath: display, Reason: "display"})
		}
	}
	for _, name := range inventory {
		if _, isSelected := selected[name]; isSelected {
			continue
		}
		if _, isDisplay := displays[name]; isDisplay {
			continue
		}
		excluded = append(excluded, ImportExcludedFile{SourcePath: name, Reason: "not-selected"})
	}
	sort.Slice(excluded, func(i, j int) bool {
		return excluded[i].SourcePath < excluded[j].SourcePath
	})
	return excluded
}

func decodeImportManifest(data []byte) (importManifest, error) {
	var document yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&document); err != nil {
		return importManifest{}, importFailure("target.import.manifest-invalid", "manifest", err)
	}
	var trailing yaml.Node
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("manifest must contain one YAML document")
		}
		return importManifest{}, importFailure("target.import.manifest-invalid", "manifest", err)
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return importManifest{}, importFailure("target.import.manifest-invalid", "manifest", errors.New("manifest must be one mapping"))
	}
	root := document.Content[0]
	if err := inspectImportYAML(root); err != nil {
		return importManifest{}, importFailure("target.import.manifest-invalid", "manifest", err)
	}
	for _, field := range []string{"apiVersion", "target_id", "kind", "name"} {
		if !isYAMLStringScalar(mappingValue(root, field)) {
			return importManifest{}, importFailure("target.import.manifest-invalid", "manifest."+field, errors.New("field must be a string"))
		}
	}
	mappings := mappingValue(root, "mappings")
	if mappings == nil || mappings.Kind != yaml.SequenceNode || len(mappings.Content) == 0 {
		return importManifest{}, importFailure("target.import.manifest-invalid", "manifest.mappings", errors.New("mappings must be a non-empty sequence"))
	}
	for _, mapping := range mappings.Content {
		if mapping.Kind != yaml.MappingNode ||
			!isYAMLStringScalar(mappingValue(mapping, "source_path")) ||
			!isYAMLStringScalar(mappingValue(mapping, "destination_path")) {
			return importManifest{}, importFailure("target.import.manifest-invalid", "manifest.mappings", errors.New("each mapping requires string source_path and destination_path"))
		}
	}
	for _, field := range []string{"display_files"} {
		if value := mappingValue(root, field); value != nil {
			if value.Kind != yaml.SequenceNode || validateYAMLStringSequence(value) != nil {
				return importManifest{}, importFailure("target.import.manifest-invalid", "manifest."+field, errors.New("field must be a sequence of strings"))
			}
		}
	}
	if metadata := mappingValue(root, "metadata"); metadata != nil && metadata.Kind != yaml.MappingNode {
		return importManifest{}, importFailure("target.import.manifest-invalid", "manifest.metadata", errors.New("metadata must be a mapping"))
	}
	if metadata := mappingValue(root, "metadata"); metadata != nil {
		if requires := mappingValue(metadata, "requires"); requires != nil && requires.Kind != yaml.SequenceNode {
			return importManifest{}, importFailure("target.import.manifest-invalid", "manifest.metadata.requires", errors.New("requires must be a sequence"))
		}
		if manual := mappingValue(metadata, "manual_setup"); manual != nil {
			if manual.Kind != yaml.SequenceNode {
				return importManifest{}, importFailure("target.import.manifest-invalid", "manifest.metadata.manual_setup", errors.New("manual_setup must be a sequence"))
			}
			for _, note := range manual.Content {
				if !isYAMLStringScalar(note) {
					return importManifest{}, importFailure("target.import.manifest-invalid", "manifest.metadata.manual_setup", errors.New("setup notes must be strings"))
				}
			}
		}
	}
	strict := yaml.NewDecoder(bytes.NewReader(data))
	strict.KnownFields(true)
	var manifest importManifest
	if err := strict.Decode(&manifest); err != nil {
		return importManifest{}, importFailure("target.import.manifest-invalid", "manifest", err)
	}
	if manifest.APIVersion != importManifestVersion {
		return importManifest{}, importFailure("target.import.manifest-invalid", "manifest.apiVersion", errors.New("unsupported import manifest version"))
	}
	return manifest, nil
}

func profileTargetID(data []byte) (string, error) {
	var document yaml.Node
	if err := yaml.NewDecoder(bytes.NewReader(data)).Decode(&document); err != nil {
		return "", err
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return "", errors.New("profile must be one YAML mapping")
	}
	id := mappingValue(document.Content[0], "id")
	if !isYAMLStringScalar(id) {
		return "", errors.New("profile id must be a string")
	}
	return id.Value, nil
}

func inspectImportYAML(node *yaml.Node) error {
	if node.Alias != nil || node.Anchor != "" || node.Tag == "!!null" {
		return errors.New("YAML aliases, anchors, and nulls are forbidden")
	}
	switch node.Kind {
	case yaml.MappingNode:
		seen := make(map[string]struct{}, len(node.Content)/2)
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || key.Value == "<<" || key.Tag == "!!merge" {
				return errors.New("mapping keys must be strings and merges are forbidden")
			}
			if _, exists := seen[key.Value]; exists {
				return fmt.Errorf("duplicate YAML key %q", key.Value)
			}
			seen[key.Value] = struct{}{}
			if err := inspectImportYAML(node.Content[i+1]); err != nil {
				return err
			}
		}
	case yaml.SequenceNode:
		for _, child := range node.Content {
			if err := inspectImportYAML(child); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateImportMetadata(metadata PackageMetadata) error {
	for i := range metadata.Requires {
		node := &metadata.Requires[i]
		switch node.Kind {
		case yaml.ScalarNode:
			if node.Tag != "!!str" {
				return importFailure("target.import.manifest-invalid", "manifest.metadata.requires", errors.New("requirement must be a string or mapping"))
			}
			kind, name, ok := strings.Cut(node.Value, ":")
			if !ok || strings.Count(node.Value, ":") != 1 {
				return importFailure("target.import.manifest-invalid", "manifest.metadata.requires", errors.New("requirement must use kind:name"))
			}
			if err := validateImportRequirement(kind, name); err != nil {
				return err
			}
		case yaml.MappingNode:
			values := make(map[string]string, len(node.Content)/2)
			for j := 0; j+1 < len(node.Content); j += 2 {
				key := node.Content[j].Value
				if key != "kind" && key != "name" && key != "registry" && key != "path" {
					return importFailure("target.import.manifest-invalid", "manifest.metadata.requires", fmt.Errorf("unknown requirement field %q", key))
				}
				if !isYAMLStringScalar(node.Content[j+1]) {
					return importFailure("target.import.manifest-invalid", "manifest.metadata.requires."+key, errors.New("requirement field must be a string"))
				}
				values[key] = node.Content[j+1].Value
			}
			if err := validateImportRequirement(values["kind"], values["name"]); err != nil {
				return err
			}
		default:
			return importFailure("target.import.manifest-invalid", "manifest.metadata.requires", errors.New("requirement must be a string or mapping"))
		}
	}
	for _, note := range metadata.ManualSetup {
		if strings.TrimSpace(note) == "" || strings.ContainsFunc(note, func(r rune) bool {
			return r != '\n' && r != '\r' && unicode.IsControl(r)
		}) {
			return importFailure("target.import.manifest-invalid", "manifest.metadata.manual_setup", errors.New("setup notes must be non-empty text"))
		}
	}
	return nil
}

func validateImportRequirement(kind, name string) error {
	if _, _, ok := nameutil.SplitPlaceKind(kind); !ok || !nameutil.IsPlacePackageName(name) {
		return importFailure("target.import.manifest-invalid", "manifest.metadata.requires", errors.New("requirement must name a target-scoped package"))
	}
	return nil
}

func validImportPath(value string) bool {
	return value != "" && !strings.Contains(value, "\\") &&
		!strings.ContainsFunc(value, unicode.IsControl) && path.Clean(value) == value &&
		fs.ValidPath(value) && value != "."
}

func listImportSourceFiles(source *fsboundary.Dir) ([]string, error) {
	root, err := source.NativeRoot()
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	var files []string
	err = fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if name == "." || entry.IsDir() {
			return nil
		}
		if entry.Type().IsRegular() {
			files = append(files, name)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

func importFailure(code, path string, err error) error {
	return &ImportError{Code: code, Path: path, err: err}
}

func importPathFailure(code, path string, err error) error {
	return &ImportError{Code: code, Path: path, err: err, LiteralPath: true}
}
