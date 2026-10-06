package backup

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

// spec: backup-recovery/archive-output-refusal-keeps-foreign-replacement
func TestArchiveOutputRefusalKeepsForeignReplacement(t *testing.T) {
	parent, foreign := t.TempDir(), t.TempDir()
	retained := filepath.Join(t.TempDir(), "retained-output")
	path := filepath.Join(parent, "backup.tar.gz")
	if err := os.WriteFile(filepath.Join(foreign, "backup.tar.gz"), []byte("foreign inert archive"), 0o640); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("inert source refusal")
	err := withArchiveOutput(path, func(file *os.File) error {
		if info, err := file.Stat(); err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("new archive was not private throughout creation: %v", err)
		}
		if _, err := file.Write([]byte("inert partial archive")); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(parent, retained); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(foreign, parent); err != nil {
			t.Fatal(err)
		}
		return failure
	})
	if !errors.Is(err, failure) {
		t.Fatalf("source error lost: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(foreign, "backup.tar.gz")); err != nil || string(data) != "foreign inert archive" {
		t.Fatalf("foreign replacement archive removed: %q %v", data, err)
	}
	if entries, err := os.ReadDir(retained); err != nil || len(entries) != 0 {
		t.Fatalf("attempt archive residue in admitted directory: %v %v", entries, err)
	}
}

func TestArchiveOutputRefusalKeepsReplacementLeaf(t *testing.T) {
	parent := t.TempDir()
	path := filepath.Join(parent, "backup.tar.gz")
	retained := filepath.Join(t.TempDir(), "retained-attempt")
	err := withArchiveOutput(path, func(file *os.File) error {
		if _, err := file.Write([]byte("inert attempt bytes")); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(path, retained); err != nil {
			t.Fatal(err)
		}
		return os.WriteFile(path, []byte("inert foreign replacement"), 0o640)
	})
	if !errors.Is(err, fsboundary.ErrGenerationConflict) {
		t.Fatalf("replacement leaf reported as attempt archive: %v", err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "inert foreign replacement" {
		t.Fatalf("foreign leaf cleanup changed replacement: %q %v", data, err)
	}
}

func TestArchiveOutputRefusesRetargetedParentGeneration(t *testing.T) {
	parent, foreign := t.TempDir(), t.TempDir()
	retained := filepath.Join(t.TempDir(), "retained-output")
	err := withArchiveOutput(filepath.Join(parent, "backup.tar.gz"), func(file *os.File) error {
		if err := os.Rename(parent, retained); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(foreign, parent); err != nil {
			t.Fatal(err)
		}
		_, err := file.Write([]byte("inert archive bytes"))
		return err
	})
	if !errors.Is(err, fsboundary.ErrGenerationConflict) {
		t.Fatalf("retargeted output accepted: %v", err)
	}
	if entries, err := os.ReadDir(foreign); err != nil || len(entries) != 0 {
		t.Fatalf("foreign output changed: %v %v", entries, err)
	}
}
