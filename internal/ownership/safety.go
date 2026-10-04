package ownership

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

const (
	backupSuffix = ".bak"
	metaSuffix   = ".meta.json"
)

// BackupManifest is the non-secret metadata written beside a safety backup.
//
// It is also the ONLY retained evidence source for orphan recovery other than
// the journal, which is why it records the managed entry identities of the
// generation the backup was taken for: "orphan-removal candidates require
// non-heuristic retained journal or safety-backup evidence linking the entry to
// prior managed output".
//
// "Backup filenames and metadata must not expose secret values": every field is
// either a canonical output path, a digest, a generation number, a timestamp, or
// an [Entry], and [Entry] structurally cannot hold a value.
type BackupManifest struct {
	Version int `json:"version"`
	// File is the output-relative path of the aggregate that was backed up.
	File string `json:"file"`
	// RootKey ties the backup to a canonical target root.
	RootKey string `json:"root_key"`
	// CanonicalRoot is stored so a backup found on disk can be attributed
	// without re-deriving the hash.
	CanonicalRoot string `json:"canonical_root"`
	// Generation is the ownership generation this backup was taken FOR, i.e. the
	// generation about to be written. The backup itself holds the bytes from
	// generation-1.
	Generation uint64 `json:"generation"`
	// Reason records why the mutation was attempted.
	Reason Reason `json:"reason"`
	// PriorDigest is the digest of the raw bytes stored in the .bak file. Empty
	// when the aggregate did not exist yet.
	PriorDigest string `json:"prior_digest"`
	// PriorAbsent records "the file did not exist", which is different from "the
	// file was empty".
	PriorAbsent bool `json:"prior_absent"`
	// PriorSize is the exact byte count of the retained raw bytes.
	PriorSize int64 `json:"prior_size"`
	// ManagedEntries are the entries the generation being written claims to own.
	ManagedEntries []Entry `json:"managed_entries"`
	CreatedAt      string  `json:"created_at"`
}

// Reason records why an aggregate was mutated. It is part of the journal and of
// backup metadata so recovery and `doctor` can describe what was interrupted.
type Reason string

const (
	// ReasonSync is normal reconciliation from marketplace configuration.
	ReasonSync Reason = "sync"
	// ReasonAdoption is `doctor --repair` adopting an exact current desired
	// match.
	ReasonAdoption Reason = "adoption"
	// ReasonOrphanRemoval is `doctor --repair` removing a confirmed,
	// evidence-backed orphan.
	ReasonOrphanRemoval Reason = "orphan-removal"
	// ReasonConflictReplace is `doctor --repair` replacing a confirmed
	// [ConflictCandidate]: an unmanaged entry whose key matches a desired entry
	// but whose value differs.
	//
	// It shares ReasonOrphanRemoval's gate rather than ReasonAdoption's, and the
	// reason is the OUTCOME, not the intent: adoption changes no bytes of the
	// user's file, while this destroys a value the user wrote. So
	// [validateMutation] refuses it without [Mutation.Confirmed].
	//
	// This package knows nothing about flags or terminals, so it cannot enforce
	// "never under --yes" by itself: what it enforces is that NO caller can
	// replace an entry without asserting per-entry confirmation. Mapping that
	// assertion to an actual interactive answer is the caller's half of the
	// contract (doctor's conflict loop never consults --yes and requires a TTY),
	// and the two halves are independent, so a regression in either one alone
	// still cannot silently destroy a value.
	ReasonConflictReplace Reason = "conflict-replace"
)

// BackupOnly retains the exact current bytes of an aggregate WITHOUT mutating
// anything: no journal is written, no ownership generation is committed, and the
// live file is not touched.
//
// It exists for the one case the plan describes where a backup is required but a
// patch is impossible: "Malformed JSON is safety-backed up before parsing, left
// unchanged, and reported with a precise parse location" (plan, "JSON
// preservation"). D10's reconciler calls it before it hands a destination
// aggregate to pkg/harness-compilation/jsonc, so a file it then refuses to touch
// is still recoverable from the state root.
//
// The manifest records no managed entries, because a backup taken without a
// mutation claims no ownership. That also means it can never nominate an orphan
// (see [Store.OrphanCandidates], which needs a recorded entry digest), so an
// unparseable file cannot become evidence for deleting a user's entry.
//
// It is nevertheless a WRITE — it creates the state directory, retains bytes, and
// PRUNES older retained backups — and its manifest records the generation it read
// from [Store.Load], so it takes the per-canonical-root lock like every other
// writer here. Without it, the generation stamped on the manifest can be one
// another process has already superseded, and the prune can race a concurrent
// mutation's own retention pass. See [Store.withWriteLock].
func (s *Store) BackupOnly(rel, absFile string, reason Reason) (string, error) {
	var path string
	err := s.withMutation("backup-only", func() error {
		var backupErr error
		path, backupErr = s.backupOnlyLocked(rel, absFile, reason)
		return backupErr
	})
	return path, err
}

// backupOnlyLocked is BackupOnly's body, running with the per-root lock held.
func (s *Store) backupOnlyLocked(rel, absFile string, reason Reason) (string, error) {
	if err := s.ensureUnderRoot(absFile); err != nil {
		return "", err
	}
	if err := s.prepareStateDirectory(filepath.Join(ownershipDirName, s.rootKey)); err != nil {
		return "", fmt.Errorf("ownership: create state directory: %w", err)
	}
	generation := uint64(0)
	if current, err := s.Load(); err == nil && current != nil {
		generation = current.Generation
	}
	path, _, _, err := s.writeSafetyBackup(rel, absFile, generation, reason, nil)
	if err != nil {
		return "", err
	}
	if err := s.pruneSafetyBackups(s.SafetyBackupDir(rel)); err != nil {
		return "", err
	}
	return path, nil
}

// writeSafetyBackup stores the exact pre-mutation bytes of absFile plus a
// manifest. It does NOT prune older backups; callers that commit the mutation
// must prune after the compare-and-swap precondition passes.
//
// "Raw pre-mutation bytes are stored under the Agent Marketplace XDG state root
// before first managed merge, adoption, or confirmed orphan removal." The bytes
// written are the bytes read — there is no decode/encode round trip anywhere on
// this path, so a malformed or comment-laden file is preserved exactly, which is
// the only version of a backup worth having for a file a user hand-wrote.
func (s *Store) writeSafetyBackup(rel, absFile string, generation uint64, reason Reason, entries []Entry) (backupPath, priorDigest string, priorAbsent bool, err error) {
	raw, readErr := s.readAbsoluteAggregate(absFile)
	if readErr != nil {
		if !os.IsNotExist(readErr) {
			return "", "", false, fmt.Errorf("ownership: read %s for safety backup: %w", absFile, readErr)
		}
		priorAbsent = true
		raw = nil
	}
	if !priorAbsent {
		priorDigest = Digest(raw)
	}

	dir := s.SafetyBackupDir(rel)
	relative := filepath.Join(safetyDirName, s.rootKey, fileSlug(rel))
	if err := s.prepareStateDirectory(relative); err != nil {
		return "", "", false, fmt.Errorf("ownership: create safety backup directory: %w", err)
	}
	parent, err := s.captureStateDirectory(relative)
	if err != nil {
		return "", "", false, err
	}
	defer func() {
		if closeErr := parent.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	stamp := s.now().UTC().Format("20060102T150405.000000000Z")

	// Validate the entries before writing any bytes: invalid entries must not
	// consume a retention slot or leave a backup file with no manifest beside it.
	for _, e := range entries {
		if err := validateEntry(e); err != nil {
			return "", "", false, err
		}
	}

	if err := s.verifyStateDirectory(parent, relative); err != nil {
		return "", "", false, err
	}
	backupFile, name, err := parent.CreateTemp(stamp + "-*" + backupSuffix)
	if err != nil {
		return "", "", false, fmt.Errorf("ownership: create safety backup: %w", err)
	}
	backupPath = filepath.Join(dir, name)
	info, err := backupFile.Stat()
	if err != nil {
		return "", "", false, errors.Join(err, backupFile.Close())
	}
	created, err := fsboundary.FileInfoIdentity(info)
	if err != nil {
		return "", "", false, errors.Join(err, backupFile.Close())
	}
	published := false
	defer func() {
		if backupFile != nil {
			if closeErr := backupFile.Close(); closeErr != nil {
				err = errors.Join(err, closeErr)
			}
		}
		if published {
			return
		}
		native, openErr := parent.NativeRoot()
		if openErr != nil {
			err = errors.Join(err, openErr)
			return
		}
		defer func() {
			if closeErr := native.Close(); closeErr != nil {
				err = errors.Join(err, closeErr)
			}
		}()
		info, inspectErr := native.Lstat(name)
		if errors.Is(inspectErr, fs.ErrNotExist) {
			return
		}
		if inspectErr != nil {
			err = errors.Join(err, inspectErr)
			return
		}
		identity, inspectErr := fsboundary.FileInfoIdentity(info)
		if inspectErr != nil {
			err = errors.Join(err, inspectErr)
			return
		}
		if identity.SameObject(created) {
			err = errors.Join(err, parent.Unlink(name))
		}
	}()
	if n, writeErr := backupFile.Write(raw); writeErr != nil {
		return "", "", false, fmt.Errorf("ownership: write safety backup: %w", writeErr)
	} else if n != len(raw) {
		return "", "", false, fmt.Errorf("ownership: write safety backup: short write (%d/%d bytes)", n, len(raw))
	}
	if err := backupFile.Sync(); err != nil {
		return "", "", false, fmt.Errorf("ownership: sync safety backup: %w", err)
	}
	info, err = backupFile.Stat()
	if err != nil {
		return "", "", false, err
	}
	produced, err := fsboundary.FileInfoIdentity(info)
	if err != nil {
		return "", "", false, err
	}
	closeErr := backupFile.Close()
	backupFile = nil
	if closeErr != nil {
		return "", "", false, fmt.Errorf("ownership: close safety backup: %w", closeErr)
	}
	manifest := BackupManifest{
		Version:        1,
		File:           rel,
		RootKey:        s.rootKey,
		CanonicalRoot:  s.canonicalRoot,
		Generation:     generation,
		Reason:         reason,
		PriorDigest:    priorDigest,
		PriorAbsent:    priorAbsent,
		PriorSize:      int64(len(raw)),
		ManagedEntries: append([]Entry(nil), entries...),
		CreatedAt:      s.now().UTC().Format(time.RFC3339Nano),
	}
	metaBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", "", false, fmt.Errorf("ownership: marshal backup manifest: %w", err)
	}
	payload, err := fsboundary.CaptureDestination(parent, name, fsboundary.LinkPolicyRepository)
	if err != nil {
		return "", "", false, err
	}
	defer func() {
		if closeErr := payload.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	if payload.Link() != nil || !payload.Identity().SameState(produced) {
		return "", "", false, fsboundary.ErrGenerationConflict
	}
	validate := func() error {
		if err := s.verifyStateDirectory(parent, relative); err != nil {
			return err
		}
		return payload.ReadTargetWith(func(io.Reader) error { return nil })
	}
	if err := validate(); err != nil {
		return "", "", false, err
	}
	if _, err := writeObservedMetadataFile(parent, filepath.Base(metaPathFor(backupPath)), append(metaBytes, '\n'), validate); err != nil {
		return "", "", false, err
	}
	published = true
	slog.Info("safety backup written",
		"root_key", s.rootKey, "file", rel, "generation", generation, "reason", string(reason),
		"prior_digest", priorDigest, "prior_absent", priorAbsent, "prior_size", len(raw), "retain", s.retain)
	return backupPath, priorDigest, priorAbsent, nil
}

func metaPathFor(backupPath string) string {
	return strings.TrimSuffix(backupPath, backupSuffix) + metaSuffix
}

// pruneSafetyBackups keeps the newest s.retain backups for one canonical output
// file and selects each older backup together with its manifest. Both members
// are validated before deletion; the pair is not one atomic removal.
//
// Retention is by NAME, and names are UTC timestamps with nanosecond precision
// plus a collision sequence, so lexical order is chronological order and the
// policy does not depend on mtimes that a restore or a copy could rewrite.
func (s *Store) pruneSafetyBackups(dir string) (err error) {
	relative, err := filepath.Rel(s.stateRoot, dir)
	if err != nil {
		return err
	}
	parent, err := s.captureStateDirectory(relative)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := parent.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	native, err := parent.NativeRoot()
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := native.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	entries, err := fs.ReadDir(native.FS(), ".")
	if err != nil {
		return fmt.Errorf("ownership: list safety backups: %w", err)
	}
	var backups []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), backupSuffix) {
			backups = append(backups, e.Name())
		}
	}
	if len(backups) <= s.retain {
		return nil
	}
	sort.Strings(backups)
	var removals []safetyLeafRemoval
	for _, name := range backups[:len(backups)-s.retain] {
		for _, member := range []string{name, filepath.Base(metaPathFor(name))} {
			pin, exists, err := captureSafetyPayload(parent, member)
			if err != nil {
				return err
			}
			removals = append(removals, safetyLeafRemoval{name: member, identity: pin.file, exists: exists})
		}
	}
	for _, removal := range removals {
		if err := s.removeSafetyLeaf(parent, relative, removal); err != nil {
			return err
		}
	}
	return nil
}

// SafetyBackups returns the retained backups for one canonical output file,
// oldest first, each paired with its manifest. A backup whose manifest is
// missing or ordinarily unreadable is skipped; unsafe shapes and cleanup
// failures refuse. Bytes without recorded provenance are not evidence.
func (s *Store) SafetyBackups(rel string) (out []RetainedBackup, err error) {
	dir := s.SafetyBackupDir(rel)
	relative := filepath.Join(safetyDirName, s.rootKey, fileSlug(rel))
	parent, err := s.captureStateDirectory(relative)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && !errors.Is(err, fsboundary.ErrUnsafePath) {
			return nil, nil
		}
		return nil, fmt.Errorf("ownership: list safety backups: %w", err)
	}
	defer func() {
		if closeErr := parent.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	native, err := parent.NativeRoot()
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := native.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	entries, err := fs.ReadDir(native.FS(), ".")
	if err != nil {
		return nil, fmt.Errorf("ownership: list safety backups: %w", err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), backupSuffix) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	out = make([]RetainedBackup, 0, len(names))
	for _, name := range names {
		path := filepath.Join(dir, name)
		metaBytes, exists, _, err := readObservedMetadataFile(parent, filepath.Base(metaPathFor(path)))
		if err != nil {
			if errors.Is(err, fsboundary.ErrUnsafePath) || errors.Is(err, fsboundary.ErrUnsafeFile) || errors.Is(err, fsboundary.ErrGenerationConflict) || errors.Is(err, fsboundary.ErrTooLarge) || errors.Is(err, errMetadataCleanup) {
				return nil, err
			}
			continue
		}
		if !exists {
			continue
		}
		var manifest BackupManifest
		if err := json.Unmarshal(metaBytes, &manifest); err != nil {
			continue
		}
		if manifest.Version != 1 || manifest.RootKey != s.rootKey || manifest.File != rel {
			continue
		}
		pin, exists, err := captureSafetyPayload(parent, name)
		if err != nil {
			return nil, err
		}
		if !exists {
			continue
		}
		out = append(out, RetainedBackup{Path: path, Manifest: manifest, source: s, relative: relative, name: name, observed: pin})
	}
	if err := s.verifyStateDirectory(parent, relative); err != nil {
		return nil, err
	}
	return out, nil
}

// RetainedBackup is one retained safety backup and its manifest.
type RetainedBackup struct {
	Path     string
	Manifest BackupManifest
	source   *Store
	relative string
	name     string
	observed metadataPin
}

// RawBytes returns the exact retained pre-mutation bytes.
func (b RetainedBackup) RawBytes() (data []byte, err error) {
	if b.source == nil {
		return nil, fsboundary.ErrUnsafePath
	}
	parent, err := b.source.captureStateDirectory(b.relative)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := parent.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
			data = nil
		}
	}()
	if !parent.Identity().SameObject(b.observed.parent) {
		return nil, fsboundary.ErrGenerationConflict
	}
	file, err := fsboundary.CaptureDestination(parent, b.name, fsboundary.LinkPolicyRepository)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
			data = nil
		}
	}()
	if file.Link() != nil || !file.Exists() || !file.Identity().SameState(b.observed.file) {
		return nil, fsboundary.ErrGenerationConflict
	}
	data, _, err = file.ReadTarget(b.observed.file.Size)
	if err != nil {
		return nil, err
	}
	if err := b.source.verifyStateDirectory(parent, b.relative); err != nil {
		return nil, err
	}
	return data, nil
}

func captureSafetyPayload(parent *fsboundary.Dir, name string) (pin metadataPin, exists bool, err error) {
	file, err := fsboundary.CaptureDestination(parent, name, fsboundary.LinkPolicyRepository)
	if err != nil {
		return pin, false, err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	if file.Link() != nil {
		return pin, false, fsboundary.ErrUnsafeFile
	}
	if !file.Exists() {
		return pin, false, nil
	}
	if err := file.ReadTargetWith(func(io.Reader) error { return nil }); err != nil {
		return pin, false, err
	}
	identity := parent.Identity()
	pin = metadataPin{parent: fsboundary.Identity{Device: identity.Device, Inode: identity.Inode}, file: file.Identity()}
	return pin, true, nil
}
