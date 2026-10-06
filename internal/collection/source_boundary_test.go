package collection

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

// spec: registry-symlink-containment/collection-source-refusal-preserves-authority
func TestDiscoverCollectionSourceRefusesForeignPayload(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	dir := filepath.Join(root, "collections", "probe")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	payload := []byte("name: probe\nresources:\n  - name: inert-skill\n")
	primary := filepath.Join(dir, collectionFileName)
	if err := os.WriteFile(primary, payload, 0o640); err != nil {
		t.Fatal(err)
	}
	control, err := DiscoverCollections(root, "key", "registry", "ref", "scope")
	if err != nil || len(control) != 1 || control[0].Name != "probe" {
		t.Fatalf("native collection positive control failed: %+v %v", control, err)
	}
	foreign := filepath.Join(outside, "foreign.yaml")
	if err := os.Rename(primary, foreign); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(foreign)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreign, primary); err != nil {
		t.Fatal(err)
	}
	got, err := DiscoverCollections(root, "key", "registry", "ref", "scope")
	if !errors.Is(err, fsboundary.ErrUnsafePath) || len(got) != 0 {
		t.Fatalf("foreign collection was imported: %+v %v", got, err)
	}
	if data, err := os.ReadFile(foreign); err != nil || string(data) != string(payload) {
		t.Fatalf("foreign collection changed: %q %v", data, err)
	}
	if after, err := os.Stat(foreign); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("foreign collection metadata changed: %v", err)
	}
}

func TestDiscoverCollectionSourceContainedAliasAndHardLink(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "collections", "probe")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	payload := filepath.Join(dir, "authored.yaml")
	if err := os.WriteFile(payload, []byte("name: probe\nresources:\n  - name: inert-skill\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("authored.yaml", filepath.Join(dir, collectionFileName)); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(payload)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DiscoverCollections(root, "catalog", "registry", "ref", "scope")
	if err != nil || len(got) != 1 || got[0].Name != "probe" || got[0].SourceFile != "collections/probe/COLLECTION.yaml" {
		t.Fatalf("contained collection alias/provenance failed: %+v %v", got, err)
	}
	if err := os.Link(payload, filepath.Join(root, "retained.yaml")); err != nil {
		t.Fatal(err)
	}
	if got, err := DiscoverCollections(root, "catalog", "registry", "ref", "scope"); !errors.Is(err, fsboundary.ErrUnsafeFile) || len(got) != 0 {
		t.Fatalf("multiply linked collection payload accepted: %+v %v", got, err)
	}
	if after, err := os.Stat(payload); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("collection read modified authored metadata: %v", err)
	}
}

func TestDiscoverCollectionSourceDanglingLeafIsNotAbsence(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "collections", "probe")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing.yaml", filepath.Join(dir, collectionFileName)); err != nil {
		t.Fatal(err)
	}
	if got, err := DiscoverCollections(root, "", "", "", ""); !errors.Is(err, fsboundary.ErrUnsafePath) || len(got) != 0 {
		t.Fatalf("dangling collection payload became optional absence: %+v %v", got, err)
	}
}
