package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
)

// spec: config-mutation/repository-edit-refuses-an-external-configuration-target
func TestRepositoryEditRefusesExternalConfigurationTarget(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", filepath.Join(base, "home"))
	repo := filepath.Join(base, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(base, "external.yaml")
	before := []byte("apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	if err := os.WriteFile(target, before, 0o640); err != nil {
		t.Fatal(err)
	}
	beforeInfo, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(repo, ".agents", "config.yaml")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readOrInitConfig(path); !errors.Is(err, dependencytxn.ErrUnsafePath) {
		t.Fatalf("read side did not refuse the external link: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	err = SetHarnessesInConfig(path, []string{"windsurf"})
	if !errors.Is(err, dependencytxn.ErrUnsafePath) {
		t.Fatalf("repository edit was not refused at its scope boundary: %v", err)
	}
	after, err := os.ReadFile(target)
	if err != nil || string(after) != string(before) {
		t.Fatalf("refusal changed external bytes: %q %v", after, err)
	}
	afterInfo, err := os.Stat(target)
	if err != nil || !os.SameFile(beforeInfo, afterInfo) || beforeInfo.Mode() != afterInfo.Mode() || !beforeInfo.ModTime().Equal(afterInfo.ModTime()) {
		t.Fatalf("refusal changed external identity, mode or mtime: %v", err)
	}
	if link, err := os.Readlink(path); err != nil || link != target {
		t.Fatalf("refusal changed the link: %q %v", link, err)
	}
	entries, err := os.ReadDir(filepath.Join(repo, ".agents"))
	if err != nil || len(entries) != 1 || entries[0].Name() != "config.yaml" {
		t.Fatalf("refusal left staging residue: %v %v", entries, err)
	}
}
