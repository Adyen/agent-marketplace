package ownership

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
)

// stateVersion is the on-disk schema version. A record with an unknown version
// is CORRUPT, not upgradable-by-guess: treating it as unmanaged loses nothing,
// while misreading it could prune a user's entry.
const stateVersion = 1

// EntryKind identifies which aggregate primitive an owned entry is.
type EntryKind string

const (
	// KindMcpServer is one keyed entry of mcp_config.json's mcpServers object.
	// "MCP identity is the server object key" (plan, "Entry identities").
	KindMcpServer EntryKind = "mcp-server"
	// KindHook is one element of a hook event array. Hook behavior identity is
	// the digest of a hook/v2 prefix, the event name, and the digest of the
	// canonical JSON rule. Any byte difference in the canonical rule produces a
	// different identity, so only the DIGEST ever reaches state.
	KindHook EntryKind = "hook"
)

// Entry is the non-secret record of one owned aggregate entry.
//
// The struct is the enforcement point for "It must not copy hook payloads, MCP
// environment values, headers, OAuth secrets, or other sensitive values": there
// is simply no field that can hold one. A hook's matcher, command, prompt, or
// any other member can only reach state through [HookIdentityDigest], and an
// entry's serialized bytes only through [Digest].
type Entry struct {
	Kind EntryKind `json:"kind"`
	// Container is the object member the entry lives under: "mcpServers" for an
	// MCP server, or the hook EVENT NAME for a hook. Both come from fixed
	// vocabularies, never from user content.
	Container string `json:"container"`
	// Key is the MCP server object key. Empty for hooks, which have no key.
	Key string `json:"key,omitempty"`
	// IdentityDigest is the digest of the behaviour identity: the server key for
	// an MCP entry, the canonical value digest for a hook.
	IdentityDigest string `json:"identity_digest"`
	// ValueDigest is the digest of the exact bytes written for the entry. Drift
	// detection compares the current entry's digest against this.
	ValueDigest string `json:"value_digest"`
	// Resource is the marketplace resource name that produced the entry.
	Resource string `json:"resource"`
	// PluginInstance is non-empty when a decomposed plugin instance owns the
	// entry. Leaf reconciliation preserves entries owned by an instance.
	PluginInstance string `json:"plugin_instance,omitempty"`
	// SourceDigest optionally records the version-1 source-hook fingerprint
	// ("sha256:" + hex) that binds an owned native hook entry to the source
	// hook a different client version selected. Only hook entries may carry
	// it, and only as a sha256 digest, so no hook payload can ride along in
	// this field. Absent means "no source proof", never a guess.
	SourceDigest string `json:"source_digest,omitempty"`
	// SourceEvidence records every additional (resource, source fingerprint)
	// pair a multi-source native writer proved for this entry, under the
	// same non-secret validation rules as the scalar evidence. Readers must
	// never add tuples while reading, and a tuple only ever matches its
	// exact pair.
	SourceEvidence    []HookSourceEvidence  `json:"source_evidence,omitempty"`
	PluginGenerations []PluginGenerationRef `json:"plugin_generations,omitempty"`
}

// HookSourceEvidence is one additional contributing source for an owned
// native hook entry: the marketplace resource name and the version-1
// source-hook fingerprint of one selected hook that produced the entry.
type HookSourceEvidence struct {
	Resource     string `json:"resource"`
	SourceDigest string `json:"source_digest"`
}

type HookSatisfaction struct {
	Container      string               `json:"container"`
	IdentityDigest string               `json:"identity_digest"`
	ValueDigest    string               `json:"value_digest"`
	PluginInstance string               `json:"plugin_instance,omitempty"`
	Sources        []HookSourceEvidence `json:"sources"`
}

func (s HookSatisfaction) ID() EntryID {
	return EntryID{Kind: KindHook, Container: s.Container, IdentityDigest: s.IdentityDigest}
}

// EntryID is the identity half of an [Entry], with no value digest, so a caller
// can ask about an entry without asserting what it contains.
type EntryID struct {
	Kind           EntryKind
	Container      string
	Key            string
	IdentityDigest string
}

// ID returns the entry's identity.
func (e Entry) ID() EntryID {
	return EntryID{Kind: e.Kind, Container: e.Container, Key: e.Key, IdentityDigest: e.IdentityDigest}
}

func (id EntryID) mapKey() string {
	return string(id.Kind) + "\x00" + id.Container + "\x00" + id.Key + "\x00" + id.IdentityDigest
}

// FileState is the owned-entry set for one canonical output file.
type FileState struct {
	// ContentDigest is the digest of the whole file as this package last wrote
	// it. A mismatch means the file changed outside our control.
	ContentDigest     string             `json:"content_digest"`
	Entries           []Entry            `json:"entries"`
	HookSatisfactions []HookSatisfaction `json:"hook_satisfactions,omitempty"`
}

// Record is the durable ownership record for one canonical target root.
type Record struct {
	Version       int                  `json:"version"`
	CanonicalRoot string               `json:"canonical_root"`
	RootKey       string               `json:"root_key"`
	Generation    uint64               `json:"generation"`
	UpdatedAt     string               `json:"updated_at"`
	Files         map[string]FileState `json:"files"`
}

// safeToken matches the only shapes allowed in a state or metadata string
// field. A command string, prompt body, matcher, URL, header value, or secret
// blob effectively always contains a space, quote, slash, brace, or equals
// sign, so this rejects them; the identities the plan permits (an MCP server
// object key, a hook event name, a resource name) all pass.
var safeToken = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

// digestPattern matches the exact digest shape produced by Digest.
var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// ErrUnsafeStateValue is returned when a caller tries to record a value that is
// not a non-secret identity.
var ErrUnsafeStateValue = errors.New("ownership: refused to record a value that is not a non-secret identity")

// ErrStateMissing is returned when no durable record exists for the root.
var ErrStateMissing = errors.New("ownership: no durable ownership record")

// ErrStateUninitialized is the FIRST-USE form of [ErrStateMissing]: no durable
// record exists AND this canonical root has no durable ownership store at all,
// so no record was ever expected.
//
// It wraps ErrStateMissing, which is what keeps this a REPORTING distinction
// only. Every fail-closed check in the product is written as
// errors.Is(err, ErrStateMissing) (or reads the ownership.state-missing code),
// and both still match: nothing is proven, so every existing entry stays
// unmanaged, collisions are still refused, and nothing is pruned. The only
// thing that changes is whether a human is told — an absence that cannot
// surprise anyone is a debug fact, not a warning.
//
// The distinction is drawn from DURABLE EVIDENCE ([Store.EverRecorded]), never
// from aggregate file bytes: if this root has an ownership directory or a
// retained safety backup, we managed it once, so a vanished record is a real
// problem worth a warning. If it has neither, there is nothing to have lost.
//
// The cost of that choice, stated plainly, is that ANYTHING which destroys the
// evidence is indistinguishable from a first run and is therefore SILENT. Two
// families do this:
//
//   - Losing the evidence: deleting the whole XDG state root, or pointing
//     XDG_STATE_HOME somewhere else. We keep no evidence outside it, so this is
//     unavoidable by construction.
//   - Changing the root's IDENTITY, which moves the evidence out from under the
//     key we look it up by: moving or renaming the checkout, .devin becoming a
//     symlink, or a case-different spelling on a case-insensitive filesystem
//     (the SPLIT that [CanonicalRoot] documents). Here the old record is still
//     on disk, but not under this root's key — so the entries this tool wrote
//     silently become unmanaged and a stale one is never pruned again, with no
//     diagnostic to explain it.
//
// Both still FAIL CLOSED — nothing is adopted, nothing is pruned, collisions
// are still refused — so no data is at risk; only the explanation is lost.
// Recovering the explanation needs evidence keyed by something other than the
// canonical root, which this package does not have today; doctor is the
// intended place to surface it.
var ErrStateUninitialized = fmt.Errorf("%w: this target root has no ownership store yet", ErrStateMissing)

// ErrStateCorrupt is returned when a record exists but cannot be trusted.
var ErrStateCorrupt = errors.New("ownership: ownership record is corrupt")

// ErrStateRootMismatch is returned when a record's canonical root does not
// match the root it was loaded for.
var ErrStateRootMismatch = errors.New("ownership: ownership record belongs to a different canonical root")

// coded builds a *diag.CodedError whose severity comes from the shared code
// catalog, so a coded ownership error and a directly emitted ownership
// diagnostic for the same code cannot disagree. diag.NewCodedError alone always
// reports SeverityError (fail-closed by construction), which is right for a
// wrapper that does not know its code's severity but wrong for
// ownership.state-missing, which the catalog defines as a warning because
// missing state is recoverable and must not fail a sync outright.
func coded(code, file string, err error) *diag.CodedError {
	e := diag.NewCodedError(code, diag.SourceRef{File: file}, err)
	if doc, ok := diag.DocForCode(code); ok {
		e.Severity = doc.Severity
	}
	return e
}

// validateEntry enforces the non-secret contract. It never includes the
// offending value in the error: the whole point is that such a value must not
// travel further, and a diagnostic is "further".
func validateEntry(e Entry) error {
	switch e.Kind {
	case KindMcpServer:
		if !safeToken.MatchString(e.Key) {
			return fmt.Errorf("%w: mcp-server entry key", ErrUnsafeStateValue)
		}
	case KindHook:
		if e.Key != "" {
			return fmt.Errorf("%w: hook entries have no key", ErrUnsafeStateValue)
		}
		if e.IdentityDigest != HookIdentityDigest(e.Container, e.ValueDigest) {
			return fmt.Errorf("%w: hook identity digest does not match value", ErrUnsafeStateValue)
		}
	default:
		return fmt.Errorf("%w: unknown entry kind", ErrUnsafeStateValue)
	}
	if !safeToken.MatchString(e.Container) {
		return fmt.Errorf("%w: entry container", ErrUnsafeStateValue)
	}
	if !safeToken.MatchString(e.Resource) {
		return fmt.Errorf("%w: entry resource name", ErrUnsafeStateValue)
	}
	if e.PluginInstance != "" && !pluginDigestToken.MatchString(e.PluginInstance) {
		return fmt.Errorf("%w: plugin instance owner", ErrUnsafeStateValue)
	}
	seenPluginGenerations := make(map[string]string, len(e.PluginGenerations))
	for _, ref := range e.PluginGenerations {
		if !pluginDigestToken.MatchString(ref.InstanceKey) ||
			!pluginDigestToken.MatchString(ref.GenerationKey) ||
			!safeToken.MatchString(ref.Name) ||
			!safeToken.MatchString(ref.Target) ||
			!safeToken.MatchString(ref.Scope) {
			return fmt.Errorf("%w: plugin generation reference", ErrUnsafeStateValue)
		}
		if generation, exists := seenPluginGenerations[ref.InstanceKey]; exists {
			if generation != ref.GenerationKey {
				return fmt.Errorf("%w: plugin instance names multiple generations", ErrUnsafeStateValue)
			}
			return fmt.Errorf("%w: duplicate plugin generation reference", ErrUnsafeStateValue)
		}
		seenPluginGenerations[ref.InstanceKey] = ref.GenerationKey
	}
	if !digestPattern.MatchString(e.IdentityDigest) {
		return fmt.Errorf("%w: identity digest is not a sha256 digest", ErrUnsafeStateValue)
	}
	if !digestPattern.MatchString(e.ValueDigest) {
		return fmt.Errorf("%w: value digest is not a sha256 digest", ErrUnsafeStateValue)
	}
	if e.SourceDigest != "" && (e.Kind != KindHook || !digestPattern.MatchString(e.SourceDigest)) {
		return fmt.Errorf("%w: source digest must be a sha256 digest on a hook entry", ErrUnsafeStateValue)
	}
	if len(e.SourceEvidence) > 0 && e.Kind != KindHook {
		return fmt.Errorf("%w: source evidence is valid only on hook entries", ErrUnsafeStateValue)
	}
	for _, ev := range e.SourceEvidence {
		if !safeToken.MatchString(ev.Resource) {
			return fmt.Errorf("%w: source evidence resource name", ErrUnsafeStateValue)
		}
		if !digestPattern.MatchString(ev.SourceDigest) {
			return fmt.Errorf("%w: source evidence digest is not a sha256 digest", ErrUnsafeStateValue)
		}
	}
	return nil
}

func validateHookSatisfaction(s HookSatisfaction) error {
	if !safeToken.MatchString(s.Container) {
		return fmt.Errorf("%w: hook satisfaction container", ErrUnsafeStateValue)
	}
	if !digestPattern.MatchString(s.IdentityDigest) {
		return fmt.Errorf("%w: hook satisfaction identity digest is not a sha256 digest", ErrUnsafeStateValue)
	}
	if !digestPattern.MatchString(s.ValueDigest) {
		return fmt.Errorf("%w: hook satisfaction value digest is not a sha256 digest", ErrUnsafeStateValue)
	}
	if s.IdentityDigest != HookIdentityDigest(s.Container, s.ValueDigest) {
		return fmt.Errorf("%w: hook satisfaction identity digest does not match value", ErrUnsafeStateValue)
	}
	if len(s.Sources) == 0 {
		return fmt.Errorf("%w: hook satisfaction has no source evidence", ErrUnsafeStateValue)
	}
	if s.PluginInstance != "" && !pluginDigestToken.MatchString(s.PluginInstance) {
		return fmt.Errorf("%w: hook satisfaction plugin instance", ErrUnsafeStateValue)
	}
	seenSources := make(map[string]struct{}, len(s.Sources))
	for _, source := range s.Sources {
		if !safeToken.MatchString(source.Resource) {
			return fmt.Errorf("%w: hook satisfaction source resource name", ErrUnsafeStateValue)
		}
		if !digestPattern.MatchString(source.SourceDigest) {
			return fmt.Errorf("%w: hook satisfaction source digest is not a sha256 digest", ErrUnsafeStateValue)
		}
		key := source.Resource + "\x00" + source.SourceDigest
		if _, duplicate := seenSources[key]; duplicate {
			return fmt.Errorf("%w: duplicate hook satisfaction source pair", ErrUnsafeStateValue)
		}
		seenSources[key] = struct{}{}
	}
	return nil
}

func validateRecord(r *Record, canonicalRoot, rootKey string) error {
	if r.Version != stateVersion {
		return fmt.Errorf("%w: unsupported state version %d", ErrStateCorrupt, r.Version)
	}
	if r.RootKey != rootKey {
		return fmt.Errorf("%w: recorded root key does not match", ErrStateCorrupt)
	}
	if r.CanonicalRoot != canonicalRoot {
		return ErrStateRootMismatch
	}
	for rel, fs := range r.Files {
		if rel == "" || filepath.IsAbs(rel) {
			return fmt.Errorf("%w: file key is not an output-relative path", ErrStateCorrupt)
		}
		if fs.ContentDigest != "" && !digestPattern.MatchString(fs.ContentDigest) {
			return fmt.Errorf("%w: file content digest is malformed", ErrStateCorrupt)
		}
		for _, e := range fs.Entries {
			if err := validateEntry(e); err != nil {
				return errors.Join(ErrStateCorrupt, err)
			}
		}
		seenSatisfactions := make(map[string]struct{}, len(fs.HookSatisfactions))
		for _, satisfaction := range fs.HookSatisfactions {
			if err := validateHookSatisfaction(satisfaction); err != nil {
				return errors.Join(ErrStateCorrupt, err)
			}
			key := satisfaction.ID().mapKey()
			if _, duplicate := seenSatisfactions[key]; duplicate {
				return errors.Join(ErrStateCorrupt, fmt.Errorf("%w: duplicate hook satisfaction identity", ErrUnsafeStateValue))
			}
			seenSatisfactions[key] = struct{}{}
		}
	}
	return nil
}

// normalizeHookEntries recomputes the identity digest for every hook entry from
// its canonical value digest. This is the lazy read-side migration from the old
// lossy hook/v1 identity to the total hook/v2 identity: it makes records written
// before the new algorithm valid on read without bumping the schema version.
func normalizeHookEntries(rec *Record) {
	if rec == nil || rec.Files == nil {
		return
	}
	for rel, fs := range rec.Files {
		for i := range fs.Entries {
			if fs.Entries[i].Kind != KindHook {
				continue
			}
			fs.Entries[i].IdentityDigest = HookIdentityDigest(fs.Entries[i].Container, fs.Entries[i].ValueDigest)
		}
		rec.Files[rel] = fs
	}
}

// EverRecorded reports whether this canonical root has any durable trace of a
// previous ownership generation: an ownership directory (created only when a
// record, a journal, or a mandatory safety backup was written for it) or a
// retained safety backup.
//
// It answers "was a record EXPECTED here?" from state this package wrote
// itself. It deliberately does not look at the target root, the aggregate
// files, or their contents — that would be ownership inference, which this
// package makes structurally impossible.
//
// Every check errs toward "recorded". A false negative SUPPRESSES a warning
// about a root we really did manage; a false positive only prints one on a
// first run. The two are not equally bad, so an answer we cannot determine —
// a directory that exists but cannot be read, say — counts as evidence.
func (s *Store) EverRecorded() bool {
	if _, err := os.Lstat(s.recordDir()); err == nil || !os.IsNotExist(err) {
		return true
	}
	entries, err := os.ReadDir(s.safetyRootDir())
	if err == nil {
		return len(entries) > 0
	}
	// Only a safety root that is genuinely NOT THERE proves nothing was kept.
	return !os.IsNotExist(err)
}

// missingStateError returns the right flavour of "no record": the first-use one
// when this root has never been recorded, the plain one when it has.
func (s *Store) missingStateError() error {
	if s.EverRecorded() {
		return ErrStateMissing
	}
	return ErrStateUninitialized
}

// Load reads the durable record.
//
// The plan's rule is "Missing or corrupt ownership state makes all existing
// aggregate entries unmanaged", so Load distinguishes the two cases by error
// (ownership.state-missing is a warning, ownership.state-corrupt is an error)
// but returns a nil record for both. Callers must not fall back to reading the
// aggregate file for ownership: with a nil record, [Store.Proof] yields the
// zero [Proof], and the zero Proof classifies every entry as unmanaged.
//
// A missing record additionally reports WHICH absence it is via
// [ErrStateUninitialized], so a caller can tell a normal first use from a
// record that went away. That is a reporting distinction only: the code, the
// severity, and the nil record are identical either way.
func (s *Store) Load() (*Record, error) {
	record, _, err := s.loadObservedRecord()
	return record, err
}

func (s *Store) loadObservedRecord() (*Record, metadataPin, error) {
	data, exists, pin, err := s.readObservedMetadata(stateFileName)
	if err != nil {
		return nil, pin, coded(diag.CodeOwnershipStateCorrupt, s.StateFile(), fmt.Errorf("%w: %w", ErrStateCorrupt, err))
	}
	if !exists {
		return nil, pin, coded(diag.CodeOwnershipStateMissing, s.StateFile(), s.missingStateError())
	}
	rec, decodeErr := s.decodeStateRecord(s.StateFile(), data)
	return rec, pin, decodeErr
}

func (s *Store) decodeStateRecord(path string, data []byte) (*Record, error) {
	var rec Record
	// The typed jsonc decode rejects literal duplicate keys and case-folded
	// aliases for modeled fields (including nested source_evidence members)
	// before any value reaches the record.
	if err := jsonc.Unmarshal(path, data, &rec); err != nil {
		return nil, coded(diag.CodeOwnershipStateCorrupt, path, fmt.Errorf("%w: malformed state JSON", ErrStateCorrupt))
	}
	normalizeHookEntries(&rec)
	if err := validateRecord(&rec, s.canonicalRoot, s.rootKey); err != nil {
		code := diag.CodeOwnershipStateCorrupt
		if errors.Is(err, ErrStateRootMismatch) {
			code = diag.CodeOwnershipStateRootMismatch
		}
		return nil, coded(code, path, err)
	}
	if rec.Files == nil {
		rec.Files = map[string]FileState{}
	}
	return &rec, nil
}

// commitRecord writes the record atomically and durably: temp file in the same
// directory, fsync the file, rename over the target, fsync the directory. On any
// crash the reader sees either the whole previous record or the whole new one.
func (s *Store) commitRecord(rec *Record) error {
	return s.commitRecordWithValidation(rec, nil)
}

func (s *Store) commitRecordWithValidation(rec *Record, validate func() error) (err error) {
	if err := validateRecord(rec, s.canonicalRoot, s.rootKey); err != nil {
		return err
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("ownership: marshal record: %w", err)
	}
	data = append(data, '\n')
	if validate == nil {
		return s.writeMetadata(stateFileName, data)
	}
	if err := validate(); err != nil {
		return err
	}
	parent, err := s.captureMetadataParent()
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := parent.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	_, _, err = writeObservedMetadataFile(parent, stateFileName, data, func() error {
		if err := s.verifyMetadataParent(parent); err != nil {
			return err
		}
		return validate()
	})
	return err
}

// nextRecord builds the record that a mutation will commit, from the currently
// loaded record (or an empty one when state is missing or corrupt).
func (s *Store) nextRecord(current *Record, rel string, contentDigest string, entries []Entry) (*Record, error) {
	return s.nextRecordMeta(current, rel, contentDigest, entries, nil)
}

func (s *Store) nextRecordMeta(current *Record, rel string, contentDigest string, entries []Entry, satisfactions *[]HookSatisfaction) (*Record, error) {
	next := &Record{
		Version:       stateVersion,
		CanonicalRoot: s.canonicalRoot,
		RootKey:       s.rootKey,
		Generation:    1,
		Files:         map[string]FileState{},
	}
	if current != nil {
		next.Generation = current.Generation + 1
		for k, v := range current.Files {
			next.Files[k] = v
		}
	}
	next.UpdatedAt = s.now().UTC().Format(time.RFC3339Nano)

	sorted, seen, err := sortedOwnedEntries(entries)
	if err != nil {
		return nil, err
	}
	fileState := next.Files[rel]
	fileState.ContentDigest = contentDigest
	fileState.Entries = sorted
	if satisfactions != nil {
		sats, err := sortedHookSatisfactions(*satisfactions)
		if err != nil {
			return nil, err
		}
		for _, satisfaction := range sats {
			if _, dup := seen[satisfaction.ID().mapKey()]; dup {
				return nil, fmt.Errorf("%w: hook satisfaction duplicates an owned entry identity", ErrUnsafeStateValue)
			}
		}
		fileState.HookSatisfactions = sats
	}
	if len(sorted) == 0 && len(fileState.HookSatisfactions) == 0 {
		delete(next.Files, rel)
	} else {
		next.Files[rel] = fileState
	}
	return next, nil
}

func sortedOwnedEntries(entries []Entry) ([]Entry, map[string]struct{}, error) {
	sorted := append([]Entry(nil), entries...)
	seen := make(map[string]struct{}, len(sorted))
	for _, entry := range sorted {
		if err := validateEntry(entry); err != nil {
			return nil, nil, err
		}
		key := entry.ID().mapKey()
		if _, dup := seen[key]; dup {
			return nil, nil, fmt.Errorf("%w: duplicate entry identity", ErrUnsafeStateValue)
		}
		seen[key] = struct{}{}
	}
	sort.Slice(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.Container != b.Container {
			return a.Container < b.Container
		}
		if a.Key != b.Key {
			return a.Key < b.Key
		}
		return a.IdentityDigest < b.IdentityDigest
	})
	return sorted, seen, nil
}

func sortedHookSatisfactions(in []HookSatisfaction) ([]HookSatisfaction, error) {
	out := make([]HookSatisfaction, len(in))
	for i, satisfaction := range in {
		if err := validateHookSatisfaction(satisfaction); err != nil {
			return nil, err
		}
		out[i] = satisfaction
		out[i].Sources = append([]HookSourceEvidence(nil), satisfaction.Sources...)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		return a.Container < b.Container || a.Container == b.Container && a.IdentityDigest < b.IdentityDigest
	})
	for i := range out {
		sort.Slice(out[i].Sources, func(a, b int) bool {
			left, right := out[i].Sources[a], out[i].Sources[b]
			return left.Resource < right.Resource || left.Resource == right.Resource && left.SourceDigest < right.SourceDigest
		})
	}
	return out, nil
}

func fileStateSemanticallyEqual(current FileState, currentPresent bool, next FileState, nextPresent bool) bool {
	if currentPresent != nextPresent {
		return false
	}
	if !currentPresent {
		return true
	}
	if current.ContentDigest != next.ContentDigest || len(current.Entries) != len(next.Entries) ||
		len(current.HookSatisfactions) != len(next.HookSatisfactions) {
		return false
	}
	entriesEqual := len(current.Entries) == 0 || reflect.DeepEqual(current.Entries, next.Entries)
	satisfactionsEqual := len(current.HookSatisfactions) == 0 || reflect.DeepEqual(current.HookSatisfactions, next.HookSatisfactions)
	return entriesEqual && satisfactionsEqual
}

// Proof returns proof-backed ownership for one canonical output file.
//
// The returned error is ADVISORY: it reports missing or corrupt state so the
// caller can surface the diagnostic, but the returned Proof is already correct
// for that case (zero value ⇒ everything unmanaged). A caller that ignores the
// error still behaves safely; a caller that treats the error as fatal also
// behaves safely. There is no third option in which state is absent and
// something is still considered owned.
func (s *Store) Proof(rel string) (Proof, error) {
	rec, err := s.Load()
	if err != nil {
		if errors.Is(err, ErrStateUninitialized) {
			// Normal first use: there is no record because nothing has ever been
			// recorded for this root. The fail-closed outcome is identical to any
			// other missing record, but there is nothing for a user to act on, so
			// it stays a debug fact.
			slog.Debug("no ownership record yet for this target root; first use, so every existing aggregate entry is unmanaged",
				"root_key", s.rootKey, "canonical_root", s.canonicalRoot, "file", rel,
				"state_file", s.StateFile(), "reason", codeOf(err))
			return Proof{}, err
		}
		slog.Warn("ownership state unavailable; treating aggregate entries as unmanaged",
			"root_key", s.rootKey, "canonical_root", s.canonicalRoot, "file", rel,
			"state_file", s.StateFile(), "reason", codeOf(err))
		return Proof{}, err
	}
	fs, ok := rec.Files[rel]
	if !ok {
		return Proof{file: rel, generation: rec.Generation}, nil
	}
	entries := make(map[string]Entry, len(fs.Entries))
	for _, e := range fs.Entries {
		entries[e.ID().mapKey()] = e
	}
	return Proof{
		file:              rel,
		generation:        rec.Generation,
		contentDigest:     fs.ContentDigest,
		entries:           entries,
		hookSatisfactions: fs.HookSatisfactions,
	}, nil
}

func codeOf(err error) string {
	if coded, ok := diag.AsCodedError(err); ok {
		return coded.Code
	}
	return "unknown"
}

// Status is the ownership verdict for one existing aggregate entry.
type Status int

const (
	// StatusUnmanaged means there is no proof this entry is ours. It is
	// preserved, never pruned, and a desired entry that collides with it fails
	// (ownership.unmanaged-key).
	StatusUnmanaged Status = iota
	// StatusOwnedClean means the entry is proven ours and still byte-identical
	// to what was written.
	StatusOwnedClean
	// StatusOwnedDrifted means the entry is proven ours but was edited by hand.
	// "Manual edits to a proven owned entry are reported as drift and replaced
	// from marketplace configuration."
	StatusOwnedDrifted
)

func (s Status) String() string {
	switch s {
	case StatusOwnedClean:
		return "owned-clean"
	case StatusOwnedDrifted:
		return "owned-drifted"
	default:
		return "unmanaged"
	}
}

// Proof is evidence of ownership read from the durable record.
//
// Every field is unexported and there is no exported constructor, so a caller
// outside this package cannot fabricate ownership: the only Proof values in
// existence are the zero value (nothing owned) and one returned by
// [Store.Proof]. This is the structural half of "doctor primitives cannot infer
// ownership heuristically".
type Proof struct {
	file              string
	generation        uint64
	contentDigest     string
	entries           map[string]Entry
	hookSatisfactions []HookSatisfaction
}

// File returns the output-relative file the proof covers.
func (p Proof) File() string { return p.file }

// Generation returns the ownership generation the proof was read from.
func (p Proof) Generation() uint64 { return p.generation }

// ContentDigest returns the digest of the file as this package last wrote it.
func (p Proof) ContentDigest() string { return p.contentDigest }

func (p Proof) HookSatisfactions() []HookSatisfaction {
	satisfactions := make([]HookSatisfaction, len(p.hookSatisfactions))
	for i, satisfaction := range p.hookSatisfactions {
		satisfactions[i] = satisfaction
		satisfactions[i].Sources = append([]HookSourceEvidence(nil), satisfaction.Sources...)
	}
	sort.Slice(satisfactions, func(i, j int) bool {
		a, b := satisfactions[i], satisfactions[j]
		return a.Container < b.Container || a.Container == b.Container && a.IdentityDigest < b.IdentityDigest
	})
	for i := range satisfactions {
		sort.Slice(satisfactions[i].Sources, func(a, b int) bool {
			left, right := satisfactions[i].Sources[a], satisfactions[i].Sources[b]
			return left.Resource < right.Resource || left.Resource == right.Resource && left.SourceDigest < right.SourceDigest
		})
	}
	return satisfactions
}

func (p Proof) HasHookResource(resource string) bool {
	if resource == "" {
		return false
	}
	for _, entry := range p.entries {
		if entry.Kind != KindHook {
			continue
		}
		if entry.Resource == resource {
			return true
		}
		for _, evidence := range entry.SourceEvidence {
			if evidence.Resource == resource {
				return true
			}
		}
	}
	for _, satisfaction := range p.hookSatisfactions {
		for _, evidence := range satisfaction.Sources {
			if evidence.Resource == resource {
				return true
			}
		}
	}
	return false
}

// OwnedCount returns how many entries the proof covers.
func (p Proof) OwnedCount() int { return len(p.entries) }

// Classify is the ONLY ownership decision function. It needs both an identity
// and the entry's current value digest, and both are compared against recorded
// digests — never against a marker field, a name pattern, or a similarity
// score.
func (p Proof) Classify(id EntryID, currentValueDigest string) Status {
	recorded, ok := p.entries[id.mapKey()]
	if !ok {
		return StatusUnmanaged
	}
	if recorded.ValueDigest == currentValueDigest {
		return StatusOwnedClean
	}
	return StatusOwnedDrifted
}

// OwnedIDs returns every proven owned identity, sorted deterministically. D10
// uses it to compute stale entries: "Stale proven owned entries are pruned",
// and only proven ones ever can be.
func (p Proof) OwnedIDs() []EntryID {
	ids := make([]EntryID, 0, len(p.entries))
	for _, e := range p.entries {
		ids = append(ids, e.ID())
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].mapKey() < ids[j].mapKey() })
	return ids
}

func (p Proof) Entry(id EntryID) (Entry, bool) {
	e, ok := p.entries[id.mapKey()]
	if !ok {
		return Entry{}, false
	}
	e.SourceEvidence = append([]HookSourceEvidence(nil), e.SourceEvidence...)
	return e, true
}

func (p Proof) ForPluginInstance(instanceKey string, activeInstances map[string]bool) (Proof, []Entry, []HookSatisfaction) {
	filtered := p
	filtered.entries = make(map[string]Entry, len(p.entries))
	var retained []Entry
	for key, entry := range p.entries {
		owners := make(map[string]bool, len(entry.PluginGenerations)+1)
		if entry.PluginInstance != "" {
			owners[entry.PluginInstance] = true
		}
		for _, ref := range entry.PluginGenerations {
			owners[ref.InstanceKey] = true
		}
		if len(owners) == 0 && instanceKey == "" {
			filtered.entries[key] = entry
			continue
		}
		ownerKeys := make([]string, 0, len(owners))
		for owner := range owners {
			ownerKeys = append(ownerKeys, owner)
		}
		sort.Strings(ownerKeys)
		ownedByCurrent := owners[instanceKey] && instanceKey != ""
		otherOwner := ""
		for _, owner := range ownerKeys {
			if owner != instanceKey && activeInstances[owner] {
				otherOwner = owner
				break
			}
		}
		satisfactionOwner := firstActivePluginSatisfaction(p.hookSatisfactions, entry.ID(), activeInstances, instanceKey)
		if ownedByCurrent && otherOwner == "" && satisfactionOwner == "" {
			entry.PluginGenerations = nil
			entry.PluginInstance = instanceKey
			filtered.entries[key] = entry
			continue
		}
		if otherOwner == "" {
			otherOwner = satisfactionOwner
		}
		if otherOwner == "" {
			if len(ownerKeys) > 0 {
				otherOwner = ownerKeys[0]
			}
		}
		if otherOwner == "" {
			retained = append(retained, entry)
			continue
		}
		entry.SourceEvidence = append([]HookSourceEvidence(nil), entry.SourceEvidence...)
		entry.PluginGenerations = nil
		entry.PluginInstance = otherOwner
		retained = append(retained, entry)
	}
	filtered.hookSatisfactions = nil
	var retainedSatisfactions []HookSatisfaction
	for _, satisfaction := range p.hookSatisfactions {
		satisfaction.Sources = append([]HookSourceEvidence(nil), satisfaction.Sources...)
		if satisfaction.PluginInstance == instanceKey {
			filtered.hookSatisfactions = append(filtered.hookSatisfactions, satisfaction)
		} else if satisfaction.PluginInstance != "" || instanceKey != "" {
			retainedSatisfactions = append(retainedSatisfactions, satisfaction)
		}
	}
	sort.Slice(retained, func(i, j int) bool { return retained[i].ID().mapKey() < retained[j].ID().mapKey() })
	sort.Slice(retainedSatisfactions, func(i, j int) bool {
		return retainedSatisfactions[i].ID().mapKey() < retainedSatisfactions[j].ID().mapKey()
	})
	return filtered, retained, retainedSatisfactions
}

func (p Proof) ForPluginInstances(instanceKeys, activeInstances map[string]bool) (Proof, []Entry, []HookSatisfaction) {
	filtered := p
	filtered.entries = make(map[string]Entry, len(p.entries))
	var retained []Entry
	for key, entry := range p.entries {
		ownedByBatch := instanceKeys[entry.PluginInstance]
		for _, ref := range entry.PluginGenerations {
			ownedByBatch = ownedByBatch || instanceKeys[ref.InstanceKey]
		}
		if !ownedByBatch {
			entry.SourceEvidence = append([]HookSourceEvidence(nil), entry.SourceEvidence...)
			if entry.PluginInstance == "" && len(entry.PluginGenerations) > 0 {
				owners := make([]string, 0, len(entry.PluginGenerations))
				for _, ref := range entry.PluginGenerations {
					owners = append(owners, ref.InstanceKey)
				}
				sort.Strings(owners)
				for _, owner := range owners {
					if activeInstances[owner] {
						entry.PluginInstance = owner
						break
					}
				}
				if entry.PluginInstance == "" {
					entry.PluginInstance = owners[0]
				}
			}
			entry.PluginGenerations = nil
			retained = append(retained, entry)
			continue
		}
		entry.PluginGenerations = nil
		filtered.entries[key] = entry
	}
	filtered.hookSatisfactions = nil
	var retainedSatisfactions []HookSatisfaction
	for _, satisfaction := range p.hookSatisfactions {
		if instanceKeys[satisfaction.PluginInstance] && satisfaction.PluginInstance != "" {
			filtered.hookSatisfactions = append(filtered.hookSatisfactions, satisfaction)
		} else {
			satisfaction.Sources = append([]HookSourceEvidence(nil), satisfaction.Sources...)
			retainedSatisfactions = append(retainedSatisfactions, satisfaction)
		}
	}
	sort.Slice(retained, func(i, j int) bool { return retained[i].ID().mapKey() < retained[j].ID().mapKey() })
	sort.Slice(retainedSatisfactions, func(i, j int) bool {
		return retainedSatisfactions[i].ID().mapKey() < retainedSatisfactions[j].ID().mapKey()
	})
	return filtered, retained, retainedSatisfactions
}

func firstActivePluginSatisfaction(satisfactions []HookSatisfaction, id EntryID, active map[string]bool, current string) string {
	for _, satisfaction := range satisfactions {
		if satisfaction.ID().mapKey() == id.mapKey() && satisfaction.PluginInstance != current && active[satisfaction.PluginInstance] {
			return satisfaction.PluginInstance
		}
	}
	return ""
}

// Resource returns the marketplace resource recorded for an owned identity.
func (p Proof) Resource(id EntryID) (string, bool) {
	e, ok := p.entries[id.mapKey()]
	return e.Resource, ok
}

// SourceDigest returns the version-1 source-hook fingerprint recorded for an
// owned hook identity. It is read-only evidence: a missing entry or a missing
// digest returns ("", false) and proves nothing. It does not widen the
// ownership decision — [Classify] remains the only authority on whether the
// entry is ours and unchanged.
func (p Proof) SourceDigest(id EntryID) (string, bool) {
	e, ok := p.entries[id.mapKey()]
	if !ok || e.SourceDigest == "" {
		return "", false
	}
	return e.SourceDigest, true
}

// HasHookSource reports whether the recorded entry for id proves that the
// selected hook named by (resource, digest) contributed to it — either as the
// entry's scalar resource/fingerprint pair or as one exact source_evidence
// tuple. It is evidence lookup only, never a current-byte grant: an absent
// digest, an unknown identity, a non-hook entry, and the zero Proof all
// return false, and the caller remains responsible for the complete
// owned-clean classification of every matching entry.
func (p Proof) HasHookSource(id EntryID, resource, digest string) bool {
	if digest == "" {
		return false
	}
	e, ok := p.entries[id.mapKey()]
	if !ok || e.Kind != KindHook {
		return false
	}
	if e.Resource == resource && e.SourceDigest == digest {
		return true
	}
	for _, ev := range e.SourceEvidence {
		if ev.Resource == resource && ev.SourceDigest == digest {
			return true
		}
	}
	return false
}

// appendLengthPrefixed adds a length-prefixed string to buf. It is used to build
// identity digests so that concatenated fields cannot be re-parsed ambiguously
// (for example Event="ab" + ValueDigest="c" cannot collide with Event="a" +
// ValueDigest="bc").
func appendLengthPrefixed(buf []byte, s string) []byte {
	buf = append(buf, []byte(fmt.Sprintf("%d:", len(s)))...)
	return append(buf, []byte(s)...)
}

// HookIdentityDigest returns the identity digest for a Devin hook. The identity
// is the SHA-256 digest of a hook/v2 prefix, the event name, and the digest of
// the hook's canonical JSON value. Because the value digest covers every byte of
// the canonical rule, any difference in the canonical value or the event
// produces a different identity. No caller can recover the rule from the digest,
// so sensitive hook payloads never enter state, a journal, or backup metadata.
func HookIdentityDigest(event, valueDigest string) string {
	var buf []byte
	buf = append(buf, []byte("hook/v2\x00")...)
	buf = appendLengthPrefixed(buf, event)
	buf = appendLengthPrefixed(buf, valueDigest)
	return Digest(buf)
}

// McpIdentityDigest returns the identity digest for an MCP server. Its identity
// IS the object key, so the digest is redundant for uniqueness; it exists so
// every entry has a uniform identity field and so a future keyed primitive can
// join the same scheme.
func McpIdentityDigest(serverKey string) string {
	var buf []byte
	buf = append(buf, []byte("mcp/v1\x00")...)
	return Digest(appendLengthPrefixed(buf, serverKey))
}
