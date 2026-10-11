package ownership

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func TestApplyRefusesOriginalRecordReplacementBeforeCommit(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	store := f.store()
	mustApply(t, store, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent), Reason: ReasonSync})
	raw, err := os.ReadFile(store.StateFile())
	if err != nil {
		t.Fatal(err)
	}
	retained := filepath.Join(t.TempDir(), "retained-record")
	hit := false
	store = f.store(WithCrasher(publicationBoundaryCrasher(func(point CrashPoint) error {
		if point != CrashAfterJournalRenamed {
			return nil
		}
		hit = true
		if err := os.Rename(store.StateFile(), retained); err != nil {
			return err
		}
		return os.WriteFile(store.StateFile(), raw, 0o600)
	})))
	_, err = store.Apply(Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(priorContent), Reason: ReasonSync, ExpectPriorGeneration: 1, ExpectPriorDigest: Digest([]byte(nextContent))})
	if !hit || !errors.Is(err, fsboundary.ErrGenerationConflict) {
		t.Fatalf("replacement overwritten at record commit: %v", err)
	}
	if after, err := os.ReadFile(store.StateFile()); err != nil || string(after) != string(raw) {
		t.Fatalf("replacement record changed: %v", err)
	}
	if got, _ := f.readAggregate(); got != priorContent {
		t.Fatalf("late boundary did not actually publish aggregate: %q", got)
	}
	if journal, err := store.ReadJournal(); err != nil || journal == nil || journal.Phase != PhaseAggregateRenamed {
		t.Fatalf("late refusal lost durable recovery journal: %v", err)
	}
}

// spec: owned-entry-reconciliation/original-record-replacement-refuses-publication
func TestApplyRefusesSameJSONOriginalRecordReplacement(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	store := f.store()
	mustApply(t, store, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent), Reason: ReasonSync})
	raw, err := os.ReadFile(store.StateFile())
	if err != nil {
		t.Fatal(err)
	}
	retained := filepath.Join(t.TempDir(), "retained-original-record")
	hit := false
	store = f.store(WithCrasher(publicationBoundaryCrasher(func(point CrashPoint) error {
		if point != CrashAfterStagedWrite {
			return nil
		}
		hit = true
		if err := os.Rename(store.StateFile(), retained); err != nil {
			return err
		}
		return os.WriteFile(store.StateFile(), raw, 0o600)
	})))
	_, err = store.Apply(Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(priorContent), Reason: ReasonSync, ExpectPriorGeneration: 1, ExpectPriorDigest: Digest([]byte(nextContent))})
	if !hit || (!errors.Is(err, fsboundary.ErrGenerationConflict) && !errors.Is(err, ErrGenerationConflict)) {
		t.Fatalf("same JSON replacement authorized stale publication: %v", err)
	}
	if got, _ := f.readAggregate(); got != nextContent {
		t.Fatalf("record refusal changed admitted native generation: %q", got)
	}
	if after, err := os.ReadFile(store.StateFile()); err != nil || string(after) != string(raw) {
		t.Fatalf("replacement record overwritten: %q %v", after, err)
	}
	if after, err := os.ReadFile(retained); err != nil || string(after) != string(raw) {
		t.Fatalf("original record changed: %v", err)
	}
	if err := os.Remove(store.StateFile()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(retained, store.StateFile()); err != nil {
		t.Fatal(err)
	}
	store = f.store()
	result, err := store.Apply(Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(priorContent), Reason: ReasonSync, ExpectPriorGeneration: 1, ExpectPriorDigest: Digest([]byte(nextContent))})
	if err != nil || result.Generation != 2 {
		t.Fatalf("healthy real generation update failed: %+v %v", result, err)
	}
	if got, _ := f.readAggregate(); got != priorContent {
		t.Fatalf("healthy native update did not occur: %q", got)
	}
}
