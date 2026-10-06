package ownership

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func TestBackupOnlyPreservesReplacedProducer(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	var directory, replaced string
	retained := filepath.Join(t.TempDir(), "retained-producer")
	store := f.store(WithClock(func() time.Time {
		if replaced == "" {
			entries, err := filepath.Glob(filepath.Join(directory, "*.bak"))
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				replaced = entries[0]
				if err := os.Rename(replaced, retained); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(replaced, []byte("inert foreign replacement"), 0o640); err != nil {
					t.Fatal(err)
				}
			}
		}
		return time.Date(2026, 10, 4, 1, 0, 0, 0, time.UTC)
	}))
	directory = store.SafetyBackupDir(f.rel)
	path, err := store.BackupOnly(f.rel, f.absFile, ReasonSync)
	if path != "" || !errors.Is(err, fsboundary.ErrGenerationConflict) || replaced == "" {
		t.Fatalf("replaced safety producer accepted: %s %v", path, err)
	}
	if data, err := os.ReadFile(replaced); err != nil || string(data) != "inert foreign replacement" {
		t.Fatalf("replacement producer removed/changed: %q %v", data, err)
	}
	if data, err := os.ReadFile(retained); err != nil || string(data) != priorContent {
		t.Fatalf("actual prior producer changed: %q %v", data, err)
	}
	if _, err := os.Lstat(metaPathFor(replaced)); !os.IsNotExist(err) {
		t.Fatalf("refusal published safety claims for replacement: %v", err)
	}
}

// spec: owned-entry-reconciliation/safety-publication-refuses-retargeted-directory
func TestBackupOnlyRefusesRetargetedSafetyDirectory(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	foreign := t.TempDir()
	retained := filepath.Join(t.TempDir(), "retained-safety-directory")
	var directory string
	changed := false
	store := f.store(WithClock(func() time.Time {
		if _, err := os.Lstat(directory); !changed && err == nil {
			changed = true
			if err := os.Rename(directory, retained); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(foreign, directory); err != nil {
				t.Fatal(err)
			}
		}
		return time.Date(2026, 10, 4, 1, 0, 0, 0, time.UTC)
	}))
	directory = store.SafetyBackupDir(f.rel)
	path, err := store.BackupOnly(f.rel, f.absFile, ReasonSync)
	if path != "" || (!errors.Is(err, fsboundary.ErrUnsafePath) && !errors.Is(err, fsboundary.ErrGenerationConflict)) {
		t.Fatalf("retargeted safety publication admitted: %s %v", path, err)
	}
	if entries, err := os.ReadDir(foreign); err != nil || len(entries) != 0 {
		t.Fatalf("safety bytes escaped to foreign directory: %v %v", entries, err)
	}
	if entries, err := os.ReadDir(retained); err != nil || len(entries) != 0 {
		t.Fatalf("refused safety attempt left payload/staging residue: %v %v", entries, err)
	}
	if got, _ := f.readAggregate(); got != priorContent {
		t.Fatalf("backup refusal changed aggregate: %q", got)
	}
	if err := os.Remove(directory); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(retained, directory); err != nil {
		t.Fatal(err)
	}
	path, err = store.BackupOnly(f.rel, f.absFile, ReasonSync)
	if err != nil || path == "" {
		t.Fatalf("healthy backup-only publication failed: %s %v", path, err)
	}
	for _, private := range []string{store.recordDir(), directory, path, metaPathFor(path)} {
		info, err := os.Stat(private)
		if err != nil {
			t.Fatal(err)
		}
		want := os.FileMode(0o600)
		if info.IsDir() {
			want = 0o700
		}
		if info.Mode().Perm() != want {
			t.Fatalf("private creation mode: %s %o want %o", private, info.Mode().Perm(), want)
		}
	}
	backups, err := store.SafetyBackups(f.rel)
	if err != nil || len(backups) != 1 {
		t.Fatalf("healthy published evidence not readable: %v %v", backups, err)
	}
	if data, err := backups[0].RawBytes(); err != nil || string(data) != priorContent {
		t.Fatalf("healthy safety backup lost actual prior bytes: %q %v", data, err)
	}
}
