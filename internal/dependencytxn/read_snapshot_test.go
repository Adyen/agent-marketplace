package dependencytxn

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestReadSnapshotReturnsBytesGenerationAndMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := []byte("apiVersion: agent-marketplace/v2\nregistries: []\n")
	if err := os.WriteFile(path, content, 0o640); err != nil {
		t.Fatal(err)
	}
	raw, generation, mode, err := ReadSnapshot(path)
	if err != nil {
		t.Fatalf("ReadSnapshot: %v", err)
	}
	if string(raw) != string(content) {
		t.Fatalf("bytes = %q", raw)
	}
	if !generation.Exists || generation.Digest != Digest(content) {
		t.Fatalf("generation = %#v", generation)
	}
	if mode != 0o640 {
		t.Fatalf("mode = %#o", mode)
	}
}

func TestReadSnapshotMissingReturnsZero(t *testing.T) {
	raw, generation, mode, err := ReadSnapshot(filepath.Join(t.TempDir(), "absent.yaml"))
	if err != nil {
		t.Fatalf("ReadSnapshot missing: %v", err)
	}
	if raw != nil || generation.Exists || generation.Digest != "" || mode != 0 {
		t.Fatalf("expected zero snapshot, got bytes=%q generation=%#v mode=%#o", raw, generation, mode)
	}
}

func TestReadSnapshotRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.yaml")
	if err := os.WriteFile(target, []byte("real"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.yaml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	raw, generation, _, err := ReadSnapshot(link)
	if err == nil {
		t.Fatalf("expected symlink refusal, got bytes=%q generation=%#v", raw, generation)
	}
	if !errors.Is(err, ErrUnsafePath) && !errors.Is(err, syscall.ELOOP) {
		t.Fatalf("expected unsafe-path refusal, got %v", err)
	}
}
