package ownership

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func TestRecoverRefusesPreparedJournalReplacementBeforeCleanup(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	crashing := f.store(WithCrasher(CrashAt(CrashAfterStagedWrite)))
	if _, err := crashing.Apply(Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent), Reason: ReasonSync, ExpectPriorDigest: Digest([]byte(priorContent))}); !errors.Is(err, ErrInjectedCrash) {
		t.Fatalf("actual prepared crash: %v", err)
	}
	journal, err := crashing.ReadJournal()
	if err != nil || journal == nil {
		t.Fatalf("actual prepared journal absent: %v", err)
	}
	original, err := os.ReadFile(crashing.JournalFile())
	if err != nil {
		t.Fatal(err)
	}
	retained := filepath.Join(t.TempDir(), "retained-prepared-journal")
	changed := false
	store := f.store(WithCrasher(publicationBoundaryCrasher(func(point CrashPoint) error {
		if point != CrashAfterRecoveryRead || changed {
			return nil
		}
		changed = true
		if err := os.Rename(crashing.JournalFile(), retained); err != nil {
			return err
		}
		return os.WriteFile(crashing.JournalFile(), original, 0o600)
	})))
	out, err := store.Recover()
	if !changed || len(out) != 0 || !errors.Is(err, fsboundary.ErrGenerationConflict) {
		t.Fatalf("stale prepared action: changed=%t outcomes=%+v err=%v", changed, out, err)
	}
	if staged, err := os.ReadFile(journal.StagedPath); err != nil || string(staged) != nextContent {
		t.Fatalf("stale journal already consumed staged evidence: %v", err)
	}
	if native, exists := f.readAggregate(); !exists || native != priorContent {
		t.Fatalf("refusal changed prior native aggregate: %q exists=%v", native, exists)
	}
	if err := os.Remove(store.JournalFile()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(retained, store.JournalFile()); err != nil {
		t.Fatal(err)
	}
	out, err = f.store().Recover()
	if err != nil || len(out) != 1 || out[0].Action != ActionRolledBack {
		t.Fatalf("restored prepared journal did not really roll back: %+v %v", out, err)
	}
	if _, err := os.Lstat(journal.StagedPath); !os.IsNotExist(err) {
		t.Fatalf("genuine rollback retained stage: %v", err)
	}
	if journal, err := store.ReadJournal(); err != nil || journal != nil {
		t.Fatalf("genuine rollback retained journal: %v", err)
	}
}

func TestRecoverRefusesOriginalRecordReplacementBeforeAction(t *testing.T) {
	f := newFixture(t)
	mustApply(t, f.store(), Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(priorContent), Reason: ReasonSync})
	crashing := f.store(WithCrasher(CrashAt(CrashAfterJournalRenamed)))
	if _, err := crashing.Apply(Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent), Reason: ReasonSync, ExpectPriorGeneration: 1, ExpectPriorDigest: Digest([]byte(priorContent))}); !errors.Is(err, ErrInjectedCrash) {
		t.Fatalf("actual second renamed-generation crash: %v", err)
	}
	original, err := os.ReadFile(crashing.StateFile())
	if err != nil {
		t.Fatal(err)
	}
	journalBytes, err := os.ReadFile(crashing.JournalFile())
	if err != nil {
		t.Fatal(err)
	}
	retained := filepath.Join(t.TempDir(), "retained-original-record")
	changed := false
	store := f.store(WithCrasher(publicationBoundaryCrasher(func(point CrashPoint) error {
		if point != CrashAfterRecoveryRead || changed {
			return nil
		}
		changed = true
		if err := os.Rename(crashing.StateFile(), retained); err != nil {
			return err
		}
		return os.WriteFile(crashing.StateFile(), original, 0o600)
	})))
	out, err := store.Recover()
	if !changed || len(out) != 0 || !errors.Is(err, fsboundary.ErrGenerationConflict) {
		t.Fatalf("stale record action: changed=%t outcomes=%+v err=%v", changed, out, err)
	}
	if raw, err := os.ReadFile(store.StateFile()); err != nil || !bytes.Equal(raw, original) {
		t.Fatalf("recovery overwrote replacement record: %v", err)
	}
	if raw, err := os.ReadFile(store.JournalFile()); err != nil || !bytes.Equal(raw, journalBytes) {
		t.Fatalf("refusal changed original recovery journal: %v", err)
	}
	if err := os.Remove(store.StateFile()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(retained, store.StateFile()); err != nil {
		t.Fatal(err)
	}
	out, err = f.store().Recover()
	if err != nil || len(out) != 1 || out[0].Action != ActionRolledForward {
		t.Fatalf("restored record did not really roll forward: %+v %v", out, err)
	}
	if record, err := f.store().Load(); err != nil || record.Generation != 2 {
		t.Fatalf("genuine state generation2 absent: %+v %v", record, err)
	}
}

// spec: owned-entry-reconciliation/recovery-refuses-original-journal-replacement-before-action
func TestRecoverRefusesOriginalJournalReplacementBeforeAction(t *testing.T) {
	f := newFixture(t)
	crashing := f.store(WithCrasher(CrashAt(CrashAfterJournalRenamed)))
	if _, err := crashing.Apply(Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent), Reason: ReasonSync}); !errors.Is(err, ErrInjectedCrash) {
		t.Fatalf("actual renamed-generation crash: %v", err)
	}
	original, err := os.ReadFile(crashing.JournalFile())
	if err != nil {
		t.Fatal(err)
	}
	retained := filepath.Join(t.TempDir(), "retained-original-journal")
	changed := false
	store := f.store(WithCrasher(publicationBoundaryCrasher(func(point CrashPoint) error {
		if point != CrashAfterRecoveryRead || changed {
			return nil
		}
		changed = true
		if err := os.Rename(crashing.JournalFile(), retained); err != nil {
			return err
		}
		return os.WriteFile(crashing.JournalFile(), original, 0o600)
	})))
	out, err := store.Recover()
	if !changed || len(out) != 0 || !errors.Is(err, fsboundary.ErrGenerationConflict) {
		t.Fatalf("stale journal action: changed=%t outcomes=%+v err=%v", changed, out, err)
	}
	if _, err := os.Lstat(store.StateFile()); !os.IsNotExist(err) {
		t.Fatalf("stale journal already committed state before refusing: %v", err)
	}
	if raw, err := os.ReadFile(store.JournalFile()); err != nil || !bytes.Equal(raw, original) {
		t.Fatalf("replacement journal changed: %v", err)
	}
	if raw, err := os.ReadFile(retained); err != nil || !bytes.Equal(raw, original) {
		t.Fatalf("retained original changed: %v", err)
	}
	if native, exists := f.readAggregate(); !exists || native != nextContent {
		t.Fatalf("refused recovery changed already-renamed aggregate: %q exists=%v", native, exists)
	}
	if err := os.Remove(store.JournalFile()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(retained, store.JournalFile()); err != nil {
		t.Fatal(err)
	}
	out, err = f.store().Recover()
	if err != nil || len(out) != 1 || out[0].Action != ActionRolledForward {
		t.Fatalf("restored original did not really roll forward: %+v %v", out, err)
	}
	if record, err := f.store().Load(); err != nil || record.Generation != 1 {
		t.Fatalf("real recovery did not commit expected authority: %+v %v", record, err)
	}
	if journal, err := f.store().ReadJournal(); err != nil || journal != nil {
		t.Fatalf("real recovery left journal: %v", err)
	}
}
