package backup

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

// spec: backup-recovery/symbolic-restore-refuses-hardlinked-destination
func TestRestoreSymlinkRefusesHardlinkedDestination(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	entry := filepath.Join(root, "entry")
	if err := os.Symlink("inert-archived-target", entry); err != nil {
		t.Fatal(err)
	}
	if err := Create(map[hcc.Target]ManagedPaths{hcc.TargetFactory: {Root: root, RelPaths: []string{"entry"}}}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(entry); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(t.TempDir(), "foreign-regular")
	if err := os.WriteFile(foreign, []byte("inert current generation"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(foreign, entry); err != nil {
		t.Fatal(err)
	}
	before, err := os.Lstat(entry)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(hcc.TargetFactory, root); !errors.Is(err, fsboundary.ErrUnsafeFile) {
		t.Fatalf("symbolic restore removed multiply linked leaf: %v", err)
	}
	if after, err := os.Lstat(entry); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("refusal changed destination generation: %v", err)
	}
	if data, err := os.ReadFile(foreign); err != nil || string(data) != "inert current generation" {
		t.Fatalf("refusal changed foreign bytes: %q %v", data, err)
	}
	if err := os.Remove(foreign); err != nil {
		t.Fatal(err)
	}
	if restored, err := Restore(hcc.TargetFactory, root); err != nil || restored == "" {
		t.Fatalf("regular destination did not genuinely restore: %q %v", restored, err)
	}
	if target, err := os.Readlink(entry); err != nil || target != "inert-archived-target" {
		t.Fatalf("actual symbolic target not restored: %q %v", target, err)
	}
}
