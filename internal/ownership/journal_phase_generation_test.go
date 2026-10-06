package ownership

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

// spec: owned-entry-reconciliation/journal-phase-update-preserves-original-generation
func TestApplyJournalPhaseUpdatePreservesReplacementGeneration(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	retained := filepath.Join(t.TempDir(), "retained-prepared-journal")
	var original []byte
	var replacementInfo os.FileInfo
	hit := false
	var store *Store
	store = f.store(WithCrasher(publicationBoundaryCrasher(func(point CrashPoint) error {
		if point != CrashAfterAggregateRename {
			return nil
		}
		hit = true
		var err error
		original, err = os.ReadFile(store.JournalFile())
		if err != nil {
			return err
		}
		if err := os.Rename(store.JournalFile(), retained); err != nil {
			return err
		}
		if err := os.WriteFile(store.JournalFile(), original, 0o600); err != nil {
			return err
		}
		replacementInfo, err = os.Lstat(store.JournalFile())
		return err
	})))
	_, err := store.Apply(Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent), Reason: ReasonSync, ExpectPriorDigest: Digest([]byte(priorContent))})
	if !hit || !errors.Is(err, fsboundary.ErrGenerationConflict) {
		t.Fatalf("journal phase update overwrote replacement: hit=%t err=%v", hit, err)
	}
	if native, _ := f.readAggregate(); native != nextContent {
		t.Fatalf("fixture did not publish real aggregate before phase refusal: %q", native)
	}
	for _, path := range []string{store.JournalFile(), retained} {
		if raw, err := os.ReadFile(path); err != nil || !bytes.Equal(raw, original) {
			t.Fatalf("phase refusal changed original/replacement journal: %s %v", path, err)
		}
	}
	if after, err := os.Lstat(store.JournalFile()); err != nil || !os.SameFile(replacementInfo, after) || after.Mode() != replacementInfo.Mode() || !after.ModTime().Equal(replacementInfo.ModTime()) {
		t.Fatalf("replacement metadata generation changed: %v", err)
	}
	if _, err := os.Lstat(store.StateFile()); !os.IsNotExist(err) {
		t.Fatalf("phase refusal committed stale ownership state: %v", err)
	}
	if entries, err := os.ReadDir(filepath.Dir(store.JournalFile())); err != nil || len(entries) != 1 || entries[0].Name() != "journal.json" {
		t.Fatalf("phase refusal left metadata staging residue: %v %v", entries, err)
	}
	if err := os.Remove(store.JournalFile()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(retained, store.JournalFile()); err != nil {
		t.Fatal(err)
	}
	outcomes, err := f.store().Recover()
	if err != nil || len(outcomes) != 1 || outcomes[0].Action != ActionRolledForward {
		t.Fatalf("restored prepared journal did not actually recover renamed aggregate: %+v %v", outcomes, err)
	}
	if record, err := f.store().Load(); err != nil || record == nil || record.Generation != 1 {
		t.Fatalf("actual recovery did not commit matching generation: %+v %v", record, err)
	}
	if journal, err := f.store().ReadJournal(); err != nil || journal != nil {
		t.Fatalf("healthy recovery retained journal: %v", err)
	}
}
