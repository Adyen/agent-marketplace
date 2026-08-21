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
// a recorded path that escapes the output root ([ErrRecordCorrupt]), or a record
// written for a different target harness ([ErrRecordOtherTarget]) all yield the
// zero [Record], which classifies every path as [StatusUnmanaged]. The worst
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
// canonical output root, keyed exactly like the aggregate-ownership records in
// internal/ownership (same canonicalization, same key derivation, separate
// directory). They are deliberately NOT written into the output tree: a marker
// inside the converted output would be indistinguishable from a file any other
// tool could write, and would itself become output that has to be materialized,
// pruned, and committed.
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

// RecordPath returns the state file that holds outRoot's record. It creates
// nothing: callers can use it to inspect or delete state.
func RecordPath(outRoot string) (string, error) {
	path, _, err := recordLocation(outRoot)
	return path, err
}

func recordLocation(outRoot string) (recordFile, canonicalRoot string, err error) {
	canonical, err := ownership.CanonicalRoot(outRoot)
	if err != nil {
		return "", "", fmt.Errorf("convertsync: %w", err)
	}
	stateRoot, err := ownership.StateRoot()
	if err != nil {
		return "", "", fmt.Errorf("convertsync: %w", err)
	}
	return filepath.Join(stateRoot, recordDirName, ownership.RootKey(canonical)+".json"), canonical, nil
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
	recordFile, canonical, err := recordLocation(outRoot)
	if err != nil {
		return Record{}, err
	}
	raw, err := os.ReadFile(recordFile)
	if err != nil {
		if os.IsNotExist(err) {
			return Record{}, fmt.Errorf("%w for %s", ErrRecordMissing, canonical)
		}
		return Record{}, fmt.Errorf("%w: read %s: %v", ErrRecordCorrupt, recordFile, err)
	}
	var record Record
	if err := json.Unmarshal(raw, &record); err != nil {
		slog.Warn("convert ownership record is not valid JSON; treating all output as unmanaged",
			"record", recordFile, "out_root", canonical)
		return Record{}, fmt.Errorf("%w: parse %s: %v", ErrRecordCorrupt, recordFile, err)
	}
	if err := validateRecord(record, canonical); err != nil {
		slog.Warn("convert ownership record refused; treating all output as unmanaged",
			"record", recordFile, "out_root", canonical, "err", err)
		return Record{}, err
	}
	if record.Target != target {
		slog.Warn("convert ownership record describes another target; treating all output as unmanaged",
			"record", recordFile, "out_root", canonical, "recorded_target", record.Target, "target", target)
		return Record{}, fmt.Errorf("%w: record describes %q, not %q", ErrRecordOtherTarget, record.Target, target)
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

// Save replaces outRoot's record for target with files.
//
// It is called only after a successful materialization, so the record always
// describes bytes that actually reached disk. Replacement rather than merge is
// deliberate: this package cannot tell which recorded files a run already
// reconciled, so the caller — which knows what it wrote and what it pruned —
// passes the complete set that is still owned, including any prior entry it
// carried over because it could not prune it (see the convert command).
func Save(outRoot, target string, files []OwnedFile) error {
	if strings.TrimSpace(target) == "" {
		return errors.New("convertsync: refusing to record output without a target harness")
	}
	recordFile, canonical, err := recordLocation(outRoot)
	if err != nil {
		return err
	}
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
		Target:        target,
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
	slog.Debug("recorded convert output ownership", "out_root", canonical, "target", target, "files", len(normalized))
	return nil
}
