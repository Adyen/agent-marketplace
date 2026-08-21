package materialize

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"

	"github.com/Adyen/agent-marketplace/internal/ownership"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// repairLockHook is nil in production and is the seam a test uses to observe
// the lock transitions of a REPAIR action from outside this file.
//
// It exists for the same reason ownedAggregateLockAcquisitionHook does on the sync side:
// the section BOUNDARY is not observable any other way. A repair whose section
// covers only ownership.Apply and one whose section covers
// requireCompleteGeneration + re-derivation + Apply write identical bytes and
// produce identical errors on an uncontended root; they differ only in how many
// times the lock is taken and at what depth. So the count is the property, and
// counting it needs a hook, because a RepairFile's Store is opened by its
// target preview and a test cannot pass an option into it.
//
// A test that installs it must not run in parallel with another that does.
var repairLockHook func(ownership.LockEvent)

func observeRepairLock(ev ownership.LockEvent) {
	if repairLockHook != nil {
		repairLockHook(ev)
	}
}

// withRepairSession runs ONE repair action's whole read-modify-commit inside a
// single held write session on this file's root.
//
// # Why the session, and where its boundary has to be
//
// Each of the three actions was separately correct and none of them was atomic
// with respect to the others. Every one of them reads the journal
// (requireCompleteGeneration), re-derives its candidate from the CURRENT file,
// and only then calls ownership.Apply — and Apply's own acquisition cannot
// serialize reads its caller already performed. So another writer could commit
// in between, and then:
//
//   - the in-flight-journal guard proves nothing (F3 of
//     docs/plans/2026-08-06-ownership-locking-design.md): the journal it read is
//     one the other writer has since replaced, so a repair can silently destroy
//     the only description of an interrupted generation;
//   - the re-derivation — which is the whole anti-staleness check, and the thing
//     that turns a human's consent into an authorization for specific bytes —
//     describes a file that no longer exists by the time Apply renames over it.
//
// # Why WithWriteSession and never the raw Lock pair
//
// [ownership.Store.WithWriteSession] carries the degrade policy every writer in
// internal/ownership uses: on a filesystem with no advisory locking (GOOS=windows
// unconditionally, an NFS-mounted state root) it warns and PROCEEDS unserialized
// rather than refusing, which is exactly today's behaviour on exactly the
// platforms the design commits to degrading on. Store.WithLock would instead
// return the acquisition error without running the body, turning "this root is
// not serialized" into "this root can never be repaired". And Store.Lock must not
// be used directly at all: it returns a NIL releaser together with a non-nil
// error deliberately, so the idiomatic `release, err := Lock(); defer release()`
// panics on precisely that degrade path.
//
// # One mutation, not a nested one
//
// The session contains exactly ONE mutation (the single ownership.Apply at the
// end of each action), which reenters the held lock at depth 2 rather than
// opening a second descriptor. ownership.ErrNestedMutation sits INSIDE
// withWriteLock and refuses a mutation that starts while another is half
// written, not a session that contains mutations one after another, so a widened
// section here cannot trip it. The acquisition regression pins both the count
// and the depth.
//
// # The lock is never held across a human prompt
//
// The section starts inside the action and ends when the action returns.
// cmd/agent-marketplace/doctor.go prompts BETWEEN calls, so a human sitting at a
// y/N never holds anything: one answer, one acquisition, one journaled
// generation, one release. That matters most for a machine-scope root, whose
// lock would otherwise block every repository for as long as the human takes
// to answer.
// TestDoctorRepairHoldsNoOwnershipLockWhileAwaitingConfirmation proves it.
func (f *RepairFile) withRepairSession(op string, fn func() (ownership.Result, error)) (ownership.Result, error) {
	var result ownership.Result
	if err := f.store.WithWriteSession(op, func() error {
		var innerErr error
		result, innerErr = fn()
		return innerErr
	}); err != nil {
		// Either the acquisition failed — in which case fn never ran and the
		// aggregate is exactly as found — or fn itself failed and has already
		// left the file untouched. Both cases return no Result.
		return ownership.Result{}, err
	}
	return result, nil
}

// Adopt records the selected exact matches as owned, in one journaled
// generation, WITHOUT changing a single byte of the aggregate.
//
// That byte-for-byte invariant is the point of "adopts exact current desired
// matches": an adopted entry is already identical to what sync would have
// written, so adoption is a statement about the ownership RECORD, never an edit
// of the user's file. The mutation still goes through ownership.Apply so it is
// journaled, backed up, and crash-recoverable like any other generation.
//
// The whole of it — the in-flight-journal guard, the re-read of the aggregate,
// the re-derivation of the exact matches, and the Apply — runs inside ONE held
// write session. See [RepairFile.withRepairSession]. Nothing is acquired for an
// empty selection: doctor calls Adopt with no indices whenever a user declined
// every adoption prompt, and a declined repair must create no lock file.
func (f *RepairFile) Adopt(indices []int) (ownership.Result, error) {
	if len(indices) == 0 {
		return ownership.Result{}, nil
	}
	return f.withRepairSession("repair-adopt", func() (ownership.Result, error) {
		return f.adoptLocked(indices)
	})
}

// adoptLocked is [RepairFile.Adopt]'s body, running with the per-root lock held.
func (f *RepairFile) adoptLocked(indices []int) (ownership.Result, error) {
	if err := f.requireCompleteGeneration(); err != nil {
		return ownership.Result{}, err
	}
	key := outputKey{target: f.Target, machineScope: f.MachineScope}
	state, diags := f.adapter.read(f.store, key, f.OutDir, f.Rel)
	if state == nil {
		return ownership.Result{}, fmt.Errorf("%w: aggregate %q could not be re-read (%s)", ErrRepairCandidateStale, f.Rel, f.adapter.diagnosticSummary(diags))
	}

	desiredValues := f.repairDesiredValues()
	current := ownership.ExactAdoptionCandidates(state.proof, state.existing, desiredValues)
	currentByID := make(map[string]ownership.AdoptionCandidate, len(current))
	for _, candidate := range current {
		currentByID[ownedAggregateIDKey(candidate.EntryID())] = candidate
	}

	entries, err := f.recordedEntries()
	if err != nil {
		return ownership.Result{}, err
	}
	seen := make(map[int]struct{}, len(indices))
	for _, index := range indices {
		if index < 0 || index >= len(f.Adoptions) {
			return ownership.Result{}, fmt.Errorf("materialize: adoption index %d is out of range for %q", index, f.Rel)
		}
		if _, ok := seen[index]; ok {
			return ownership.Result{}, fmt.Errorf("materialize: adoption index %d was selected more than once for %q", index, f.Rel)
		}
		seen[index] = struct{}{}
		selected := f.Adoptions[index]
		id := ownedAggregateIDKey(selected.Candidate.EntryID())
		fresh, ok := currentByID[id]
		if !ok || fresh.ValueDigest() != selected.Candidate.ValueDigest() {
			// The entry changed between preview and confirmation. Adopting it
			// now would record ownership of bytes the user never saw described.
			return ownership.Result{}, fmt.Errorf("%w: %s entry %q in %q", ErrRepairCandidateStale,
				selected.EntryKind(), selected.Identity(), f.Rel)
		}
		entries = append(entries, fresh.Entry(selected.Resource))
	}

	priorGen, priorDigest := ownedAggregatePriorForApply(state.raw, nil, state.proof)
	result, err := f.store.Apply(ownership.Mutation{
		Rel:                   f.Rel,
		AbsFile:               f.AbsFile(),
		NextBytes:             state.raw,
		Entries:               entries,
		Reason:                ownership.ReasonAdoption,
		ExpectPriorGeneration: priorGen,
		ExpectPriorDigest:     priorDigest,
	})
	if err != nil {
		slog.Error(repairLogMessage(f.Target, "adoption failed; the aggregate was left exactly as found"),
			"target", f.Target, "file", f.Rel, "out_dir", f.OutDir, "adopted", len(indices), "error", err,
			"code", diag.CodeDoctorRepairFailed)
		return ownership.Result{}, err
	}
	slog.Info(repairLogMessage(f.Target, "adopted exact matches"),
		"target", f.Target, "file", f.Rel, "out_dir", f.OutDir, "adopted", len(indices),
		"owned_entries", len(entries), "generation", result.Generation, "backup", result.BackupPath)
	return result, nil
}

func (f *RepairFile) repairDesiredValues() []ownership.EntryValue {
	return repairDesiredValues(f.desired)
}

// RemoveOrphan removes ONE evidence-backed orphan entry.
//
// One entry per call, one journaled generation per entry, one fresh safety
// backup per generation: "orphan-removal candidates require ... explicit
// per-entry confirmation and a fresh backup". Batch removal is deliberately not
// offered, because a batch cannot carry per-entry confirmation.
//
// confirmed is passed straight through to ownership.Mutation.Confirmed. Passing
// false does not "skip" the removal quietly — ownership.Apply refuses it with
// ownership.ErrConfirmationRequired, so an unconfirmed removal is a loud
// failure rather than a silent deletion.
//
// The in-flight-journal guard, the re-read, the RE-NOMINATION from current
// evidence and the Apply all run inside ONE held write session; only the index
// bounds check — which touches neither disk nor store — is outside it. See
// [RepairFile.withRepairSession].
func (f *RepairFile) RemoveOrphan(index int, confirmed bool) (ownership.Result, error) {
	if index < 0 || index >= len(f.Orphans) {
		return ownership.Result{}, fmt.Errorf("materialize: orphan index %d is out of range for %q", index, f.Rel)
	}
	return f.withRepairSession("repair-remove-orphan", func() (ownership.Result, error) {
		return f.removeOrphanLocked(index, confirmed)
	})
}

// removeOrphanLocked is [RepairFile.RemoveOrphan]'s body, running with the
// per-root lock held.
func (f *RepairFile) removeOrphanLocked(index int, confirmed bool) (ownership.Result, error) {
	if err := f.requireCompleteGeneration(); err != nil {
		return ownership.Result{}, err
	}
	selected := f.Orphans[index]
	key := outputKey{target: f.Target, machineScope: f.MachineScope}
	state, diags := f.adapter.read(f.store, key, f.OutDir, f.Rel)
	if state == nil || state.remove == nil {
		return ownership.Result{}, fmt.Errorf("%w: aggregate %q could not be re-read (%s)", ErrRepairCandidateStale, f.Rel, f.adapter.diagnosticSummary(diags))
	}

	// Re-nominate from the CURRENT file and the CURRENT evidence. A candidate
	// that stopped being nominated between preview and confirmation is refused:
	// the evidence, not the user's earlier "yes", is what authorizes a delete.
	desiredValues := f.repairDesiredValues()
	fresh, err := f.store.OrphanCandidates(state.proof, f.Rel, state.existing, desiredValues)
	if err != nil {
		return ownership.Result{}, err
	}
	wanted := ownedAggregateIDKey(selected.Candidate.EntryID())
	found := false
	for _, candidate := range fresh {
		if ownedAggregateIDKey(candidate.EntryID()) == wanted && candidate.ValueDigest() == selected.Candidate.ValueDigest() {
			found = true
			break
		}
	}
	if !found {
		return ownership.Result{}, fmt.Errorf("%w: %s entry %q in %q is no longer an evidence-backed orphan candidate",
			ErrRepairCandidateStale, selected.EntryKind(), selected.Identity(), f.Rel)
	}

	// The CONFIRMED value digest is passed alongside the identity, and it is not
	// redundant: it is what makes the deletion pick the element the user was
	// shown. See [removeOwnedJSONEntry].
	next, err := state.remove(selected.Candidate.EntryID(), selected.Candidate.ValueDigest())
	if err != nil {
		return ownership.Result{}, err
	}
	if bytes.Equal(state.raw, next) {
		return ownership.Result{}, fmt.Errorf("%w: %s entry %q in %q produced no edit",
			ErrRepairCandidateStale, selected.EntryKind(), selected.Identity(), f.Rel)
	}

	entries, err := f.recordedEntries()
	if err != nil {
		return ownership.Result{}, err
	}
	priorGen, priorDigest := ownedAggregatePriorForApply(state.raw, nil, state.proof)
	result, applyErr := f.store.Apply(ownership.Mutation{
		Rel:                   f.Rel,
		AbsFile:               f.AbsFile(),
		NextBytes:             next,
		Entries:               entries,
		Reason:                ownership.ReasonOrphanRemoval,
		Confirmed:             confirmed,
		ExpectPriorGeneration: priorGen,
		ExpectPriorDigest:     priorDigest,
	})
	if applyErr != nil {
		slog.Error(repairLogMessage(f.Target, "orphan removal failed; the aggregate was left exactly as found"),
			"target", f.Target, "file", f.Rel, "out_dir", f.OutDir, "kind", selected.EntryKind(), "identity", selected.Identity(),
			"error", applyErr, "code", diag.CodeDoctorRepairFailed)
		return ownership.Result{}, applyErr
	}
	slog.Info(repairLogMessage(f.Target, "removed an evidence-backed orphan entry"),
		"target", f.Target, "file", f.Rel, "out_dir", f.OutDir, "kind", selected.EntryKind(), "identity", selected.Identity(),
		"generation", result.Generation, "backup", result.BackupPath)
	return result, nil
}

// ReplaceConflict replaces ONE conflicting unmanaged entry with the generated
// entry, and records ownership of what it wrote.
//
// This is the destructive half of the third repair class, and it is deliberately
// shaped like [RepairFile.RemoveOrphan] rather than like [RepairFile.Adopt]:
//
//   - One entry per call, one journaled generation per entry, one fresh
//     mandatory safety backup per generation. A batch cannot carry per-entry
//     consent, so no batch form is offered.
//   - confirmed is passed straight into ownership.Mutation.Confirmed, and
//     ownership.Apply refuses [ownership.ReasonConflictReplace] without it — and
//     refuses it BEFORE the backup step, so an unconfirmed replacement writes
//     nothing at all, not even a backup. That refusal is one of two independent
//     gates; the other is doctor's conflict loop, which requires a TTY and never
//     consults --yes.
//   - It refuses outright while an interrupted generation is still in flight
//     ([ErrRepairGenerationInFlight]). ownership.Apply writes its journal
//     unconditionally, so mutating here would overwrite — and permanently
//     destroy — the only record of entries that may already be live in the
//     aggregate.
//   - The candidate is re-derived from the CURRENT file before anything is
//     written, and compared against BOTH digests the preview showed. If the
//     existing value on disk changed, the replacement is refused with
//     [ErrRepairCandidateStale] and the aggregate is left exactly as found:
//     consent covers the difference that was described, not whatever now
//     occupies that key. The desired side is the set the preview was computed
//     from (f.desired), so the desired-digest comparison is a defensive
//     invariant rather than a re-resolution of configuration: a configuration
//     change needs a new `doctor --repair` run, which is the same rule adoption
//     and orphan removal follow.
//
// The TOCTOU window that used to remain here is CLOSED by the held write
// session: the in-flight-journal guard, the re-derivation, the jsonc edit and
// ownership.Apply's rename are one section, so another agent-marketplace process
// cannot commit between the re-derivation and the rename. What the section
// cannot exclude is a foreign tool with no knowledge of this lock, and on a
// filesystem with no advisory locking the whole section degrades to today's
// unserialized behaviour (see [RepairFile.withRepairSession]). In both cases the
// mandatory safety backup is taken from the bytes on disk at that moment, so
// those bytes are always recoverable.
//
// Every untouched member keeps its exact bytes, comments, and position, because
// the single edit goes through pkg/harness-compilation/jsonc.
func (f *RepairFile) ReplaceConflict(index int, confirmed bool) (ownership.Result, error) {
	if index < 0 || index >= len(f.Conflicts) {
		return ownership.Result{}, fmt.Errorf("materialize: conflict index %d is out of range for %q", index, f.Rel)
	}
	selected := f.Conflicts[index]
	id := selected.Candidate.EntryID()
	if f.adapter == nil || !f.adapter.replaceable(id) {
		// The scope fence is decided from the aggregate's name and the entry's
		// identity alone, so it is answered before any lock is taken: refusing a
		// hook must not create locks/<rootKey>.lock.
		return ownership.Result{}, fmt.Errorf("materialize: %s entry %q in %q is not a replaceable conflict",
			selected.EntryKind(), selected.Identity(), f.Rel)
	}
	return f.withRepairSession("repair-replace-conflict", func() (ownership.Result, error) {
		return f.replaceConflictLocked(index, confirmed)
	})
}

// replaceConflictLocked is [RepairFile.ReplaceConflict]'s body, running with the
// per-root lock held.
func (f *RepairFile) replaceConflictLocked(index int, confirmed bool) (ownership.Result, error) {
	selected := f.Conflicts[index]
	id := selected.Candidate.EntryID()

	if err := f.requireCompleteGeneration(); err != nil {
		return ownership.Result{}, err
	}

	key := outputKey{target: f.Target, machineScope: f.MachineScope}
	state, diags := f.adapter.read(f.store, key, f.OutDir, f.Rel)
	if state == nil || state.replace == nil {
		return ownership.Result{}, fmt.Errorf("%w: aggregate %q could not be re-read (%s)", ErrRepairCandidateStale, f.Rel, f.adapter.diagnosticSummary(diags))
	}

	desiredValues := f.repairDesiredValues()
	fresh, ok := findRepairConflict(ownership.ValueConflictCandidates(state.proof, state.existing, desiredValues), id)
	if !ok ||
		fresh.ExistingValueDigest() != selected.Candidate.ExistingValueDigest() ||
		fresh.DesiredValueDigest() != selected.Candidate.DesiredValueDigest() {
		return ownership.Result{}, fmt.Errorf("%w: %s entry %q in %q no longer presents the difference that was confirmed",
			ErrRepairCandidateStale, selected.EntryKind(), selected.Identity(), f.Rel)
	}

	replacement, resource, ok := f.repairDesiredEntry(id)
	if !ok {
		return ownership.Result{}, fmt.Errorf("%w: %s entry %q in %q is no longer desired",
			ErrRepairCandidateStale, selected.EntryKind(), selected.Identity(), f.Rel)
	}
	next, err := state.replace(id, replacement)
	if err != nil {
		return ownership.Result{}, err
	}
	if bytes.Equal(state.raw, next) {
		return ownership.Result{}, fmt.Errorf("%w: %s entry %q in %q produced no edit",
			ErrRepairCandidateStale, selected.EntryKind(), selected.Identity(), f.Rel)
	}

	entries, err := f.recordedEntries()
	if err != nil {
		return ownership.Result{}, err
	}
	// The replaced entry becomes OURS at the desired digest. That is the whole
	// point: the next sync classifies it clean instead of reporting the same
	// collision, which is what opens the loop.
	entries = append(entries, selected.Candidate.Entry(resource))

	priorGen, priorDigest := ownedAggregatePriorForApply(state.raw, nil, state.proof)
	result, applyErr := f.store.Apply(ownership.Mutation{
		Rel:                   f.Rel,
		AbsFile:               f.AbsFile(),
		NextBytes:             next,
		Entries:               entries,
		Reason:                ownership.ReasonConflictReplace,
		Confirmed:             confirmed,
		ExpectPriorGeneration: priorGen,
		ExpectPriorDigest:     priorDigest,
	})
	if applyErr != nil {
		slog.Error(repairLogMessage(f.Target, "conflict replacement failed; the aggregate was left exactly as found"),
			"target", f.Target, "file", f.Rel, "out_dir", f.OutDir, "kind", selected.EntryKind(), "identity", selected.Identity(),
			"existing_digest", selected.ExistingValueDigest(), "generated_digest", selected.DesiredValueDigest(),
			"error", applyErr, "code", diag.CodeDoctorRepairFailed)
		return ownership.Result{}, applyErr
	}
	slog.Info(repairLogMessage(f.Target, "replaced a conflicting unmanaged entry with the generated entry"),
		"target", f.Target, "file", f.Rel, "out_dir", f.OutDir, "kind", selected.EntryKind(), "identity", selected.Identity(),
		"resource", resource, "replaced_digest", selected.ExistingValueDigest(),
		"generated_digest", selected.DesiredValueDigest(), "generation", result.Generation,
		"backup", result.BackupPath, "code", diag.CodeDoctorRepairConflictReplaceable)
	return result, nil
}

func repairLogMessage(target hcc.Target, action string) string {
	return fmt.Sprintf("%s repair %s", target, action)
}

func (f *RepairFile) repairDesiredEntry(id ownership.EntryID) ([]byte, string, bool) {
	want := ownedAggregateIDKey(id)
	for _, entry := range f.desired {
		if ownedAggregateIDKey(entry.value.ID) == want {
			return entry.raw, entry.resource, true
		}
	}
	return nil, "", false
}

// findRepairConflict looks one identity up in a freshly derived candidate list.
func findRepairConflict(candidates []ownership.ConflictCandidate, id ownership.EntryID) (ownership.ConflictCandidate, bool) {
	want := ownedAggregateIDKey(id)
	for _, candidate := range candidates {
		if ownedAggregateIDKey(candidate.EntryID()) == want {
			return candidate, true
		}
	}
	return ownership.ConflictCandidate{}, false
}

// requireCompleteGeneration refuses every repair mutation while the target
// root's ownership store still holds an in-flight generation.
//
// It is shared by all three repair classes because the hazard is shared: each
// of them ends in ownership.Apply, and Apply writes its journal
// UNCONDITIONALLY, so any of them silently destroys an interrupted generation's
// record. See [ErrRepairGenerationInFlight].
//
// A journal that cannot even be READ (corrupt, or belonging to another root) is
// refused for the same reason, and more strongly: it is the "corrupt state" the
// exit contract reports rather than resolves.
func (f *RepairFile) requireCompleteGeneration() error {
	journal, err := f.store.ReadJournal()
	if err != nil {
		return fmt.Errorf("read ownership journal for %q under %q: %w", f.Rel, f.OutDir, err)
	}
	if journal == nil {
		return nil
	}
	slog.Warn(repairLogMessage(f.Target, "refused: an interrupted ownership generation is still in flight"),
		"target", f.Target, "file", f.Rel, "out_dir", f.OutDir, "in_flight_file", journal.File,
		"in_flight_generation", journal.Generation, "phase", string(journal.Phase),
		"code", diag.CodeDoctorRepairFailed)
	return fmt.Errorf("%w (generation %d for %q is in phase %q)",
		ErrRepairGenerationInFlight, journal.Generation, journal.File, journal.Phase)
}

// recordedEntries returns the entries the durable record currently claims for
// this file.
//
// Every mutation replaces a file's entry set wholesale, so an adoption that
// forgot the already-owned entries would silently disown them. Reading them
// back from the RECORD (rather than re-deriving them from the file) also keeps
// the recorded value digest of a drifted entry intact: repair adopts and
// removes, it never launders drift into cleanliness.
func (f *RepairFile) recordedEntries() ([]ownership.Entry, error) {
	record, err := f.store.Load()
	if err != nil {
		if errors.Is(err, ownership.ErrStateMissing) {
			return nil, nil
		}
		// A corrupt or foreign record must not be overwritten by a repair: that
		// is the "corrupt state" the exit contract reports rather than resolves.
		return nil, err
	}
	fileState, ok := record.Files[f.Rel]
	if !ok {
		return nil, nil
	}
	return append([]ownership.Entry(nil), fileState.Entries...), nil
}
