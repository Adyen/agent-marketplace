package materialize

import (
	"errors"
	"fmt"
	"os"

	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

// nativeHookLayout is the documented native hook file for one (target, scope)
// pair, fixed by the hook-native-location-tolerance spec table. rel is the
// file's path relative to the target's resolved destination root, which is
// also the canonical root the ownership store is opened on. container is the
// object member carrying the hook event map — "" for Factory Droid, whose
// file IS the top-level event map. requireVersion pins Cursor's envelope,
// which documents `version: 1` beside `hooks`.
type nativeHookLayout struct {
	rel            string
	container      string
	requireVersion bool
	shape          targetmeta.HookShape
	envelope       map[string]any
}

// nativeHookLayoutFor returns the single documented native hook file for a
// (target, machineScope) pair. It never guesses: the table below lists only
// the files PR #403 plans to write, and a target without a native hook file
// gets no layout rather than a broad root walk.
func nativeHookLayoutFor(target hcc.Target, machineScope bool) (nativeHookLayout, bool) {
	switch target {
	case hcc.TargetClaude:
		// .claude/settings.json / ~/.claude/settings.json, member "hooks".
		return nativeHookLayout{rel: "settings.json", container: "hooks"}, true
	case hcc.TargetCursor:
		// .cursor/hooks.json / ~/.cursor/hooks.json, member "hooks" plus the
		// documented `version: 1` envelope.
		return nativeHookLayout{rel: "hooks.json", container: "hooks", requireVersion: true}, true
	case hcc.TargetFactory:
		// .factory/hooks.json / ~/.factory/hooks.json, top-level event map.
		return nativeHookLayout{rel: "hooks.json"}, true
	case hcc.TargetWindsurf:
		if machineScope {
			// ~/.codeium/windsurf/hooks.json — the same file the legacy
			// marker merge touches, so inspection must stay read-only and
			// keep unproven siblings in place.
			return nativeHookLayout{rel: "hooks.json", container: "hooks"}, true
		}
		// <repo>/.devin/hooks.json under the repository-scope root.
		return nativeHookLayout{rel: ".devin/hooks.json", container: "hooks"}, true
	}
	return nativeHookLayout{}, false
}

// NativeHookEntry is one hook rule found in a target's documented native
// hook file, classified against durable ownership proof. It carries digests
// only — never rule bytes — so inspection cannot leak a hook payload.
type NativeHookEntry struct {
	// Event is the hook event name the rule sits under (its container).
	Event string
	// ID is the hook/v2 ownership identity derived from the event and the
	// canonical rule digest.
	ID ownership.EntryID
	// ValueDigest is the digest of the rule's canonical JSON bytes as found
	// on disk.
	ValueDigest string
	// Status is the ownership verdict for the current bytes.
	Status ownership.Status
	// SourceDigest is the version-1 source-hook fingerprint recorded with
	// the entry's ownership proof. It is exposed only when Status is
	// StatusOwnedClean: an unmanaged or drifted entry carries no usable
	// proof, so its recorded digest (if any) is withheld rather than
	// presented as evidence.
	SourceDigest string
}

// InspectNativeHooks performs the read-only half of the
// hook-native-location-tolerance contract: it opens exactly the documented
// native hook file for (target, machineScope) under outDir, parses it
// strictly, and classifies every rule against existing ownership proof.
//
// It never writes the native file, the legacy file, ownership state, or the
// state root: ownership.Open and Store.Proof are side-effect free, and the
// file is read through ownership.ReadAggregateFileAt, which resolves every
// path component beneath outDir through descriptor-anchored openat calls with
// O_NOFOLLOW, fstat-validates the opened descriptor as a regular file without
// extra hard links, and reads it through a bounded LimitReader. A missing file
// or a missing hooks member yields no entries and no error — absent native
// output simply proves nothing.
//
// The check fails closed for THIS file only: malformed JSON, a wrong
// envelope (non-object root, a non-object hooks member, a missing or wrong
// Cursor version, a non-array event, or a non-object rule), a duplicate JSON
// key, an unsafe path, or corrupt ownership state returns a coded error and
// no entries. The caller decides which target to refuse; independent targets
// are unaffected.
func InspectNativeHooks(store *ownership.Store, outDir string, target hcc.Target, machineScope bool) ([]NativeHookEntry, error) {
	ins, err := inspectNativeHookFile(store, outDir, target, machineScope)
	if err != nil {
		return nil, err
	}
	return ins.entries, nil
}

// inspectNativeHookFile is the shared strict read behind InspectNativeHooks
// and the planner's cached inspection. Beyond the classified entries it
// retains the durable proof and the current entry digests so a caller can
// apply the full source-fingerprint match — including detecting a recorded
// hook entry that is no longer cleanly present (partial proof).
func inspectNativeHookFile(store *ownership.Store, outDir string, target hcc.Target, machineScope bool) (*nativeHookInspection, error) {
	layout, ok := nativeHookLayoutFor(target, machineScope)
	if !ok {
		return nil, nativeHookRefused(outDir, "", fmt.Errorf("target %s has no documented native hook file", target))
	}
	if err := harness.ValidateRelPath(layout.rel); err != nil {
		return nil, nativeHookRefused(outDir, layout.rel, err)
	}
	raw, err := ownership.ReadAggregateFileAt(outDir, layout.rel)
	if err != nil {
		if os.IsNotExist(err) {
			return &nativeHookInspection{}, nil
		}
		return nil, nativeHookRefused(outDir, layout.rel, err)
	}

	doc, err := jsonc.Parse(layout.rel, raw)
	if err != nil {
		// A duplicate key keeps its own stable code; every other parse
		// failure is an ambiguous native file and refuses with the
		// tolerance code.
		if coded, ok := diag.AsCodedError(err); ok && coded.Code == diag.CodeJSONDuplicateKey {
			return nil, err
		}
		return nil, nativeHookRefused(outDir, layout.rel, err)
	}
	root := doc.Root()
	if root.Kind() != jsonc.KindObject {
		return nil, nativeHookRefused(outDir, layout.rel, fmt.Errorf("native hook file root is not a JSON object"))
	}
	if layout.requireVersion {
		if err := nativeHookCheckVersion(root); err != nil {
			return nil, nativeHookRefused(outDir, layout.rel, err)
		}
	}

	container := root
	if layout.container != "" {
		node, ok := root.Member(layout.container)
		if !ok {
			// No hooks member: the file exists but proves nothing.
			return &nativeHookInspection{}, nil
		}
		if node.Kind() != jsonc.KindObject {
			return nil, nativeHookRefused(outDir, layout.rel, fmt.Errorf("native hook member %q is not a JSON object", layout.container))
		}
		container = node
	}

	existing, err := nativeHookEntries(outDir, layout, container)
	if err != nil {
		return nil, err
	}

	proof, proofErr := store.Proof(layout.rel)
	if proofErr != nil && !errors.Is(proofErr, ownership.ErrStateMissing) {
		// Corrupt ownership evidence cannot prove anything and must not be
		// waved through as "unmanaged": refuse the file.
		return nil, nativeHookRefused(outDir, layout.rel, proofErr)
	}

	entries := make([]NativeHookEntry, 0, len(existing))
	current := make(map[ownership.EntryID]string, len(existing))
	for _, item := range existing {
		entry := NativeHookEntry{
			Event:       item.value.ID.Container,
			ID:          item.value.ID,
			ValueDigest: item.value.ValueDigest,
			Status:      proof.Classify(item.value.ID, item.value.ValueDigest),
		}
		if entry.Status == ownership.StatusOwnedClean {
			if digest, ok := proof.SourceDigest(item.value.ID); ok {
				entry.SourceDigest = digest
			}
		}
		entries = append(entries, entry)
		current[item.value.ID] = item.value.ValueDigest
	}
	return &nativeHookInspection{entries: entries, current: current, proof: proof}, nil
}

// nativeHookEntries enumerates a hook container strictly: every member must
// be an event array and every element a JSON object. It mirrors the identity
// derivation of ownedJSONExistingEntries so a rule's hook/v2 identity and
// canonical value digest match what the native writer will record.
func nativeHookEntries(outDir string, layout nativeHookLayout, container *jsonc.Node) ([]ownedJSONExistingEntry, error) {
	var out []ownedJSONExistingEntry
	for _, event := range container.Keys() {
		node, _ := container.Member(event)
		if node.Kind() != jsonc.KindArray {
			return nil, nativeHookRefused(outDir, layout.rel, fmt.Errorf("native hook event %q is not a JSON array", event))
		}
		for i := 0; i < node.Len(); i++ {
			element, _ := node.Element(i)
			if element.Kind() != jsonc.KindObject {
				return nil, nativeHookRefused(outDir, layout.rel, fmt.Errorf("native hook event %q entry %d is not a JSON object", event, i))
			}
			canonical, err := jsonc.Canonicalize(layout.rel, element.Raw())
			if err != nil {
				return nil, nativeHookRefused(outDir, layout.rel, fmt.Errorf("native hook event %q entry %d is not valid JSON: %w", event, i, err))
			}
			normalized, err := canonicalOwnedJSON(canonical)
			if err != nil {
				return nil, nativeHookRefused(outDir, layout.rel, fmt.Errorf("native hook event %q entry %d could not be normalized: %w", event, i, err))
			}
			valueDigest := ownership.Digest(normalized)
			out = append(out, ownedJSONExistingEntry{
				value: ownership.EntryValue{
					ID:          ownership.EntryID{Kind: ownership.KindHook, Container: event, IdentityDigest: ownership.HookIdentityDigest(event, valueDigest)},
					ValueDigest: valueDigest,
				},
			})
		}
	}
	return out, nil
}

// nativeHookCheckVersion enforces the documented `version: 1` envelope on
// files that declare one (Cursor). A missing or non-1 version is a wrong
// envelope, not a hook file this reader may interpret.
func nativeHookCheckVersion(root *jsonc.Node) error {
	node, ok := root.Member("version")
	if !ok {
		return fmt.Errorf("native hook file is missing the documented version member")
	}
	canonical, err := canonicalOwnedJSON(node.Raw())
	if err != nil || string(canonical) != "1" {
		return fmt.Errorf("native hook file version is not the documented version 1")
	}
	return nil
}

// nativeHookRefused builds the stable coded refusal for an unsafe or
// ambiguous native hook file. The target's other files and other targets are
// unaffected; the message carries the file identity and a field-level cause,
// never hook payload bytes.
func nativeHookRefused(outDir, rel string, err error) *diag.CodedError {
	file := rel
	if file == "" {
		file = outDir
	}
	return diag.NewCodedError(diag.CodeHookNativeToleranceRefused, diag.SourceRef{File: file}, err)
}
