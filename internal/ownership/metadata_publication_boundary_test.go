package ownership

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func TestJournalPublicationRefusalIdentifiesOperation(t *testing.T) {
	store := newFixture(t).store()
	if err := os.MkdirAll(store.recordDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(t.TempDir(), "inert-foreign-journal")
	if err := os.WriteFile(foreign, []byte("foreign journal"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreign, store.JournalFile()); err != nil {
		t.Fatal(err)
	}
	journal := &Journal{Version: journalVersion, RootKey: store.rootKey, CanonicalRoot: store.canonicalRoot, Phase: PhasePrepared}
	err := store.writeJournal(journal)
	if !errors.Is(err, fsboundary.ErrUnsafePath) {
		t.Fatalf("journal alias was not refused: %v", err)
	}
	if !strings.Contains(err.Error(), "ownership: write journal") {
		t.Fatalf("journal publication refusal omitted its operation: %v", err)
	}
	if raw, err := os.ReadFile(foreign); err != nil || string(raw) != "foreign journal" {
		t.Fatalf("journal refusal changed foreign bytes: %q %v", raw, err)
	}
	if err := os.Remove(store.JournalFile()); err != nil {
		t.Fatal(err)
	}
	if err := store.writeJournal(journal); err != nil {
		t.Fatal(err)
	}
	if actual, err := store.ReadJournal(); err != nil || actual == nil || actual.Phase != PhasePrepared {
		t.Fatalf("real journal publication failed: %+v %v", actual, err)
	}
}

func TestApplyMetadataPublicationKeepsPrivateModes(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	var store *Store
	hit := false
	store = f.store(WithCrasher(publicationBoundaryCrasher(func(point CrashPoint) error {
		if point != CrashAfterStateCommit {
			return nil
		}
		hit = true
		for _, path := range []string{store.StateFile(), store.JournalFile()} {
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0o600 {
				t.Fatalf("metadata mode not private: %s %v", path, err)
			}
		}
		return nil
	})))
	mustApply(t, store, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent), Reason: ReasonSync})
	if !hit {
		t.Fatal("metadata publication control did not commit state")
	}
	if record, err := store.Load(); err != nil || record == nil || record.Generation != 1 {
		t.Fatalf("real private state not readable: %v", err)
	}
	if got, _ := f.readAggregate(); got != nextContent {
		t.Fatalf("valid mutation did not publish aggregate: %q", got)
	}
	entries, err := os.ReadDir(store.recordDir())
	if err != nil || len(entries) != 1 || entries[0].Name() != stateFileName {
		t.Fatalf("metadata publication left staging/journal residue: %v %v", entries, err)
	}
}

// spec: owned-entry-reconciliation/retargeted-record-parent-cannot-receive-metadata-publication
func TestApplyMetadataPublicationRefusesRetargetedRecordParent(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	foreign := t.TempDir()
	foreignState := filepath.Join(foreign, stateFileName)
	foreignJournal := filepath.Join(foreign, journalFileName)
	for _, path := range []string{foreignState, foreignJournal} {
		if err := os.WriteFile(path, []byte("inert foreign metadata"), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	retained := filepath.Join(t.TempDir(), "retained-record-directory")
	hit := false
	var store *Store
	store = f.store(WithCrasher(publicationBoundaryCrasher(func(point CrashPoint) error {
		if point != CrashAfterAggregateRename {
			return nil
		}
		hit = true
		if err := os.Rename(store.recordDir(), retained); err != nil {
			t.Fatal(err)
		}
		return os.Symlink(foreign, store.recordDir())
	})))
	_, err := store.Apply(Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent), Reason: ReasonSync, ExpectPriorDigest: Digest([]byte(priorContent))})
	if !hit || (!errors.Is(err, fsboundary.ErrUnsafePath) && !errors.Is(err, fsboundary.ErrGenerationConflict)) {
		t.Fatalf("retargeted metadata parent accepted: hit=%v err=%v", hit, err)
	}
	for _, path := range []string{foreignState, foreignJournal} {
		if data, err := os.ReadFile(path); err != nil || string(data) != "inert foreign metadata" {
			t.Fatalf("foreign metadata changed: %s %q %v", path, data, err)
		}
	}
	if entries, err := os.ReadDir(foreign); err != nil || len(entries) != 2 {
		t.Fatalf("foreign metadata staging residue: %v %v", entries, err)
	}
	if got, _ := f.readAggregate(); got != nextContent {
		t.Fatalf("fixture did not publish actual aggregate before metadata refusal: %q", got)
	}
	if data, err := os.ReadFile(filepath.Join(retained, journalFileName)); err != nil || len(data) == 0 {
		t.Fatalf("admitted prepared journal lost: %v", err)
	}
}
