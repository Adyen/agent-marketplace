package backup

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

// spec: backup-recovery/restore-payload-hardlink-refuses-before-extraction
func TestRestoreRefusesHardlinkedArchivePayload(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	file := filepath.Join(root, "skills", "demo", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("inert archived generation"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := Create(map[hcc.Target]ManagedPaths{hcc.TargetFactory: {Root: root, RelPaths: []string{"skills"}}}); err != nil {
		t.Fatal(err)
	}
	archives, err := filepath.Glob(filepath.Join(home, ".agent-marketplace", "backups", "factory", "*.tar.gz"))
	if err != nil || len(archives) != 1 {
		t.Fatalf("real archive absent: %v %v", archives, err)
	}
	before, err := os.ReadFile(archives[0])
	if err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(home, "foreign.tar.gz")
	if err := os.Link(archives[0], foreign); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("inert current generation"), 0o640); err != nil {
		t.Fatal(err)
	}
	if restored, err := Restore(hcc.TargetFactory, root); restored != "" || !errors.Is(err, fsboundary.ErrUnsafeFile) {
		t.Fatalf("hardlinked payload restored: %q %v", restored, err)
	}
	if after, err := os.ReadFile(file); err != nil || string(after) != "inert current generation" {
		t.Fatalf("unsafe archive changed current payload: %q %v", after, err)
	}
	if after, err := os.ReadFile(foreign); err != nil || !bytes.Equal(after, before) {
		t.Fatalf("foreign archive changed: %v", err)
	}
	if err := os.Remove(foreign); err != nil {
		t.Fatal(err)
	}
	if restored, err := Restore(hcc.TargetFactory, root); err != nil || restored == "" {
		t.Fatalf("regular payload restore failed: %q %v", restored, err)
	}
	if after, err := os.ReadFile(file); err != nil || string(after) != "inert archived generation" {
		t.Fatalf("positive extraction did not update: %q %v", after, err)
	}
}
