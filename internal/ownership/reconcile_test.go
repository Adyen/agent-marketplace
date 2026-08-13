package ownership

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func mcpValue(command string) string { return `{"command":"` + command + `"}` }

// mcpEV builds the EntryValue a caller derives from a parsed aggregate.
func mcpEV(key, value string) EntryValue {
	return EntryValue{
		ID:          EntryID{Kind: KindMcpServer, Container: "mcpServers", Key: key, IdentityDigest: McpIdentityDigest(key)},
		ValueDigest: Digest([]byte(value)),
	}
}

// A drifted owned entry is reported and replaced; an unmanaged entry is
// preserved; a stale owned entry is prunable; an unmanaged collision fails only
// the generated entry.
func TestClassifyPartitionsExistingEntries(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	st := f.store()

	ownedClean := mcpEntry("clean", "clean-server", mcpValue("clean"))
	ownedDrift := mcpEntry("drifted", "drift-server", mcpValue("original"))
	ownedStale := mcpEntry("stale", "stale-server", mcpValue("stale"))
	mustApply(t, st, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent),
		Entries: []Entry{ownedClean, ownedDrift, ownedStale}, Reason: ReasonSync})

	proof, err := st.Proof(f.rel)
	if err != nil {
		t.Fatalf("Proof: %v", err)
	}

	existing := []EntryValue{
		mcpEV("clean", mcpValue("clean")),
		mcpEV("drifted", mcpValue("hand-edited")), // user changed our entry
		mcpEV("stale", mcpValue("stale")),
		mcpEV("handwritten", mcpValue("theirs")), // never ours
	}
	desired := []EntryValue{
		mcpEV("clean", mcpValue("clean")),
		mcpEV("drifted", mcpValue("original")),
		mcpEV("handwritten", mcpValue("ours-would-be")), // collides with a user entry
	}

	report := Classify(proof, existing, desired)
	// "stale" is BOTH owned-clean (its bytes still match what we wrote) and stale
	// (it is no longer desired). Clean/Drifted/Unmanaged partition the existing
	// entries; Stale is a separate axis over the proven owned set.
	assertIDKeys(t, "clean", report.Clean, []string{"clean", "stale"})
	assertIDKeys(t, "drifted", report.Drifted, []string{"drifted"})
	if len(report.Unmanaged) != 1 || report.Unmanaged[0].Key != "handwritten" {
		t.Fatalf("unmanaged = %+v, want just the handwritten entry", report.Unmanaged)
	}
	assertIDKeys(t, "stale", report.Stale, []string{"stale"})
	assertIDKeys(t, "collisions", report.Collisions, []string{"handwritten"})
	if report.File != f.rel {
		t.Fatalf("report file = %q, want %q", report.File, f.rel)
	}
}

// The zero Proof is the fail-closed state: everything unmanaged, nothing stale.
func TestClassifyWithoutProofOwnsNothing(t *testing.T) {
	existing := []EntryValue{mcpEV("a", mcpValue("a")), mcpEV("b", mcpValue("b"))}
	desired := []EntryValue{mcpEV("a", mcpValue("a"))}
	report := Classify(Proof{}, existing, desired)
	if len(report.Clean) != 0 || len(report.Drifted) != 0 || len(report.Stale) != 0 {
		t.Fatalf("report = %+v, want nothing owned and nothing stale", report)
	}
	if len(report.Unmanaged) != 2 {
		t.Fatalf("unmanaged = %+v, want both existing entries", report.Unmanaged)
	}
	assertIDKeys(t, "collisions", report.Collisions, []string{"a"})
}

// A proven owned entry that is already gone from the file is not "stale": there
// is nothing left to prune, and reporting it would make a repair preview offer a
// removal that cannot happen.
func TestClassifyDoesNotReportAlreadyRemovedOwnedEntriesAsStale(t *testing.T) {
	f := newFixture(t)
	st := f.store()
	owned := mcpEntry("gone", "gone-server", mcpValue("gone"))
	mustApply(t, st, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(`{"mcpServers":{}}`),
		Entries: []Entry{owned}, Reason: ReasonSync})
	proof, err := st.Proof(f.rel)
	if err != nil {
		t.Fatal(err)
	}
	report := Classify(proof, nil, nil)
	if len(report.Stale) != 0 {
		t.Fatalf("stale = %+v, want empty", report.Stale)
	}
}

// Adoption is EXACT. Same key with different content is not adoptable; a
// different key is not adoptable; an already-owned entry is not adoptable.
func TestExactAdoptionCandidatesRequireByteEquality(t *testing.T) {
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
		mcpEV("exact", mcpValue("same")),
		mcpEV("nearly", mcpValue("theirs")),
		mcpEV("only-in-file", mcpValue("x")),
		mcpEV("owned", mcpValue("owned")),
	}
	desired := []EntryValue{
		mcpEV("exact", mcpValue("same")),
		mcpEV("nearly", mcpValue("ours")), // same key, different bytes
		mcpEV("only-desired", mcpValue("y")),
		mcpEV("owned", mcpValue("owned")),
	}

	candidates := ExactAdoptionCandidates(proof, existing, desired)
	if len(candidates) != 1 {
		t.Fatalf("candidates = %+v, want exactly the byte-identical match", candidates)
	}
	c := candidates[0]
	if c.EntryID().Key != "exact" {
		t.Fatalf("candidate key = %q, want %q", c.EntryID().Key, "exact")
	}
	if c.ValueDigest() != Digest([]byte(mcpValue("same"))) {
		t.Fatalf("candidate digest = %q, want the shared digest", c.ValueDigest())
	}
	if c.File() != f.rel {
		t.Fatalf("candidate file = %q, want %q", c.File(), f.rel)
	}
	entry := c.Entry("adopted-server")
	if err := validateEntry(entry); err != nil {
		t.Fatalf("adopted entry is not recordable: %v", err)
	}
	if entry.Resource != "adopted-server" || entry.ValueDigest != c.ValueDigest() {
		t.Fatalf("adopted entry = %+v", entry)
	}
}

// Orphan nomination requires recorded evidence. This walks the full lifecycle:
// a managed entry is written (recording evidence in the safety-backup manifest
// of the NEXT generation), then state is destroyed so the entry becomes
// unmanaged, and only then can it be nominated.
func TestOrphanCandidatesRequireRecordedEvidence(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(`{"mcpServers":{}}`)
	st := f.store()

	orphan := mcpEntry("orphan", "orphan-server", mcpValue("orphan"))
	mustApply(t, st, Mutation{Rel: f.rel, AbsFile: f.absFile,
		NextBytes: []byte(`{"mcpServers":{"orphan":` + mcpValue("orphan") + `}}`),
		Entries:   []Entry{orphan}, Reason: ReasonSync})
	// A second generation so the manifest that records the orphan is retained.
	mustApply(t, st, Mutation{Rel: f.rel, AbsFile: f.absFile,
		NextBytes: []byte(`{"mcpServers":{"orphan":` + mcpValue("orphan") + `}}`),
		Entries:   []Entry{orphan}, Reason: ReasonSync})

	existing := []EntryValue{
		mcpEV("orphan", mcpValue("orphan")),
		mcpEV("stranger", mcpValue("stranger")), // never managed: no evidence
	}

	// While the entry is still PROVEN OWNED it is not an orphan.
	proof, err := st.Proof(f.rel)
	if err != nil {
		t.Fatal(err)
	}
	owned, err := st.OrphanCandidates(proof, f.rel, existing, nil)
	if err != nil {
		t.Fatalf("OrphanCandidates: %v", err)
	}
	if len(owned) != 0 {
		t.Fatalf("candidates = %+v, want none while the entry is proven owned", owned)
	}

	// Destroy state: the entry is now unmanaged, which is exactly the situation
	// orphan recovery exists for.
	if err := os.Remove(st.StateFile()); err != nil {
		t.Fatal(err)
	}
	empty, err := st.Proof(f.rel)
	if err == nil {
		t.Fatal("Proof returned no error after state was destroyed")
	}
	if empty.OwnedCount() != 0 {
		t.Fatal("Proof is non-empty after state was destroyed")
	}

	candidates, err := st.OrphanCandidates(empty, f.rel, existing, nil)
	if err != nil {
		t.Fatalf("OrphanCandidates: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidates = %+v, want exactly the evidence-backed orphan", candidates)
	}
	c := candidates[0]
	if c.EntryID().Key != "orphan" {
		t.Fatalf("candidate key = %q, want %q", c.EntryID().Key, "orphan")
	}
	ev := c.Evidence()
	if ev.Kind() != EvidenceSafetyBackup {
		t.Fatalf("evidence kind = %q, want %q", ev.Kind(), EvidenceSafetyBackup)
	}
	if ev.Generation() == 0 {
		t.Fatal("evidence generation = 0")
	}
	if ev.Resource() != "orphan-server" {
		t.Fatalf("evidence resource = %q, want %q", ev.Resource(), "orphan-server")
	}
	if !strings.Contains(ev.Summary(), "orphan-server") || !strings.Contains(ev.Summary(), string(EvidenceSafetyBackup)) {
		t.Fatalf("evidence summary = %q, want it to name the kind and the resource", ev.Summary())
	}
	// The summary describes evidence, never current ownership.
	for _, forbidden := range []string{"owned", "managed by"} {
		if strings.Contains(strings.ToLower(ev.Summary()), forbidden) {
			t.Fatalf("evidence summary %q describes the entry as owned", ev.Summary())
		}
	}
}

// A hand-edited former-managed entry has no matching recorded digest and is
// therefore NEVER nominated: "entries without such evidence are never nominated;
// users must edit them manually".
func TestOrphanCandidatesRejectHandEditedFormerManagedEntries(t *testing.T) {
	f := newFixture(t)
	st := f.store()
	entry := mcpEntry("was-ours", "was-ours-server", mcpValue("original"))
	for i := 0; i < 2; i++ {
		mustApply(t, st, Mutation{Rel: f.rel, AbsFile: f.absFile,
			NextBytes: []byte(`{"mcpServers":{"was-ours":` + mcpValue("original") + `}}`),
			Entries:   []Entry{entry}, Reason: ReasonSync})
	}
	if err := os.Remove(st.StateFile()); err != nil {
		t.Fatal(err)
	}
	// The user edited the entry after we last wrote it.
	existing := []EntryValue{mcpEV("was-ours", mcpValue("hand-edited"))}
	candidates, err := st.OrphanCandidates(Proof{}, f.rel, existing, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 0 {
		t.Fatalf("candidates = %+v, want none: the entry no longer matches any recorded digest", candidates)
	}
}

// A desired entry is not an orphan, even with evidence: it is about to be
// rewritten, not removed.
func TestOrphanCandidatesSkipDesiredEntries(t *testing.T) {
	f := newFixture(t)
	st := f.store()
	entry := mcpEntry("still-wanted", "wanted-server", mcpValue("v"))
	for i := 0; i < 2; i++ {
		mustApply(t, st, Mutation{Rel: f.rel, AbsFile: f.absFile,
			NextBytes: []byte(`{"mcpServers":{"still-wanted":` + mcpValue("v") + `}}`),
			Entries:   []Entry{entry}, Reason: ReasonSync})
	}
	if err := os.Remove(st.StateFile()); err != nil {
		t.Fatal(err)
	}
	existing := []EntryValue{mcpEV("still-wanted", mcpValue("v"))}
	candidates, err := st.OrphanCandidates(Proof{}, f.rel, existing, existing)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 0 {
		t.Fatalf("candidates = %+v, want none for a still-desired entry", candidates)
	}
}

// An interrupted generation's journal is the other permitted evidence source.
func TestOrphanCandidatesAcceptRetainedJournalEvidence(t *testing.T) {
	f := newFixture(t)
	entry := mcpEntry("journal-orphan", "journal-server", mcpValue("j"))
	crashing := f.store(WithCrasher(CrashAt(CrashAfterAggregateRename)))
	if _, err := crashing.Apply(Mutation{Rel: f.rel, AbsFile: f.absFile,
		NextBytes: []byte(`{"mcpServers":{"journal-orphan":` + mcpValue("j") + `}}`),
		Entries:   []Entry{entry}, Reason: ReasonSync}); err == nil {
		t.Fatal("Apply did not crash")
	}
	// The aggregate is published, state is not committed, and the journal holds
	// the generation. The entry is unmanaged but the journal is evidence.
	existing := []EntryValue{mcpEV("journal-orphan", mcpValue("j"))}
	candidates, err := crashing.OrphanCandidates(Proof{}, f.rel, existing, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidates = %+v, want the journal-backed orphan", candidates)
	}
	if got := candidates[0].Evidence().Kind(); got != EvidenceRetainedJournal {
		t.Fatalf("evidence kind = %q, want %q", got, EvidenceRetainedJournal)
	}
}

// Evidence and adoption candidates cannot be fabricated from outside the
// package: every field is unexported. This is a compile-time property; the test
// documents it by asserting the reflected struct shape.
func TestOwnershipVerdictTypesCannotBeFabricated(t *testing.T) {
	for _, v := range []any{Proof{}, Evidence{}, AdoptionCandidate{}, OrphanCandidate{}, ConflictCandidate{}} {
		typ := reflect.TypeOf(v)
		for i := 0; i < typ.NumField(); i++ {
			if typ.Field(i).IsExported() {
				t.Fatalf("%s.%s is exported: an ownership verdict must not be constructible by a caller",
					typ.Name(), typ.Field(i).Name)
			}
		}
	}
	// And neither candidate type may expose an ownership verdict at all.
	for _, v := range []any{OrphanCandidate{}, ConflictCandidate{}} {
		typ := reflect.TypeOf(v)
		for i := 0; i < typ.NumMethod(); i++ {
			switch typ.Method(i).Name {
			case "Owned", "Status", "IsOwned", "Classify":
				t.Fatalf("%s exposes %s: a candidate is never described as currently owned",
					typ.Name(), typ.Method(i).Name)
			}
		}
	}
}

func assertIDKeys(t *testing.T, label string, got []EntryID, wantKeys []string) {
	t.Helper()
	if len(got) != len(wantKeys) {
		t.Fatalf("%s = %+v, want keys %v", label, got, wantKeys)
	}
	for i, id := range got {
		if id.Key != wantKeys[i] {
			t.Fatalf("%s[%d] key = %q, want %q", label, i, id.Key, wantKeys[i])
		}
	}
}
