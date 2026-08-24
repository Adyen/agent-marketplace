package ownership

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// Phase is the recorded phase of an in-flight generation.
//
// "Each mutation uses a durable transaction journal with non-secret digests and
// explicit phases: prepare after safety backup, atomically rename the patched
// aggregate, atomically commit the matching ownership generation, then clear the
// journal. On startup, incomplete generations are replayed or rolled back
// according to the recorded phase" (plan, "Journaled generations").
type Phase string

const (
	// PhasePrepared means the safety backup exists and the journal is durable.
	// The aggregate may or may not have been renamed yet; which one it is, is
	// decided by comparing the live file against the digests the journal
	// RECORDED, never by inspecting what the entries look like.
	PhasePrepared Phase = "prepared"
	// PhaseAggregateRenamed means the patched aggregate is live and the matching
	// ownership generation still has to be committed.
	PhaseAggregateRenamed Phase = "aggregate-renamed"
)

const (
	journalVersion       = 2
	legacyJournalVersion = 1
)

// Journal is the durable record of one in-flight generation. Like [Entry], it
// carries digests and identities only: "a durable transaction journal with
// non-secret digests".
type Journal struct {
	Version       int    `json:"version"`
	RootKey       string `json:"root_key"`
	CanonicalRoot string `json:"canonical_root"`
	Generation    uint64 `json:"generation"`
	Phase         Phase  `json:"phase"`
	Reason        Reason `json:"reason"`
	// File is the output-relative aggregate path; AbsFile is where it lives.
	File    string `json:"file"`
	AbsFile string `json:"abs_file"`
	// StagedPath is the fully written, fsynced replacement waiting to be renamed.
	StagedPath string `json:"staged_path"`
	// StagingDir is the private same-filesystem directory containing StagedPath.
	// It is optional for backwards compatibility with version-1 journals written
	// before private staging directories were introduced.
	StagingDir string `json:"staging_dir,omitempty"`
	// BackupPath is the mandatory raw-byte safety backup for this generation.
	BackupPath string `json:"backup_path"`
	// PriorDigest/PriorAbsent describe the aggregate BEFORE the mutation.
	PriorDigest string `json:"prior_digest"`
	PriorAbsent bool   `json:"prior_absent"`
	// NextDigest is the digest of the staged replacement.
	NextDigest string `json:"next_digest"`
	// NextRecord is the ownership generation to commit once the rename is known
	// to have happened.
	NextRecord *Record `json:"next_record"`
	UpdatedAt  string  `json:"updated_at"`
}

// Mutation is one aggregate write plus the ownership it claims.
type Mutation struct {
	// Rel is the canonical output-relative path, slash separated (for example
	// "hooks.v1.json" or "mcp_config.json").
	Rel string
	// AbsFile is the absolute path of the aggregate on disk. It must live under
	// the store's canonical target root.
	AbsFile string
	// NextBytes is the fully patched file content, produced by
	// pkg/harness-compilation/jsonc so comments and key order survive.
	NextBytes []byte
	// Entries are the entries the new content claims to own. An empty slice
	// removes the file's ownership record entirely.
	Entries []Entry
	Reason  Reason
	// Confirmed must be set for every DESTRUCTIVE reason — [ReasonOrphanRemoval]
	// and [ReasonConflictReplace]: "orphan-removal candidates require ...
	// explicit per-entry confirmation and a fresh backup", and replacing a
	// conflicting unmanaged entry destroys user-visible content the same way.
	// Apply refuses an unconfirmed destructive mutation, so an off-TTY preview
	// cannot mutate anything even if a caller forgets the TTY check.
	Confirmed bool

	// ExpectPriorGeneration is the root ownership generation the caller read
	// before it built NextBytes. It is compared against Store.Load()'s
	// Record.Generation immediately before the aggregate rename.
	ExpectPriorGeneration uint64

	// ExpectPriorDigest is the Digest of the aggregate bytes the caller read.
	// An empty string means "the file was absent when I read it". A non-empty
	// string must be a sha256 digest produced by [Digest].
	ExpectPriorDigest string
}

// Result reports what one applied generation did.
type Result struct {
	Generation  uint64
	BackupPath  string
	PriorDigest string
	PriorAbsent bool
	NextDigest  string
}

// ErrConfirmationRequired is returned when a DESTRUCTIVE repair — an orphan
// removal or a conflicting-entry replacement — is applied without explicit
// confirmation. Both destroy user-visible content, so both need the same gate.
var ErrConfirmationRequired = errors.New("ownership: orphan removal and conflicting-entry replacement require explicit per-entry confirmation")

// ErrForeignChange is returned when the aggregate on disk is neither the
// pre-mutation content nor the staged content.
var ErrForeignChange = errors.New("ownership: aggregate changed underneath a staged generation")

// ErrGenerationConflict is returned when the durable ownership record or the
// live aggregate file has changed since the caller read them. It is the live
// compare-and-swap refusal; [ErrForeignChange] remains recovery-only.
var ErrGenerationConflict = errors.New(
	"ownership: aggregate or ownership generation changed underneath the mutation")

// Apply performs one journaled generation, serialized against every other
// process working on this canonical target root.
//
// The lock opens HERE, around the WHOLE body including validation, and closes
// when the generation is complete. Two properties fall out of that boundary and
// neither is incidental:
//
//   - The read-modify-commit is atomic with respect to other processes. Apply's
//     generation number and next record come from the Load at the top of
//     applyLocked; without the lock another writer can commit between that Load
//     and the rename at step 4, and then the durable record no longer describes
//     the file (the defect this bead's parent exists to fix).
//   - A REFUSED mutation is serialized too, because acquisition happens before
//     [validateMutation]. That is deliberate: it costs one uncontended flock on
//     a path that then writes nothing, and it means the refusal decision is made
//     against the same state the mutation would have been made against.
//
// In addition to the lock, Apply checks a compare-and-swap precondition just
// before renaming the aggregate: [Mutation.ExpectPriorGeneration] must still
// equal the durable record's Generation and [Mutation.ExpectPriorDigest] must
// still equal the live aggregate's Digest (or "" when the file was absent).
// This is the backstop for the unserialized/degraded path and for external
// writers that do not participate in the per-root lock. Zero values mean the
// caller expects no record and an absent file, which is exactly the first-use
// case; callers that read an existing file or record must populate both fields.
//
// Acquisition is reentrant per Store, so a caller that already opened a section
// around Recover plus several Apply calls only bumps a depth counter here. See
// [Store.withWriteLock] for the full contract, including what happens on a
// filesystem with no advisory locking.
func (s *Store) Apply(m Mutation) (Result, error) {
	var res Result
	err := s.withMutation("apply", func() error {
		var applyErr error
		res, applyErr = s.applyLocked(m)
		return applyErr
	})
	return res, err
}

// applyLocked is Apply's body. It runs with the per-root lock held (or, on a
// filesystem that has no advisory locking, with the documented degradation).
//
// The write ordering below is the plan's, and every gap between two steps is a
// crash point with a defined recovery. The invariant that makes each gap safe is
// stated at the step that establishes it.
func (s *Store) applyLocked(m Mutation) (Result, error) {
	if err := validateMutation(m); err != nil {
		return Result{}, err
	}
	if err := s.ensureUnderRoot(m.AbsFile); err != nil {
		return Result{}, err
	}
	for _, e := range m.Entries {
		if err := validateEntry(e); err != nil {
			return Result{}, coded(diag.CodeOwnershipStateUnsafeValue, m.Rel, err)
		}
	}

	current, loadErr := s.Load()
	if loadErr != nil && !errors.Is(loadErr, ErrStateMissing) {
		// A key collision must never be resolved by overwriting the other root's
		// record, and corrupt state must not be silently re-based at generation 1.
		return Result{}, loadErr
	}
	nextDigest := Digest(m.NextBytes)
	generation := uint64(1)
	if current != nil {
		generation = current.Generation + 1
	}
	next, err := s.nextRecord(current, m.Rel, nextDigest, m.Entries)
	if err != nil {
		return Result{}, coded(diag.CodeOwnershipStateUnsafeValue, m.Rel, err)
	}

	// CRASH POINT 1 (before-backup): nothing has been written. Recovery has
	// nothing to do; the aggregate is exactly as the user left it.
	if err := s.crash(CrashBeforeBackup); err != nil {
		return Result{}, err
	}

	// STEP 1: mandatory raw-byte safety backup, BEFORE anything is prepared.
	// Invariant established: the user's exact bytes are recoverable from the XDG
	// state root for every later crash point.
	if err := os.MkdirAll(s.recordDir(), 0o700); err != nil {
		return Result{}, coded(diag.CodeOwnershipSafetyBackup, m.Rel, err)
	}
	backupPath, priorDigest, priorAbsent, err := s.writeSafetyBackup(m.Rel, m.AbsFile, generation, m.Reason, m.Entries)
	if err != nil {
		// A failed mandatory backup REFUSES the mutation. Writing without a
		// backup would be exactly the failure mode the plan forbids.
		return Result{}, coded(diag.CodeOwnershipSafetyBackup, m.Rel, err)
	}

	// CRASH POINT 2 (after-backup): the backup exists but no journal does.
	// Recovery does nothing (there is no generation to replay); the aggregate is
	// still the user's bytes and state still records the previous generation. The
	// only residue is a retained backup, which is also retained evidence.
	if err := s.crash(CrashAfterBackup); err != nil {
		return Result{}, err
	}

	stagingDir, stagedPath, err := stagedPathFor(m.AbsFile)
	if err != nil {
		return Result{}, err
	}
	journal := &Journal{
		Version:       journalVersion,
		RootKey:       s.rootKey,
		CanonicalRoot: s.canonicalRoot,
		Generation:    generation,
		Phase:         PhasePrepared,
		Reason:        m.Reason,
		File:          m.Rel,
		AbsFile:       m.AbsFile,
		StagedPath:    stagedPath,
		StagingDir:    stagingDir,
		BackupPath:    backupPath,
		PriorDigest:   priorDigest,
		PriorAbsent:   priorAbsent,
		NextDigest:    nextDigest,
		NextRecord:    next,
		UpdatedAt:     s.now().UTC().Format(time.RFC3339Nano),
	}

	// STEP 2: journal phase "prepared", fsynced. Invariant established: recovery
	// can name the file, the staged path, the backup, both digests, and the exact
	// ownership generation to commit.
	if err := s.writeJournal(journal); err != nil {
		return Result{}, err
	}

	// CRASH POINT 3 (after-journal-prepared): journal says prepared, no staged
	// file exists yet. Recovery reads the live file: it equals PriorDigest, so it
	// rolls BACK — remove staged (absent), clear journal. Nothing was lost.
	if err := s.crash(CrashAfterJournalPrepared); err != nil {
		return Result{}, err
	}
	staging, err := newStagingTransaction(s.canonicalRoot, m.AbsFile, journal.StagingDir, journal.StagedPath)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = staging.close() }()
	if err := staging.create(); err != nil {
		return Result{}, err
	}

	// STEP 3: write the replacement into the private staging directory and fsync
	// it. The returned descriptor remains open through publication so the
	// identity check can detect a pathname replacement before rename.
	// Invariant established: the patched bytes are durable, so the rename in step
	// 4 cannot produce a truncated aggregate.
	staged, err := staging.write(m.NextBytes, staging.aggregateMode(filepath.Base(m.AbsFile)))
	if err != nil {
		// The staging directory was created exclusively a few lines above and
		// nothing outside this package has a reason to hold anything in it, so
		// tearing it down here discards no evidence — unlike recovery, which is
		// resolving a directory some earlier process left behind and therefore
		// insists on the recorded digest before it unlinks anything.
		cleanupErr := staging.remove("")
		return Result{}, errors.Join(err, cleanupErr)
	}

	// CRASH POINT 4 (after-staged-write): journal says prepared and a complete
	// staged file exists, but the aggregate is untouched. Recovery sees the live
	// file at PriorDigest and rolls BACK: it deletes the staged file rather than
	// renaming it. Rolling forward here would be indistinguishable from rolling
	// forward a generation whose desired state has since changed, so the plan's
	// "replayed OR rolled back according to the recorded phase" is honoured by
	// treating "prepared" as not-yet-authorized-to-publish.
	if err := s.crash(CrashAfterStagedWrite); err != nil {
		return Result{}, errors.Join(err, staged.Close())
	}

	// CAS PRECONDITION: refuse if the record or aggregate changed since the caller
	// read them. With locking this should never fire; without locking it is the
	// only thing that prevents lost updates.
	if err := s.assertPrecondition(m); err != nil {
		closeErr := staged.Close()
		s.cleanupAfterCASFailure(journal, backupPath)
		s.observeLock(LockEvent{Kind: LockCASConflict, Path: m.AbsFile, Err: err})
		if errors.Is(err, ErrGenerationConflict) {
			return Result{}, coded(diag.CodeOwnershipGenerationConflict, m.Rel, errors.Join(err, closeErr))
		}
		return Result{}, errors.Join(err, closeErr)
	}

	// STEP 4: publish the patched aggregate through the opened staging root. The
	// publication boundary re-verifies the staged descriptor immediately before
	// the rename and confirms the destination immediately after it, so a
	// generation whose staging entry was replaced is refused rather than
	// published.
	if err := staging.publish(staged, filepath.Base(m.AbsFile), func() error {
		return s.crash(CrashBeforeAggregateRename)
	}); err != nil {
		return Result{}, errors.Join(err, staged.Close())
	}

	// Prune retained backups now that publication succeeded. Retention is not
	// part of the atomicity contract, so a failure here is best-effort.
	if err := s.pruneSafetyBackups(s.SafetyBackupDir(m.Rel)); err != nil {
		slog.Warn("could not prune retained safety backups after publication",
			"root_key", s.rootKey, "file", m.Rel, "err", err)
	}

	// CRASH POINT 5 (after-aggregate-rename): the aggregate is the NEW content
	// but the journal still says prepared and state still records the previous
	// generation. Recovery distinguishes this from crash point 4 by comparing the
	// live digest against the RECORDED NextDigest — recorded evidence, not a
	// heuristic — and rolls FORWARD, committing the journal's ownership
	// generation so state matches the bytes that are live.
	if err := s.crash(CrashAfterAggregateRename); err != nil {
		return Result{}, errors.Join(err, staged.Close())
	}
	if err := staged.Close(); err != nil {
		return Result{}, fmt.Errorf("ownership: close published staged file %s: %w", journal.StagedPath, err)
	}
	if err := staging.remove(""); err != nil {
		return Result{}, err
	}

	// STEP 5: journal phase "aggregate-renamed", fsynced. This is a pure
	// narrowing of recovery's search: it lets recovery roll forward without
	// having to trust a digest comparison.
	journal.Phase = PhaseAggregateRenamed
	journal.UpdatedAt = s.now().UTC().Format(time.RFC3339Nano)
	if err := s.writeJournal(journal); err != nil {
		return Result{}, err
	}

	// CRASH POINT 6 (after-journal-renamed): journal says aggregate-renamed and
	// state is one generation behind. Recovery rolls FORWARD unconditionally.
	if err := s.crash(CrashAfterJournalRenamed); err != nil {
		return Result{}, err
	}

	// STEP 6: atomically commit the matching ownership generation.
	if err := s.commitRecord(next); err != nil {
		return Result{}, coded(diag.CodeOwnershipStateCorrupt, s.StateFile(), err)
	}

	// CRASH POINT 7 (after-state-commit): file and state agree; only the journal
	// is stale. Recovery notices state.Generation >= journal.Generation and just
	// clears the journal. This is why no third journal phase is needed: the
	// committed record IS the record of having committed.
	if err := s.crash(CrashAfterStateCommit); err != nil {
		return Result{}, err
	}

	// STEP 7: clear the journal.
	if err := s.clearJournal(); err != nil {
		return Result{}, err
	}

	// CRASH POINT 8 (after-journal-cleanup): the generation is complete and
	// recovery is a no-op.
	if err := s.crash(CrashAfterJournalCleanup); err != nil {
		return Result{}, err
	}

	slog.Info("ownership generation committed",
		"root_key", s.rootKey, "file", m.Rel, "generation", generation, "reason", string(m.Reason),
		"entries", len(m.Entries), "prior_digest", priorDigest, "next_digest", nextDigest)

	return Result{
		Generation:  generation,
		BackupPath:  backupPath,
		PriorDigest: priorDigest,
		PriorAbsent: priorAbsent,
		NextDigest:  nextDigest,
	}, nil
}

// assertPrecondition is the compare-and-swap check that prevents a mutation
// from committing when the durable record or the live aggregate changed
// underneath the caller. It is intentionally fail-closed: a caller that passes
// zeroes asserts "no record and no file", which is exactly the first-use case.
func (s *Store) assertPrecondition(m Mutation) error {
	current, loadErr := s.Load()
	if loadErr != nil && !errors.Is(loadErr, ErrStateMissing) {
		return loadErr
	}
	var currentGen uint64
	if current != nil {
		currentGen = current.Generation
	}
	if currentGen != m.ExpectPriorGeneration {
		return fmt.Errorf("%w: expected prior generation %d, found %d",
			ErrGenerationConflict, m.ExpectPriorGeneration, currentGen)
	}

	raw, readErr := os.ReadFile(m.AbsFile)
	if readErr != nil && !os.IsNotExist(readErr) {
		return fmt.Errorf("ownership: read %s for CAS precondition: %w", m.AbsFile, readErr)
	}
	var liveDigest string
	if readErr == nil {
		liveDigest = Digest(raw)
	}
	if liveDigest != m.ExpectPriorDigest {
		return fmt.Errorf("%w: expected prior digest %s, found %s",
			ErrGenerationConflict, m.ExpectPriorDigest, liveDigest)
	}
	return nil
}

// cleanupAfterCASFailure removes the staged file, the journal, and the safety
// backup just written, so a failed compare-and-swap leaves no durable trace.
func (s *Store) cleanupAfterCASFailure(journal *Journal, backupPath string) {
	if err := s.removeStaged(journal); err != nil {
		slog.Warn("could not remove the staged file after a CAS failure",
			"root_key", s.rootKey, "staged_path", journal.StagedPath, "err", err)
	}
	if err := s.clearJournal(); err != nil {
		slog.Warn("could not clear the journal after a CAS failure",
			"root_key", s.rootKey, "journal", s.JournalFile(), "err", err)
	}
	if backupPath != "" {
		if err := os.Remove(backupPath); err != nil {
			slog.Warn("could not remove the safety backup after a CAS failure",
				"root_key", s.rootKey, "backup_path", backupPath, "err", err)
		}
		if err := os.Remove(metaPathFor(backupPath)); err != nil {
			slog.Warn("could not remove the safety backup manifest after a CAS failure",
				"root_key", s.rootKey, "meta_path", metaPathFor(backupPath), "err", err)
		}
	}
	// best-effort: remove empty backup directories
	dir := s.SafetyBackupDir(journal.File)
	if entries, err := os.ReadDir(dir); err == nil && len(entries) == 0 {
		if err := os.Remove(dir); err != nil {
			slog.Warn("could not remove an empty safety backup directory after a CAS failure",
				"root_key", s.rootKey, "dir", dir, "err", err)
		}
	}
}

func validateMutation(m Mutation) error {
	if m.Rel == "" || filepath.IsAbs(m.Rel) || strings.Contains(m.Rel, "\\") || strings.Contains(m.Rel, "..") {
		return fmt.Errorf("ownership: mutation file %q must be an output-relative slash path", m.Rel)
	}
	if !filepath.IsAbs(m.AbsFile) {
		return fmt.Errorf("ownership: mutation abs file %q must be absolute", m.AbsFile)
	}
	if m.ExpectPriorDigest != "" && !digestPattern.MatchString(m.ExpectPriorDigest) {
		return fmt.Errorf("ownership: expect-prior-digest %q is not a sha256 digest", m.ExpectPriorDigest)
	}
	switch m.Reason {
	case ReasonSync, ReasonAdoption:
	case ReasonOrphanRemoval, ReasonConflictReplace:
		// The DESTRUCTIVE reasons. The gate is checked here — before the
		// mandatory safety backup and therefore before a single byte is written
		// anywhere — so a refused destructive mutation leaves the state root
		// untouched too, not just the aggregate.
		if !m.Confirmed {
			return ErrConfirmationRequired
		}
	default:
		return fmt.Errorf("ownership: unknown mutation reason %q", m.Reason)
	}
	return nil
}

// ensureUnderRoot refuses to mutate anything outside the canonical target root
// the store is keyed by. Without it, a caller bug could back up one root's file
// under another root's key.
func (s *Store) ensureUnderRoot(absFile string) error {
	canonicalDir, err := CanonicalRoot(filepath.Dir(absFile))
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(s.canonicalRoot, filepath.Join(canonicalDir, filepath.Base(absFile)))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("ownership: %s is outside the canonical target root %s", absFile, s.canonicalRoot)
	}
	return nil
}

func (s *Store) writeJournal(j *Journal) error {
	data, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return fmt.Errorf("ownership: marshal journal: %w", err)
	}
	if err := atomicWrite(s.JournalFile(), append(data, '\n'), 0o600); err != nil {
		return err
	}
	slog.Debug("ownership journal written",
		"root_key", s.rootKey, "file", j.File, "generation", j.Generation, "phase", string(j.Phase))
	return nil
}

func (s *Store) clearJournal() error {
	if err := os.Remove(s.JournalFile()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("ownership: clear journal: %w", err)
	}
	return fsyncDir(s.recordDir())
}

// ReadJournal returns the in-flight journal, or nil when there is none.
func (s *Store) ReadJournal() (*Journal, error) {
	data, err := os.ReadFile(s.JournalFile())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("ownership: read journal: %w", err)
	}
	var j Journal
	if err := json.Unmarshal(data, &j); err != nil {
		return nil, coded(diag.CodeOwnershipStateCorrupt, s.JournalFile(), fmt.Errorf("%w: malformed journal JSON", ErrStateCorrupt))
	}
	if j.Version != journalVersion && j.Version != legacyJournalVersion {
		return nil, coded(diag.CodeOwnershipStateCorrupt, s.JournalFile(), fmt.Errorf("%w: unsupported journal version %d", ErrStateCorrupt, j.Version))
	}
	if j.RootKey != s.rootKey || j.CanonicalRoot != s.canonicalRoot {
		return nil, coded(diag.CodeOwnershipStateRootMismatch, s.JournalFile(), ErrStateRootMismatch)
	}
	// Recovery writes to, and unlinks next to, AbsFile. Confining it here means
	// no recovery path has to re-derive that authorization, in either journal
	// version.
	if !filepath.IsAbs(j.AbsFile) {
		return nil, coded(diag.CodeOwnershipStateCorrupt, s.JournalFile(),
			fmt.Errorf("%w: journal aggregate path %q must be absolute", ErrStateCorrupt, j.AbsFile))
	}
	if err := s.ensureUnderRoot(j.AbsFile); err != nil {
		return nil, coded(diag.CodeOwnershipStateCorrupt, s.JournalFile(),
			fmt.Errorf("%w: journal aggregate path is unsafe: %v", ErrStateCorrupt, err))
	}
	if j.Version == journalVersion {
		if err := validateStagingPaths(&j); err != nil {
			return nil, coded(diag.CodeOwnershipStateCorrupt, s.JournalFile(), err)
		}
	} else if err := validateLegacyStagingPath(&j); err != nil {
		return nil, coded(diag.CodeOwnershipStateCorrupt, s.JournalFile(), err)
	}
	normalizeHookEntries(j.NextRecord)
	return &j, nil
}

// RecoveryAction is what recovery did with an incomplete generation.
type RecoveryAction string

const (
	// ActionRolledForward means the aggregate was already published, so the
	// matching ownership generation was committed.
	ActionRolledForward RecoveryAction = "rolled-forward"
	// ActionRolledBack means the aggregate was never published, so the staged
	// file was discarded and state was left at the previous generation.
	ActionRolledBack RecoveryAction = "rolled-back"
	// ActionAlreadyCommitted means only the journal was stale.
	ActionAlreadyCommitted RecoveryAction = "already-committed"
	// ActionAbandoned means the aggregate is neither the recorded pre-mutation
	// content nor the recorded staged content, so the generation was abandoned
	// and the file left exactly as found.
	ActionAbandoned RecoveryAction = "abandoned"
)

// RecoveryOutcome describes one recovered generation.
type RecoveryOutcome struct {
	File       string
	Generation uint64
	Phase      Phase
	Action     RecoveryAction
}

// Recover replays or rolls back an incomplete generation, serialized against
// every other process working on this canonical target root. Callers run it once
// before any mutation.
//
// Recover is a WRITER, not a read, and taking the lock here is the most
// important acquisition in this package: rollForward commits an ownership
// generation, and rollBack and abandon delete a staged file and clear the
// journal. Unserialized, process B's Recover can therefore abandon process A's
// genuinely IN-FLIGHT generation — deleting the staged file out from under A's
// rename, or committing A's NextRecord while A is still staging — which is
// strictly worse than the stale-record race that motivates the lock on Apply.
//
// Two consequences of it being inside the section are worth stating, because
// they are behaviour changes and not just performance:
//
//   - The double-Recover hole is closed. Exactly one process ever inspects and
//     resolves a given journal, and it does so with no other writer able to
//     create a second one, so a journal can no longer be resolved twice or
//     resolved by the process that did not write it.
//   - ownership.foreign-change means what it was always documented to mean.
//     abandon's diagnostic is evidence that something OUTSIDE
//     agent-marketplace rewrote the aggregate, rather than something two of our
//     own processes routinely trigger on each other.
//
// Every decision below is driven by the recorded journal phase and by digests
// this package previously recorded. Nothing looks at what the aggregate's
// entries LOOK like, which is the plan's "crash recovery never infers ownership
// from file content".
func (s *Store) Recover() ([]RecoveryOutcome, error) {
	var out []RecoveryOutcome
	err := s.withMutation("recover", func() error {
		var recoverErr error
		out, recoverErr = s.recoverLocked()
		return recoverErr
	})
	return out, err
}

// recoverLocked is Recover's body, running with the per-root lock held.
func (s *Store) recoverLocked() ([]RecoveryOutcome, error) {
	j, err := s.ReadJournal()
	if err != nil {
		// A corrupt journal or a foreign root is left in place: it is the only
		// remaining description of an interrupted generation, and deleting it
		// would destroy evidence. The caller reports it and exits non-zero.
		return nil, err
	}
	if j == nil {
		return nil, nil
	}

	current, loadErr := s.Load()
	if loadErr != nil && errors.Is(loadErr, ErrStateRootMismatch) {
		return nil, loadErr
	}

	switch j.Phase {
	case PhaseAggregateRenamed:
		return s.rollForward(j, current)
	case PhasePrepared:
		live, liveErr := os.ReadFile(j.AbsFile)
		absent := liveErr != nil && os.IsNotExist(liveErr)
		if liveErr != nil && !absent {
			return nil, fmt.Errorf("ownership: read %s during recovery: %w", j.AbsFile, liveErr)
		}
		switch {
		case !absent && Digest(live) == j.NextDigest:
			// The rename happened in the window before the phase update.
			return s.rollForward(j, current)
		case absent && j.PriorAbsent, !absent && Digest(live) == j.PriorDigest:
			return s.rollBack(j)
		default:
			return s.abandon(j)
		}
	default:
		return nil, coded(diag.CodeOwnershipStateCorrupt, s.JournalFile(),
			fmt.Errorf("%w: unknown journal phase %q", ErrStateCorrupt, j.Phase))
	}
}

func (s *Store) rollForward(j *Journal, current *Record) ([]RecoveryOutcome, error) {
	action := ActionRolledForward
	if current != nil && current.Generation >= j.Generation {
		action = ActionAlreadyCommitted
	} else {
		if j.NextRecord == nil {
			return nil, coded(diag.CodeOwnershipStateCorrupt, s.JournalFile(),
				fmt.Errorf("%w: journal has no ownership generation to commit", ErrStateCorrupt))
		}
		if err := s.commitRecord(j.NextRecord); err != nil {
			return nil, coded(diag.CodeOwnershipStateCorrupt, s.StateFile(), err)
		}
	}
	if err := s.removeStaged(j); err != nil {
		return nil, err
	}
	if err := s.clearJournal(); err != nil {
		return nil, err
	}
	slog.Warn("recovered an incomplete ownership generation",
		"root_key", s.rootKey, "file", j.File, "generation", j.Generation,
		"phase", string(j.Phase), "action", string(action), "code", diag.CodeOwnershipRecovered)
	return []RecoveryOutcome{{File: j.File, Generation: j.Generation, Phase: j.Phase, Action: action}}, nil
}

func (s *Store) rollBack(j *Journal) ([]RecoveryOutcome, error) {
	if err := s.removeStaged(j); err != nil {
		return nil, err
	}
	if err := s.clearJournal(); err != nil {
		return nil, err
	}
	slog.Warn("rolled back an incomplete ownership generation",
		"root_key", s.rootKey, "file", j.File, "generation", j.Generation,
		"phase", string(j.Phase), "action", string(ActionRolledBack), "code", diag.CodeOwnershipRecovered)
	return []RecoveryOutcome{{File: j.File, Generation: j.Generation, Phase: j.Phase, Action: ActionRolledBack}}, nil
}

// abandon discards a staged generation whose target changed underneath it. The
// live file is NOT touched: "Without proof of ownership, sync preserves existing
// entries", and bytes we did not write are not ours to overwrite.
func (s *Store) abandon(j *Journal) ([]RecoveryOutcome, error) {
	if err := s.removeStaged(j); err != nil {
		return nil, err
	}
	if err := s.clearJournal(); err != nil {
		return nil, err
	}
	slog.Error("abandoned a staged ownership generation: the aggregate changed underneath it",
		"root_key", s.rootKey, "file", j.File, "generation", j.Generation,
		"prior_digest", j.PriorDigest, "next_digest", j.NextDigest, "code", diag.CodeOwnershipForeignChange)
	outcomes := []RecoveryOutcome{{File: j.File, Generation: j.Generation, Phase: j.Phase, Action: ActionAbandoned}}
	return outcomes, coded(diag.CodeOwnershipForeignChange, j.File, ErrForeignChange)
}

func (s *Store) removeStaged(j *Journal) error {
	if j.Version == legacyJournalVersion {
		return s.removeLegacyStaged(j)
	}
	staging, err := newStagingTransaction(s.canonicalRoot, j.AbsFile, j.StagingDir, j.StagedPath)
	if err != nil {
		return err
	}
	defer func() { _ = staging.close() }()
	return staging.remove(j.NextDigest)
}

// removeLegacyStaged discards the staged file of a version-1 journal, which kept
// it as a sibling of the aggregate instead of inside a private directory. The
// pathname is pinned by [validateLegacyStagingPath], but the unlink still runs
// through an open parent descriptor: a version-1 residue is by definition old,
// and its directory has had unbounded time to acquire a planted entry.
func (s *Store) removeLegacyStaged(j *Journal) error {
	if j.StagedPath == "" {
		return nil
	}
	parent, err := openAggregateParent(s.canonicalRoot, j.AbsFile)
	if err != nil {
		return err
	}
	defer func() { _ = parent.Close() }()
	name := filepath.Base(j.StagedPath)
	info, err := parent.Lstat(name)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("ownership: inspect legacy staged file %s: %w", j.StagedPath, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("ownership: legacy staged path %s is not a regular file", j.StagedPath)
	}
	data, err := parent.ReadFile(name)
	if err != nil {
		return fmt.Errorf("ownership: read legacy staged file %s: %w", j.StagedPath, err)
	}
	if j.NextDigest != "" && Digest(data) != j.NextDigest {
		return fmt.Errorf("ownership: legacy staged file %s changed before cleanup", j.StagedPath)
	}
	if err := parent.Remove(name); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("ownership: remove legacy staged file %s: %w", j.StagedPath, err)
	}
	return syncRootDirectory(parent)
}
