package materialize

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
)

type legacyFallbackError struct {
	rel string
	err error
}

func (e legacyFallbackError) Error() string {
	return fmt.Sprintf("legacy fallback %q: %v", e.rel, e.err)
}

func (e legacyFallbackError) Unwrap() error {
	return e.err
}

func importLegacyHandwrittenFallbacks(outDir string, file ownedJSONFileSpec, rels []string, existing []ownedJSONExistingEntry, hasDesired bool) ([]ownedJSONExistingEntry, []nativeHookAggregateSnapshot, error) {
	if len(rels) == 0 || len(existing) > 0 || !hasDesired {
		return nil, nil, nil
	}
	var imported []ownedJSONExistingEntry
	var snapshots []nativeHookAggregateSnapshot
	for _, rel := range rels {
		raw, err := ownership.ReadAggregateFileAt(outDir, rel)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, nil, legacyFallbackError{rel: rel, err: err}
		}
		abs := filepath.Join(outDir, filepath.FromSlash(rel))
		snapshot, err := nativeHookAggregateSnapshotAtPlan(abs, rel, raw, nil)
		if err != nil {
			return nil, nil, err
		}
		snapshots = append(snapshots, *snapshot)
		fallback := ownedJSONFileSpec{rel: rel, container: file.container, hookShape: file.hookShape}
		doc, err := jsonc.Parse(rel, raw)
		if err != nil {
			return nil, nil, legacyFallbackError{rel: rel, err: err}
		}
		if err := validateOwnedJSONEnvelope(doc, fallback, false); err != nil {
			return nil, nil, legacyFallbackError{rel: rel, err: err}
		}
		container, _, err := ownedJSONContainerNode(doc, fallback)
		if err != nil {
			return nil, nil, legacyFallbackError{rel: rel, err: err}
		}
		if container == nil {
			continue
		}
		for _, event := range container.Keys() {
			node, _ := container.Member(event)
			if node.Kind() != jsonc.KindArray {
				return nil, nil, legacyFallbackError{rel: rel, err: fmt.Errorf("event %q is not an array of hook handlers", event)}
			}
			for i := 0; i < node.Len(); i++ {
				element, _ := node.Element(i)
				if isManagedNode(element) {
					continue
				}
				if err := validateOwnedJSONHookRule(file.hookShape, element); err != nil {
					return nil, nil, legacyFallbackError{rel: rel, err: err}
				}
				canonical, err := jsonc.Canonicalize(rel, element.Raw())
				if err != nil {
					return nil, nil, legacyFallbackError{rel: rel, err: err}
				}
				normalized, err := canonicalOwnedJSON(canonical)
				if err != nil {
					return nil, nil, legacyFallbackError{rel: rel, err: err}
				}
				identity, err := ownedJSONHookIdentity(event, normalized)
				if err != nil {
					return nil, nil, legacyFallbackError{rel: rel, err: err}
				}
				valueDigest := ownership.Digest(normalized)
				if file.rawValueDigest {
					valueDigest = ownership.Digest(element.Raw())
				}
				imported = append(imported, ownedJSONExistingEntry{
					value:     ownership.EntryValue{ID: ownership.EntryID{Kind: ownership.KindHook, Container: event, IdentityDigest: identity}, ValueDigest: valueDigest},
					container: event,
					raw:       element.Raw(),
				})
			}
		}
	}
	return imported, snapshots, nil
}

func appendLegacyFallbackAssertions(extras *nativeHookTransactionExtras, snapshots []nativeHookAggregateSnapshot) error {
	for _, snapshot := range snapshots {
		matched := false
		for _, participants := range [][]dependencytxn.DesiredFile{extras.participants, extras.assertions} {
			for _, participant := range participants {
				if participant.Path != snapshot.abs {
					continue
				}
				if participant.Expected != snapshot.generation || participant.ExpectedMode.Perm() != snapshot.mode.Perm() {
					return fmt.Errorf("%w: %s", dependencytxn.ErrGenerationConflict, snapshot.abs)
				}
				matched = true
			}
		}
		if !matched {
			extras.assertions = append(extras.assertions, nativeHookSupportSnapshotAssertion(&snapshot))
		}
	}
	return nil
}
