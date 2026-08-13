package ownership

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// TestUnmanagedValueConflictIsTheGapBetweenAdoptionAndOrphanRemoval reproduces
// the state a beta tester could not get out of, and pins that the THIRD repair
// class is the only thing that describes it.
//
// The tester hand-wrote an mcp_config.json entry and then selected a marketplace
// MCP server with the SAME key but a different value. There is no ownership
// state at all, so:
//
//   - Classify reports a collision (key equality is all a collision needs), so
//     sync preserves the existing entry and only the generated entry fails;
//   - ExactAdoptionCandidates returns nothing, because adoption additionally
//     requires value-digest equality;
//   - OrphanCandidates returns nothing, because there is no retained backup or
//     journal evidence — and there never will be, since Store.Apply is what
//     produces that evidence and sync drops the colliding entry from the patch.
//
// Every sync therefore reports the same collision forever and `doctor --repair`
// answers "nothing to repair". [ValueConflictCandidates] is what breaks the
// loop: it names the entry, states that the values DIFFER, and carries both
// digests so the difference can be shown without showing either value.
func TestUnmanagedValueConflictIsTheGapBetweenAdoptionAndOrphanRemoval(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(`{"mcpServers":{"genai-atlassian":` + mcpValue("hand-written-binary") + `}}`)
	st := f.store()

	// No state was ever recorded for this root: exactly the tester's situation.
	proof, err := st.Proof(f.rel)
	if err == nil {
		t.Fatal("Proof must report the absent record")
	}
	if !errors.Is(err, ErrStateMissing) {
		t.Fatalf("Proof error = %v, want an ErrStateMissing wrapper", err)
	}

	existing := []EntryValue{mcpEV("genai-atlassian", mcpValue("hand-written-binary"))}
	desired := []EntryValue{mcpEV("genai-atlassian", mcpValue("generated-binary"))}

	// The closed loop, measured.
	report := Classify(proof, existing, desired)
	assertIDKeys(t, "collisions", report.Collisions, []string{"genai-atlassian"})
	if adoptions := ExactAdoptionCandidates(proof, existing, desired); len(adoptions) != 0 {
		t.Fatalf("adoptions = %+v, want none: the values differ", adoptions)
	}
	orphans, err := st.OrphanCandidates(proof, f.rel, existing, desired)
	if err != nil {
		t.Fatalf("OrphanCandidates: %v", err)
	}
	if len(orphans) != 0 {
		t.Fatalf("orphans = %+v, want none: there is no retained evidence", orphans)
	}

	// The escape hatch.
	conflicts := ValueConflictCandidates(proof, existing, desired)
	if len(conflicts) != 1 {
		t.Fatalf("conflicts = %+v, want exactly the same-key different-value entry", conflicts)
	}
	c := conflicts[0]
	if c.EntryID().Key != "genai-atlassian" {
		t.Fatalf("conflict key = %q, want %q", c.EntryID().Key, "genai-atlassian")
	}
	if !c.Differs() {
		t.Fatal("a conflict candidate must report that the values differ")
	}
	if c.ExistingValueDigest() != Digest([]byte(mcpValue("hand-written-binary"))) {
		t.Fatalf("existing digest = %q", c.ExistingValueDigest())
	}
	if c.DesiredValueDigest() != Digest([]byte(mcpValue("generated-binary"))) {
		t.Fatalf("desired digest = %q", c.DesiredValueDigest())
	}
	if c.ExistingValueDigest() == c.DesiredValueDigest() {
		t.Fatal("a conflict candidate cannot carry two equal digests")
	}

	// Replacing records ownership of the DESIRED value, so the next sync sees a
	// proven-clean entry rather than a collision.
	entry := c.Entry("genai-atlassian")
	if err := validateEntry(entry); err != nil {
		t.Fatalf("conflict entry is not recordable: %v", err)
	}
	if entry.ValueDigest != c.DesiredValueDigest() {
		t.Fatalf("recorded digest = %q, want the desired digest %q", entry.ValueDigest, c.DesiredValueDigest())
	}
}

// TestValueConflictCandidatesRequireUnmanagedKeyEqualityAndValueDifference pins
// the boundaries of the class: an exact match belongs to adoption, an undesired
// entry belongs to orphan removal, and a proven owned entry is drift — none of
// them is a conflict.
func TestValueConflictCandidatesRequireUnmanagedKeyEqualityAndValueDifference(t *testing.T) {
	f := newFixture(t)
	st := f.store()
	owned := mcpEntry("owned", "owned-server", mcpValue("owned"))
	mustApply(t, st, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(`{"mcpServers":{}}`),
		Entries: []Entry{owned}, Reason: ReasonSync})
	proof, err := st.Proof(f.rel)
	if err != nil {
		t.Fatal(err)
	}

	existing := []EntryValue{
		mcpEV("exact", mcpValue("same")),         // adoption's business
		mcpEV("conflicting", mcpValue("theirs")), // the new class
		mcpEV("undesired", mcpValue("whatever")), // orphan removal's business
		mcpEV("owned", mcpValue("hand-edited")),  // proven ours: drift, not a conflict
	}
	desired := []EntryValue{
		mcpEV("exact", mcpValue("same")),
		mcpEV("conflicting", mcpValue("ours")),
		mcpEV("owned", mcpValue("owned")),
		mcpEV("only-desired", mcpValue("new")),
	}

	conflicts := ValueConflictCandidates(proof, existing, desired)
	if len(conflicts) != 1 || conflicts[0].EntryID().Key != "conflicting" {
		t.Fatalf("conflicts = %+v, want exactly the unmanaged same-key different-value entry", conflicts)
	}
}

// TestConflictReplacementRefusesWithoutConfirmationAndBacksUpFirst pins the
// safety contract the class INHERITS FROM ORPHAN REMOVAL, not from adoption:
// destroying user-visible content needs explicit confirmation, and the user's
// exact bytes have to be recoverable before anything is written.
func TestConflictReplacementRefusesWithoutConfirmationAndBacksUpFirst(t *testing.T) {
	f := newFixture(t)
	handwritten := `{"mcpServers":{"conflicting":{"command":"` + fakeCommand + `"}}}`
	f.writeAggregate(handwritten)
	st := f.store()

	replaced := `{"mcpServers":{"conflicting":` + mcpValue("generated-binary") + `}}`
	entry := mcpEntry("conflicting", "conflicting-server", mcpValue("generated-binary"))

	// Unconfirmed: refused before any byte is written, exactly like an
	// unconfirmed orphan removal.
	_, err := st.Apply(Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(replaced),
		Entries: []Entry{entry}, Reason: ReasonConflictReplace})
	if !errors.Is(err, ErrConfirmationRequired) {
		t.Fatalf("unconfirmed replacement error = %v, want ErrConfirmationRequired", err)
	}
	if got, _ := f.readAggregate(); got != handwritten {
		t.Fatalf("an unconfirmed replacement changed the aggregate:\n%s", got)
	}
	if backups, err := st.SafetyBackups(f.rel); err != nil || len(backups) != 0 {
		t.Fatalf("a refused replacement took a backup (%d, %v); it must refuse before step 1", len(backups), err)
	}

	// Confirmed: the mandatory safety backup is taken BEFORE the mutation, so it
	// holds the user's original bytes rather than ours.
	result := mustApply(t, st, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(replaced),
		Entries: []Entry{entry}, Reason: ReasonConflictReplace, Confirmed: true})
	if result.BackupPath == "" {
		t.Fatal("a confirmed replacement must take a fresh safety backup")
	}
	backedUp, err := readFile(result.BackupPath)
	if err != nil {
		t.Fatalf("read safety backup: %v", err)
	}
	if backedUp != handwritten {
		t.Fatalf("the safety backup does not hold the pre-mutation bytes:\n%s", backedUp)
	}
	if got, _ := f.readAggregate(); got != replaced {
		t.Fatalf("a confirmed replacement did not replace the entry:\n%s", got)
	}

	// The replaced value is now proven owned, so the next Classify sees a clean
	// entry instead of a collision: the loop is open.
	proof, err := st.Proof(f.rel)
	if err != nil {
		t.Fatalf("Proof: %v", err)
	}
	existing := []EntryValue{mcpEV("conflicting", mcpValue("generated-binary"))}
	report := Classify(proof, existing, existing)
	if len(report.Collisions) != 0 || len(report.Clean) != 1 {
		t.Fatalf("after replacement report = %+v, want one clean entry and no collision", report)
	}
	if len(ValueConflictCandidates(proof, existing, existing)) != 0 {
		t.Fatal("a proven owned entry must not be nominated as a conflict again")
	}

	// Nothing anywhere in durable state may carry the replaced VALUE.
	for _, path := range []string{st.StateFile(), metaPathFor(result.BackupPath)} {
		body, readErr := readFile(path)
		if readErr != nil {
			continue
		}
		if strings.Contains(body, fakeCommand) {
			t.Fatalf("%s leaked a replaced entry value", path)
		}
	}
	if _, err := os.Stat(st.JournalFile()); err == nil {
		body, _ := readFile(st.JournalFile())
		if strings.Contains(body, fakeCommand) {
			t.Fatal("the journal leaked a replaced entry value")
		}
	}
}
