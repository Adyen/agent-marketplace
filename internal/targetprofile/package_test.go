package targetprofile

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

const packageSidecarFixture = `target_id: invented-app
kind: invented-app/mod
name: sample
files:
  - source_path: src/z.bin
    destination_path: mods/z.bin
  - source_path: src/a.bin
    destination_path: mods/a.bin
display_files: [README.md]
metadata:
  requires: [invented-app/mod:base]
  manual_setup: [Back up settings]
provenance: {owner: registry}
`

func TestLoadPackageFromExactProfile(t *testing.T) {
	profileBytes := []byte(validProfileYAML)
	profileID := profileIdentity(profileBytes, "invented-app")
	kind := "invented-app/mod"
	limits := PackageLimits{FileCount: 3, FileBytes: 1024, TotalBytes: 64}
	wantFiles := []PackageFile{
		{SourcePath: "src/a.bin", DestinationPath: "mods/a.bin", Mode: 0o644, Bytes: []byte{0, 1}},
		{SourcePath: "src/z.bin", DestinationPath: "mods/z.bin", Mode: 0o755, Bytes: []byte("zeta")},
	}

	t.Run("loads exact package", func(t *testing.T) {
		registry, _ := openPackageFixture(t, packageSidecarFixture)
		got, err := LoadPackage(registry, kind, "sample", profileBytes, profileID, limits)
		if err != nil {
			t.Fatalf("LoadPackage() error = %v", err)
		}
		if !reflect.DeepEqual(got.Files, wantFiles) {
			t.Fatalf("files = %#v, want %#v", got.Files, wantFiles)
		}
		if got.Binding != (Binding{
			PackageContentDigest:  "sha256:24d2f8c125bf6bcb61c2e96e84d09f29702170816327177cfa6dc1d96bfa8e77",
			DeliveryMappingDigest: "sha256:33f11bb140e21789ed4bfcf264432fffe881dbaa1e7202206a51319223b2b0b9",
			ProfileDigest:         profileID.RevisionDigest,
			ProfileIdentity:       profileID,
		}) {
			t.Fatalf("binding = %#v", got.Binding)
		}
		if len(got.Metadata.Requires) != 1 || got.Metadata.Requires[0].Value != "invented-app/mod:base" ||
			!reflect.DeepEqual(got.Metadata.ManualSetup, []string{"Back up settings"}) {
			t.Fatalf("metadata = %#v", got.Metadata)
		}
	})

	tests := []struct {
		name, code string
		sidecar    string
		setup      func(*testing.T, string)
		wrongID    bool
		limits     PackageLimits
	}{
		{name: "wrong exact profile digest", code: profileDigestMismatchCode, wrongID: true},
		{name: "destination outside profile", code: "marketplace.package.destination-invalid",
			sidecar: strings.Replace(packageSidecarFixture, "destination_path: mods/a.bin", "destination_path: outside/a.bin", 1)},
		{name: "unmapped payload leaf", code: "marketplace.package.payload-invalid",
			setup: func(t *testing.T, pkg string) {
				writePackageFixtureFile(t, filepath.Join(pkg, "payload", "src", "extra.bin"), []byte("extra"), 0o644)
			}},
		{name: "symlink payload leaf", code: "marketplace.package.payload-invalid",
			setup: func(t *testing.T, pkg string) {
				file := filepath.Join(pkg, "payload", "src", "a.bin")
				if err := os.Remove(file); err != nil {
					t.Fatal(err)
				}
				outside := filepath.Join(t.TempDir(), "outside.bin")
				writePackageFixtureFile(t, outside, []byte("outside"), 0o644)
				if err := os.Symlink(outside, file); err != nil {
					t.Fatal(err)
				}
			}},
		{name: "changed payload mode", code: "marketplace.package.file-mode-invalid",
			setup: func(t *testing.T, pkg string) {
				if err := os.Chmod(filepath.Join(pkg, "payload", "src", "a.bin"), 0o664); err != nil {
					t.Fatal(err)
				}
			}},
		{name: "forged content digest", code: "marketplace.package.claim-mismatch",
			sidecar: strings.Replace(packageSidecarFixture, "display_files:", "content_digest: sha256:"+strings.Repeat("0", 64)+"\ndisplay_files:", 1)},
		{name: "forged assessment authority", code: "marketplace.package.authority-forbidden",
			sidecar: strings.Replace(packageSidecarFixture, "provenance: {owner: registry}", "provenance: {assessment_digest: forged, owner: registry}", 1)},
		{name: "display count limit", code: "marketplace.package.limits-exceeded",
			limits: PackageLimits{FileCount: 2, FileBytes: 1024, TotalBytes: 64}},
		{name: "display total byte limit", code: "marketplace.package.limits-exceeded",
			limits: PackageLimits{FileCount: 3, FileBytes: 1024, TotalBytes: 13}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sidecar := tt.sidecar
			if sidecar == "" {
				sidecar = packageSidecarFixture
			}
			registry, pkg := openPackageFixture(t, sidecar)
			if tt.setup != nil {
				tt.setup(t, pkg)
			}
			id := profileID
			if tt.wrongID {
				id.RevisionDigest = "sha256:" + strings.Repeat("0", 64)
			}
			caseLimits := tt.limits
			if caseLimits.FileCount == 0 {
				caseLimits = limits
			}
			_, err := LoadPackage(registry, kind, "sample", profileBytes, id, caseLimits)
			if err == nil || !strings.HasPrefix(err.Error(), tt.code+":") {
				t.Fatalf("LoadPackage() error = %v, want %s refusal", err, tt.code)
			}
		})
	}
}

func openPackageFixture(t *testing.T, sidecar string) (*fsboundary.Dir, string) {
	t.Helper()
	rootPath := t.TempDir()
	pkg := filepath.Join(rootPath, "packages", "invented-app", "mod", "sample")
	writePackageFixtureFile(t, filepath.Join(pkg, "agent-marketplace.yaml"), []byte(sidecar), 0o644)
	writePackageFixtureFile(t, filepath.Join(pkg, "payload", "src", "z.bin"), []byte("zeta"), 0o755)
	writePackageFixtureFile(t, filepath.Join(pkg, "payload", "src", "a.bin"), []byte{0, 1}, 0o644)
	writePackageFixtureFile(t, filepath.Join(pkg, "README.md"), []byte("display\n"), 0o644)
	registry, err := fsboundary.OpenDir(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := registry.Close(); err != nil {
			t.Errorf("close fixture registry: %v", err)
		}
	})
	return registry, pkg
}

func writePackageFixtureFile(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}
