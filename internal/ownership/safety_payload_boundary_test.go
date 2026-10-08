package ownership

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func TestRetainedSafetyPayloadRefusesReplacementState(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	store := f.store()
	result := mustApply(t, store, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent), Reason: ReasonSync})
	backups, err := store.SafetyBackups(f.rel)
	if err != nil || len(backups) != 1 {
		t.Fatalf("retained payload fixture: %v %v", backups, err)
	}
	if err := os.WriteFile(result.BackupPath, []byte("inert changed safety payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	if data, err := backups[0].RawBytes(); len(data) != 0 || !errors.Is(err, fsboundary.ErrGenerationConflict) {
		t.Fatalf("retained reader admitted changed state: %q %v", data, err)
	}
	if data, err := os.ReadFile(result.BackupPath); err != nil || string(data) != "inert changed safety payload" {
		t.Fatalf("changed payload modified: %q %v", data, err)
	}
}

func TestSafetyPayloadAdmissionRefusesHardlinks(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	store := f.store()
	result := mustApply(t, store, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent), Reason: ReasonSync})
	foreign := filepath.Join(t.TempDir(), "foreign-hardlink")
	if err := os.Link(result.BackupPath, foreign); err != nil {
		t.Fatal(err)
	}
	if backups, err := store.SafetyBackups(f.rel); len(backups) != 0 || !errors.Is(err, fsboundary.ErrUnsafeFile) {
		t.Fatalf("hardlinked safety payload admitted: %v %v", backups, err)
	}
	if data, err := os.ReadFile(foreign); err != nil || string(data) != priorContent {
		t.Fatalf("foreign counterpart changed: %q %v", data, err)
	}
}

// spec: owned-entry-reconciliation/retained-safety-payload-alias-refuses-before-reading
func TestRetainedSafetyPayloadRefusesAliasedGeneration(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	store := f.store()
	result := mustApply(t, store, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent), Reason: ReasonSync})
	backups, err := store.SafetyBackups(f.rel)
	if err != nil || len(backups) != 1 {
		t.Fatalf("real retained payload was not listed: %v %v", backups, err)
	}
	retained := filepath.Join(t.TempDir(), "retained-attempt-payload")
	if err := os.Rename(result.BackupPath, retained); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(t.TempDir(), "inert-foreign-payload")
	if err := os.WriteFile(foreign, []byte("inert foreign payload bytes"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreign, result.BackupPath); err != nil {
		t.Fatal(err)
	}
	data, err := backups[0].RawBytes()
	if len(data) != 0 || (!errors.Is(err, fsboundary.ErrUnsafePath) && !errors.Is(err, fsboundary.ErrUnsafeFile) && !errors.Is(err, fsboundary.ErrGenerationConflict)) {
		t.Fatalf("retained reader imported aliased foreign bytes: %q %v", data, err)
	}
	if data, err := os.ReadFile(foreign); err != nil || string(data) != "inert foreign payload bytes" {
		t.Fatalf("foreign payload changed: %q %v", data, err)
	}
	if data, err := os.ReadFile(retained); err != nil || string(data) != priorContent {
		t.Fatalf("original payload changed: %q %v", data, err)
	}
	if err := os.Remove(result.BackupPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(retained, result.BackupPath); err != nil {
		t.Fatal(err)
	}
	backups, err = store.SafetyBackups(f.rel)
	if err != nil || len(backups) != 1 {
		t.Fatalf("restored real payload not listed: %v %v", backups, err)
	}
	data, err = backups[0].RawBytes()
	if err != nil || string(data) != priorContent {
		t.Fatalf("valid actual raw backup bytes lost: %q %v", data, err)
	}
}
