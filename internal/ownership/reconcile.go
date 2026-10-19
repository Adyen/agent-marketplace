package ownership

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// EntryValue pairs an entry identity with the digest of its current bytes. It is
// the input shape for adoption and orphan analysis: callers derive it from the
// aggregate they just parsed with pkg/harness-compilation/jsonc.
//
// It carries a DIGEST, not bytes, so no analysis primitive can be handed a
// payload it might then log or record.
type EntryValue struct {
	ID          EntryID
	ValueDigest string
}

// DriftReport is the per-file classification D10 needs before patching.
type DriftReport struct {
	File string
	// Clean, Drifted, and Unmanaged partition the EXISTING entries of the file.
	Clean     []EntryID
	Drifted   []EntryID
	Unmanaged []EntryID
	// Stale are proven owned entries that are no longer desired. Only these may
	// be pruned: "Stale proven owned entries are pruned", and an unmanaged entry
	// is never stale because it was never ours.
	Stale []EntryID
	// Collisions are desired MCP keys that already exist unmanaged. "An unmanaged
	// MCP key collision is preserved and causes only the generated entry to
	// fail" (diag code ownership.unmanaged-key).
	Collisions []EntryID
}

// Classify produces the drift report for one file.
//
// proof is the ONLY ownership input. Passing the zero Proof (missing or corrupt
// state) puts every existing entry in Unmanaged, leaves Stale empty, and reports
// every desired key that already exists as a collision — exactly the plan's
// "Missing or corrupt ownership state makes all existing aggregate entries
// unmanaged" and "Without proof of ownership, sync preserves existing entries and
// refuses collisions or pruning".
func Classify(proof Proof, existing, desired []EntryValue) DriftReport {
	report := DriftReport{File: proof.file}
	desiredByID := make(map[string]EntryValue, len(desired))
	for _, d := range desired {
		desiredByID[d.ID.mapKey()] = d
	}
	existingByID := make(map[string]EntryValue, len(existing))

	for _, e := range existing {
		existingByID[e.ID.mapKey()] = e
		switch proof.Classify(e.ID, e.ValueDigest) {
		case StatusOwnedClean:
			report.Clean = append(report.Clean, e.ID)
		case StatusOwnedDrifted:
			report.Drifted = append(report.Drifted, e.ID)
			slog.Warn("owned aggregate entry drifted and will be replaced from marketplace configuration",
				"file", proof.file, "kind", string(e.ID.Kind), "container", e.ID.Container,
				"key", e.ID.Key, "identity_digest", e.ID.IdentityDigest, "code", diag.CodeOwnershipEntryDrift)
		default:
			report.Unmanaged = append(report.Unmanaged, e.ID)
		}
	}

	for _, id := range proof.OwnedIDs() {
		if _, stillDesired := desiredByID[id.mapKey()]; stillDesired {
			continue
		}
		if _, stillPresent := existingByID[id.mapKey()]; !stillPresent {
			// Already gone from the file; nothing to prune.
			continue
		}
		report.Stale = append(report.Stale, id)
	}

	for _, d := range desired {
		existingValue, present := existingByID[d.ID.mapKey()]
		if !present {
			continue
		}
		if proof.Classify(d.ID, existingValue.ValueDigest) == StatusUnmanaged {
			report.Collisions = append(report.Collisions, d.ID)
			slog.Warn("desired aggregate entry collides with an unmanaged entry; the existing entry is preserved and the generated entry fails",
				"file", proof.file, "kind", string(d.ID.Kind), "container", d.ID.Container,
				"key", d.ID.Key, "code", diag.CodeOwnershipUnmanagedKey)
		}
	}

	sortIDs(report.Clean)
	sortIDs(report.Drifted)
	sortIDs(report.Unmanaged)
	sortIDs(report.Stale)
	sortIDs(report.Collisions)
	return report
}

func sortIDs(ids []EntryID) {
	sort.Slice(ids, func(i, j int) bool { return ids[i].mapKey() < ids[j].mapKey() })
}

// AdoptionCandidate is an unmanaged entry that matches a desired entry EXACTLY.
//
// Its fields are unexported so a caller cannot assemble one: the only way to get
// a candidate is [ExactAdoptionCandidates], which requires digest equality. That
// is what "previews exact adoption matches" and "adopts exact current desired
// matches" mean operationally — there is no near-match, no normalization pass,
// and no similarity threshold anywhere in this package.
type AdoptionCandidate struct {
	file        string
	id          EntryID
	valueDigest string
}

// File returns the output-relative file the candidate lives in.
func (c AdoptionCandidate) File() string { return c.file }

// EntryID returns the candidate's identity.
func (c AdoptionCandidate) EntryID() EntryID { return c.id }

// ValueDigest returns the digest both the existing and the desired entry share.
func (c AdoptionCandidate) ValueDigest() string { return c.valueDigest }

// Entry builds the ownership entry that adopting this candidate records.
func (c AdoptionCandidate) Entry(resource string) Entry {
	return Entry{
		Kind:           c.id.Kind,
		Container:      c.id.Container,
		Key:            c.id.Key,
		IdentityDigest: c.id.IdentityDigest,
		ValueDigest:    c.valueDigest,
		Resource:       resource,
	}
}

// ExactAdoptionCandidates returns the entries that `doctor --repair` may adopt:
// present in the file, NOT proven owned, and byte-identical to the desired
// entry. Anything else — a same-key entry with different content, a similar
// hook, an entry the caller merely believes is ours — is not returned at all.
func ExactAdoptionCandidates(proof Proof, existing, desired []EntryValue) []AdoptionCandidate {
	desiredByID := make(map[string]EntryValue, len(desired))
	for _, d := range desired {
		desiredByID[d.ID.mapKey()] = d
	}
	var out []AdoptionCandidate
	for _, e := range existing {
		if proof.Classify(e.ID, e.ValueDigest) != StatusUnmanaged {
			continue
		}
		want, ok := desiredByID[e.ID.mapKey()]
		if !ok || want.ValueDigest != e.ValueDigest {
			continue
		}
		out = append(out, AdoptionCandidate{file: proof.file, id: e.ID, valueDigest: e.ValueDigest})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id.mapKey() < out[j].id.mapKey() })
	return out
}

// ConflictCandidate is an existing UNMANAGED entry whose identity matches a
// desired entry but whose VALUE differs.
//
// It is the third repair class, and it exists because the first two provably
// cannot describe this state:
//
//   - [Classify] reports it as a collision, because a collision needs only KEY
//     equality. Sync therefore preserves the existing entry and fails only the
//     generated one, on every run, forever.
//   - [ExactAdoptionCandidates] skips it, because adoption additionally requires
//     value-digest equality. Adopting a DIFFERENT value would record ownership
//     of bytes the marketplace never wrote.
//   - [Store.OrphanCandidates] skips it, because orphan nomination needs
//     retained backup or journal evidence — and that evidence is only ever
//     produced by [Store.Apply], which never runs for a colliding entry.
//
// So the entry sits in a gap: reported forever, repairable by nothing. This type
// closes the gap by naming the entry, stating that the two values DIFFER, and
// carrying both digests — never either value.
//
// Its fields are unexported and there is no literal constructor outside this
// package: the only way to obtain one is [ValueConflictCandidates], which
// requires the zero-or-unmanaged verdict from [Proof] plus digest INEQUALITY.
// There is deliberately no accessor that returns a value, and none that
// describes the candidate as owned.
//
// Acting on a candidate DESTROYS user-visible content, so it inherits orphan
// removal's safety contract rather than adoption's: see [ReasonConflictReplace].
//
// Unlike [AdoptionCandidate] it carries no file path, and that is deliberate: a
// candidate only exists when the entry is unmanaged, which includes the case
// where there is NO ownership record at all — and a zero [Proof] names no file.
// An accessor that returned "" exactly in the situation this class was built for
// would be a trap, so the file stays where it is actually known: with the caller
// that read the aggregate.
type ConflictCandidate struct {
	id             EntryID
	existingDigest string
	desiredDigest  string
}

// EntryID returns the candidate's identity.
func (c ConflictCandidate) EntryID() EntryID { return c.id }

// ExistingValueDigest returns the digest of the entry as it exists now.
func (c ConflictCandidate) ExistingValueDigest() string { return c.existingDigest }

// DesiredValueDigest returns the digest of the entry the configuration wants.
func (c ConflictCandidate) DesiredValueDigest() string { return c.desiredDigest }

// Differs reports that the existing and desired values are not the same bytes.
// It is true for every candidate by construction; it exists so a preview can
// state the fact without either digest having to be interpreted by the caller.
func (c ConflictCandidate) Differs() bool { return c.existingDigest != c.desiredDigest }

// Entry builds the ownership entry that REPLACING this candidate records. The
// recorded value digest is the DESIRED one, because replacing installs the
// generated entry: that is what makes the next sync see a proven clean entry
// instead of the same collision.
func (c ConflictCandidate) Entry(resource string) Entry {
	return Entry{
		Kind:           c.id.Kind,
		Container:      c.id.Container,
		Key:            c.id.Key,
		IdentityDigest: c.id.IdentityDigest,
		ValueDigest:    c.desiredDigest,
		Resource:       resource,
	}
}

// ValueConflictCandidates returns the entries `doctor --repair` may offer to
// REPLACE: present in the file, NOT proven owned, matching a desired entry's
// identity, and carrying a DIFFERENT value.
//
// The three exclusions are what keep the classes disjoint: an exact match is
// adoption's (byte-neutral, batchable), an undesired entry is orphan removal's
// (evidence-backed), and a proven owned entry is drift, which sync already
// replaces without asking. Only the unmanaged same-identity different-value
// entry is returned here.
func ValueConflictCandidates(proof Proof, existing, desired []EntryValue) []ConflictCandidate {
	desiredByID := make(map[string]EntryValue, len(desired))
	for _, d := range desired {
		desiredByID[d.ID.mapKey()] = d
	}
	var out []ConflictCandidate
	for _, e := range existing {
		if proof.Classify(e.ID, e.ValueDigest) != StatusUnmanaged {
			continue
		}
		want, ok := desiredByID[e.ID.mapKey()]
		if !ok || want.ValueDigest == e.ValueDigest {
			continue
		}
		out = append(out, ConflictCandidate{
			id:             e.ID,
			existingDigest: e.ValueDigest,
			desiredDigest:  want.ValueDigest,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id.mapKey() < out[j].id.mapKey() })
	slog.Debug("unmanaged value-conflict nomination completed",
		"file", proof.file, "existing", len(existing), "desired", len(desired), "nominated", len(out))
	return out
}

// EvidenceKind names where retained evidence came from.
type EvidenceKind string

const (
	// EvidenceSafetyBackup is a retained safety-backup manifest recording that
	// the generation it was taken for claimed this exact entry.
	EvidenceSafetyBackup EvidenceKind = "safety-backup"
	// EvidenceRetainedJournal is an in-flight journal whose ownership generation
	// claims this exact entry.
	EvidenceRetainedJournal EvidenceKind = "retained-journal"
)

// Evidence is a recorded, non-heuristic link between an aggregate entry and
// prior managed output.
//
// Its fields are unexported and it is only ever constructed inside
// [Store.OrphanCandidates] from a retained safety-backup manifest or from the
// journal. A caller therefore cannot manufacture the justification for deleting
// a user's entry, which is the structural form of "entries without such evidence
// are never nominated; users must edit them manually".
type Evidence struct {
	kind       EvidenceKind
	source     string
	generation uint64
	// recordedValueDigest is the value digest the evidence recorded. Orphan
	// nomination requires the CURRENT entry to still match it exactly.
	recordedValueDigest string
	resource            string
	store               *Store
	directory           string
	metadataName        string
	observed            metadataPin
}

// Kind returns the evidence kind.
func (e Evidence) Kind() EvidenceKind { return e.kind }

// Source returns the path of the retained artifact the evidence came from.
func (e Evidence) Source() string { return e.source }

// Generation returns the ownership generation the evidence belongs to.
func (e Evidence) Generation() uint64 { return e.generation }

// Resource returns the marketplace resource the evidence attributes the entry to.
func (e Evidence) Resource() string { return e.resource }

// Summary renders the evidence for a repair preview. It contains only the
// evidence kind, the retained artifact path, the generation, and the resource
// name.
func (e Evidence) Summary() string {
	return fmt.Sprintf("%s generation %d (%s) attributed to resource %q", e.kind, e.generation, e.source, e.resource)
}

// OrphanCandidate is an entry that is NOT currently owned but that retained
// evidence links to prior managed output.
//
// There is deliberately no Owned() or Status() accessor: "orphan candidates are
// never described as currently owned". The type can only ever say "here is the
// evidence"; the decision is the user's, per entry.
type OrphanCandidate struct {
	file        string
	id          EntryID
	valueDigest string
	evidence    Evidence
}

// File returns the output-relative file the candidate lives in.
func (c OrphanCandidate) File() string { return c.file }

// EntryID returns the candidate's identity.
func (c OrphanCandidate) EntryID() EntryID { return c.id }

// ValueDigest returns the digest of the entry as it exists now.
func (c OrphanCandidate) ValueDigest() string { return c.valueDigest }

// Evidence returns the recorded justification for nominating the entry.
func (c OrphanCandidate) Evidence() Evidence { return c.evidence }

// OrphanCandidates nominates entries for evidence-backed removal.
//
// An entry is nominated only when ALL of these hold:
//
//  1. it exists in the file now;
//  2. it is NOT proven owned (an owned entry is not an orphan, it is either
//     clean, drifted, or stale);
//  3. it is not desired by the current configuration;
//  4. a retained safety-backup manifest or the journal records the SAME identity
//     with the SAME value digest.
//
// Condition 4 is what makes removal evidence-backed rather than heuristic: the
// entry has to be byte-identical to something this package recorded writing. A
// hand-edited former-managed entry therefore fails condition 4 and is never
// nominated — the user must edit it manually, which is what the plan requires.
func (s *Store) OrphanCandidates(proof Proof, rel string, existing, desired []EntryValue) ([]OrphanCandidate, error) {
	desiredByID := make(map[string]struct{}, len(desired))
	for _, d := range desired {
		desiredByID[d.ID.mapKey()] = struct{}{}
	}

	evidence, err := s.retainedEvidence(rel)
	if err != nil {
		return nil, err
	}

	var out []OrphanCandidate
	for _, e := range existing {
		if proof.Classify(e.ID, e.ValueDigest) != StatusUnmanaged {
			continue
		}
		if _, wanted := desiredByID[e.ID.mapKey()]; wanted {
			continue
		}
		ev, ok := evidence[e.ID.mapKey()]
		if !ok || ev.recordedValueDigest != e.ValueDigest {
			continue
		}
		out = append(out, OrphanCandidate{file: rel, id: e.ID, valueDigest: e.ValueDigest, evidence: ev})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id.mapKey() < out[j].id.mapKey() })
	slog.Info("evidence-backed orphan nomination completed",
		"root_key", s.rootKey, "file", rel, "existing", len(existing), "nominated", len(out),
		"evidence_records", len(evidence))
	return out, nil
}

// retainedEvidence indexes every retained record that could justify an orphan
// nomination, newest generation winning. The two sources are exactly the ones
// the plan names.
func (s *Store) retainedEvidence(rel string) (map[string]Evidence, error) {
	index := make(map[string]Evidence)
	// The newest generation wins. On a tie the journal wins, because it is
	// consulted last and describes an in-flight generation: if both a retained
	// backup manifest and the journal record generation N, the journal is the
	// more precise statement of what that generation was about to own.
	add := func(ev Evidence, id EntryID) {
		key := id.mapKey()
		if prev, ok := index[key]; ok && prev.generation > ev.generation {
			return
		}
		index[key] = ev
	}

	backups, err := s.SafetyBackups(rel)
	if err != nil {
		return nil, err
	}
	for _, b := range backups {
		for _, e := range b.Manifest.ManagedEntries {
			entry := e
			if entry.Kind == KindHook {
				entry.IdentityDigest = HookIdentityDigest(entry.Container, entry.ValueDigest)
			}
			add(Evidence{
				kind:                EvidenceSafetyBackup,
				source:              b.Path,
				generation:          b.Manifest.Generation,
				recordedValueDigest: entry.ValueDigest,
				resource:            entry.Resource,
				store:               s,
				directory:           b.relative,
				metadataName:        filepath.Base(metaPathFor(b.Path)),
				observed:            b.manifestObserved,
			}, entry.ID())
		}
	}

	journal, err := s.ReadJournal()
	if err != nil {
		slog.Warn("skipping journal evidence: the journal is unreadable",
			"root_key", s.rootKey, "file", rel, "err", err)
	}
	if err == nil && journal != nil && journal.File == rel && journal.NextRecord != nil {
		if fs, ok := journal.NextRecord.Files[rel]; ok {
			for _, e := range fs.Entries {
				entry := e
				if entry.Kind == KindHook {
					entry.IdentityDigest = HookIdentityDigest(entry.Container, entry.ValueDigest)
				}
				add(Evidence{
					kind:                EvidenceRetainedJournal,
					source:              s.JournalFile(),
					generation:          journal.Generation,
					recordedValueDigest: entry.ValueDigest,
					resource:            entry.Resource,
					store:               s,
					directory:           filepath.Join(ownershipDirName, s.rootKey),
					metadataName:        journalFileName,
					observed:            journal.observed,
				}, entry.ID())
			}
		}
	}
	return index, nil
}
