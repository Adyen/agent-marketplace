package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSnapshotConfigFileRestoresSymlinkTargetWithoutReplacingLink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "shared.yaml")
	link := filepath.Join(dir, "config.yaml")
	original := []byte("original\x00bytes\n")
	if err := os.WriteFile(target, original, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Base(target), link); err != nil {
		t.Fatal(err)
	}
	snapshot, err := SnapshotConfigFile(link)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Restore(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("restored target = %q, want %q", got, original)
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("rollback replaced config symlink")
	}
}

func TestSnapshotConfigFileRefusesNonRegularFile(t *testing.T) {
	dir := t.TempDir()
	_, err := SnapshotConfigFile(dir)
	if err == nil {
		t.Fatal("SnapshotConfigFile accepted a directory")
	}
}
