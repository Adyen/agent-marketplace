package ownership

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func TestSafetyRemovalRefusesReplacedGeneration(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	store := f.store()
	path, err := store.BackupOnly(f.rel, f.absFile, ReasonSync)
	if err != nil {
		t.Fatal(err)
	}
	relative := filepath.Join(safetyDirName, store.rootKey, fileSlug(f.rel))
	parent, err := store.captureStateDirectory(relative)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := parent.Close(); err != nil {
			t.Error(err)
		}
	}()
	name := filepath.Base(path)
	pin, exists, err := captureSafetyPayload(parent, name)
	if err != nil || !exists {
		t.Fatalf("selected real payload: %v", err)
	}
	if err := os.Rename(path, filepath.Join(t.TempDir(), "retained-generation")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("inert replacement generation"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.removeSafetyLeaf(parent, relative, safetyLeafRemoval{name: name, identity: pin.file, exists: true}); !errors.Is(err, fsboundary.ErrGenerationConflict) {
		t.Fatalf("selected replacement removed: %v", err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "inert replacement generation" {
		t.Fatalf("replacement safety payload changed: %q %v", data, err)
	}
}

// spec: owned-entry-reconciliation/safety-retention-preserves-linked-selected-members
func TestBackupOnlyRetentionRefusesHardlinkedMember(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	store := f.store()
	var old string
	for i := 0; i < 5; i++ {
		path, err := store.BackupOnly(f.rel, f.absFile, ReasonSync)
		if err != nil || path == "" {
			t.Fatalf("healthy initial backup: %s %v", path, err)
		}
		if i == 0 {
			old = path
		}
	}
	foreign := filepath.Join(t.TempDir(), "foreign-hardlink")
	if err := os.Link(old, foreign); err != nil {
		t.Fatal(err)
	}
	meta, err := os.ReadFile(metaPathFor(old))
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.BackupOnly(f.rel, f.absFile, ReasonSync)
	if !errors.Is(err, fsboundary.ErrUnsafeFile) {
		t.Fatalf("linked retained payload removed by name: %v", err)
	}
	for _, path := range []string{old, foreign} {
		if data, err := os.ReadFile(path); err != nil || string(data) != priorContent {
			t.Fatalf("retention changed selected/foreign payload: %s %q %v", path, data, err)
		}
	}
	if after, err := os.ReadFile(metaPathFor(old)); err != nil || string(after) != string(meta) {
		t.Fatalf("retention refusal lost selected metadata: %v", err)
	}
	if err := os.Remove(foreign); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BackupOnly(f.rel, f.absFile, ReasonSync); err != nil {
		t.Fatalf("healthy retention after unsafe shape removed: %v", err)
	}
	backups, err := store.SafetyBackups(f.rel)
	if err != nil || len(backups) != 5 {
		t.Fatalf("healthy five-newest retention failed: %v %v", backups, err)
	}
	if _, err := os.Lstat(old); !os.IsNotExist(err) {
		t.Fatalf("healthy retention did not remove old payload: %v", err)
	}
	if data, err := backups[0].RawBytes(); err != nil || string(data) != priorContent {
		t.Fatalf("retained current backup not real/readable: %q %v", data, err)
	}
}
