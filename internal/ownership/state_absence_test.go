package ownership

import (
	"errors"
	"os"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// TestFirstUseAbsenceIsDistinguishedFromVanishedRecord pins the REPORTING
// distinction this package draws, and pins that it is only a reporting
// distinction.
//
// "Missing or corrupt ownership state makes all existing aggregate entries
// unmanaged" is unchanged in both cases; what differs is whether a caller has
// anything worth telling a user. A first-ever run cannot have a record — nothing
// bootstraps one before the first Apply — so its absence is normal and carries
// ErrStateUninitialized. Once a generation has been committed for the root, a
// record that is no longer there is a real event and carries plain
// ErrStateMissing.
func TestFirstUseAbsenceIsDistinguishedFromVanishedRecord(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	st := f.store()

	// FIRST USE. Nothing has ever been recorded for this root.
	if st.EverRecorded() {
		t.Fatal("EverRecorded reported a durable trace before anything was written")
	}
	rec, err := st.Load()
	if rec != nil {
		t.Fatalf("Load returned a record for a first-ever use: %+v", rec)
	}
	if !errors.Is(err, ErrStateUninitialized) {
		t.Fatalf("error = %v, want ErrStateUninitialized on a first-ever use", err)
	}
	// The fail-closed contract is expressed through ErrStateMissing and the
	// ownership.state-missing code, and BOTH must still match: the wrapper must
	// not create a hole any caller could fall through.
	if !errors.Is(err, ErrStateMissing) {
		t.Fatalf("error = %v, want it to still be an ErrStateMissing", err)
	}
	coded, ok := diag.AsCodedError(err)
	if !ok {
		t.Fatalf("error %T is not a *diag.CodedError", err)
	}
	if coded.Code != diag.CodeOwnershipStateMissing {
		t.Fatalf("code = %q, want %q", coded.Code, diag.CodeOwnershipStateMissing)
	}
	if coded.Severity != diag.SeverityWarning {
		t.Fatalf("severity = %v, want warning", coded.Severity)
	}
	entry := mcpEntry("handwritten", "some-resource", `{"command":"keep"}`)
	proof, _ := st.Proof(f.rel)
	if proof.OwnedCount() != 0 || proof.Classify(entry.ID(), entry.ValueDigest) != StatusUnmanaged {
		t.Fatal("a first-use absence stopped failing closed")
	}

	// VANISHED RECORD. One generation has been committed, then the record is
	// removed: the root has a durable trace, so a record WAS expected.
	generated := mcpEntry("generated", "generated-server", `{"command":"new"}`)
	mustApply(t, st, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent),
		Entries: []Entry{generated}, Reason: ReasonSync})
	if err := os.Remove(st.StateFile()); err != nil {
		t.Fatalf("drop the record: %v", err)
	}
	if !st.EverRecorded() {
		t.Fatal("EverRecorded lost the durable trace of a committed generation")
	}
	rec, err = st.Load()
	if rec != nil {
		t.Fatalf("Load returned a record after the record was removed: %+v", rec)
	}
	if !errors.Is(err, ErrStateMissing) {
		t.Fatalf("error = %v, want ErrStateMissing", err)
	}
	if errors.Is(err, ErrStateUninitialized) {
		t.Fatalf("a vanished record was reported as a first use: %v", err)
	}
	// Identical code, identical severity, identical fail-closed outcome.
	coded, ok = diag.AsCodedError(err)
	if !ok {
		t.Fatalf("error %T is not a *diag.CodedError", err)
	}
	if coded.Code != diag.CodeOwnershipStateMissing || coded.Severity != diag.SeverityWarning {
		t.Fatalf("code/severity = %q/%v, want %q/warning", coded.Code, coded.Severity, diag.CodeOwnershipStateMissing)
	}
	proof, _ = st.Proof(f.rel)
	if proof.OwnedCount() != 0 || proof.Classify(generated.ID(), generated.ValueDigest) != StatusUnmanaged {
		t.Fatal("a vanished record stopped failing closed: an entry we really did write is still considered owned")
	}
}

// TestRetainedSafetyBackupCountsAsADurableTrace covers the second evidence
// source: a mandatory safety backup is taken even when a mutation is refused, so
// a root can have been managed without ever committing a record. Reporting that
// as a first use would hide a genuine loss.
func TestRetainedSafetyBackupCountsAsADurableTrace(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	st := f.store()

	if _, err := st.BackupOnly(f.rel, f.absFile, ReasonSync); err != nil {
		t.Fatalf("BackupOnly: %v", err)
	}
	// Remove the ownership directory so only the retained backup is left: this
	// pins the SECOND evidence source rather than passing on the first.
	if err := os.RemoveAll(st.recordDir()); err != nil {
		t.Fatalf("drop the ownership directory: %v", err)
	}
	if !st.EverRecorded() {
		t.Fatal("a retained safety backup is durable evidence that this root was managed")
	}
	if _, err := st.Load(); errors.Is(err, ErrStateUninitialized) {
		t.Fatalf("a root with a retained backup was reported as a first use: %v", err)
	}
}
