package ownership

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

func TestOwnershipMetadataPreservesInitialStateRootAlias(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	store := f.store()
	mustApply(t, store, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent), Reason: ReasonSync})
	alias := filepath.Join(t.TempDir(), "state-alias")
	if err := os.Symlink(f.stateRoot, alias); err != nil {
		t.Fatal(err)
	}
	aliased, err := Open(f.targetRoot, WithStateRoot(alias))
	if err != nil {
		t.Fatal(err)
	}
	if record, err := aliased.Load(); err != nil || record == nil || record.Generation != 1 {
		t.Fatalf("initial explicitly selected state-root alias refused: %v", err)
	}
	if journal, err := aliased.ReadJournal(); err != nil || journal != nil {
		t.Fatalf("absent journal semantics changed: %v %v", journal, err)
	}
	if aliased.StateRoot() != alias {
		t.Fatalf("public state path changed: %s", aliased.StateRoot())
	}
}

func TestOwnershipMetadataRefusesUnsafeShapes(t *testing.T) {
	for _, kind := range []string{"state", "journal"} {
		for _, shape := range []string{"hardlink", "external parent", "dangling parent"} {
			t.Run(kind+"/"+shape, func(t *testing.T) {
				f := newFixture(t)
				f.writeAggregate(priorContent)
				store := f.store()
				if kind == "state" {
					mustApply(t, store, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent), Reason: ReasonSync})
				} else {
					crashing := f.store(WithCrasher(CrashAt(CrashAfterJournalPrepared)))
					if _, err := crashing.Apply(Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent), Reason: ReasonSync, ExpectPriorDigest: Digest([]byte(priorContent))}); !errors.Is(err, ErrInjectedCrash) {
						t.Fatalf("journal fixture: %v", err)
					}
				}
				path := store.StateFile()
				if kind == "journal" {
					path = store.JournalFile()
				}
				before, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				foreign := filepath.Join(t.TempDir(), "foreign-metadata")
				switch shape {
				case "hardlink":
					if err := os.Link(path, foreign); err != nil {
						t.Fatal(err)
					}
				default:
					parent := filepath.Dir(path)
					if err := os.Rename(parent, foreign); err != nil {
						t.Fatal(err)
					}
					link := foreign
					if shape == "dangling parent" {
						link = filepath.Join(t.TempDir(), "absent")
					}
					if err := os.Symlink(link, parent); err != nil {
						t.Fatal(err)
					}
					foreign = filepath.Join(foreign, filepath.Base(path))
				}
				if kind == "state" {
					_, err = store.Load()
				} else {
					_, err = store.ReadJournal()
				}
				if !errors.Is(err, fsboundary.ErrUnsafeFile) && !errors.Is(err, fsboundary.ErrUnsafePath) {
					t.Fatalf("unsafe metadata admitted as absence/payload: %v", err)
				}
				if data, err := os.ReadFile(foreign); err != nil || string(data) != string(before) {
					t.Fatalf("foreign metadata changed: %v", err)
				}
			})
		}
	}
}

// spec: owned-entry-reconciliation/aliased-journal-cannot-authorize-recovery
func TestRecoverRefusesAliasedJournal(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	crashing := f.store(WithCrasher(CrashAt(CrashAfterJournalPrepared)))
	_, err := crashing.Apply(Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent), Reason: ReasonSync, ExpectPriorDigest: Digest([]byte(priorContent))})
	if !errors.Is(err, ErrInjectedCrash) {
		t.Fatalf("journal fixture did not reach prepared crash: %v", err)
	}
	store := f.store()
	foreign := filepath.Join(t.TempDir(), "foreign-journal")
	if err := os.Rename(store.JournalFile(), foreign); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(foreign)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreign, store.JournalFile()); err != nil {
		t.Fatal(err)
	}
	outcomes, err := store.Recover()
	if (!errors.Is(err, fsboundary.ErrUnsafePath) && !errors.Is(err, fsboundary.ErrUnsafeFile)) || len(outcomes) != 0 {
		t.Fatalf("foreign journal authorized recovery: %v %v", outcomes, err)
	}
	if link, err := os.Readlink(store.JournalFile()); err != nil || link != foreign {
		t.Fatalf("recovery changed journal alias: %q %v", link, err)
	}
	if got, err := os.ReadFile(foreign); err != nil || string(got) != string(before) {
		t.Fatalf("foreign journal changed: %v", err)
	}
	if got, _ := f.readAggregate(); got != priorContent {
		t.Fatalf("unsafe recovery changed aggregate: %q", got)
	}
	if err := os.Remove(store.JournalFile()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(foreign, store.JournalFile()); err != nil {
		t.Fatal(err)
	}
	outcomes, err = store.Recover()
	if err != nil || len(outcomes) != 1 || outcomes[0].Action != ActionRolledBack {
		t.Fatalf("valid prepared journal failed actual recovery: %v %v", outcomes, err)
	}
}

// spec: owned-entry-reconciliation/aliased-ownership-state-cannot-grant-proof
func TestProofRefusesAliasedOwnershipState(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	store := f.store()
	entry := mcpEntry("generated", "generated-server", `{"command":"new"}`)
	mustApply(t, store, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent), Entries: []Entry{entry}, Reason: ReasonSync})
	foreign := filepath.Join(t.TempDir(), "foreign-state")
	if err := os.Rename(store.StateFile(), foreign); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(foreign)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreign, store.StateFile()); err != nil {
		t.Fatal(err)
	}
	proof, err := store.Proof(f.rel)
	if !errors.Is(err, ErrStateCorrupt) || (!errors.Is(err, fsboundary.ErrUnsafePath) && !errors.Is(err, fsboundary.ErrUnsafeFile)) {
		t.Fatalf("foreign state granted ownership proof: %v", err)
	}
	coded, ok := diag.AsCodedError(err)
	if !ok || coded.Code != diag.CodeOwnershipStateCorrupt {
		t.Fatalf("state refusal diagnostic: %v", err)
	}
	if proof.OwnedCount() != 0 || proof.Classify(entry.ID(), entry.ValueDigest) != StatusUnmanaged {
		t.Fatal("aliased state authorized an owned entry")
	}
	if got, err := os.ReadFile(foreign); err != nil || string(got) != string(before) {
		t.Fatalf("foreign state changed: %v", err)
	}
	if got, _ := f.readAggregate(); got != nextContent {
		t.Fatalf("proof read changed aggregate: %q", got)
	}
	if err := os.Remove(store.StateFile()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(foreign, store.StateFile()); err != nil {
		t.Fatal(err)
	}
	proof, err = store.Proof(f.rel)
	if err != nil || proof.OwnedCount() != 1 || proof.Classify(entry.ID(), entry.ValueDigest) != StatusOwnedClean {
		t.Fatalf("valid real state lost ownership proof: %v", err)
	}
}
