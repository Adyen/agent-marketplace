//go:build unix

package ownership

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
)

func prepareTxnFixture(t *testing.T) (*fixture, *Store, string) {
	t.Helper()
	f := newFixture(t)
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(base, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	return f, f.store(), filepath.Join(base, "depstate")
}

func prepareAggregateWith(value string) string {
	return `{"hooks":{"PreToolUse":[` + value + `]}}` + "\n"
}

func prepareSatisfactionMutation(f *fixture, next []byte, entries []Entry) Mutation {
	return Mutation{
		Rel:       f.rel,
		AbsFile:   f.absFile,
		NextBytes: next,
		Entries:   entries,
		Reason:    ReasonSync,
	}
}

func aggregateAssertion(t *testing.T, path string) dependencytxn.DesiredFile {
	t.Helper()
	data, generation, mode, err := dependencytxn.ReadSnapshot(path)
	if err != nil {
		t.Fatalf("snapshot aggregate: %v", err)
	}
	if !generation.Exists {
		t.Fatalf("aggregate %s missing", path)
	}
	return dependencytxn.DesiredFile{
		Path:         path,
		Expected:     generation,
		ExpectedMode: mode,
		Bytes:        data,
		Mode:         mode,
	}
}

func commitPrepared(t *testing.T, stateDir string, files ...dependencytxn.DesiredFile) dependencytxn.Result {
	t.Helper()
	for _, file := range files {
		if err := os.MkdirAll(filepath.Dir(file.Path), 0o700); err != nil {
			t.Fatalf("create destination parent: %v", err)
		}
	}
	result, err := dependencytxn.Commit(context.Background(), stateDir, files)
	if err != nil {
		t.Fatalf("dependencytxn.Commit: %v", err)
	}
	return result
}

func assertPrepareRefusedClean(t *testing.T, f *fixture, st *Store, err error, sentinel error, stateExists bool) {
	t.Helper()
	if err == nil || !errors.Is(err, sentinel) {
		t.Fatalf("Prepare error = %v, want %v in chain", err, sentinel)
	}
	if !stateExists && fileExists(t, st.StateFile()) {
		t.Fatal("refused Prepare wrote the state record")
	}
	assertNoRecordResidue(t, st)
}

func assertNoRecordResidue(t *testing.T, st *Store) {
	t.Helper()
	entries, err := os.ReadDir(st.recordDir())
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatalf("read record directory: %v", err)
	}
	for _, entry := range entries {
		if entry.Name() != stateFileName {
			t.Fatalf("refused Prepare left record-dir entry %q", entry.Name())
		}
	}
}

func TestHookMetadataPreparePublishesSatisfactionOnlyState(t *testing.T) {
	f, st, stateDir := prepareTxnFixture(t)
	aggregate := prepareAggregateWith(hookSatisfactionValueA)
	f.writeAggregate(aggregate)
	satisfaction := fixtureHookSatisfaction("PreToolUse", hookSatisfactionValueA,
		HookSourceEvidence{Resource: "alpha", SourceDigest: hookSatisfactionFPAlpha},
		HookSourceEvidence{Resource: "beta", SourceDigest: hookSatisfactionFPBeta},
	)
	mutation := prepareSatisfactionMutation(f, []byte(aggregate), nil)
	mutation.ExpectPriorDigest = Digest([]byte(aggregate))
	satisfactions := []HookSatisfaction{satisfaction}

	stateFile, err := st.Prepare(mutation, &satisfactions)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if stateFile.Path == "" {
		t.Fatal("first satisfaction publication produced no desired file")
	}
	if fileExists(t, st.StateFile()) {
		t.Fatal("Prepare persisted the record itself")
	}
	assertNoRecordResidue(t, st)
	aggregateBefore, aggregateInfo := fileSnapshot(t, f.absFile)
	commitPrepared(t, stateDir, aggregateAssertion(t, f.absFile), stateFile)
	assertOwnershipFileUnchanged(t, f.absFile, aggregateBefore, aggregateInfo)

	proof, err := st.Proof(f.rel)
	if err != nil {
		t.Fatalf("Proof after commit: %v", err)
	}
	if proof.OwnedCount() != 0 || len(proof.OwnedIDs()) != 0 {
		t.Fatalf("satisfaction-only commit granted ownership: %d/%v", proof.OwnedCount(), proof.OwnedIDs())
	}
	records := proof.HookSatisfactions()
	if len(records) != 1 || records[0].ID() != satisfaction.ID() {
		t.Fatalf("satisfaction records = %+v", records)
	}
	if !reflect.DeepEqual(records[0].Sources, []HookSourceEvidence{
		{Resource: "alpha", SourceDigest: hookSatisfactionFPAlpha},
		{Resource: "beta", SourceDigest: hookSatisfactionFPBeta},
	}) {
		t.Fatalf("satisfaction sources = %+v", records[0].Sources)
	}
}

func TestStorePrepareBatchCombinesRootOwnershipMutations(t *testing.T) {
	f, st, stateDir := prepareTxnFixture(t)
	firstBytes := []byte(`{"mcpServers":{"alpha":{"command":"alpha"}}}` + "\n")
	secondRel := "hooks.v1.json"
	secondPath := filepath.Join(f.targetRoot, secondRel)
	secondBytes := []byte(`{"PreToolUse":[{"type":"command","command":"echo beta"}]}` + "\n")
	if err := os.WriteFile(f.absFile, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secondPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	firstSnapshot, firstGeneration, firstMode, err := dependencytxn.ReadSnapshot(f.absFile)
	if err != nil {
		t.Fatal(err)
	}
	secondSnapshot, secondGeneration, secondMode, err := dependencytxn.ReadSnapshot(secondPath)
	if err != nil {
		t.Fatal(err)
	}
	stateSnapshot, err := st.SnapshotState()
	if err != nil {
		t.Fatal(err)
	}
	mutations := []Mutation{
		{
			Rel: f.rel, AbsFile: f.absFile, NextBytes: firstBytes,
			Entries: []Entry{mcpEntry("alpha", "plugin-alpha", `{"command":"alpha"}`)},
			Reason:  ReasonSync, ExpectPriorGeneration: 0, ExpectPriorDigest: Digest(firstSnapshot),
		},
		{
			Rel: secondRel, AbsFile: secondPath, NextBytes: secondBytes,
			Entries: []Entry{hookEntry("PreToolUse", "plugin-beta", `{"type":"command","command":"echo beta"}`)},
			Reason:  ReasonSync, ExpectPriorGeneration: 0, ExpectPriorDigest: Digest(secondSnapshot),
		},
	}
	stateFile, err := st.PrepareBatch(stateSnapshot, mutations, nil)
	if err != nil {
		t.Fatalf("PrepareBatch: %v", err)
	}
	if stateFile.Path != st.StateFile() {
		t.Fatalf("batch ownership participant path = %q, want %q", stateFile.Path, st.StateFile())
	}
	if fileExists(t, st.StateFile()) {
		t.Fatal("PrepareBatch persisted the ownership record before dependencytxn.Commit")
	}
	if err := os.MkdirAll(filepath.Dir(stateFile.Path), 0o700); err != nil {
		t.Fatal(err)
	}
	result, err := dependencytxn.Commit(context.Background(), stateDir, []dependencytxn.DesiredFile{
		{Path: f.absFile, Expected: firstGeneration, ExpectedMode: firstMode, Bytes: firstBytes, Mode: firstMode},
		{Path: secondPath, Expected: secondGeneration, ExpectedMode: secondMode, Bytes: secondBytes, Mode: secondMode},
		stateFile,
	})
	if err != nil {
		t.Fatalf("commit aggregate batch: %v", err)
	}
	if len(result.Changed) != 3 {
		t.Fatalf("batch commit participants = %v, want both aggregates and one root state", result.Changed)
	}
	firstProof, err := st.Proof(f.rel)
	if err != nil {
		t.Fatalf("first proof: %v", err)
	}
	secondProof, err := st.Proof(secondRel)
	if err != nil {
		t.Fatalf("second proof: %v", err)
	}
	if firstProof.OwnedCount() != 1 || secondProof.OwnedCount() != 1 || firstProof.Generation() != secondProof.Generation() {
		t.Fatalf("batch proofs = %+v / %+v, want both files at one ownership generation", firstProof, secondProof)
	}
}

func TestHookMetadataPrepareRepeatIsNoop(t *testing.T) {
	f, st, stateDir := prepareTxnFixture(t)
	aggregate := prepareAggregateWith(hookSatisfactionValueA)
	f.writeAggregate(aggregate)
	satisfaction := fixtureHookSatisfaction("PreToolUse", hookSatisfactionValueA,
		HookSourceEvidence{Resource: "alpha", SourceDigest: hookSatisfactionFPAlpha})
	mutation := prepareSatisfactionMutation(f, []byte(aggregate), nil)
	mutation.ExpectPriorDigest = Digest([]byte(aggregate))
	satisfactions := []HookSatisfaction{satisfaction}
	stateFile, err := st.Prepare(mutation, &satisfactions)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	commitPrepared(t, stateDir, aggregateAssertion(t, f.absFile), stateFile)
	stateBefore, stateInfo := fileSnapshot(t, st.StateFile())

	mutation.ExpectPriorGeneration = 1
	again, err := st.Prepare(mutation, &satisfactions)
	if err != nil {
		t.Fatalf("repeat Prepare: %v", err)
	}
	if again.Path != "" {
		t.Fatalf("repeat Prepare wanted to rewrite state: %+v", again)
	}
	assertOwnershipFileUnchanged(t, st.StateFile(), stateBefore, stateInfo)
}

func TestHookMetadataPrepareReplaceAndClearSemantics(t *testing.T) {
	f, st, stateDir := prepareTxnFixture(t)
	aggregate := prepareAggregateWith(hookSatisfactionValueA)
	f.writeAggregate(aggregate)
	mutation := prepareSatisfactionMutation(f, []byte(aggregate), nil)
	mutation.ExpectPriorDigest = Digest([]byte(aggregate))
	alpha := fixtureHookSatisfaction("PreToolUse", hookSatisfactionValueA,
		HookSourceEvidence{Resource: "alpha", SourceDigest: hookSatisfactionFPAlpha})
	satisfactions := []HookSatisfaction{alpha}
	stateFile, err := st.Prepare(mutation, &satisfactions)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	commitPrepared(t, stateDir, aggregateAssertion(t, f.absFile), stateFile)

	beta := fixtureHookSatisfaction("PreToolUse", hookSatisfactionValueA,
		HookSourceEvidence{Resource: "beta", SourceDigest: hookSatisfactionFPBeta})
	replacement := []HookSatisfaction{beta}
	mutation.ExpectPriorGeneration = 1
	nextFile, err := st.Prepare(mutation, &replacement)
	if err != nil {
		t.Fatalf("replace Prepare: %v", err)
	}
	if nextFile.Path == "" {
		t.Fatal("satisfaction replacement produced no desired file")
	}
	commitPrepared(t, stateDir, aggregateAssertion(t, f.absFile), nextFile)
	proof, err := st.Proof(f.rel)
	if err != nil {
		t.Fatalf("Proof after replace: %v", err)
	}
	if records := proof.HookSatisfactions(); len(records) != 1 || records[0].ID() != beta.ID() {
		t.Fatalf("replaced satisfactions = %+v", records)
	}

	empty := []HookSatisfaction{}
	mutation.ExpectPriorGeneration = 2
	cleared, err := st.Prepare(mutation, &empty)
	if err != nil {
		t.Fatalf("clear Prepare: %v", err)
	}
	if cleared.Path == "" {
		t.Fatal("explicit empty satisfaction set produced no desired file")
	}
	commitPrepared(t, stateDir, aggregateAssertion(t, f.absFile), cleared)
	proof, err = st.Proof(f.rel)
	if err != nil {
		t.Fatalf("Proof after clear: %v", err)
	}
	if len(proof.HookSatisfactions()) != 0 {
		t.Fatalf("cleared satisfactions = %+v", proof.HookSatisfactions())
	}

	restored := []HookSatisfaction{beta}
	mutation.ExpectPriorGeneration = 3
	stateFile, err = st.Prepare(mutation, &restored)
	if err != nil {
		t.Fatalf("reseed Prepare: %v", err)
	}
	commitPrepared(t, stateDir, aggregateAssertion(t, f.absFile), stateFile)
	owned := mcpEntry("docs", "docs-server", `{"command":"echo docs"}`)
	applyMutation := prepareSatisfactionMutation(f, []byte(aggregate), []Entry{owned})
	applyMutation.ExpectPriorGeneration = 4
	applyMutation.ExpectPriorDigest = mutation.ExpectPriorDigest
	mustApply(t, st, applyMutation)
	proof, err = st.Proof(f.rel)
	if err != nil {
		t.Fatalf("Proof after Apply: %v", err)
	}
	if records := proof.HookSatisfactions(); len(records) != 1 || records[0].ID() != beta.ID() {
		t.Fatalf("Apply overwrote satisfaction evidence: %+v", records)
	}
}

func TestHookMetadataPrepareRefusals(t *testing.T) {
	aggregate := prepareAggregateWith(hookSatisfactionValueA)
	satisfaction := fixtureHookSatisfaction("PreToolUse", hookSatisfactionValueA,
		HookSourceEvidence{Resource: "alpha", SourceDigest: hookSatisfactionFPAlpha})

	t.Run("stale_aggregate_digest", func(t *testing.T) {
		f, st, _ := prepareTxnFixture(t)
		f.writeAggregate(aggregate)
		mutation := prepareSatisfactionMutation(f, []byte(aggregate), nil)
		mutation.ExpectPriorDigest = Digest([]byte("something else"))
		satisfactions := []HookSatisfaction{satisfaction}
		before, info := fileSnapshot(t, f.absFile)
		_, err := st.Prepare(mutation, &satisfactions)
		assertPrepareRefusedClean(t, f, st, err, ErrGenerationConflict, false)
		assertOwnershipFileUnchanged(t, f.absFile, before, info)
	})
	t.Run("stale_generation", func(t *testing.T) {
		f, st, stateDir := prepareTxnFixture(t)
		f.writeAggregate(aggregate)
		mutation := prepareSatisfactionMutation(f, []byte(aggregate), nil)
		mutation.ExpectPriorDigest = Digest([]byte(aggregate))
		satisfactions := []HookSatisfaction{satisfaction}
		stateFile, err := st.Prepare(mutation, &satisfactions)
		if err != nil {
			t.Fatalf("seed Prepare: %v", err)
		}
		commitPrepared(t, stateDir, aggregateAssertion(t, f.absFile), stateFile)
		before, info := fileSnapshot(t, st.StateFile())
		mutation.ExpectPriorGeneration = 7
		_, err = st.Prepare(mutation, &satisfactions)
		assertPrepareRefusedClean(t, f, st, err, ErrGenerationConflict, true)
		assertOwnershipFileUnchanged(t, st.StateFile(), before, info)
	})
	t.Run("corrupt_record", func(t *testing.T) {
		f, st, stateDir := prepareTxnFixture(t)
		f.writeAggregate(aggregate)
		mutation := prepareSatisfactionMutation(f, []byte(aggregate), nil)
		mutation.ExpectPriorDigest = Digest([]byte(aggregate))
		satisfactions := []HookSatisfaction{satisfaction}
		stateFile, err := st.Prepare(mutation, &satisfactions)
		if err != nil {
			t.Fatalf("seed Prepare: %v", err)
		}
		commitPrepared(t, stateDir, aggregateAssertion(t, f.absFile), stateFile)
		if err := os.WriteFile(st.StateFile(), []byte("{not json"), 0o600); err != nil {
			t.Fatal(err)
		}
		mutation.ExpectPriorGeneration = 1
		before, info := fileSnapshot(t, st.StateFile())
		_, err = st.Prepare(mutation, &satisfactions)
		assertPrepareRefusedClean(t, f, st, err, ErrStateCorrupt, true)
		assertOwnershipFileUnchanged(t, st.StateFile(), before, info)
	})
	t.Run("rel_does_not_name_abs_file", func(t *testing.T) {
		f, st, _ := prepareTxnFixture(t)
		f.writeAggregate(aggregate)
		mutation := prepareSatisfactionMutation(f, []byte(aggregate), nil)
		mutation.Rel = "other.json"
		mutation.ExpectPriorDigest = Digest([]byte(aggregate))
		satisfactions := []HookSatisfaction{satisfaction}
		before, info := fileSnapshot(t, f.absFile)
		_, err := st.Prepare(mutation, &satisfactions)
		if err == nil {
			t.Fatal("Prepare accepted a Rel that does not name AbsFile")
		}
		if fileExists(t, st.StateFile()) {
			t.Fatal("refused Prepare wrote the state record")
		}
		assertOwnershipFileUnchanged(t, f.absFile, before, info)
	})
	t.Run("malformed_satisfaction", func(t *testing.T) {
		f, st, _ := prepareTxnFixture(t)
		f.writeAggregate(aggregate)
		mutation := prepareSatisfactionMutation(f, []byte(aggregate), nil)
		mutation.ExpectPriorDigest = Digest([]byte(aggregate))
		bad := satisfaction
		bad.Sources = []HookSourceEvidence{{Resource: fakeCommand, SourceDigest: hookSatisfactionFPAlpha}}
		satisfactions := []HookSatisfaction{bad}
		before, info := fileSnapshot(t, f.absFile)
		_, err := st.Prepare(mutation, &satisfactions)
		assertPrepareRefusedClean(t, f, st, err, ErrUnsafeStateValue, false)
		assertOwnershipFileUnchanged(t, f.absFile, before, info)
	})
	t.Run("duplicate_identity", func(t *testing.T) {
		f, st, _ := prepareTxnFixture(t)
		f.writeAggregate(aggregate)
		mutation := prepareSatisfactionMutation(f, []byte(aggregate), nil)
		mutation.ExpectPriorDigest = Digest([]byte(aggregate))
		satisfactions := []HookSatisfaction{satisfaction, satisfaction}
		_, err := st.Prepare(mutation, &satisfactions)
		assertPrepareRefusedClean(t, f, st, err, ErrUnsafeStateValue, false)
	})
	t.Run("duplicate_source_pair", func(t *testing.T) {
		f, st, _ := prepareTxnFixture(t)
		f.writeAggregate(aggregate)
		mutation := prepareSatisfactionMutation(f, []byte(aggregate), nil)
		mutation.ExpectPriorDigest = Digest([]byte(aggregate))
		dup := satisfaction
		pair := satisfaction.Sources[0]
		dup.Sources = []HookSourceEvidence{pair, pair}
		satisfactions := []HookSatisfaction{dup}
		_, err := st.Prepare(mutation, &satisfactions)
		assertPrepareRefusedClean(t, f, st, err, ErrUnsafeStateValue, false)
	})
	t.Run("satisfaction_overlaps_owned_entry", func(t *testing.T) {
		f, st, _ := prepareTxnFixture(t)
		f.writeAggregate(aggregate)
		overlap := hookEntry("PreToolUse", "alpha", hookSatisfactionValueA)
		mutation := prepareSatisfactionMutation(f, []byte(aggregate), []Entry{overlap})
		mutation.ExpectPriorDigest = Digest([]byte(aggregate))
		satisfactions := []HookSatisfaction{satisfaction}
		_, err := st.Prepare(mutation, &satisfactions)
		assertPrepareRefusedClean(t, f, st, err, ErrUnsafeStateValue, false)
	})
	t.Run("aggregate_symlink", func(t *testing.T) {
		f, st, _ := prepareTxnFixture(t)
		target := filepath.Join(t.TempDir(), "target.json")
		if err := os.WriteFile(target, []byte(aggregate), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, f.absFile); err != nil {
			t.Fatal(err)
		}
		mutation := prepareSatisfactionMutation(f, []byte(aggregate), nil)
		mutation.ExpectPriorDigest = Digest([]byte(aggregate))
		satisfactions := []HookSatisfaction{satisfaction}
		_, err := st.Prepare(mutation, &satisfactions)
		assertPrepareRefusedClean(t, f, st, err, ErrUnsafeAggregateFile, false)
	})
	t.Run("aggregate_hardlink", func(t *testing.T) {
		f, st, _ := prepareTxnFixture(t)
		f.writeAggregate(aggregate)
		twin := filepath.Join(t.TempDir(), "twin.json")
		if err := os.Link(f.absFile, twin); err != nil {
			t.Fatal(err)
		}
		mutation := prepareSatisfactionMutation(f, []byte(aggregate), nil)
		mutation.ExpectPriorDigest = Digest([]byte(aggregate))
		satisfactions := []HookSatisfaction{satisfaction}
		_, err := st.Prepare(mutation, &satisfactions)
		assertPrepareRefusedClean(t, f, st, err, ErrUnsafeAggregateFile, false)
	})
}

func TestHookMetadataPrepareCommitRefusesExternalStateChange(t *testing.T) {
	f, st, stateDir := prepareTxnFixture(t)
	aggregate := prepareAggregateWith(hookSatisfactionValueA)
	f.writeAggregate(aggregate)
	satisfaction := fixtureHookSatisfaction("PreToolUse", hookSatisfactionValueA,
		HookSourceEvidence{Resource: "alpha", SourceDigest: hookSatisfactionFPAlpha})
	mutation := prepareSatisfactionMutation(f, []byte(aggregate), nil)
	mutation.ExpectPriorDigest = Digest([]byte(aggregate))
	satisfactions := []HookSatisfaction{satisfaction}
	stateFile, err := st.Prepare(mutation, &satisfactions)
	if err != nil {
		t.Fatalf("seed Prepare: %v", err)
	}
	commitPrepared(t, stateDir, aggregateAssertion(t, f.absFile), stateFile)

	mutation.ExpectPriorGeneration = 1
	change := fixtureHookSatisfaction("PreToolUse", hookSatisfactionValueA,
		HookSourceEvidence{Resource: "beta", SourceDigest: hookSatisfactionFPBeta})
	next := []HookSatisfaction{change}
	nextFile, err := st.Prepare(mutation, &next)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if nextFile.Path == "" {
		t.Fatal("satisfaction change produced no desired file")
	}
	external := []byte("external writer record\n")
	if err := os.WriteFile(st.StateFile(), external, 0o600); err != nil {
		t.Fatal(err)
	}
	aggregateBefore, aggregateInfo := fileSnapshot(t, f.absFile)
	_, commitErr := dependencytxn.Commit(context.Background(), stateDir,
		[]dependencytxn.DesiredFile{aggregateAssertion(t, f.absFile), nextFile})
	if commitErr == nil || !errors.Is(commitErr, dependencytxn.ErrGenerationConflict) {
		t.Fatalf("Commit after external state change = %v, want ErrGenerationConflict", commitErr)
	}
	data, err := os.ReadFile(st.StateFile())
	if err != nil || string(data) != string(external) {
		t.Fatalf("externally written record changed: %q, %v", data, err)
	}
	assertOwnershipFileUnchanged(t, f.absFile, aggregateBefore, aggregateInfo)
}

func TestHookMetadataPrepareDoesNotAliasCallerSlices(t *testing.T) {
	f, st, stateDir := prepareTxnFixture(t)
	aggregate := prepareAggregateWith(hookSatisfactionValueA)
	f.writeAggregate(aggregate)
	mutation := prepareSatisfactionMutation(f, []byte(aggregate), nil)
	mutation.ExpectPriorDigest = Digest([]byte(aggregate))
	satisfaction := fixtureHookSatisfaction("PreToolUse", hookSatisfactionValueA,
		HookSourceEvidence{Resource: "alpha", SourceDigest: hookSatisfactionFPAlpha})
	satisfactions := []HookSatisfaction{satisfaction}
	stateFile, err := st.Prepare(mutation, &satisfactions)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	satisfactions[0].Sources[0].Resource = fakeCommand
	satisfactions[0].Sources[0].SourceDigest = fakeCommand
	satisfactions[0].IdentityDigest = fakeCommand
	commitPrepared(t, stateDir, aggregateAssertion(t, f.absFile), stateFile)
	proof, err := st.Proof(f.rel)
	if err != nil {
		t.Fatalf("Proof: %v", err)
	}
	records := proof.HookSatisfactions()
	if len(records) != 1 || records[0].ID() != satisfaction.ID() {
		t.Fatalf("mutated input reached the committed record: %+v", records)
	}
	if !reflect.DeepEqual(records[0].Sources, []HookSourceEvidence{
		{Resource: "alpha", SourceDigest: hookSatisfactionFPAlpha},
	}) {
		t.Fatalf("mutated source slice reached the committed record: %+v", records[0].Sources)
	}
}
