package ownership

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func TestRecoverMalformedStateKeepsPreparedRollback(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	store := f.store()
	mustApply(t, store, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent), Reason: ReasonSync})
	store = f.store(WithCrasher(publicationBoundaryCrasher(func(point CrashPoint) error {
		if point == CrashAfterStagedWrite {
			return ErrInjectedCrash
		}
		return nil
	})))
	if _, err := store.Apply(Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(priorContent), Reason: ReasonSync}); !errors.Is(err, ErrInjectedCrash) {
		t.Fatalf("actual interrupted generation: %v", err)
	}
	journal, err := store.ReadJournal()
	if err != nil || journal == nil {
		t.Fatalf("prepared journal: %v", err)
	}
	if err := os.WriteFile(store.StateFile(), []byte("{inert malformed JSON"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := f.store().Recover()
	if err != nil || len(out) != 1 || out[0].Action != ActionRolledBack {
		t.Fatalf("ordinary corrupt-state recovery changed: %+v %v", out, err)
	}
	if raw, err := os.ReadFile(store.StateFile()); err != nil || string(raw) != "{inert malformed JSON" {
		t.Fatalf("ordinary malformed state changed: %v", err)
	}
	if _, err := os.Lstat(journal.StagedPath); !os.IsNotExist(err) {
		t.Fatalf("real corrupt-state rollback did not remove stage: %v", err)
	}
	if _, err := os.Lstat(store.JournalFile()); !os.IsNotExist(err) {
		t.Fatalf("real corrupt-state rollback did not clear journal: %v", err)
	}
}

// spec: owned-entry-reconciliation/recovery-state-refusal-preserves-prepared-generation
func TestRecoverRefusesAliasedStateBeforeRollback(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	store := f.store()
	mustApply(t, store, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent), Reason: ReasonSync})
	store = f.store(WithCrasher(publicationBoundaryCrasher(func(point CrashPoint) error {
		if point == CrashAfterStagedWrite {
			return ErrInjectedCrash
		}
		return nil
	})))
	_, err := store.Apply(Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(priorContent), Reason: ReasonSync})
	if !errors.Is(err, ErrInjectedCrash) {
		t.Fatalf("prepare actual interrupted generation: %v", err)
	}
	journal, err := store.ReadJournal()
	if err != nil || journal == nil || journal.Phase != PhasePrepared {
		t.Fatalf("actual prepared journal: %v", err)
	}
	journalBytes, err := os.ReadFile(store.JournalFile())
	if err != nil {
		t.Fatal(err)
	}
	stageBytes, err := os.ReadFile(journal.StagedPath)
	if err != nil {
		t.Fatal(err)
	}
	stateBytes, err := os.ReadFile(store.StateFile())
	if err != nil {
		t.Fatal(err)
	}
	retained := filepath.Join(t.TempDir(), "retained-original-state")
	foreign := filepath.Join(t.TempDir(), "inert-foreign-state")
	if err := os.WriteFile(foreign, stateBytes, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(store.StateFile(), retained); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreign, store.StateFile()); err != nil {
		t.Fatal(err)
	}
	out, err := f.store().Recover()
	if len(out) != 0 || !errors.Is(err, fsboundary.ErrUnsafePath) {
		t.Fatalf("state refusal became successful cleanup: %+v %v", out, err)
	}
	for _, item := range []struct {
		path string
		data []byte
	}{{foreign, stateBytes}, {retained, stateBytes}, {store.JournalFile(), journalBytes}, {journal.StagedPath, stageBytes}} {
		if actual, err := os.ReadFile(item.path); err != nil || string(actual) != string(item.data) {
			t.Fatalf("refusal changed recovery evidence %s: %v", item.path, err)
		}
	}
	if got, _ := f.readAggregate(); got != nextContent {
		t.Fatalf("refused recovery changed native bytes: %q", got)
	}
	if err := os.Remove(store.StateFile()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(retained, store.StateFile()); err != nil {
		t.Fatal(err)
	}
	out, err = f.store().Recover()
	if err != nil || len(out) != 1 || out[0].Action != ActionRolledBack {
		t.Fatalf("restored real authority did not roll back: %+v %v", out, err)
	}
	if _, err := os.Lstat(journal.StagedPath); !os.IsNotExist(err) {
		t.Fatalf("healthy rollback did not remove stage: %v", err)
	}
	if _, err := os.Lstat(store.JournalFile()); !os.IsNotExist(err) {
		t.Fatalf("healthy rollback did not clear journal: %v", err)
	}
}
