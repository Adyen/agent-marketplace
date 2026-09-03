package ownership

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

const (
	priorContent = "{\n  // handwritten\n  \"mcpServers\": {\n    \"handwritten\": {\"command\": \"keep\"}\n  }\n}\n"
	nextContent  = "{\n  // handwritten\n  \"mcpServers\": {\n    \"handwritten\": {\"command\": \"keep\"},\n    \"generated\": {\"command\": \"new\"}\n  }\n}\n"
)

// TestCrashInjectionAtEveryBoundary is the headline test for D9. It drives one
// generation to each crash point in production code (no mocks: the injector is
// consulted by Apply itself), asserts the exact on-disk state a real crash would
// leave, then runs Recover and asserts the exact recovered state.
//
// The plan requires that "Crash-injection tests cover every boundary between
// backup, journal fsync, aggregate rename, state commit, and journal cleanup"
// and that "On startup, incomplete generations are replayed or rolled back
// according to the recorded phase".
func TestCrashInjectionAtEveryBoundary(t *testing.T) {
	tests := []struct {
		name  string
		point CrashPoint

		// State immediately after the crash.
		wantFileAfterCrash    string
		wantBackupAfterCrash  bool
		wantJournalAfterCrash Phase // "" means no journal
		wantStateAfterCrash   uint64

		// State after Recover.
		wantActions           []RecoveryAction
		wantFileAfterRecover  string
		wantStateAfterRecover uint64
	}{
		{
			name:                  "before backup",
			point:                 CrashBeforeBackup,
			wantFileAfterCrash:    priorContent,
			wantJournalAfterCrash: "",
			wantActions:           nil,
			wantFileAfterRecover:  priorContent,
		},
		{
			name:                  "after mandatory safety backup",
			point:                 CrashAfterBackup,
			wantFileAfterCrash:    priorContent,
			wantBackupAfterCrash:  true,
			wantJournalAfterCrash: "",
			wantActions:           nil,
			wantFileAfterRecover:  priorContent,
		},
		{
			name:                  "after journal fsync in phase prepared",
			point:                 CrashAfterJournalPrepared,
			wantFileAfterCrash:    priorContent,
			wantBackupAfterCrash:  true,
			wantJournalAfterCrash: PhasePrepared,
			wantActions:           []RecoveryAction{ActionRolledBack},
			wantFileAfterRecover:  priorContent,
		},
		{
			name:                  "after staged write",
			point:                 CrashAfterStagedWrite,
			wantFileAfterCrash:    priorContent,
			wantBackupAfterCrash:  true,
			wantJournalAfterCrash: PhasePrepared,
			wantActions:           []RecoveryAction{ActionRolledBack},
			wantFileAfterRecover:  priorContent,
		},
		{
			name:                  "after aggregate rename but before the phase update",
			point:                 CrashAfterAggregateRename,
			wantFileAfterCrash:    nextContent,
			wantBackupAfterCrash:  true,
			wantJournalAfterCrash: PhasePrepared,
			wantActions:           []RecoveryAction{ActionRolledForward},
			wantFileAfterRecover:  nextContent,
			wantStateAfterRecover: 1,
		},
		{
			name:                  "after journal fsync in phase aggregate-renamed",
			point:                 CrashAfterJournalRenamed,
			wantFileAfterCrash:    nextContent,
			wantBackupAfterCrash:  true,
			wantJournalAfterCrash: PhaseAggregateRenamed,
			wantActions:           []RecoveryAction{ActionRolledForward},
			wantFileAfterRecover:  nextContent,
			wantStateAfterRecover: 1,
		},
		{
			name:                  "after state commit but before journal cleanup",
			point:                 CrashAfterStateCommit,
			wantFileAfterCrash:    nextContent,
			wantBackupAfterCrash:  true,
			wantJournalAfterCrash: PhaseAggregateRenamed,
			wantStateAfterCrash:   1,
			wantActions:           []RecoveryAction{ActionAlreadyCommitted},
			wantFileAfterRecover:  nextContent,
			wantStateAfterRecover: 1,
		},
		{
			name:                  "after journal cleanup",
			point:                 CrashAfterJournalCleanup,
			wantFileAfterCrash:    nextContent,
			wantBackupAfterCrash:  true,
			wantJournalAfterCrash: "",
			wantStateAfterCrash:   1,
			wantActions:           nil,
			wantFileAfterRecover:  nextContent,
			wantStateAfterRecover: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			f.writeAggregate(priorContent)

			crashing := f.store(WithCrasher(CrashAt(tt.point)))
			proof, _ := crashing.Proof(f.rel)
			_, err := crashing.Apply(Mutation{
				Rel:                   f.rel,
				AbsFile:               f.absFile,
				NextBytes:             []byte(nextContent),
				Entries:               []Entry{mcpEntry("generated", "generated-server", `{"command":"new"}`)},
				Reason:                ReasonSync,
				ExpectPriorGeneration: proof.Generation(),
				ExpectPriorDigest:     Digest([]byte(priorContent)),
			})
			if !errors.Is(err, ErrInjectedCrash) {
				t.Fatalf("Apply error = %v, want an injected crash at %s", err, tt.point)
			}

			// --- exact on-disk state a real crash would leave ---
			got, _ := f.readAggregate()
			if got != tt.wantFileAfterCrash {
				t.Fatalf("aggregate after crash = %q, want %q", got, tt.wantFileAfterCrash)
			}
			assertGeneration(t, crashing, tt.wantStateAfterCrash)
			assertJournalPhase(t, crashing, tt.wantJournalAfterCrash)
			backups, err := crashing.SafetyBackups(f.rel)
			if err != nil {
				t.Fatalf("SafetyBackups: %v", err)
			}
			if got, want := len(backups) > 0, tt.wantBackupAfterCrash; got != want {
				t.Fatalf("safety backup present = %v, want %v", got, want)
			}
			if tt.wantBackupAfterCrash {
				// The retained bytes are the RAW pre-mutation bytes, not a
				// re-serialization: comments and formatting are byte-identical.
				raw, err := backups[len(backups)-1].RawBytes()
				if err != nil {
					t.Fatalf("RawBytes: %v", err)
				}
				if string(raw) != priorContent {
					t.Fatalf("safety backup bytes = %q, want the exact pre-mutation bytes %q", raw, priorContent)
				}
			}

			// --- recovery, from a fresh store as a new process would ---
			recovering := f.store()
			outcomes, err := recovering.Recover()
			if err != nil {
				t.Fatalf("Recover: %v", err)
			}
			var actions []RecoveryAction
			for _, o := range outcomes {
				actions = append(actions, o.Action)
				if o.File != f.rel {
					t.Fatalf("outcome file = %q, want %q", o.File, f.rel)
				}
				if o.Generation != 1 {
					t.Fatalf("outcome generation = %d, want 1", o.Generation)
				}
			}
			if len(actions) != len(tt.wantActions) {
				t.Fatalf("recovery actions = %v, want %v", actions, tt.wantActions)
			}
			for i := range actions {
				if actions[i] != tt.wantActions[i] {
					t.Fatalf("recovery action[%d] = %q, want %q", i, actions[i], tt.wantActions[i])
				}
			}

			got, _ = f.readAggregate()
			if got != tt.wantFileAfterRecover {
				t.Fatalf("aggregate after recovery = %q, want %q", got, tt.wantFileAfterRecover)
			}
			assertGeneration(t, recovering, tt.wantStateAfterRecover)
			assertJournalPhase(t, recovering, "")
			assertNoStagedFiles(t, f)

			// Recovery is idempotent: a second run must be a no-op.
			again, err := recovering.Recover()
			if err != nil {
				t.Fatalf("second Recover: %v", err)
			}
			if len(again) != 0 {
				t.Fatalf("second Recover returned %v, want no outcomes", again)
			}
		})
	}
}

// A crash before the aggregate is published, followed by a user edit, must
// abandon the staged generation and leave the user's bytes alone.
func TestApplyRejectsConcurrentModeChangeWithExpectedPriorMode(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	if err := os.Chmod(f.absFile, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := f.store().Apply(Mutation{
		Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent),
		Entries: []Entry{mcpEntry("generated", "generated-server", `{"command":"new"}`)}, Reason: ReasonSync,
		ExpectPriorDigest: Digest([]byte(priorContent)), ExpectPriorMode: 0o644,
	})
	if !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("Apply error = %v, want ErrGenerationConflict", err)
	}
	if got, present := f.readAggregate(); !present || got != priorContent {
		t.Fatalf("aggregate changed on mode conflict: %q (present=%v)", got, present)
	}
	if info, statErr := os.Stat(f.absFile); statErr != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("aggregate mode after conflict = %v/%v, want 0600", info, statErr)
	}
	assertJournalPhase(t, f.store(), "")
	if backups, backupErr := f.store().SafetyBackups(f.rel); backupErr != nil || len(backups) != 0 {
		t.Fatalf("safety backups after mode conflict = %d/%v, want none", len(backups), backupErr)
	}
	assertNoStagedFiles(t, f)
}

func TestRecoveryAbandonsAStagedGenerationWhenTheAggregateChangedUnderneath(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)

	crashing := f.store(WithCrasher(CrashAt(CrashAfterStagedWrite)))
	_, err := crashing.Apply(Mutation{
		Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent),
		Entries: []Entry{mcpEntry("generated", "generated-server", `{"command":"new"}`)},
		Reason:  ReasonSync,
	})
	if !errors.Is(err, ErrInjectedCrash) {
		t.Fatalf("Apply error = %v, want an injected crash", err)
	}

	const userEdit = "{\n  // the user got there first\n  \"mcpServers\": {}\n}\n"
	f.writeAggregate(userEdit)

	recovering := f.store()
	outcomes, err := recovering.Recover()
	if !errors.Is(err, ErrForeignChange) {
		t.Fatalf("Recover error = %v, want ErrForeignChange", err)
	}
	coded, ok := diag.AsCodedError(err)
	if !ok {
		t.Fatalf("error %T is not a *diag.CodedError", err)
	}
	if coded.Code != diag.CodeOwnershipForeignChange {
		t.Fatalf("code = %q, want %q", coded.Code, diag.CodeOwnershipForeignChange)
	}
	if coded.Severity != diag.SeverityError {
		t.Fatalf("severity = %v, want error", coded.Severity)
	}
	if coded.Source.File != f.rel {
		t.Fatalf("source file = %q, want %q", coded.Source.File, f.rel)
	}
	if len(outcomes) != 1 || outcomes[0].Action != ActionAbandoned {
		t.Fatalf("outcomes = %+v, want one abandoned outcome", outcomes)
	}

	if got, _ := f.readAggregate(); got != userEdit {
		t.Fatalf("aggregate = %q, want the user's edit preserved %q", got, userEdit)
	}
	assertGeneration(t, recovering, 0)
	assertJournalPhase(t, recovering, "")
	assertNoStagedFiles(t, f)
}

// A first-ever write has no prior file. Rolling back must leave NO file behind:
// PriorAbsent is recorded, so recovery knows "absent" is the correct rollback
// target rather than "empty".
func TestRollbackOfAFirstWriteLeavesNoFile(t *testing.T) {
	f := newFixture(t)
	crashing := f.store(WithCrasher(CrashAt(CrashAfterStagedWrite)))
	_, err := crashing.Apply(Mutation{
		Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent),
		Entries: []Entry{mcpEntry("generated", "generated-server", `{"command":"new"}`)},
		Reason:  ReasonSync,
	})
	if !errors.Is(err, ErrInjectedCrash) {
		t.Fatalf("Apply error = %v, want an injected crash", err)
	}
	if _, exists := f.readAggregate(); exists {
		t.Fatal("aggregate exists after a crash before publication")
	}

	recovering := f.store()
	outcomes, err := recovering.Recover()
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if len(outcomes) != 1 || outcomes[0].Action != ActionRolledBack {
		t.Fatalf("outcomes = %+v, want one rolled-back outcome", outcomes)
	}
	if _, exists := f.readAggregate(); exists {
		t.Fatal("rollback of a first write created a file")
	}
	backups, err := recovering.SafetyBackups(f.rel)
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 {
		t.Fatalf("safety backups = %d, want 1 even for an absent prior file", len(backups))
	}
	if !backups[0].Manifest.PriorAbsent {
		t.Fatal("manifest PriorAbsent = false for a first write")
	}
	if backups[0].Manifest.PriorSize != 0 {
		t.Fatalf("manifest PriorSize = %d, want 0", backups[0].Manifest.PriorSize)
	}
}

// A directory fsync failure after the journal rename returns an error but keeps
// the renamed journal so recovery can finish the published generation.
func TestApplyRetainsJournalAndStagedFileAfterStagedDirectoryFsyncFailure(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	injected := errors.New("injected aggregate directory fsync failure")
	failed := false
	st := f.store(WithFsyncDir(func(dir string) error {
		if dir == f.targetRoot && !failed {
			failed = true
			return injected
		}
		return fsyncDir(dir)
	}))

	_, err := st.Apply(Mutation{
		Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent),
		Entries: []Entry{mcpEntry("generated", "generated-server", `{"command":"new"}`)}, Reason: ReasonSync,
		ExpectPriorDigest: Digest([]byte(priorContent)),
	})
	if !errors.Is(err, injected) {
		t.Fatalf("Apply error = %v, want injected staged-directory fsync failure", err)
	}
	if got, _ := f.readAggregate(); got != priorContent {
		t.Fatalf("aggregate = %q, want pre-mutation bytes", got)
	}
	journal, err := st.ReadJournal()
	if err != nil || journal == nil || journal.Phase != PhasePrepared {
		t.Fatalf("journal = %#v, %v, want retained prepared evidence", journal, err)
	}
	if !fileExists(t, journal.StagedPath) {
		t.Fatal("staged recovery file disappeared after its directory fsync failed")
	}

	st.syncDir = fsyncDir
	outcomes, err := st.Recover()
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if len(outcomes) != 1 || outcomes[0].Action != ActionRolledBack {
		t.Fatalf("recovery outcomes = %#v, want one rollback", outcomes)
	}
	assertJournalPhase(t, st, "")
	assertNoStagedFiles(t, f)
}

func TestApplyRetainsJournalAfterJournalRenameFsyncFailure(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	injected := errors.New("injected journal directory fsync failure")
	st := f.store()
	recordSyncs := 0
	st.syncDir = func(dir string) error {
		if dir == st.recordDir() {
			recordSyncs++
			if recordSyncs == 2 {
				return injected
			}
		}
		return fsyncDir(dir)
	}

	_, err := st.Apply(Mutation{
		Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent),
		Entries: []Entry{mcpEntry("generated", "generated-server", `{"command":"new"}`)}, Reason: ReasonSync,
		ExpectPriorDigest: Digest([]byte(priorContent)),
	})
	if !errors.Is(err, injected) {
		t.Fatalf("Apply error = %v, want injected journal fsync failure", err)
	}
	if got, _ := f.readAggregate(); got != nextContent {
		t.Fatalf("aggregate = %q, want published bytes", got)
	}
	journal, err := st.ReadJournal()
	if err != nil {
		t.Fatal(err)
	}
	if journal == nil || journal.Phase != PhaseAggregateRenamed {
		t.Fatalf("journal = %#v, want retained aggregate-renamed evidence", journal)
	}
	assertGeneration(t, st, 0)

	st.syncDir = func(dir string) error {
		if dir == st.recordDir() {
			recordSyncs++
		}
		return fsyncDir(dir)
	}
	outcomes, err := st.Recover()
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if len(outcomes) != 1 || outcomes[0].Action != ActionRolledForward {
		t.Fatalf("recovery outcomes = %#v, want one roll-forward", outcomes)
	}
	assertGeneration(t, st, 1)
	assertJournalPhase(t, st, "")
	if recordSyncs < 4 {
		t.Fatalf("record directory syncs = %d, want journal and recovery syncs", recordSyncs)
	}
}

func TestRollForwardRejectsUserEditAfterPublication(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	crashing := f.store(WithCrasher(CrashAt(CrashAfterJournalRenamed)))
	proof, _ := crashing.Proof(f.rel)
	if _, err := crashing.Apply(Mutation{
		Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent),
		Entries: []Entry{mcpEntry("generated", "generated-server", `{"command":"new"}`)}, Reason: ReasonSync,
		ExpectPriorGeneration: proof.Generation(), ExpectPriorDigest: Digest([]byte(priorContent)),
	}); !errors.Is(err, ErrInjectedCrash) {
		t.Fatalf("Apply error = %v, want an injected crash", err)
	}
	const userEdit = "{\n  // edited after publication\n  \"mcpServers\": {}\n}\n"
	f.writeAggregate(userEdit)

	recovering := f.store()
	_, err := recovering.Recover()
	if !errors.Is(err, ErrForeignChange) {
		t.Fatalf("Recover error = %v, want ErrForeignChange", err)
	}
	coded, ok := diag.AsCodedError(err)
	if !ok || coded.Code != diag.CodeOwnershipForeignChange {
		t.Fatalf("error = %v, want code %q", err, diag.CodeOwnershipForeignChange)
	}
	if got, _ := f.readAggregate(); got != userEdit {
		t.Fatalf("aggregate = %q, want user edit preserved", got)
	}
	assertGeneration(t, recovering, 0)
	journal, readErr := recovering.ReadJournal()
	if readErr != nil || journal == nil {
		t.Fatalf("journal = %#v, %v, want retained foreign-change evidence", journal, readErr)
	}
}

// A crash after publication rolls forward, and the recovered generation must be
// the SAME generation the interrupted run intended, entries included.
func TestRollForwardCommitsTheRecordedGeneration(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	entry := mcpEntry("generated", "generated-server", `{"command":"new"}`)

	crashing := f.store(WithCrasher(CrashAt(CrashAfterJournalRenamed)))
	proof, _ := crashing.Proof(f.rel)
	if _, err := crashing.Apply(Mutation{
		Rel:                   f.rel,
		AbsFile:               f.absFile,
		NextBytes:             []byte(nextContent),
		Entries:               []Entry{entry},
		Reason:                ReasonSync,
		ExpectPriorGeneration: proof.Generation(),
		ExpectPriorDigest:     Digest([]byte(priorContent)),
	}); !errors.Is(err, ErrInjectedCrash) {
		t.Fatalf("Apply error = %v, want an injected crash", err)
	}

	recovering := f.store()
	if _, err := recovering.Recover(); err != nil {
		t.Fatalf("Recover: %v", err)
	}
	proof, err := recovering.Proof(f.rel)
	if err != nil {
		t.Fatalf("Proof: %v", err)
	}
	if proof.OwnedCount() != 1 {
		t.Fatalf("owned entries = %d, want 1", proof.OwnedCount())
	}
	if status := proof.Classify(entry.ID(), entry.ValueDigest); status != StatusOwnedClean {
		t.Fatalf("status = %v, want owned-clean", status)
	}
	if proof.ContentDigest() != Digest([]byte(nextContent)) {
		t.Fatalf("content digest = %q, want the digest of the published bytes", proof.ContentDigest())
	}
}

// A journal belonging to a different canonical root is refused rather than
// replayed: a state-key collision must never let one root commit another's
// generation.
func TestRecoveryRefusesAJournalFromAnotherRoot(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	st := f.store(WithCrasher(CrashAt(CrashAfterJournalRenamed)))
	proof, _ := st.Proof(f.rel)
	if _, err := st.Apply(Mutation{
		Rel:                   f.rel,
		AbsFile:               f.absFile,
		NextBytes:             []byte(nextContent),
		Entries:               []Entry{mcpEntry("generated", "generated-server", `{"command":"new"}`)},
		Reason:                ReasonSync,
		ExpectPriorGeneration: proof.Generation(),
		ExpectPriorDigest:     Digest([]byte(priorContent)),
	}); !errors.Is(err, ErrInjectedCrash) {
		t.Fatalf("Apply error = %v, want an injected crash", err)
	}

	// Rewrite the journal's canonical root, simulating a collision.
	data, err := os.ReadFile(st.JournalFile())
	if err != nil {
		t.Fatal(err)
	}
	tampered := []byte(replaceOnce(string(data), st.CanonicalRoot(), "/some/other/root"))
	if err := os.WriteFile(st.JournalFile(), tampered, 0o600); err != nil {
		t.Fatal(err)
	}

	fresh := f.store()
	_, err = fresh.Recover()
	if !errors.Is(err, ErrStateRootMismatch) {
		t.Fatalf("Recover error = %v, want ErrStateRootMismatch", err)
	}
	coded, ok := diag.AsCodedError(err)
	if !ok || coded.Code != diag.CodeOwnershipStateRootMismatch {
		t.Fatalf("error = %v, want code %q", err, diag.CodeOwnershipStateRootMismatch)
	}
	// The journal is left in place: it is the only remaining description of the
	// interrupted generation.
	if !fileExists(t, fresh.JournalFile()) {
		t.Fatal("a foreign journal was deleted; evidence must be retained")
	}
}

func TestRecoveryRefusesACorruptJournal(t *testing.T) {
	f := newFixture(t)
	st := f.store()
	if err := os.MkdirAll(filepath.Dir(st.JournalFile()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.JournalFile(), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := st.Recover()
	coded, ok := diag.AsCodedError(err)
	if !ok || coded.Code != diag.CodeOwnershipStateCorrupt {
		t.Fatalf("Recover error = %v, want code %q", err, diag.CodeOwnershipStateCorrupt)
	}
	if coded.Severity != diag.SeverityError {
		t.Fatalf("severity = %v, want error", coded.Severity)
	}
}

func TestRecoveryRefusesCorruptOwnershipStateWithoutMutation(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	st := f.store(WithCrasher(CrashAt(CrashAfterJournalRenamed)))
	_, err := st.Apply(Mutation{
		Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent),
		Entries: []Entry{mcpEntry("generated", "g", `{"command":"new"}`)}, Reason: ReasonSync,
		ExpectPriorGeneration: 0, ExpectPriorDigest: Digest([]byte(priorContent)),
	})
	if !errors.Is(err, ErrInjectedCrash) {
		t.Fatalf("Apply error = %v, want an injected crash", err)
	}
	journalBefore, err := os.ReadFile(st.JournalFile())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.StateFile(), []byte("{corrupt state"), 0o600); err != nil {
		t.Fatal(err)
	}
	stateBefore, err := os.ReadFile(st.StateFile())
	if err != nil {
		t.Fatal(err)
	}

	_, err = f.store().Recover()
	if !errors.Is(err, ErrStateCorrupt) {
		t.Fatalf("Recover error = %v, want ErrStateCorrupt", err)
	}
	if got, _ := f.readAggregate(); got != nextContent {
		t.Fatalf("aggregate = %q, want published bytes unchanged", got)
	}
	if got, err := os.ReadFile(st.JournalFile()); err != nil || string(got) != string(journalBefore) {
		t.Fatalf("journal changed during refused recovery: %q, %v", got, err)
	}
	if got, err := os.ReadFile(st.StateFile()); err != nil || string(got) != string(stateBefore) {
		t.Fatalf("corrupt state changed during refused recovery: %q, %v", got, err)
	}
}

func TestRecoveryRejectsMalformedJournalWithoutMutation(t *testing.T) {
	cases := []struct {
		name    string
		prepare CrashPoint
		tamper  func(*Journal, *fixture)
	}{
		{name: "file escapes root", tamper: func(j *Journal, _ *fixture) { j.File = "../outside.json" }},
		{name: "absolute aggregate path", tamper: func(j *Journal, f *fixture) { j.AbsFile = filepath.Join(filepath.Dir(f.targetRoot), "outside.json") }},
		{name: "staged path escapes aggregate directory", prepare: CrashAfterStagedWrite, tamper: func(j *Journal, f *fixture) {
			j.StagedPath = filepath.Join(filepath.Dir(f.targetRoot), "outside-staged")
		}},
		{name: "backup path escapes safety area", tamper: func(j *Journal, f *fixture) {
			j.BackupPath = filepath.Join(filepath.Dir(f.targetRoot), "outside-backup.bak")
		}},
		{name: "prior digest malformed", tamper: func(j *Journal, _ *fixture) { j.PriorDigest = "not-a-digest" }},
		{name: "next digest malformed", tamper: func(j *Journal, _ *fixture) { j.NextDigest = "not-a-digest" }},
		{name: "reason malformed", tamper: func(j *Journal, _ *fixture) { j.Reason = Reason("future-reason") }},
		{name: "updated at malformed", tamper: func(j *Journal, _ *fixture) { j.UpdatedAt = "not-a-time" }},
		{name: "next record missing", tamper: func(j *Journal, _ *fixture) { j.NextRecord = nil }},
		{name: "next record file escapes root", tamper: func(j *Journal, _ *fixture) {
			j.NextRecord.Files["../outside.json"] = FileState{ContentDigest: j.NextDigest}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.writeAggregate(priorContent)
			prepare := tc.prepare
			if prepare == "" {
				prepare = CrashAfterJournalRenamed
			}
			st := f.store(WithCrasher(CrashAt(prepare)))
			if _, err := st.Apply(Mutation{
				Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent),
				Entries: []Entry{mcpEntry("generated", "g", `{"command":"new"}`)}, Reason: ReasonSync,
				ExpectPriorGeneration: 0, ExpectPriorDigest: Digest([]byte(priorContent)),
			}); !errors.Is(err, ErrInjectedCrash) {
				t.Fatalf("Apply error = %v, want an injected crash", err)
			}
			journal, err := st.ReadJournal()
			if err != nil || journal == nil {
				t.Fatalf("ReadJournal before tampering: %v, %#v", err, journal)
			}
			originalStaged := journal.StagedPath
			tc.tamper(journal, f)
			data, err := json.Marshal(journal)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(st.JournalFile(), data, 0o600); err != nil {
				t.Fatal(err)
			}
			journalBefore, err := os.ReadFile(st.JournalFile())
			if err != nil {
				t.Fatal(err)
			}
			_, recoverErr := f.store().Recover()
			if !errors.Is(recoverErr, ErrStateCorrupt) && !errors.Is(recoverErr, ErrStateRootMismatch) {
				t.Fatalf("Recover error = %v, want corrupt-journal refusal", recoverErr)
			}
			if got, _ := f.readAggregate(); got != priorContent && got != nextContent {
				t.Fatalf("aggregate changed during refused recovery: %q", got)
			}
			if got, err := os.ReadFile(st.JournalFile()); err != nil || string(got) != string(journalBefore) {
				t.Fatalf("journal changed during refused recovery: %q, %v", got, err)
			}
			if prepare == CrashAfterStagedWrite && !fileExists(t, originalStaged) {
				t.Fatal("valid staged file was removed while refusing malformed journal")
			}
		})
	}
}

func TestRecoveryRefusesAnUnknownJournalPhase(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	st := f.store(WithCrasher(CrashAt(CrashAfterJournalPrepared)))
	if _, err := st.Apply(Mutation{
		Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent),
		Entries: []Entry{mcpEntry("generated", "g", `{"command":"new"}`)}, Reason: ReasonSync,
	}); !errors.Is(err, ErrInjectedCrash) {
		t.Fatalf("Apply error = %v, want an injected crash", err)
	}
	data, err := os.ReadFile(st.JournalFile())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.JournalFile(), []byte(replaceOnce(string(data), `"phase": "prepared"`, `"phase": "from-the-future"`)), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = f.store().Recover()
	coded, ok := diag.AsCodedError(err)
	if !ok || coded.Code != diag.CodeOwnershipStateCorrupt {
		t.Fatalf("Recover error = %v, want code %q", err, diag.CodeOwnershipStateCorrupt)
	}
}

func TestApplyRefusesAFileOutsideTheCanonicalRoot(t *testing.T) {
	f := newFixture(t)
	outside := filepath.Join(filepath.Dir(f.targetRoot), "outside.json")
	_, err := f.store().Apply(Mutation{
		Rel: f.rel, AbsFile: outside, NextBytes: []byte("{}"), Reason: ReasonSync,
	})
	if err == nil {
		t.Fatal("Apply accepted a file outside the canonical target root")
	}
}

type stagedCollisionCrasher struct {
	store  *Store
	target string
	staged string
}

func (c *stagedCollisionCrasher) Crash(point CrashPoint) error {
	if point != CrashAfterJournalPrepared {
		return nil
	}
	journal, err := c.store.ReadJournal()
	if err != nil {
		return err
	}
	if journal == nil {
		return errors.New("ownership test: prepared journal is missing")
	}
	c.staged = journal.StagedPath
	return os.Symlink(c.target, c.staged)
}

func TestApplyPreservesPreexistingStagedSymlinkThroughRecovery(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	sentinel := filepath.Join(filepath.Dir(f.targetRoot), "sentinel")
	const original = "outside data\n"
	if err := os.WriteFile(sentinel, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	probe := sentinel + ".symlink-probe"
	if err := os.Symlink(sentinel, probe); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Remove(probe); err != nil {
		t.Fatal(err)
	}

	crasher := &stagedCollisionCrasher{target: sentinel}
	st := f.store(WithCrasher(crasher))
	crasher.store = st
	_, err := st.Apply(Mutation{
		Rel:                   f.rel,
		AbsFile:               f.absFile,
		NextBytes:             []byte(nextContent),
		Reason:                ReasonSync,
		ExpectPriorDigest:     Digest([]byte(priorContent)),
		ExpectPriorGeneration: 0,
	})
	if !errors.Is(err, os.ErrExist) {
		t.Fatalf("Apply error = %v, want staged-path collision", err)
	}

	assertCollisionUntouched := func() {
		t.Helper()
		gotTarget, readErr := os.Readlink(crasher.staged)
		if readErr != nil {
			t.Errorf("read staged symlink: %v", readErr)
			return
		}
		if gotTarget != sentinel {
			t.Errorf("staged symlink target = %q, want %q", gotTarget, sentinel)
		}
		got, readErr := os.ReadFile(sentinel)
		if readErr != nil {
			t.Errorf("read outside sentinel: %v", readErr)
			return
		}
		if string(got) != original {
			t.Errorf("outside file changed through staged symlink: %q", got)
		}
	}
	assertCollisionUntouched()
	if got, ok := f.readAggregate(); !ok || got != priorContent {
		t.Errorf("aggregate after refused Apply = %q, exists %v", got, ok)
	}
	journal, err := st.ReadJournal()
	if err != nil {
		t.Fatal(err)
	}
	if journal != nil {
		t.Errorf("refused Apply left a prepared journal: %#v", journal)
	}
	backups, err := st.SafetyBackups(f.rel)
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 0 {
		t.Errorf("refused Apply left %d safety backups", len(backups))
	}

	outcomes, err := st.Recover()
	if err != nil {
		t.Fatalf("Recover after refused Apply: %v", err)
	}
	if len(outcomes) != 0 {
		t.Errorf("Recover returned %v, want no pending generation", outcomes)
	}
	assertCollisionUntouched()
}

func TestApplyPreservesPreexistingStagedSymlinkWhenJournalCleanupFails(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	sentinel := filepath.Join(filepath.Dir(f.targetRoot), "sentinel")
	const original = "outside data\n"
	if err := os.WriteFile(sentinel, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	probe := sentinel + ".symlink-probe"
	if err := os.Symlink(sentinel, probe); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Remove(probe); err != nil {
		t.Fatal(err)
	}

	crasher := &stagedCollisionCrasher{target: sentinel}
	st := f.store(WithCrasher(crasher))
	crasher.store = st
	clearAttempts := 0
	st.removeJournalFile = func(string) error {
		clearAttempts++
		return errors.New("injected journal cleanup failure")
	}
	_, err := st.Apply(Mutation{
		Rel:                   f.rel,
		AbsFile:               f.absFile,
		NextBytes:             []byte(nextContent),
		Reason:                ReasonSync,
		ExpectPriorDigest:     Digest([]byte(priorContent)),
		ExpectPriorGeneration: 0,
	})
	if !errors.Is(err, os.ErrExist) {
		t.Fatalf("Apply error = %v, want staged-path collision", err)
	}
	if clearAttempts != 1 {
		t.Fatalf("journal cleanup attempts = %d, want 1", clearAttempts)
	}
	if gotTarget, readErr := os.Readlink(crasher.staged); readErr != nil || gotTarget != sentinel {
		t.Fatalf("staged collision after Apply = %q, %v; want symlink to %q", gotTarget, readErr, sentinel)
	}

	journal, err := st.ReadJournal()
	if err != nil {
		t.Fatal(err)
	}
	if journal == nil || journal.StagedPath != "" {
		t.Errorf("durable refused-Apply journal = %#v, want empty staged path", journal)
	}
	st.removeJournalFile = nil
	outcomes, err := st.Recover()
	if err != nil {
		t.Fatalf("Recover after failed journal cleanup: %v", err)
	}
	if len(outcomes) != 1 || outcomes[0].Action != ActionRolledBack {
		t.Errorf("Recover outcomes = %#v, want one rollback", outcomes)
	}
	if gotTarget, readErr := os.Readlink(crasher.staged); readErr != nil || gotTarget != sentinel {
		t.Errorf("staged collision after Recover = %q, %v; want symlink to %q", gotTarget, readErr, sentinel)
	}
	if got, readErr := os.ReadFile(sentinel); readErr != nil || string(got) != original {
		t.Errorf("outside sentinel after Recover = %q, %v; want %q", got, readErr, original)
	}
}

func TestApplyRefusesInvalidMutations(t *testing.T) {
	f := newFixture(t)
	st := f.store()
	tests := []struct {
		name string
		m    Mutation
		want error
	}{
		{"relative abs file", Mutation{Rel: "a.json", AbsFile: "a.json", Reason: ReasonSync}, nil},
		{"absolute rel", Mutation{Rel: "/a.json", AbsFile: f.absFile, Reason: ReasonSync}, nil},
		{"escaping rel", Mutation{Rel: "../a.json", AbsFile: f.absFile, Reason: ReasonSync}, nil},
		{"unknown reason", Mutation{Rel: "a.json", AbsFile: f.absFile, Reason: Reason("whatever")}, nil},
		{"unconfirmed orphan removal", Mutation{Rel: "a.json", AbsFile: f.absFile, Reason: ReasonOrphanRemoval}, ErrConfirmationRequired},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := st.Apply(tt.m)
			if err == nil {
				t.Fatal("Apply accepted an invalid mutation")
			}
			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
		})
	}
}

func assertGeneration(t *testing.T, st *Store, want uint64) {
	t.Helper()
	rec, err := st.Load()
	if want == 0 {
		if err == nil {
			t.Fatalf("state exists with generation %d, want no state", rec.Generation)
		}
		if !errors.Is(err, ErrStateMissing) {
			t.Fatalf("Load error = %v, want ErrStateMissing", err)
		}
		return
	}
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if rec.Generation != want {
		t.Fatalf("generation = %d, want %d", rec.Generation, want)
	}
}

func assertJournalPhase(t *testing.T, st *Store, want Phase) {
	t.Helper()
	j, err := st.ReadJournal()
	if err != nil {
		t.Fatalf("ReadJournal: %v", err)
	}
	if want == "" {
		if j != nil {
			t.Fatalf("journal exists in phase %q, want no journal", j.Phase)
		}
		return
	}
	if j == nil {
		t.Fatalf("no journal, want phase %q", want)
	}
	if j.Phase != want {
		t.Fatalf("journal phase = %q, want %q", j.Phase, want)
	}
}

func assertNoStagedFiles(t *testing.T, f *fixture) {
	t.Helper()
	entries, err := os.ReadDir(f.targetRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != f.rel && strings.HasPrefix(e.Name(), f.rel) {
			t.Fatalf("staged residue left behind: %s", e.Name())
		}
	}
}

func replaceOnce(s, old, replacement string) string {
	return strings.Replace(s, old, replacement, 1)
}
