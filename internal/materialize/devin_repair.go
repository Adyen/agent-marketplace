// Devin aggregate REPAIR primitives (work package D11 of
// docs/plans/2026-08-03-devin-first-class-harness.md).
//
// This file implements the plan's recovery surface:
//
//	"`doctor --repair` previews exact adoption matches and evidence-backed
//	orphan candidates; on a TTY it then prompts for each authorized action"
//	"off-TTY, `doctor --repair` is preview-only"
//	"`doctor --repair --yes` adopts exact current desired matches without prompting"
//	"orphan-removal candidates require non-heuristic retained journal or
//	safety-backup evidence linking the entry to prior managed output, plus
//	explicit per-entry confirmation and a fresh backup"
//
// It also implements the THIRD repair class, which the plan's two classes could
// not describe (bead agent-marketplace-0w2g): an existing unmanaged entry whose
// KEY matches a desired entry but whose VALUE differs. A collision needs only
// key equality, adoption needs value equality, and orphan nomination needs
// retained evidence that only a completed mutation produces — so such an entry
// was reported by every sync and repairable by nothing. See [ConflictCandidate]
// and [RepairFile.ReplaceConflict]; it destroys user-visible content, so it
// inherits orphan removal's contract, not adoption's.
//
// Nothing here decides ownership, exactness, evidence, or difference. Those
// questions are answered exclusively by internal/ownership's
// [ownership.ExactAdoptionCandidates], [ownership.Store.OrphanCandidates],
// [ownership.ValueConflictCandidates], and [ownership.Proof], whose result types
// cannot be constructed outside that package. This file only decides which
// candidates the user authorized and turns an authorized candidate into a
// journaled mutation.
//
// Two structural properties matter more than any check written here:
//
//  1. [DevinRepairPreview] never writes. It opens stores, reads aggregates and
//     ownership records, and returns candidates. There is no mutation path
//     reachable from it, which is what makes "off-TTY, doctor --repair is
//     preview-only" a property of the code rather than of a flag test.
//  2. [RepairFile.RemoveOrphan] and [RepairFile.ReplaceConflict] take a
//     `confirmed` argument they pass straight into
//     ownership.Mutation.Confirmed, and ownership.Apply refuses either
//     unconfirmed with ownership.ErrConfirmationRequired. A caller cannot
//     delete or overwrite a user's entry without ASSERTING per-entry consent;
//     that the assertion corresponds to a real interactive answer is doctor's
//     half of the contract, and the two halves fail independently.
//
// No message, hint, or log attribute in this file carries a hook payload, a
// matcher, a timeout value, an MCP env value, a header, an OAuth secret, or
// aggregate file content. Only aggregate paths (install root plus
// output-relative path, so two aggregates under one root are distinguishable),
// non-secret entry identities (an MCP server object key or a hook event name),
// resource names, digests, and generation numbers are reported.
package materialize

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"

	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

// ErrRepairCandidateStale is returned when an authorized candidate no longer
// matches what the preview showed. The aggregate is left exactly as found: a
// preview the user approved is not authorization for a different edit.
var ErrRepairCandidateStale = errors.New("materialize: repair candidate no longer matches the previewed state")

// ErrRepairGenerationInFlight is returned when a repair action is asked to
// mutate an aggregate whose ownership store still holds an INCOMPLETE
// generation.
//
// internal/ownership's contract is "Recover ... once, before any mutation", and
// sync honours it (see the Store.Recover call in devin.go). The repair path
// cannot: [DevinRepairPreview] is read-only by construction and therefore must
// not recover, and ownership.Apply does not recover either — it simply writes a
// new journal over the old one.
//
// That overwrite is silent and irreversible: the interrupted generation's
// journal is the ONLY remaining description of entries that may already be live
// in the aggregate but whose ownership was never committed. Destroying it turns
// proven-owned entries into unmanaged ones forever, which is exactly the closed
// loop the conflict class exists to open.
//
// So a repair fails closed here and points at the command that DOES recover.
// Refusing costs one `sync`; the alternative costs an ownership record that
// cannot be reconstructed from anything on disk.
var ErrRepairGenerationInFlight = errors.New("materialize: an interrupted ownership generation is still in flight for this target root; run `agent-marketplace sync` to recover it before repairing")

// AdoptionCandidate is one exact adoption match, paired with the marketplace
// resource that desires it.
//
// The embedded ownership candidate is the only thing that establishes
// exactness; Resource is display and record metadata.
type AdoptionCandidate struct {
	Candidate ownership.AdoptionCandidate
	Resource  string
}

// EntryKind returns the non-secret kind of the adopted entry.
func (c AdoptionCandidate) EntryKind() string { return string(c.Candidate.EntryID().Kind) }

// Identity returns the non-secret identity label: an MCP server object key, or
// a hook event name. It is never a matcher or a payload.
func (c AdoptionCandidate) Identity() string { return devinIdentityLabel(c.Candidate.EntryID()) }

// ConflictCandidate is one unmanaged entry whose key matches a desired entry
// but whose VALUE differs, paired with the marketplace resource that desires it.
//
// It is the third repair class. Sync reports it as a collision on every run and
// installs nothing; adoption cannot take it (the values differ) and orphan
// removal cannot take it (no evidence exists, and none ever will, because the
// evidence is produced by a mutation that never happens). Replacing it is
// therefore the only way out of that state — and because replacing DESTROYS a
// value the user wrote, it is gated exactly like an orphan removal, never like
// an adoption. See [RepairFile.ReplaceConflict].
type ConflictCandidate struct {
	Candidate ownership.ConflictCandidate
	Resource  string
}

// EntryKind returns the non-secret kind of the conflicting entry.
func (c ConflictCandidate) EntryKind() string { return string(c.Candidate.EntryID().Kind) }

// Identity returns the non-secret identity label of the conflicting entry: an
// MCP server object key. It is never a command, an env value, or a header.
func (c ConflictCandidate) Identity() string { return devinIdentityLabel(c.Candidate.EntryID()) }

// ExistingValueDigest returns the digest of the value on disk now.
func (c ConflictCandidate) ExistingValueDigest() string { return c.Candidate.ExistingValueDigest() }

// DesiredValueDigest returns the digest of the value marketplace configuration
// wants.
func (c ConflictCandidate) DesiredValueDigest() string { return c.Candidate.DesiredValueDigest() }

// Differs reports that the two values are not the same bytes.
func (c ConflictCandidate) Differs() bool { return c.Candidate.Differs() }

// Difference renders the fact a user needs in order to decide, and nothing
// more: THAT the values differ, plus both digests.
//
// A diff of the two values would be the obvious thing to print and is exactly
// what this must never do: an MCP entry holds env values, headers, and OAuth
// secrets. Digests prove difference without disclosing either side.
func (c ConflictCandidate) Difference() string {
	return fmt.Sprintf("existing value digest %s, generated value digest %s",
		c.Candidate.ExistingValueDigest(), c.Candidate.DesiredValueDigest())
}

// OrphanCandidate is one evidence-backed orphan nomination.
type OrphanCandidate struct {
	Candidate ownership.OrphanCandidate
}

// EntryKind returns the non-secret kind of the nominated entry.
func (c OrphanCandidate) EntryKind() string { return string(c.Candidate.EntryID().Kind) }

// Identity returns the non-secret identity label of the nominated entry.
func (c OrphanCandidate) Identity() string { return devinIdentityLabel(c.Candidate.EntryID()) }

// Evidence renders the recorded, non-heuristic justification for the
// nomination. There is deliberately no "owned" accessor anywhere on this type:
// "orphan candidates are never described as currently owned".
func (c OrphanCandidate) Evidence() string { return c.Candidate.Evidence().Summary() }

// RepairFile is the repair preview for ONE Devin aggregate file, plus the
// handles needed to act on it.
type RepairFile struct {
	Target       hcc.Target
	MachineScope bool
	// OutDir is the canonical Devin install root the aggregate lives under.
	OutDir string
	// Rel is the output-relative aggregate path (hooks.v1.json, config.json, or
	// mcp_config.json).
	Rel string

	Adoptions []AdoptionCandidate
	Orphans   []OrphanCandidate
	Conflicts []ConflictCandidate

	// ExistingEntries and DesiredEntries are the sizes of the two sets the
	// candidates were derived from. They exist so a file with NO candidates can
	// still be reported as INSPECTED: "nothing to repair" has to be a statement
	// about known files, not an unexplained dead end (a beta tester could not
	// tell whether repair had even looked at the aggregate they were staring
	// at). They are counts, never content.
	ExistingEntries int
	DesiredEntries  int

	store   *ownership.Store
	desired []ownedJSONEntry
}

// AbsFile returns the absolute aggregate path.
func (f *RepairFile) AbsFile() string { return filepath.Join(f.OutDir, f.Rel) }

// RepairPreview is the whole read-only repair picture.
type RepairPreview struct {
	Files []*RepairFile
	// Diags are the problems found while previewing: a corrupt ownership
	// record, a malformed aggregate, an unpatchable shape. They carry the same
	// stable codes sync uses, and an error-severity entry is what makes
	// `doctor --repair` exit 1 for "corrupt state/file".
	Diags []diag.Diagnostic
}

// Actionable reports whether anything could be repaired.
func (p *RepairPreview) Actionable() bool {
	for _, file := range p.Files {
		if len(file.Adoptions) > 0 || len(file.Orphans) > 0 || len(file.Conflicts) > 0 {
			return true
		}
	}
	return false
}

// HasErrors reports whether the preview itself found a corrupt state or file.
func (p *RepairPreview) HasErrors() bool {
	for _, d := range p.Diags {
		// diag.Severity counts DOWN: SeverityError is 0, SeverityWarning is 1,
		// SeverityInfo is 2. An equality test is therefore the only correct one
		// here; `>=` would classify every warning as an error.
		if d.Severity == diag.SeverityError {
			return true
		}
	}
	return false
}

// DevinRepairPreview computes exact adoption matches and evidence-backed orphan
// candidates for every Devin aggregate file the current configuration touches.
//
// It is READ-ONLY by construction: it never calls Store.Apply, Store.Recover,
// or Store.BackupOnly, and it never writes a file. That is the whole of
// "off-TTY, `doctor --repair` is preview-only" — the off-TTY path simply stops
// here.
func DevinRepairPreview(ctx context.Context, repoRoot string, resolved []resolver.ResolvedArtifact) (*RepairPreview, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	preview := &RepairPreview{}

	aggregates, keys, diags := devinDesiredAggregates(repoRoot, resolved)
	preview.Diags = append(preview.Diags, diags...)

	roots, ok := harness.MakeInstallRoots()[hcc.TargetDevin]
	if !ok {
		return preview, nil
	}

	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		outDir := roots.For(key.machineScope, repoRoot)
		info, ok := harness.TargetInfoMap[hcc.TargetDevin]
		if !ok {
			continue
		}
		rels := append([]string(nil), info.AggregateFilesForScope(key.machineScope)...)
		sort.Strings(rels)

		desiredByRel := make(map[string][]ownedJSONEntry, len(rels))
		if work := aggregates[key]; work != nil {
			for _, entry := range work.entries {
				desiredByRel[entry.rel] = append(desiredByRel[entry.rel], entry)
			}
		}

		var store *ownership.Store
		for _, rel := range rels {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			abs := filepath.Join(outDir, rel)
			if _, err := os.Lstat(abs); err != nil {
				// No aggregate on disk means there is nothing to adopt and
				// nothing to remove. An absent file is not a repair candidate.
				continue
			}
			if store == nil {
				// The observer is installed on the PREVIEW's store because that
				// is the store the three mutators later act through: a
				// RepairFile carries it. The preview itself takes no lock, so
				// installing one here is also how a test asserts that — a
				// preview that produced a single lock event would be a
				// side-effecting read.
				opened, err := ownership.Open(outDir, ownership.WithLockObserver(observeDevinRepairLock))
				if err != nil {
					preview.Diags = append(preview.Diags, devinDiag(diag.CodeMaterializeDevinAggregateWrite, key, "",
						fmt.Sprintf("open ownership store for %s: %v", outDir, err)))
					break
				}
				store = opened
			}
			file, fileDiags := previewDevinAggregateFile(store, key, outDir, rel, desiredByRel[rel])
			preview.Diags = append(preview.Diags, fileDiags...)
			if file != nil {
				preview.Files = append(preview.Files, file)
			}
		}
	}

	slog.Info("devin repair preview completed",
		"repo_root", repoRoot, "files", len(preview.Files), "diagnostics", len(preview.Diags))
	return preview, nil
}

// previewDevinAggregateFile computes the candidates for one aggregate file.
func previewDevinAggregateFile(store *ownership.Store, key outputKey, outDir, rel string, desired []ownedJSONEntry) (*RepairFile, []diag.Diagnostic) {
	var diags []diag.Diagnostic
	state, stateDiags := readDevinAggregateState(store, key, outDir, rel)
	diags = append(diags, stateDiags...)
	if state == nil {
		return nil, diags
	}

	desiredValues := devinEntryValues(desired)
	adoptions := ownership.ExactAdoptionCandidates(state.proof, state.existingValues, desiredValues)
	resourceByID := make(map[string]string, len(desired))
	for _, entry := range desired {
		resourceByID[devinIDKey(entry.id())] = entry.resource
	}

	file := &RepairFile{
		Target:          key.target,
		MachineScope:    key.machineScope,
		OutDir:          outDir,
		Rel:             rel,
		ExistingEntries: len(state.existingValues),
		DesiredEntries:  len(desiredValues),
		store:           store,
		desired:         desired,
	}
	for _, candidate := range adoptions {
		file.Adoptions = append(file.Adoptions, AdoptionCandidate{
			Candidate: candidate,
			Resource:  resourceByID[devinIDKey(candidate.EntryID())],
		})
	}

	orphans, err := store.OrphanCandidates(state.proof, rel, state.existingValues, desiredValues)
	if err != nil {
		diags = append(diags, devinAggregateCodedDiag(key, outDir, rel, err, diag.CodeMaterializeDevinAggregateWrite))
	}
	for _, candidate := range orphans {
		file.Orphans = append(file.Orphans, OrphanCandidate{Candidate: candidate})
	}

	for _, candidate := range ownership.ValueConflictCandidates(state.proof, state.existingValues, desiredValues) {
		if !devinConflictReplaceable(rel, candidate.EntryID()) {
			continue
		}
		file.Conflicts = append(file.Conflicts, ConflictCandidate{
			Candidate: candidate,
			Resource:  resourceByID[devinIDKey(candidate.EntryID())],
		})
	}

	slog.Debug("devin repair preview for aggregate",
		"target", key.target, "machine_scope", key.machineScope, "file", rel,
		"existing_entries", len(state.existingValues), "desired_entries", len(desiredValues),
		"adoption_candidates", len(file.Adoptions), "orphan_candidates", len(file.Orphans),
		"conflict_candidates", len(file.Conflicts))
	return file, diags
}

// devinConflictReplaceable fences the replace class to the entries whose
// IDENTITY IS THEIR KEY — currently Devin's mcp_config.json members.
//
// This is a deliberate scope boundary and a defense-in-depth guard. With a
// total hook identity (event plus the digest of the canonical hook value), two
// hooks that differ in value also differ in identity, so
// ValueConflictCandidates can never nominate a real hook conflict. The
// MCP-only predicate remains so a bug or future identity change cannot silently
// turn a hook collision into a replacement.
//
// Generalizing later is a matter of widening this predicate plus teaching
// [replaceDevinEntry] the second container shape; nothing else in the class is
// MCP-specific.
func devinConflictReplaceable(rel string, id ownership.EntryID) bool {
	return rel == harness.DevinMcpConfigFile && id.Kind == ownership.KindMcpServer && id.Key != ""
}

// devinAggregateState is the parsed, classified snapshot of one aggregate file.
type devinAggregateState struct {
	raw             []byte
	doc             *jsonc.Document
	container       *jsonc.Node
	containerMember string
	existing        []devinExistingEntry
	existingValues  []ownership.EntryValue
	proof           ownership.Proof
}

// readDevinAggregateState reads and classifies one aggregate file WITHOUT
// mutating anything. It is shared by the preview and by every apply path, so an
// applied action is decided from exactly the same derivation the preview showed.
func readDevinAggregateState(store *ownership.Store, key outputKey, outDir, rel string) (*devinAggregateState, []diag.Diagnostic) {
	var diags []diag.Diagnostic
	abs := filepath.Join(outDir, rel)
	raw, err := os.ReadFile(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, append(diags, devinDiag(diag.CodeMaterializeDevinAggregateWrite, key, rel,
			fmt.Sprintf("read aggregate %q: %v", devinAggregateLabel(outDir, rel), err)))
	}
	doc, parseErr := jsonc.Parse(rel, raw)
	if parseErr != nil {
		// The file is left exactly as found; repair cannot patch what it cannot
		// parse, and a malformed aggregate is a "corrupt state/file" for the
		// exit-code contract.
		return nil, append(diags, devinAggregateCodedDiag(key, outDir, rel, parseErr, diag.CodeMaterializeDevinAggregateMalformed))
	}
	container, containerMember, shapeErr := ownedJSONContainerNode(doc, rel)
	if shapeErr != nil {
		return nil, append(diags, devinShapeDiag(key, outDir, rel, shapeErr))
	}
	state := &devinAggregateState{raw: raw, doc: doc, container: container, containerMember: containerMember}
	if container != nil {
		state.existing, shapeErr = devinExistingEntries(rel, container)
		if shapeErr != nil {
			return nil, append(diags, devinShapeDiag(key, outDir, rel, shapeErr))
		}
	}
	state.existingValues = make([]ownership.EntryValue, 0, len(state.existing))
	for _, entry := range state.existing {
		state.existingValues = append(state.existingValues, entry.value)
	}

	proof, proofErr := store.Proof(rel)
	// Advisory, exactly as in sync: the zero Proof already means "nothing is
	// owned". A record that went away is a warning (nothing is proven, so nothing
	// is adopted or pruned without the user saying so); a corrupt record is an
	// error and drives the exit code; a root that never had a record at all is a
	// debug fact, because a first use has nothing to repair and nothing was lost.
	//
	// This is also why the same warning no longer appears once per aggregate with
	// identical text: devinProofDiags names the aggregate, so one root's
	// config.json and mcp_config.json report distinguishably (and a caller that
	// dedups by message therefore keeps both).
	diags = append(diags, devinProofDiags(key, outDir, rel, proofErr)...)
	state.proof = proof
	return state, diags
}

func devinEntryValues(entries []ownedJSONEntry) []ownership.EntryValue {
	values := make([]ownership.EntryValue, 0, len(entries))
	for _, entry := range entries {
		values = append(values, ownership.EntryValue{ID: entry.id(), ValueDigest: entry.valueDigest()})
	}
	return values
}

// devinRepairLockHook is nil in production and is the seam a test uses to observe
// the lock transitions of a REPAIR action from outside this file.
//
// It exists for the same reason devinLockAcquisitionHook does on the sync side:
// the section BOUNDARY is not observable any other way. A repair whose section
// covers only ownership.Apply and one whose section covers
// requireCompleteGeneration + re-derivation + Apply write identical bytes and
// produce identical errors on an uncontended root; they differ only in how many
// times the lock is taken and at what depth. So the count is the property, and
// counting it needs a hook, because a RepairFile's Store is opened inside
// DevinRepairPreview and a test cannot pass an option into it.
//
// A test that installs it must not run in parallel with another that does.
var devinRepairLockHook func(ownership.LockEvent)

func observeDevinRepairLock(ev ownership.LockEvent) {
	if devinRepairLockHook != nil {
		devinRepairLockHook(ev)
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
// section here cannot trip it. TestDevinRepairSectionIsOneAcquisitionPerAction
// pins both the acquisition count and the depth.
//
// # The lock is never held across a human prompt
//
// The section starts inside the action and ends when the action returns.
// cmd/agent-marketplace/doctor.go prompts BETWEEN calls, so a human sitting at a
// y/N never holds anything: one answer, one acquisition, one journaled
// generation, one release. That matters most for the machine-scope root
// ~/.config/devin, whose lock would otherwise block every sync in every repo on
// the machine for as long as the human takes to answer.
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
	state, diags := readDevinAggregateState(f.store, key, f.OutDir, f.Rel)
	if state == nil {
		return ownership.Result{}, fmt.Errorf("%w: aggregate %q could not be re-read (%s)", ErrRepairCandidateStale, f.Rel, devinDiagSummary(diags))
	}

	desiredValues := devinEntryValues(f.desired)
	current := ownership.ExactAdoptionCandidates(state.proof, state.existingValues, desiredValues)
	currentByID := make(map[string]ownership.AdoptionCandidate, len(current))
	for _, candidate := range current {
		currentByID[devinIDKey(candidate.EntryID())] = candidate
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
		id := devinIDKey(selected.Candidate.EntryID())
		fresh, ok := currentByID[id]
		if !ok || fresh.ValueDigest() != selected.Candidate.ValueDigest() {
			// The entry changed between preview and confirmation. Adopting it
			// now would record ownership of bytes the user never saw described.
			return ownership.Result{}, fmt.Errorf("%w: %s entry %q in %q", ErrRepairCandidateStale,
				selected.EntryKind(), selected.Identity(), f.Rel)
		}
		entries = append(entries, fresh.Entry(selected.Resource))
	}

	priorGen, priorDigest := devinPriorForApply(state.raw, nil, state.proof)
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
		slog.Error("devin repair adoption failed; the aggregate was left exactly as found",
			"file", f.Rel, "out_dir", f.OutDir, "adopted", len(indices), "error", err,
			"code", diag.CodeDoctorRepairFailed)
		return ownership.Result{}, err
	}
	slog.Info("devin repair adopted exact matches",
		"file", f.Rel, "out_dir", f.OutDir, "adopted", len(indices),
		"owned_entries", len(entries), "generation", result.Generation, "backup", result.BackupPath)
	return result, nil
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
	state, diags := readDevinAggregateState(f.store, key, f.OutDir, f.Rel)
	if state == nil || state.container == nil {
		return ownership.Result{}, fmt.Errorf("%w: aggregate %q could not be re-read (%s)", ErrRepairCandidateStale, f.Rel, devinDiagSummary(diags))
	}

	// Re-nominate from the CURRENT file and the CURRENT evidence. A candidate
	// that stopped being nominated between preview and confirmation is refused:
	// the evidence, not the user's earlier "yes", is what authorizes a delete.
	desiredValues := devinEntryValues(f.desired)
	fresh, err := f.store.OrphanCandidates(state.proof, f.Rel, state.existingValues, desiredValues)
	if err != nil {
		return ownership.Result{}, err
	}
	wanted := devinIDKey(selected.Candidate.EntryID())
	found := false
	for _, candidate := range fresh {
		if devinIDKey(candidate.EntryID()) == wanted && candidate.ValueDigest() == selected.Candidate.ValueDigest() {
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
	// shown. See [removeDevinEntry].
	if err := removeDevinEntry(state, f.Rel, selected.Candidate.EntryID(), selected.Candidate.ValueDigest()); err != nil {
		return ownership.Result{}, err
	}
	if !state.doc.Changed() {
		return ownership.Result{}, fmt.Errorf("%w: %s entry %q in %q produced no edit",
			ErrRepairCandidateStale, selected.EntryKind(), selected.Identity(), f.Rel)
	}

	entries, err := f.recordedEntries()
	if err != nil {
		return ownership.Result{}, err
	}
	priorGen, priorDigest := devinPriorForApply(state.raw, nil, state.proof)
	result, applyErr := f.store.Apply(ownership.Mutation{
		Rel:                   f.Rel,
		AbsFile:               f.AbsFile(),
		NextBytes:             state.doc.Bytes(),
		Entries:               entries,
		Reason:                ownership.ReasonOrphanRemoval,
		Confirmed:             confirmed,
		ExpectPriorGeneration: priorGen,
		ExpectPriorDigest:     priorDigest,
	})
	if applyErr != nil {
		slog.Error("devin repair orphan removal failed; the aggregate was left exactly as found",
			"file", f.Rel, "out_dir", f.OutDir, "kind", selected.EntryKind(), "identity", selected.Identity(),
			"error", applyErr, "code", diag.CodeDoctorRepairFailed)
		return ownership.Result{}, applyErr
	}
	slog.Info("devin repair removed an evidence-backed orphan entry",
		"file", f.Rel, "out_dir", f.OutDir, "kind", selected.EntryKind(), "identity", selected.Identity(),
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
	if !devinConflictReplaceable(f.Rel, id) {
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
	state, diags := readDevinAggregateState(f.store, key, f.OutDir, f.Rel)
	if state == nil || state.container == nil {
		return ownership.Result{}, fmt.Errorf("%w: aggregate %q could not be re-read (%s)", ErrRepairCandidateStale, f.Rel, devinDiagSummary(diags))
	}

	desiredValues := devinEntryValues(f.desired)
	fresh, ok := findDevinConflict(ownership.ValueConflictCandidates(state.proof, state.existingValues, desiredValues), id)
	if !ok ||
		fresh.ExistingValueDigest() != selected.Candidate.ExistingValueDigest() ||
		fresh.DesiredValueDigest() != selected.Candidate.DesiredValueDigest() {
		return ownership.Result{}, fmt.Errorf("%w: %s entry %q in %q no longer presents the difference that was confirmed",
			ErrRepairCandidateStale, selected.EntryKind(), selected.Identity(), f.Rel)
	}

	replacement, ok := devinDesiredEntry(f.desired, id)
	if !ok {
		return ownership.Result{}, fmt.Errorf("%w: %s entry %q in %q is no longer desired",
			ErrRepairCandidateStale, selected.EntryKind(), selected.Identity(), f.Rel)
	}
	if err := replaceDevinEntry(state, f.Rel, id, replacement.raw); err != nil {
		return ownership.Result{}, err
	}
	if !state.doc.Changed() {
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
	entries = append(entries, selected.Candidate.Entry(replacement.resource))

	priorGen, priorDigest := devinPriorForApply(state.raw, nil, state.proof)
	result, applyErr := f.store.Apply(ownership.Mutation{
		Rel:                   f.Rel,
		AbsFile:               f.AbsFile(),
		NextBytes:             state.doc.Bytes(),
		Entries:               entries,
		Reason:                ownership.ReasonConflictReplace,
		Confirmed:             confirmed,
		ExpectPriorGeneration: priorGen,
		ExpectPriorDigest:     priorDigest,
	})
	if applyErr != nil {
		slog.Error("devin repair conflict replacement failed; the aggregate was left exactly as found",
			"file", f.Rel, "out_dir", f.OutDir, "kind", selected.EntryKind(), "identity", selected.Identity(),
			"existing_digest", selected.ExistingValueDigest(), "generated_digest", selected.DesiredValueDigest(),
			"error", applyErr, "code", diag.CodeDoctorRepairFailed)
		return ownership.Result{}, applyErr
	}
	slog.Info("devin repair replaced a conflicting unmanaged entry with the generated entry",
		"file", f.Rel, "out_dir", f.OutDir, "kind", selected.EntryKind(), "identity", selected.Identity(),
		"resource", replacement.resource, "replaced_digest", selected.ExistingValueDigest(),
		"generated_digest", selected.DesiredValueDigest(), "generation", result.Generation,
		"backup", result.BackupPath, "code", diag.CodeDoctorRepairConflictReplaceable)
	return result, nil
}

// findDevinConflict looks one identity up in a freshly derived candidate list.
func findDevinConflict(candidates []ownership.ConflictCandidate, id ownership.EntryID) (ownership.ConflictCandidate, bool) {
	want := devinIDKey(id)
	for _, candidate := range candidates {
		if devinIDKey(candidate.EntryID()) == want {
			return candidate, true
		}
	}
	return ownership.ConflictCandidate{}, false
}

// devinDesiredEntry returns the desired entry for one identity, which carries
// the canonical bytes a replacement writes.
func devinDesiredEntry(desired []ownedJSONEntry, id ownership.EntryID) (ownedJSONEntry, bool) {
	want := devinIDKey(id)
	for _, entry := range desired {
		if devinIDKey(entry.id()) == want {
			return entry, true
		}
	}
	return ownedJSONEntry{}, false
}

// replaceDevinEntry overwrites the value of ONE existing member through jsonc,
// so every other member — and every comment — keeps its exact bytes.
//
// It handles only the keyed MCP shape, which is the same fence
// [devinConflictReplaceable] applies; the error is what a future generalization
// would replace with the hook-array branch.
func replaceDevinEntry(state *devinAggregateState, rel string, id ownership.EntryID, raw []byte) error {
	if rel != harness.DevinMcpConfigFile || id.Key == "" {
		return fmt.Errorf("aggregate %q has no replaceable keyed entry for this identity", rel)
	}
	if _, ok := state.container.Member(id.Key); !ok {
		return fmt.Errorf("aggregate %q no longer has member %q", rel, id.Key)
	}
	if !state.doc.SetMember(state.container, id.Key, raw) {
		return fmt.Errorf("aggregate %q could not replace member %q", rel, id.Key)
	}
	return nil
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
	slog.Warn("devin repair refused: an interrupted ownership generation is still in flight",
		"file", f.Rel, "out_dir", f.OutDir, "in_flight_file", journal.File,
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

// devinExistingEntryMatches reports whether the aggregate, as read into state,
// still holds an entry with this identity AT the confirmed value digest.
func devinExistingEntryMatches(state *devinAggregateState, id ownership.EntryID, valueDigest string) bool {
	want := devinIDKey(id)
	for _, entry := range state.existing {
		if devinIDKey(entry.value.ID) == want && entry.value.ValueDigest == valueDigest {
			return true
		}
	}
	return false
}

// removeDevinEntry deletes one entry from the parsed document through jsonc, so
// every untouched member keeps its exact bytes, its comments, and its position.
//
// # Why the value digest is a parameter and not an optimization
//
// valueDigest is the digest the user's confirmation was taken against, and
// matching on it is what makes this function delete the element that was
// DESCRIBED rather than merely one that shares an identity.
//
// [ownership.EntryID] carries no value. With a total hook identity (event plus
// the digest of the canonical hook value), two different values never share an
// identity, so identity alone would already distinguish the described element.
// The value digest is still required for the case where two BYTE-IDENTICAL hooks
// share the same identity and the user confirmed exactly one of them.
//
// Nomination enforces the same contract: ownership.Store.OrphanCandidates
// requires the retained evidence to record the same identity AND the same value
// digest, and [RepairFile.removeOrphanLocked] re-checks both before calling
// here. Matching on identity alone would remove the first element with the
// identity, which for the layout AGENTS.md documents ("handwritten hooks
// precede generated ones") can be the wrong one when the confirmed element is not
// first.
//
// Two BYTE-IDENTICAL hooks still share both digests, so "exactly one element per
// confirmation" is unchanged for the case it was written for.
func removeDevinEntry(state *devinAggregateState, rel string, id ownership.EntryID, valueDigest string) error {
	if rel == harness.DevinMcpConfigFile {
		// An MCP member is identified by its KEY, so there is never more than one
		// candidate element — but the digest is still checked, because a delete
		// that cannot state which bytes it is deleting is the defect above.
		if !devinExistingEntryMatches(state, id, valueDigest) {
			return fmt.Errorf("aggregate %q member %q no longer holds the confirmed value", rel, id.Key)
		}
		if !state.doc.DeleteMember(state.container, id.Key) {
			return fmt.Errorf("aggregate %q could not delete member %q", rel, id.Key)
		}
		return nil
	}
	node, ok := state.container.Member(id.Container)
	if !ok || node.Kind() != jsonc.KindArray {
		return fmt.Errorf("aggregate %q has no %q hook array to edit", rel, id.Container)
	}
	var kept [][]byte
	removed := 0
	for _, entry := range state.existing {
		if entry.container != id.Container {
			continue
		}
		if removed == 0 && devinIDKey(entry.value.ID) == devinIDKey(id) && entry.value.ValueDigest == valueDigest {
			// Exactly one element is removed per confirmation, even if the file
			// happens to contain two byte-identical hooks: the user confirmed
			// one entry.
			removed++
			continue
		}
		kept = append(kept, entry.raw)
	}
	if removed == 0 {
		return fmt.Errorf("aggregate %q hook entry for %q at the confirmed value was not found", rel, id.Container)
	}
	if len(kept) == 0 {
		if !state.doc.DeleteMember(state.container, id.Container) {
			return fmt.Errorf("aggregate %q could not delete hook event %q", rel, id.Container)
		}
		return nil
	}
	if !state.doc.SetElements(node, kept) {
		return fmt.Errorf("aggregate %q could not rewrite hook event %q", rel, id.Container)
	}
	return nil
}

// devinDesiredAggregates compiles the resolved selection and returns the DESIRED
// Devin aggregate entries per output key, without touching disk.
//
// It reuses the same buildWorkItems/compileWorkItems phases sync uses, filtered
// to the Devin target, so the desired set a repair compares against is the same
// desired set sync would write. Deriving it a second, independent way would
// make "exact match" mean two different things in two commands.
func devinDesiredAggregates(repoRoot string, resolved []resolver.ResolvedArtifact) (map[outputKey]*ownedJSONAggregateWork, []outputKey, []diag.Diagnostic) {
	// The fourth return is the whole-run diagnostic list (nil-resource warnings
	// for every target). It is dropped rather than reported: those warnings
	// belong to sync, and repeating a Claude resource's warning in a Devin
	// repair preview would be noise the user cannot act on here.
	work, allOutputs, blocked, _ := buildWorkItems(resolved, repoRoot)

	devinWork := work[:0:0]
	for _, item := range work {
		if item.target == hcc.TargetDevin {
			devinWork = append(devinWork, item)
		}
	}
	devinOutputs := make(map[outputKey]bool)
	for key := range allOutputs {
		if key.target == hcc.TargetDevin {
			devinOutputs[key] = true
		}
	}
	devinBlocked := make(blockedOutputs)
	for key, components := range blocked {
		if key.target != hcc.TargetDevin {
			continue
		}
		devinBlocked.mark(key)
		for component := range components {
			devinBlocked.mark(key, component)
		}
	}

	compiled := compileWorkItems(devinWork, nil, devinOutputs, devinBlocked, repoRoot)

	keys := make([]outputKey, 0, len(devinOutputs))
	for key := range devinOutputs {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].machineScope != keys[j].machineScope {
			return !keys[i].machineScope
		}
		return string(keys[i].target) < string(keys[j].target)
	})

	return compiled.devinAggregates, keys, compiled.allDiags
}
