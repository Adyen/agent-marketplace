package ownership

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestApplyCASCleanupRemovesActualSafetyAttempt(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	store := f.store()
	refused := errors.New("inert policy refusal")
	checks := 0
	_, err := store.Apply(Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent), Reason: ReasonSync, ExpectPriorDigest: Digest([]byte(priorContent)), ValidatePolicy: func() error {
		checks++
		if checks > 1 {
			return refused
		}
		return nil
	}})
	if !errors.Is(err, refused) || checks < 2 {
		t.Fatalf("late healthy CAS fixture: %v", err)
	}
	if got, _ := f.readAggregate(); got != priorContent {
		t.Fatalf("CAS refusal changed actual aggregate: %q", got)
	}
	if _, err := os.Lstat(store.SafetyBackupDir(f.rel)); !os.IsNotExist(err) {
		t.Fatalf("healthy actual attempt directory not removed: %v", err)
	}
	if _, err := os.Lstat(store.JournalFile()); !os.IsNotExist(err) {
		t.Fatalf("healthy actual journal not removed: %v", err)
	}
	if stages, err := filepath.Glob(f.absFile + ".am-staged-*"); err != nil || len(stages) != 0 {
		t.Fatalf("healthy actual stage cleanup failed: %v %v", stages, err)
	}
}

// spec: owned-entry-reconciliation/cas-cleanup-preserves-replaced-safety-backup
func TestApplyCASCleanupPreservesReplacementSafetyBackup(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	retained := filepath.Join(t.TempDir(), "retained-safety-attempt")
	var store *Store
	var replacement string
	store = f.store(WithCrasher(publicationBoundaryCrasher(func(point CrashPoint) error {
		if point != CrashAfterStagedWrite {
			return nil
		}
		paths, err := filepath.Glob(filepath.Join(store.SafetyBackupDir(f.rel), "*.bak"))
		if err != nil || len(paths) != 1 {
			t.Fatalf("actual attempt backup missing: %v %v", paths, err)
		}
		replacement = paths[0]
		if err := os.Rename(replacement, retained); err != nil {
			t.Fatal(err)
		}
		return os.WriteFile(replacement, []byte("inert replacement safety bytes"), 0o640)
	})))
	policyChanged := errors.New("inert policy generation changed")
	checks := 0
	_, err := store.Apply(Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent), Reason: ReasonSync, ExpectPriorDigest: Digest([]byte(priorContent)), ValidatePolicy: func() error {
		checks++
		if replacement != "" {
			return policyChanged
		}
		return nil
	}})
	if !errors.Is(err, policyChanged) || checks < 2 || replacement == "" {
		t.Fatalf("actual late CAS refusal missing: %v", err)
	}
	if data, err := os.ReadFile(replacement); err != nil || string(data) != "inert replacement safety bytes" {
		t.Fatalf("CAS cleanup deleted replacement: %q %v", data, err)
	}
	if data, err := os.ReadFile(retained); err != nil || string(data) != priorContent {
		t.Fatalf("original attempt payload lost: %q %v", data, err)
	}
	if _, err := os.Lstat(metaPathFor(replacement)); err != nil {
		t.Fatalf("replacement refusal lost paired evidence: %v", err)
	}
	if got, _ := f.readAggregate(); got != priorContent {
		t.Fatalf("late refused mutation changed aggregate: %q", got)
	}
	if _, err := os.Lstat(store.JournalFile()); !os.IsNotExist(err) {
		t.Fatalf("CAS journal was not cleaned: %v", err)
	}
	if stages, err := filepath.Glob(f.absFile + ".am-staged-*"); err != nil || len(stages) != 0 {
		t.Fatalf("CAS staged cleanup failed: %v %v", stages, err)
	}
}
