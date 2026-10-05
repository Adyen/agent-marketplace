package backup

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

// spec: backup-recovery/codex-manifest-alias-refuses-before-restore
func TestRestoreCodexSetRefusesAliasedManifest(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, ".agents")
	managed := filepath.Join(root, "skills", "demo", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(managed), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(managed, []byte("inert archived generation"), 0o640); err != nil {
		t.Fatal(err)
	}
	parts := []CodexPartition{{ID: "machine-agents", Root: root, RelPaths: []string{"skills"}}, {ID: "machine-codex", Root: filepath.Join(home, ".codex"), RelPaths: []string{"agents", "hooks.json", "config.toml"}}}
	operation, err := CreateCodexSet(parts)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(managed, []byte("inert current generation"), 0o640); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(home, ".agent-marketplace", "backups", "codex", operation+".set.json")
	raw, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(t.TempDir(), "foreign-manifest.json")
	if err := os.Rename(manifest, foreign); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreign, manifest); err != nil {
		t.Fatal(err)
	}
	if restored, err := RestoreCodexSet(parts); restored != "" || !errors.Is(err, fsboundary.ErrUnsafePath) {
		t.Fatalf("aliased manifest authorized restore: %q %v", restored, err)
	}
	if after, err := os.ReadFile(managed); err != nil || string(after) != "inert current generation" {
		t.Fatalf("manifest refusal changed live payload: %q %v", after, err)
	}
	if after, err := os.ReadFile(foreign); err != nil || !bytes.Equal(after, raw) {
		t.Fatalf("foreign metadata changed: %q %v", after, err)
	}
	if err := os.Remove(manifest); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(foreign, manifest); err != nil {
		t.Fatal(err)
	}
	if restored, err := RestoreCodexSet(parts); err != nil || restored != operation {
		t.Fatalf("regular manifest restore control failed: %q %v", restored, err)
	}
	if after, err := os.ReadFile(managed); err != nil || string(after) != "inert archived generation" {
		t.Fatalf("positive restore did not update payload: %q %v", after, err)
	}
}

// spec: backup-recovery/codex-manifest-alias-refuses-before-restore
func TestRestoreCodexSetRefusesOversizedManifest(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	parts := []CodexPartition{{ID: "machine-agents", Root: filepath.Join(home, ".agents"), RelPaths: []string{"skills"}}, {ID: "machine-codex", Root: filepath.Join(home, ".codex"), RelPaths: []string{"agents", "hooks.json", "config.toml"}}}
	operation, err := CreateCodexSet(parts)
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(home, ".agent-marketplace", "backups", "codex", operation+".set.json")
	raw, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	oversize := make([]byte, maxCodexManifestBytes+1)
	if err := os.WriteFile(manifest, oversize, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(manifest); err != nil {
		t.Fatal(err)
	}
	if restored, err := RestoreCodexSet(parts); restored != "" || !errors.Is(err, fsboundary.ErrTooLarge) {
		t.Fatalf("oversized manifest authorized restore: %q %v", restored, err)
	}
	if err := os.WriteFile(manifest, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if restored, err := RestoreCodexSet(parts); err != nil || restored != operation {
		t.Fatalf("regular manifest control failed: %q %v", restored, err)
	}
}
