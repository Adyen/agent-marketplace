package backup

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func TestRetentionMemberRefusesReplacementGeneration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "archive.tar.gz")
	if err := os.WriteFile(path, []byte("inert admitted archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := fsboundary.OpenDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	identity, err := inspectRetentionMember(root, "archive.tar.gz", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, filepath.Join(t.TempDir(), "retained-archive")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("inert replacement archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := inspectRetentionMember(root, "archive.tar.gz", &identity); !errors.Is(err, fsboundary.ErrGenerationConflict) {
		t.Fatalf("replacement admitted for retention: %v", err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "inert replacement archive" {
		t.Fatalf("replacement changed: %q %v", data, err)
	}
}

// spec: backup-recovery/ordinary-retention-refuses-hardlinked-archive
func TestRotateRefusesHardlinkedArchive(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".agent-marketplace", "backups", "factory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(dir, "2026-01-01-000000.tar.gz")
	latest := filepath.Join(dir, "2026-01-02-000000.tar.gz")
	for _, name := range []string{old, latest} {
		if err := os.WriteFile(name, []byte("inert archive bytes"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	foreign := filepath.Join(t.TempDir(), "foreign-archive")
	if err := os.Link(old, foreign); err != nil {
		t.Fatal(err)
	}
	if err := Rotate(Config{MaxDays: 1, MaxPerDay: 1}); !errors.Is(err, fsboundary.ErrUnsafeFile) {
		t.Fatalf("multiply linked retention member admitted: %v", err)
	}
	for _, name := range []string{old, latest, foreign} {
		if data, err := os.ReadFile(name); err != nil || string(data) != "inert archive bytes" {
			t.Fatalf("refusal changed archive %s: %q %v", name, data, err)
		}
	}
	if err := os.Remove(foreign); err != nil {
		t.Fatal(err)
	}
	if err := Rotate(Config{MaxDays: 1, MaxPerDay: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(old); !os.IsNotExist(err) {
		t.Fatalf("valid old archive not removed: %v", err)
	}
	if data, err := os.ReadFile(latest); err != nil || string(data) != "inert archive bytes" {
		t.Fatalf("newest archive not retained: %q %v", data, err)
	}
}
