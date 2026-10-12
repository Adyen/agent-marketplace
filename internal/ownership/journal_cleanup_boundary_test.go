package ownership

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

// spec: owned-entry-reconciliation/journal-cleanup-preserves-replacement-generation
func TestApplyJournalCleanupPreservesReplacementGeneration(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	retained := filepath.Join(t.TempDir(), "retained-journal")
	hit := false
	var store *Store
	store = f.store(WithCrasher(publicationBoundaryCrasher(func(point CrashPoint) error {
		if point != CrashAfterStateCommit {
			return nil
		}
		hit = true
		if err := os.Rename(store.JournalFile(), retained); err != nil {
			t.Fatal(err)
		}
		return os.WriteFile(store.JournalFile(), []byte("inert foreign replacement journal"), 0o640)
	})))
	_, err := store.Apply(Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent), Reason: ReasonSync, ExpectPriorDigest: Digest([]byte(priorContent))})
	if !hit || !errors.Is(err, fsboundary.ErrGenerationConflict) {
		t.Fatalf("replacement journal deleted: hit=%v err=%v", hit, err)
	}
	if data, err := os.ReadFile(store.JournalFile()); err != nil || string(data) != "inert foreign replacement journal" {
		t.Fatalf("replacement journal changed: %q %v", data, err)
	}
	if data, err := os.ReadFile(retained); err != nil || len(data) == 0 {
		t.Fatalf("admitted journal generation lost: %v", err)
	}
	if got, _ := f.readAggregate(); got != nextContent {
		t.Fatalf("mutation fixture did not commit actual aggregate: %q", got)
	}
	if record, err := store.Load(); err != nil || record == nil || record.Generation != 1 {
		t.Fatalf("mutation fixture did not commit actual state: %v", err)
	}
	if err := os.Remove(store.JournalFile()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(retained, store.JournalFile()); err != nil {
		t.Fatal(err)
	}
	outcomes, err := store.Recover()
	if err != nil || len(outcomes) != 1 || outcomes[0].Action != ActionAlreadyCommitted {
		t.Fatalf("restored admitted journal did not clean through actual recovery: %v %v", outcomes, err)
	}
	if _, err := os.Lstat(store.JournalFile()); !os.IsNotExist(err) {
		t.Fatalf("healthy recovery left journal: %v", err)
	}
}
