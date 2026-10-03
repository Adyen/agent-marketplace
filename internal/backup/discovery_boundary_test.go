package backup

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

// spec: backup-recovery/backup-metadata-ancestor-alias-refuses-before-import
func TestCreateRefusesAliasedMetadataAncestor(t *testing.T) {
	home, root, foreign := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(root, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("inert-foreign-link-metadata", filepath.Join(foreign, "entry")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreign, filepath.Join(root, "skills", "alias")); err != nil {
		t.Fatal(err)
	}
	paths := map[hcc.Target]ManagedPaths{hcc.TargetFactory: {Root: root, RelPaths: []string{"skills/alias/entry"}}}
	if err := Create(paths); !errors.Is(err, fsboundary.ErrUnsafePath) {
		t.Fatalf("foreign symbolic metadata imported: %v", err)
	}
	if link, err := os.Readlink(filepath.Join(foreign, "entry")); err != nil || link != "inert-foreign-link-metadata" {
		t.Fatalf("foreign metadata changed: %q %v", link, err)
	}
	if archives, err := filepath.Glob(filepath.Join(home, ".agent-marketplace", "backups", "factory", "*")); err != nil || len(archives) != 0 {
		t.Fatalf("unsafe metadata archive residue: %v %v", archives, err)
	}
	if err := os.Remove(filepath.Join(root, "skills", "alias")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "skills", "alias"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("inert-contained-link-metadata", filepath.Join(root, "skills", "alias", "entry")); err != nil {
		t.Fatal(err)
	}
	if err := Create(paths); err != nil {
		t.Fatalf("valid symbolic metadata backup refused: %v", err)
	}
	if err := os.Remove(filepath.Join(root, "skills", "alias", "entry")); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(hcc.TargetFactory, root); err != nil {
		t.Fatal(err)
	}
	if link, err := os.Readlink(filepath.Join(root, "skills", "alias", "entry")); err != nil || link != "inert-contained-link-metadata" {
		t.Fatalf("real archive missed symbolic metadata: %q %v", link, err)
	}
}
