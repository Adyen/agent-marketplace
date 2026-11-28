package targetprofile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"gopkg.in/yaml.v3"
)

const validTargetImportYAML = `apiVersion: agent-marketplace.io/target-import/v1
target_id: invented-app
kind: invented-app/mod
name: sample-links
mappings:
  - source_path: mods/sample-links/main.js
    destination_path: mods/sample-links/main.js
  - source_path: mods/sample-links/mod.json
    destination_path: mods/sample-links/mod.json
display_files: [README.md]
metadata:
  requires: ["invented-app/mod:sample-tabs"]
  manual_setup: [Bind the test key in settings.]
`

func importTestLimits() PackageLimits {
	return PackageLimits{FileCount: 64, FileBytes: 1 << 20, TotalBytes: 4 << 20}
}

func newImportTestSource(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for name, data := range map[string]string{
		"mods/sample-links/main.js":  "export const value = 1;\n",
		"mods/sample-links/mod.json": "{\"name\":\"sample-links\"}\n",
		"mods/sample-tabs/main.js":   "export const sibling = true;\n",
		"README.md":                  "display\n",
		"notes/idea.md":              "not selected\n",
	} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestImportLocalPackagePreservesMappingsAndNormalizesForLoadPackage(t *testing.T) {
	profileBytes := []byte(validProfileYAML)
	source := newImportTestSource(t)
	if err := os.MkdirAll(filepath.Join(source, "payload"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(source, "README.md"), filepath.Join(source, "payload", "README.md")); err != nil {
		t.Fatal(err)
	}
	manifest := strings.Replace(validTargetImportYAML, "display_files: [README.md]", "display_files: [payload/README.md]", 1)
	imported, err := ImportLocalPackage(profileBytes, source, []byte(manifest), importTestLimits())
	if err != nil {
		t.Fatalf("ImportLocalPackage() error = %v", err)
	}
	if imported.TargetID != "invented-app" || imported.Kind != "invented-app/mod" ||
		imported.Name != "sample-links" || imported.Identity != profileIdentity(profileBytes, "invented-app") {
		t.Fatalf("import identity = %#v", imported)
	}
	wantFiles := []PackageFile{{
		SourcePath: "mods/sample-links/main.js", DestinationPath: "mods/sample-links/main.js",
		Mode: 0o644, Bytes: []byte("export const value = 1;\n"),
	}, {
		SourcePath: "mods/sample-links/mod.json", DestinationPath: "mods/sample-links/mod.json",
		Mode: 0o644, Bytes: []byte("{\"name\":\"sample-links\"}\n"),
	}}
	if !reflect.DeepEqual(imported.Files, wantFiles) {
		t.Fatalf("imported payload = %#v", imported.Files)
	}
	excluded := map[string]string{}
	for _, file := range imported.ExcludedFiles {
		excluded[file.SourcePath] = file.Reason
	}
	for path, reason := range map[string]string{
		"payload/README.md":        "display",
		"notes/idea.md":            "not-selected",
		"mods/sample-tabs/main.js": "not-selected",
	} {
		if excluded[path] != reason {
			t.Errorf("excluded file %q = %q, want %q", path, excluded[path], reason)
		}
	}
	if len(imported.Metadata.Requires) != 1 || len(imported.Metadata.ManualSetup) != 1 ||
		imported.Metadata.ManualSetup[0] != "Bind the test key in settings." {
		t.Fatalf("import metadata = %#v", imported.Metadata)
	}

	registryPath := t.TempDir()
	packagePath := filepath.Join(registryPath, "packages", "invented-app", "mod", "sample-links")
	for _, dir := range []string{
		packagePath,
		filepath.Join(packagePath, "payload", "mods", "sample-links"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	sidecar := packageSidecar{
		TargetID: imported.TargetID, Kind: imported.Kind, Name: imported.Name,
		DisplayFiles: []string{"README.md"}, Metadata: imported.Metadata,
		ContentDigest: &imported.Binding.PackageContentDigest,
		MappingDigest: &imported.Binding.DeliveryMappingDigest,
		Provenance:    map[string]yaml.Node{},
	}
	for _, file := range imported.Files {
		sidecar.Files = append(sidecar.Files, packageFileMapping{
			SourcePath: file.SourcePath, DestinationPath: file.DestinationPath,
		})
		payloadPath := filepath.Join(packagePath, "payload", filepath.FromSlash(file.SourcePath))
		if err := os.MkdirAll(filepath.Dir(payloadPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(payloadPath, file.Bytes, file.Mode); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(packagePath, "README.md"), []byte("display\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sidecarBytes, err := yaml.Marshal(sidecar)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packagePath, "agent-marketplace.yaml"), sidecarBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	registry, err := fsboundary.OpenDir(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = registry.Close() }()
	loaded, err := LoadPackage(registry, imported.Kind, imported.Name, profileBytes, imported.Identity, importTestLimits())
	if err != nil {
		t.Fatalf("LoadPackage() normalized import: %v", err)
	}
	if !reflect.DeepEqual(loaded.Files, wantFiles) ||
		!reflect.DeepEqual(loaded.Binding, imported.Binding) ||
		len(loaded.Metadata.Requires) != 1 ||
		loaded.Metadata.Requires[0].Value != "invented-app/mod:sample-tabs" ||
		!reflect.DeepEqual(loaded.Metadata.ManualSetup, []string{"Bind the test key in settings."}) {
		t.Fatalf("LoadPackage() = %#v; imported = %#v", loaded, imported)
	}
}

// spec: target-profile-import/payload-mappings-preserve-bytes-and-list-source-exclusions
func TestImportLocalPackageListsUnselectedFilesBeyondPackageLimit(t *testing.T) {
	source := newImportTestSource(t)
	unselectedRoot := filepath.Join(source, "unselected")
	if err := os.MkdirAll(unselectedRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := range 65 {
		path := filepath.Join(unselectedRoot, fmt.Sprintf("%03d.txt", i))
		if err := os.WriteFile(path, []byte("unselected\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	imported, err := ImportLocalPackage(
		[]byte(validProfileYAML), source, []byte(validTargetImportYAML), importTestLimits(),
	)
	if err != nil {
		t.Fatalf("ImportLocalPackage() rejected unrelated source files: %v", err)
	}
	if got, want := len(imported.ExcludedFiles), 68; got != want {
		t.Fatalf("excluded file count = %d, want %d", got, want)
	}
	if imported.ExcludedFiles[len(imported.ExcludedFiles)-1] !=
		(ImportExcludedFile{SourcePath: "unselected/064.txt", Reason: "not-selected"}) {
		t.Fatalf("last excluded file = %#v", imported.ExcludedFiles[len(imported.ExcludedFiles)-1])
	}
}

func TestImportExclusions(t *testing.T) {
	inventory := []string{"selected.js", "inventory-only.md", "display-both.md"}
	selected := map[string]struct{}{"selected.js": {}}
	displays := map[string]struct{}{
		"display-both.md":    {},
		"missing-display.md": {},
	}

	got := importExclusions(inventory, selected, displays)
	want := []ImportExcludedFile{
		{SourcePath: "display-both.md", Reason: "display"},
		{SourcePath: "inventory-only.md", Reason: "not-selected"},
		{SourcePath: "missing-display.md", Reason: "display"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("importExclusions() = %#v, want %#v", got, want)
	}
}

func TestImportLocalPackageRejectsStrictManifestAndIdentityViolations(t *testing.T) {
	valid := validTargetImportYAML
	tests := []struct {
		name     string
		manifest string
		path     string
	}{
		{"duplicate key", strings.Replace(valid, "name: sample-links\n", "name: sample-links\nname: other\n", 1), "name"},
		{"alias", strings.Replace(strings.Replace(valid, "display_files: [README.md]", "display_files: &shown [README.md]", 1),
			"requires: [\"invented-app/mod:sample-tabs\"]", "requires: [*shown]", 1), "manifest"},
		{"merge", strings.Replace(valid, "metadata:\n", "metadata:\n  <<: {manual_setup: [merged]}\n", 1), "manifest"},
		{"unknown field", strings.Replace(valid, "name: sample-links\n", "name: sample-links\nunexpected: true\n", 1), "unexpected"},
		{"invalid version", strings.Replace(valid, "agent-marketplace.io/target-import/v1", "agent-marketplace.io/target-import/v2", 1), "apiVersion"},
		{"malformed mapping", strings.Replace(valid, "destination_path: mods/sample-links/main.js", "destination_path: 8", 1), "destination_path"},
		{"empty mappings", strings.Replace(valid, "mappings:\n  - source_path: mods/sample-links/main.js\n    destination_path: mods/sample-links/main.js\n  - source_path: mods/sample-links/mod.json\n    destination_path: mods/sample-links/mod.json", "mappings: []", 1), "mappings"},
		{"second document", valid + "---\nname: other\n", "manifest"},
		{"invalid target ID", strings.Replace(valid, "target_id: invented-app", "target_id: Invented-App", 1), "target_id"},
		{"profile target mismatch", strings.Replace(valid, "target_id: invented-app", "target_id: other-app", 1), "target_id"},
		{"kind target mismatch", strings.Replace(valid, "kind: invented-app/mod", "kind: other-app/mod", 1), "kind"},
		{"invalid name", strings.Replace(valid, "name: sample-links", "name: ../sample-links", 1), "name"},
		{"duplicate display", strings.Replace(valid, "display_files: [README.md]", "display_files: [README.md, README.md]", 1), "manifest.display_files"},
		{"display traversal", strings.Replace(valid, "display_files: [README.md]", "display_files: [../README.md]", 1), "manifest.display_files"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ImportLocalPackage([]byte(validProfileYAML), newImportTestSource(t), []byte(tt.manifest), importTestLimits())
			assertImportErrorPath(t, err, tt.path)
		})
	}
}

func TestImportLocalPackageRejectsUnsafeAndMissingMappedFiles(t *testing.T) {
	valid := validTargetImportYAML
	tests := []struct {
		name       string
		manifest   string
		sourceEdit func(*testing.T, string)
		path       string
	}{
		{"source traversal", strings.Replace(valid, "source_path: mods/sample-links/main.js", "source_path: ../outside.js", 1), nil, "../outside.js"},
		{"destination traversal", strings.Replace(valid, "destination_path: mods/sample-links/main.js", "destination_path: ../outside.js", 1), nil, "../outside.js"},
		{"backslash", strings.Replace(valid, "source_path: mods/sample-links/main.js", `source_path: mods\sample-links\main.js`, 1), nil, "main.js"},
		{"duplicate source", strings.Replace(valid, "source_path: mods/sample-links/mod.json", "source_path: mods/sample-links/main.js", 1), nil, "main.js"},
		{"duplicate destination", strings.Replace(valid, "destination_path: mods/sample-links/mod.json", "destination_path: mods/sample-links/main.js", 1), nil, "main.js"},
		{"missing mapped helper", strings.Replace(valid, "display_files:", "  - source_path: mods/sample-links/helper.js\n    destination_path: mods/sample-links/helper.js\ndisplay_files:", 1), nil, "helper.js"},
		{"selected symlink", valid, func(t *testing.T, source string) {
			path := filepath.Join(source, "mods", "sample-links", "main.js")
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(source, "README.md"), path); err != nil {
				t.Fatal(err)
			}
		}, "main.js"},
		{"selected FIFO", valid, func(t *testing.T, source string) {
			path := filepath.Join(source, "mods", "sample-links", "main.js")
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := syscall.Mkfifo(path, 0o600); err != nil {
				t.Fatal(err)
			}
		}, "main.js"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := newImportTestSource(t)
			if tt.sourceEdit != nil {
				tt.sourceEdit(t, source)
			}
			_, err := ImportLocalPackage([]byte(validProfileYAML), source, []byte(tt.manifest), importTestLimits())
			assertImportErrorPath(t, err, tt.path)
		})
	}
}

func TestImportLocalPackageRejectsShippedNameAndKeepsExternalRequirementSeparate(t *testing.T) {
	profileBytes := []byte(strings.Replace(validProfileYAML,
		"host: {", "host: {shipped: {mod: [sample-links]}, ", 1))
	_, err := ImportLocalPackage(profileBytes, newImportTestSource(t), []byte(validTargetImportYAML), importTestLimits())
	assertImportErrorPath(t, err, "sample-links")

	profileBytes = []byte(validProfileYAML)
	imported, err := ImportLocalPackage(profileBytes, newImportTestSource(t), []byte(validTargetImportYAML), importTestLimits())
	if err != nil {
		t.Fatalf("advisory external prerequisite refused: %v", err)
	}
	if len(imported.Metadata.Requires) != 1 || len(imported.Files) != 2 {
		t.Fatalf("external requirement or explicit payload mappings were lost: %#v", imported)
	}
}

func assertImportErrorPath(t *testing.T, err error, wantPath string) {
	t.Helper()
	var refusal *ImportError
	if !errors.As(err, &refusal) {
		t.Fatalf("ImportLocalPackage() error = %v, want *ImportError", err)
	}
	if refusal.Code == "" || !strings.Contains(refusal.Path+refusal.Error(), wantPath) {
		t.Fatalf("import refusal = %#v, want stable code and path containing %q", refusal, wantPath)
	}
}
