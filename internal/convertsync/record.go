// Package convertsync is the durable authority for which files inside a
// `convert --sync` output tree Agent Marketplace itself wrote.
//
// # Why a durable record instead of a directory scan
//
// `convert --sync` reconciles an output directory: it writes the converted
// resources and removes what the conversion no longer produces. A plain
// directory scan cannot tell "output of a resource that has since been deleted"
// apart from "a file the user wrote by hand", so scan-driven pruning deletes
// unrelated work. This package supplies the missing evidence: after a
// successful sync it records the output-relative path of every file the run
// materialized together with the digest of the bytes it wrote, so a later run
// can prune exactly those paths and nothing else.
//
// # Fail-closed
//
// Every failure answers "not ours". A missing record ([ErrRecordMissing]), an
// unreadable or malformed record, a record written for a different output root,
// a recorded path that escapes the output root ([ErrRecordCorrupt]), a record
// written for a different target harness ([ErrRecordOtherTarget]), or a target
// name that cannot key a record at all ([ErrTargetUnusable]) all yield the zero
// [Record], which classifies every path as [StatusUnmanaged]. The worst
// outcome is then a stale generated file the user can delete, never a deleted
// file the user wrote.
//
// A recorded file whose current bytes differ from the recorded digest is
// [StatusOwnedDrifted]: it was ours when written, but it carries edits that were
// not, so it is preserved as well.
//
// # Where state lives
//
// Records live under the Agent Marketplace XDG state root, one file per
// canonical output root AND target harness, keyed by the same canonicalization
// and key derivation as the aggregate-ownership records in internal/ownership
// (separate directory). They are deliberately NOT written into the output tree:
// a marker inside the converted output would be indistinguishable from a file
// any other tool could write, and would itself become output that has to be
// materialized, pruned, and committed.
//
// The target belongs in the key, not only in the record body. Converting two
// harnesses into one directory is legitimate — the layouts occupy different
// paths — and a single record per root means whichever target syncs last
// overwrites the other's record, so alternating targets each permanently forget
// output they still own and can no longer prune it.
//
// A pre-target-keyed record (one file per root, no target in the name) is still
// read, but only for the target it names, and only until that target's next
// successful [Save] rewrites it under its target key.
package convertsync

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Adyen/agent-marketplace/internal/ownership"
)

// recordVersion is the on-disk schema version. An unknown version is CORRUPT
// rather than upgradable-by-guess: reading it as "owns nothing" costs a stale
// file, misreading it could delete a user's file.
//
// Adding the target to the record's file name did not bump it: the schema is
// unchanged, and a pre-target-keyed file still describes its own target's output
// correctly, so invalidating it would forget ownership that is still true.
const recordVersion = 1

// recordDirName is the state-root subdirectory holding convert output records.
// It is a sibling of internal/ownership's aggregate-entry directory, so the two
// key spaces cannot overlap even though they share a key derivation.
const recordDirName = "convert"

// ErrRecordMissing is returned when no durable record exists for the output
// root, which is the normal state of a first-ever convert into a directory.
var ErrRecordMissing = errors.New("convertsync: no durable convert ownership record")

// ErrRecordCorrupt is returned when a record exists but cannot be trusted.
var ErrRecordCorrupt = errors.New("convertsync: durable convert ownership record is unusable")

// ErrRecordOtherTarget is returned when a well-formed record for this output
// root describes a different target harness. It is a distinct condition from
// corruption: the state is intact, it just does not describe the layout this
// run produces, so it cannot authorize deleting anything.
var ErrRecordOtherTarget = errors.New("convertsync: convert ownership record was written for another target harness")

// ErrTargetUnusable is returned when a target harness name cannot key a record.
var ErrTargetUnusable = errors.New("convertsync: target harness cannot key a convert ownership record")

// targetNamePattern is the shape a target harness name must have before it can
// key a record file. The name becomes part of a file name, and that file decides
// which output paths may be deleted, so anything that could change the path it
// resolves to — a separator, a traversal segment, the dot that separates the
// root key from the target — is refused outright rather than sanitized into a
// name that could collide with another target's key.
var targetNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// normalizeTarget folds a target name to the single spelling that keys its
// record, so "Factory" and "factory" cannot own two competing records.
func normalizeTarget(target string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(target))
	if !targetNamePattern.MatchString(normalized) {
		return "", fmt.Errorf("%w: %q", ErrTargetUnusable, target)
	}
	return normalized, nil
}

// Status is the ownership verdict for one path inside the output root.
type Status int

const (
	// StatusUnmanaged means no evidence exists that this run's product wrote the
	// path. It must never be pruned or replaced without explicit authorization.
	StatusUnmanaged Status = iota
	// StatusOwned means the path was recorded by an earlier successful sync and
	// its bytes are still exactly the bytes that were written.
	StatusOwned
	// StatusOwnedDrifted means the path was recorded but its bytes changed since,
	// so it now carries content Agent Marketplace did not write.
	StatusOwnedDrifted
)

func (s Status) String() string {
	switch s {
	case StatusOwned:
		return "owned"
	case StatusOwnedDrifted:
		return "owned-drifted"
	default:
		return "unmanaged"
	}
}

// OwnedFile is one materialized output file: its slash-separated path relative
// to the output root, and the digest of the bytes that were written there.
type OwnedFile struct {
	Path   string `json:"path"`
	Digest string `json:"digest"`
}

// Record is the durable ownership record for one canonical output root.
type Record struct {
	Version       int         `json:"version"`
	CanonicalRoot string      `json:"canonical_root"`
	RootKey       string      `json:"root_key"`
	Target        string      `json:"target"`
	UpdatedAt     string      `json:"updated_at"`
	Files         []OwnedFile `json:"files"`

	// index is the path -> digest lookup built by Load. A Record obtained any
	// other way (a literal in a test, a json.Unmarshal by a caller) has a nil
	// index and falls back to scanning Files, so Classify never depends on
	// having gone through Load.
	index map[string]string
}

// digestPattern matches the exact digest shape [DigestOf] produces. Anything
// else in a record is corruption, not a digest to be trusted.
var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// DigestOf returns the content digest this package records. It is always a
// digest OF content and never content: a record can therefore describe a file
// without copying anything the file contains.
func DigestOf(b []byte) string { return ownership.Digest(b) }

// Classify reports whether current is the content this product wrote at rel.
//
// It deliberately requires the CURRENT bytes rather than only a path: ownership
// of a path is not ownership of whatever now sits at it, and a caller that only
// has a path cannot obtain an owned verdict.
func (r Record) Classify(rel string, current []byte) Status {
	want, ok := r.recordedDigest(rel)
	if !ok {
		return StatusUnmanaged
	}
	if DigestOf(current) != want {
		return StatusOwnedDrifted
	}
	return StatusOwned
}

// Recorded reports whether rel appears in the record at all, regardless of its
// current content.
func (r Record) Recorded(rel string) bool {
	_, ok := r.recordedDigest(rel)
	return ok
}

func (r Record) recordedDigest(rel string) (string, bool) {
	key := filepath.ToSlash(rel)
	if r.index != nil {
		digest, ok := r.index[key]
		return digest, ok
	}
	for _, f := range r.Files {
		if f.Path == key {
			return f.Digest, true
		}
	}
	return "", false
}

// RecordPath returns the state file that holds outRoot's record for target. It
// creates nothing: callers can use it to inspect or delete state.
func RecordPath(outRoot, target string) (string, error) {
	loc, err := recordLocation(outRoot, target)
	return loc.path, err
}

// LegacyRecordPath returns the pre-target-keyed state file for outRoot: one
// record per output root, with no target in its name. It exists only so the
// migration path can be inspected and tested; nothing writes there any more.
func LegacyRecordPath(outRoot string) (string, error) {
	// Any valid target yields the same legacy path, which is exactly the
	// ambiguity that made per-target keying necessary.
	loc, err := recordLocation(outRoot, "factory")
	return loc.legacy, err
}

// recordFiles is where one output root's state lives for one target.
type recordFiles struct {
	// path is the target-keyed record this run reads and writes.
	path string
	// legacy is the pre-target-keyed record, read only when path is absent and
	// only when it names this same target.
	legacy string
	// canonicalRoot is the single spelling of the output root that keys both.
	canonicalRoot string
	// target is the normalized target name stored in and matched against records.
	target string
}

func recordLocation(outRoot, target string) (recordFiles, error) {
	normalizedTarget, err := normalizeTarget(target)
	if err != nil {
		return recordFiles{}, err
	}
	canonical, err := ownership.CanonicalRoot(outRoot)
	if err != nil {
		return recordFiles{}, fmt.Errorf("convertsync: %w", err)
	}
	stateRoot, err := ownership.StateRoot()
	if err != nil {
		return recordFiles{}, fmt.Errorf("convertsync: %w", err)
	}
	dir := filepath.Join(stateRoot, recordDirName)
	key := ownership.RootKey(canonical)
	return recordFiles{
		path:          filepath.Join(dir, key+"."+normalizedTarget+".json"),
		legacy:        filepath.Join(dir, key+".json"),
		canonicalRoot: canonical,
		target:        normalizedTarget,
	}, nil
}

// Load reads the durable record outRoot holds for target.
//
// target is required: ownership of an output path is ownership of the bytes ONE
// harness layout produced there, so a record written for a different target
// cannot authorize deleting or replacing anything in this run
// ([ErrRecordOtherTarget]).
//
// It always returns a usable Record: on any error the zero value, which owns
// nothing. The error is advisory and exists so callers can distinguish a normal
// first run ([ErrRecordMissing]) from state that was there and is now unusable
// ([ErrRecordCorrupt]) or describes another target ([ErrRecordOtherTarget]).
func Load(outRoot, target string) (Record, error) {
	loc, err := recordLocation(outRoot, target)
	if err != nil {
		return Record{}, err
	}
	record, err := readRecord(loc.path, loc)
	if err == nil {
		return record, nil
	}
	if !errors.Is(err, ErrRecordMissing) {
		return Record{}, err
	}

	// No record under the target key: fall back to the pre-target-keyed file,
	// which is adopted only when it names this same target. A legacy file
	// belonging to another target is left untouched for that target's own run to
	// claim, and authorizes nothing here.
	legacy, legacyErr := readRecord(loc.legacy, loc)
	if legacyErr != nil {
		if errors.Is(legacyErr, ErrRecordMissing) {
			return Record{}, fmt.Errorf("%w for %s (target %s)", ErrRecordMissing, loc.canonicalRoot, loc.target)
		}
		return Record{}, legacyErr
	}
	slog.Info("adopted pre-target-keyed convert ownership record; the next successful sync rewrites it under its target key",
		"record", loc.legacy, "out_root", loc.canonicalRoot, "target", loc.target, "files", len(legacy.Files))
	return legacy, nil
}

// readRecord reads and validates one candidate record file for loc's root and
// target. A file that is absent, unreadable, malformed, written for another root
// or written for another target yields no record at all.
func readRecord(recordFile string, loc recordFiles) (Record, error) {
	raw, err := os.ReadFile(recordFile)
	if err != nil {
		if os.IsNotExist(err) {
			return Record{}, fmt.Errorf("%w: %s", ErrRecordMissing, recordFile)
		}
		return Record{}, fmt.Errorf("%w: read %s: %v", ErrRecordCorrupt, recordFile, err)
	}
	var record Record
	if err := json.Unmarshal(raw, &record); err != nil {
		slog.Warn("convert ownership record is not valid JSON; treating all output as unmanaged",
			"record", recordFile, "out_root", loc.canonicalRoot)
		return Record{}, fmt.Errorf("%w: parse %s: %v", ErrRecordCorrupt, recordFile, err)
	}
	if err := validateRecord(record, loc.canonicalRoot); err != nil {
		slog.Warn("convert ownership record refused; treating all output as unmanaged",
			"record", recordFile, "out_root", loc.canonicalRoot, "err", err)
		return Record{}, err
	}
	recordedTarget, err := normalizeTarget(record.Target)
	if err != nil {
		return Record{}, fmt.Errorf("%w: %v", ErrRecordCorrupt, err)
	}
	if recordedTarget != loc.target {
		slog.Warn("convert ownership record describes another target; treating all output as unmanaged",
			"record", recordFile, "out_root", loc.canonicalRoot, "recorded_target", recordedTarget, "target", loc.target)
		return Record{}, fmt.Errorf("%w: %s describes %q, not %q", ErrRecordOtherTarget, recordFile, recordedTarget, loc.target)
	}
	record.index = make(map[string]string, len(record.Files))
	for _, f := range record.Files {
		record.index[f.Path] = f.Digest
	}
	return record, nil
}

func validateRecord(record Record, canonicalRoot string) error {
	if record.Version != recordVersion {
		return fmt.Errorf("%w: unsupported version %d", ErrRecordCorrupt, record.Version)
	}
	// A hash is a lossy name, so the record carries the root it was written for
	// and a mismatch fails closed instead of letting one output root prune
	// another's files after a key collision or a hand-edited state file.
	if record.CanonicalRoot != canonicalRoot {
		return fmt.Errorf("%w: record belongs to %q, not %q", ErrRecordCorrupt, record.CanonicalRoot, canonicalRoot)
	}
	if record.RootKey != ownership.RootKey(canonicalRoot) {
		return fmt.Errorf("%w: record key does not derive from %q", ErrRecordCorrupt, canonicalRoot)
	}
	// Ownership is per target layout, so a record that names no target cannot be
	// matched against one and is refused rather than accepted for any target.
	if strings.TrimSpace(record.Target) == "" {
		return fmt.Errorf("%w: record names no target harness", ErrRecordCorrupt)
	}
	seen := make(map[string]struct{}, len(record.Files))
	for _, f := range record.Files {
		if err := validateOwnedFile(f); err != nil {
			return fmt.Errorf("%w: %v", ErrRecordCorrupt, err)
		}
		if _, duplicate := seen[f.Path]; duplicate {
			return fmt.Errorf("%w: duplicate recorded path %q", ErrRecordCorrupt, f.Path)
		}
		seen[f.Path] = struct{}{}
	}
	return nil
}

func validateOwnedFile(f OwnedFile) error {
	if err := validateRelPath(f.Path); err != nil {
		return err
	}
	if !digestPattern.MatchString(f.Digest) {
		return fmt.Errorf("recorded path %q has a malformed digest", f.Path)
	}
	return nil
}

// validateRelPath accepts only a clean, relative, slash-separated path with no
// traversal segment. A record is consulted to decide what may be DELETED, so a
// path that could resolve outside the output root is refused outright.
func validateRelPath(p string) error {
	if p == "" {
		return errors.New("recorded path is empty")
	}
	if p != filepath.ToSlash(p) {
		return fmt.Errorf("recorded path %q is not slash-separated", p)
	}
	if strings.HasPrefix(p, "/") || filepath.IsAbs(p) || strings.HasPrefix(p, "./") {
		return fmt.Errorf("recorded path %q is not relative", p)
	}
	if p != path.Clean(p) {
		return fmt.Errorf("recorded path %q is not clean", p)
	}
	for _, segment := range strings.Split(p, "/") {
		switch segment {
		case "", ".", "..":
			return fmt.Errorf("recorded path %q contains an unusable segment", p)
		}
	}
	return nil
}

// Save replaces outRoot's record for target with files, and retires the
// pre-target-keyed record once this target no longer needs it.
//
// It is called only after a successful materialization, so the record always
// describes bytes that actually reached disk. Replacement rather than merge is
// deliberate: this package cannot tell which recorded files a run already
// reconciled, so the caller — which knows what it wrote and what it pruned —
// passes the complete set that is still owned, including any prior entry it
// carried over because it could not prune it (see the convert command).
func Save(outRoot, target string, files []OwnedFile) error {
	loc, err := recordLocation(outRoot, target)
	if err != nil {
		return err
	}
	recordFile, canonical := loc.path, loc.canonicalRoot
	normalized := make([]OwnedFile, 0, len(files))
	seen := make(map[string]string, len(files))
	for _, f := range files {
		f.Path = filepath.ToSlash(f.Path)
		if err := validateOwnedFile(f); err != nil {
			return fmt.Errorf("convertsync: refusing to record %q: %w", f.Path, err)
		}
		if previous, duplicate := seen[f.Path]; duplicate {
			if previous != f.Digest {
				return fmt.Errorf("convertsync: refusing to record %q with two different digests", f.Path)
			}
			continue
		}
		seen[f.Path] = f.Digest
		normalized = append(normalized, f)
	}
	sort.Slice(normalized, func(i, j int) bool { return normalized[i].Path < normalized[j].Path })

	record := Record{
		Version:       recordVersion,
		CanonicalRoot: canonical,
		RootKey:       ownership.RootKey(canonical),
		Target:        loc.target,
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
		Files:         normalized,
	}
	encoded, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("convertsync: encode record: %w", err)
	}
	encoded = append(encoded, '\n')

	dir := filepath.Dir(recordFile)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("convertsync: create state directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".record-*")
	if err != nil {
		return fmt.Errorf("convertsync: stage record: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(encoded); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("convertsync: write record: %w", err)
	}
	// The record authorizes deletion, so a half-written one must never be
	// readable: it is fsynced before it is published under its real name.
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("convertsync: sync record: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("convertsync: close record: %w", err)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return fmt.Errorf("convertsync: set record permissions: %w", err)
	}
	if err := os.Rename(tmpName, recordFile); err != nil {
		return fmt.Errorf("convertsync: publish record: %w", err)
	}
	retireLegacyRecord(loc)
	slog.Debug("recorded convert output ownership", "out_root", canonical, "target", loc.target, "files", len(normalized))
	return nil
}

// retireLegacyRecord deletes the pre-target-keyed record once the target it
// names has published a target-keyed one, completing the migration.
//
// It removes the file only after confirming the file names THIS target: a legacy
// record for another target is that target's only evidence of what it owns, and
// deleting it would make that output unprunable forever. Every failure is
// non-fatal — the authoritative record is already published, and a surviving
// legacy file is never consulted again for this target.
func retireLegacyRecord(loc recordFiles) {
	raw, err := os.ReadFile(loc.legacy)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Debug("could not read the pre-target-keyed convert ownership record; leaving it in place",
				"record", loc.legacy, "err", err)
		}
		return
	}
	var legacy Record
	if err := json.Unmarshal(raw, &legacy); err != nil {
		slog.Warn("pre-target-keyed convert ownership record is unreadable; leaving it in place",
			"record", loc.legacy, "err", err)
		return
	}
	recordedTarget, err := normalizeTarget(legacy.Target)
	if err != nil || recordedTarget != loc.target {
		return
	}
	if err := os.Remove(loc.legacy); err != nil && !os.IsNotExist(err) {
		slog.Warn("could not remove the migrated pre-target-keyed convert ownership record",
			"record", loc.legacy, "err", err)
		return
	}
	slog.Info("migrated pre-target-keyed convert ownership record to its target key",
		"from", loc.legacy, "to", loc.path, "target", loc.target)
}
