package ownership

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func TestRecoverStagedCleanupPreservesChangedPayload(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	store := f.store(WithCrasher(CrashAt(CrashAfterStagedWrite)))
	if _, err := store.Apply(Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent), Reason: ReasonSync, ExpectPriorDigest: Digest([]byte(priorContent))}); !errors.Is(err, ErrInjectedCrash) {
		t.Fatalf("actual stage fixture: %v", err)
	}
	journal, err := store.ReadJournal()
	if err != nil || journal == nil {
		t.Fatalf("prepared journal: %v", err)
	}
	if err := os.WriteFile(journal.StagedPath, []byte("inert replacement stage bytes"), 0o640); err != nil {
		t.Fatal(err)
	}
	outcomes, err := f.store().Recover()
	if !errors.Is(err, fsboundary.ErrGenerationConflict) || len(outcomes) != 0 {
		t.Fatalf("changed stage removed: %v %v", outcomes, err)
	}
	if data, err := os.ReadFile(journal.StagedPath); err != nil || string(data) != "inert replacement stage bytes" {
		t.Fatalf("changed stage overwritten/deleted: %q %v", data, err)
	}
	if _, err := os.Lstat(store.JournalFile()); err != nil {
		t.Fatalf("refusal lost recovery evidence: %v", err)
	}
}

// spec: owned-entry-reconciliation/recovery-staged-cleanup-refuses-foreign-path-authority
func TestRecoverStagedCleanupRefusesForeignPathAuthority(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	store := f.store(WithCrasher(CrashAt(CrashAfterStagedWrite)))
	_, err := store.Apply(Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent), Reason: ReasonSync, ExpectPriorDigest: Digest([]byte(priorContent))})
	if !errors.Is(err, ErrInjectedCrash) {
		t.Fatalf("fixture did not stage an actual interrupted mutation: %v", err)
	}
	journal, err := store.ReadJournal()
	if err != nil || journal == nil {
		t.Fatalf("prepared journal missing: %v", err)
	}
	stagedPath := journal.StagedPath
	foreign := filepath.Join(t.TempDir(), "inert-foreign-file")
	if err := os.WriteFile(foreign, []byte("inert foreign bytes"), 0o640); err != nil {
		t.Fatal(err)
	}
	journal.StagedPath = foreign
	raw, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.JournalFile(), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	outcomes, err := f.store().Recover()
	if !errors.Is(err, fsboundary.ErrUnsafePath) || len(outcomes) != 0 {
		t.Fatalf("journal granted foreign deletion authority: %v %v", outcomes, err)
	}
	if data, err := os.ReadFile(foreign); err != nil || string(data) != "inert foreign bytes" {
		t.Fatalf("foreign cleanup bytes changed: %q %v", data, err)
	}
	if data, err := os.ReadFile(stagedPath); err != nil || string(data) != nextContent {
		t.Fatalf("admitted stage changed on refusal: %q %v", data, err)
	}
	if got, _ := f.readAggregate(); got != priorContent {
		t.Fatalf("recovery refusal changed live aggregate: %q", got)
	}
	journal.StagedPath = stagedPath
	raw, err = json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.JournalFile(), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	outcomes, err = f.store().Recover()
	if err != nil || len(outcomes) != 1 || outcomes[0].Action != ActionRolledBack {
		t.Fatalf("valid real staged rollback failed: %v %v", outcomes, err)
	}
	for _, path := range []string{stagedPath, store.JournalFile()} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("healthy recovery left stage/journal: %s %v", path, err)
		}
	}
}
