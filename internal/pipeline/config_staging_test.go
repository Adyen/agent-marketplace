package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
)

func TestConfigStagingManagerCoalescesAndCleans(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "config.yaml")
	original := []byte("apiVersion: v2\n")
	if err := os.WriteFile(destination, original, 0o640); err != nil {
		t.Fatal(err)
	}
	manager, err := newConfigStagingManager()
	if err != nil {
		t.Fatal(err)
	}
	shadow, err := manager.Path(destination)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(shadow) != filepath.Base(destination) {
		t.Fatalf("shadow basename = %q", filepath.Base(shadow))
	}
	if err := os.WriteFile(shadow, []byte("apiVersion: v2\nskills: [one]\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	again, err := manager.Path(destination)
	if err != nil || again != shadow {
		t.Fatalf("repeated stage = %q, %v", again, err)
	}
	if err := os.WriteFile(again, []byte("apiVersion: v2\nskills: [two]\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(destination)
	if err != nil || string(got) != string(original) {
		t.Fatalf("real config changed before reconcile: %q, %v", got, err)
	}
	edits, err := manager.Edits()
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := canonicalConfigDestination(destination)
	if err != nil {
		t.Fatal(err)
	}
	if len(edits) != 1 || edits[0].RequestedPath != destination || edits[0].Path != canonical || !edits[0].Expected.Exists || edits[0].Expected.Digest != dependencytxn.Digest(original) || string(edits[0].Bytes) != "apiVersion: v2\nskills: [two]\n" || edits[0].Mode != 0o640 {
		t.Fatalf("edits = %#v", edits)
	}
	root := manager.root
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("staging root remains: %v", err)
	}
}

func TestConfigStagingManagerSupportsMissingAndSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.yaml")
	if err := os.WriteFile(real, []byte("apiVersion: v2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "config.yaml")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	manager, err := newConfigStagingManager()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := manager.Close(); err != nil {
			t.Error(err)
		}
	})
	shadow, err := manager.Path(link)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shadow, []byte("apiVersion: v2\nskills: [linked]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "new", "config.local.yaml")
	missingShadow, err := manager.Path(missing)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(missingShadow), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(missingShadow, []byte("skills: [new]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	edits, err := manager.Edits()
	if err != nil {
		t.Fatal(err)
	}
	canonicalReal, err := canonicalConfigDestination(real)
	if err != nil {
		t.Fatal(err)
	}
	foundReal := false
	for _, edit := range edits {
		if edit.Path == canonicalReal {
			foundReal = edit.RequestedPath == link && edit.Expected.Exists
		}
	}
	if len(edits) != 2 || !foundReal {
		t.Fatalf("edits = %#v", edits)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("missing destination created early: %v", err)
	}
}
