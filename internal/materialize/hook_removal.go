package materialize

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

type hookRemovalEntry struct {
	root  string
	file  ownedJSONFileSpec
	key   outputKey
	entry ownedJSONEntry
}

// HasActiveNativeHooks is a conservative read-only admission check for native
// hook aggregates. Without aggregate entries, ordinary resource removal need
// not resolve additional sources. Unreadable files require the full check.
func HasActiveNativeHooks(repoRoot string) bool {
	for _, target := range hcc.Targets() {
		if _, ok := targetmeta.LookupHookProjection(targetmeta.Target(target)); !ok {
			continue
		}
		for _, machine := range []bool{false, true} {
			destination, file, err := nativeHookJSONLayout(outputKey{target: target, machineScope: machine}, repoRoot)
			if err != nil {
				return true
			}
			raw, err := ownership.ReadAggregateFileAt(destination.Root, file.rel)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return true
			}
			doc, err := jsonc.Parse(file.rel, raw)
			if err != nil {
				return true
			}
			container, _, err := ownedJSONContainerNode(doc, file)
			if err != nil {
				return true
			}
			if container != nil {
				entries, err := ownedJSONExistingEntries(file, container)
				if err != nil || len(entries) != 0 {
					return true
				}
			}
		}
	}
	return false
}

// CheckHookRemoval identifies native aggregate entries that remain active after
// deselection without clean ownership proof. Matching content is evidence of
// residue only: this read-only check never adopts or deletes an entry.
func CheckHookRemoval(repoRoot string, before, after []resolver.ResolvedArtifact) []diag.Diagnostic {
	type selection struct {
		key          outputKey
		source, name string
	}
	type selectedWork struct {
		selection
		work skillTarget
	}
	selections := func(resolved []resolver.ResolvedArtifact) []selectedWork {
		var out []selectedWork
		for _, resource := range resolver.MaterializationWinners(resolved) {
			if resource.Kind != ir.KindHook {
				continue
			}
			source, err := nativeHookSourceIdentity(resource)
			if err != nil {
				source = resource.RegistryURL + "\x00" + resource.Path
			}
			name := resource.InstalledName
			if name == "" {
				name = resource.Name
			}
			targets := resource.Harnesses
			if targets == nil {
				targets = []hcc.Target{hcc.TargetClaude}
			}
			for _, target := range targets {
				if _, ok := targetmeta.LookupHookProjection(targetmeta.Target(target)); !ok {
					continue
				}
				key := outputKey{target: target, machineScope: resource.Source.IsMachineScope()}
				out = append(out, selectedWork{
					selection: selection{key: key, source: source, name: name},
					work:      skillTarget{skill: resource, target: target, machineScope: key.machineScope},
				})
			}
		}
		return out
	}
	remaining := make(map[selection]bool)
	for _, selected := range selections(after) {
		remaining[selected.selection] = true
	}
	var removed []resolver.ResolvedArtifact
	for _, selected := range selections(before) {
		work := selected.work
		if !remaining[selected.selection] {
			resource := work.skill
			resource.Harnesses = []hcc.Target{work.target}
			removed = append(removed, resource)
		}
	}
	if len(removed) == 0 {
		return nil
	}
	project := func(resolved []resolver.ResolvedArtifact, checkPresence bool) ([]hookRemovalEntry, []diag.Diagnostic) {
		var entries []hookRemovalEntry
		for _, resource := range resolver.MaterializationWinners(resolved) {
			if resource.Kind != ir.KindHook {
				continue
			}
			targets := resource.Harnesses
			if targets == nil {
				targets = []hcc.Target{hcc.TargetClaude}
			}
			for _, target := range targets {
				key := outputKey{target: target, machineScope: resource.Source.IsMachineScope()}
				destination, file, err := nativeHookJSONLayout(key, repoRoot)
				if err != nil {
					if !checkPresence {
						continue
					}
					return nil, []diag.Diagnostic{hookRemovalRefusal(key, resource.Name, file.rel)}
				}
				if checkPresence {
					if _, err := ownership.ReadAggregateFileAt(destination.Root, file.rel); errors.Is(err, os.ErrNotExist) {
						continue
					} else if err != nil {
						return nil, []diag.Diagnostic{hookRemovalRefusal(key, resource.Name, filepath.Join(destination.Root, file.rel))}
					}
				}
				projection, err := prepareNativeHookProjection(skillTarget{skill: resource, target: target, machineScope: key.machineScope}, repoRoot)
				if err != nil {
					// A refused remaining sibling proves no retention, but must
					// not block removal of a different, cleanly owned entry.
					if !checkPresence {
						continue
					}
					if errors.Is(err, errHookSupportReferenceMissing) {
						return nil, []diag.Diagnostic{{
							Code:     diag.CodeHookSupportReferenceMissing,
							Severity: diag.SeverityError,
							Target:   string(key.target),
							Resource: resource.Name,
							Message:  "A plugin-root reference names no installed support file or directory under either meaning; hook suppressed",
							Hint:     "Ship the referenced file in the hook bundle or remove the reference",
						}}
					}
					return nil, []diag.Diagnostic{hookRemovalRefusal(key, resource.Name, filepath.Join(destination.Root, file.rel))}
				}
				name := resource.InstalledName
				if name == "" {
					name = resource.Name
				}
				contributions, err := nativeHookEntriesForArtifacts(key, repoRoot, name, projection.sourceDigest, projection.artifacts)
				if err != nil {
					if !checkPresence {
						continue
					}
					return nil, []diag.Diagnostic{hookRemovalRefusal(key, name, filepath.Join(destination.Root, file.rel))}
				}
				for _, entry := range contributions {
					entries = append(entries, hookRemovalEntry{root: destination.Root, file: file, key: key, entry: entry})
				}
			}
		}
		return entries, nil
	}
	oldEntries, diagnostics := project(removed, true)
	if len(diagnostics) != 0 || len(oldEntries) == 0 {
		return diagnostics
	}
	newEntries, diagnostics := project(after, false)
	if len(diagnostics) != 0 {
		return diagnostics
	}
	for _, old := range oldEntries {
		// ponytail: O(removed * remaining); index identities if bulk removal needs it.
		retained := false
		for _, next := range newEntries {
			if old.root == next.root && old.file.rel == next.file.rel && old.entry.id() == next.entry.id() {
				retained = true
				break
			}
		}
		if retained {
			continue
		}
		path := filepath.Join(old.root, old.file.rel)
		store, err := ownership.Open(old.root)
		if err != nil {
			return []diag.Diagnostic{hookRemovalRefusal(old.key, old.entry.resource, path)}
		}
		raw, err := store.ReadAggregate(old.file.rel)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return []diag.Diagnostic{hookRemovalRefusal(old.key, old.entry.resource, path)}
		}
		doc, err := jsonc.Parse(old.file.rel, raw)
		if err != nil || validateOwnedJSONEnvelope(doc, old.file, false) != nil {
			return []diag.Diagnostic{hookRemovalRefusal(old.key, old.entry.resource, path)}
		}
		container, _, err := ownedJSONContainerNode(doc, old.file)
		if err != nil {
			return []diag.Diagnostic{hookRemovalRefusal(old.key, old.entry.resource, path)}
		}
		if container == nil {
			continue
		}
		existing, err := ownedJSONExistingEntries(old.file, container)
		if err != nil {
			return []diag.Diagnostic{hookRemovalRefusal(old.key, old.entry.resource, path)}
		}
		proof, _ := store.Proof(old.file.rel)
		for _, entry := range existing {
			if entry.container != old.entry.container {
				continue
			}
			matches := entry.value.ID == old.entry.id()
			if !matches {
				value, ok := nativeHookValueWithoutOwnershipMarker(entry.raw)
				matches = ok && ownership.Digest(value) == old.entry.valueDigest()
			}
			if matches && proof.Classify(entry.value.ID, entry.value.ValueDigest) != ownership.StatusOwnedClean {
				return []diag.Diagnostic{hookRemovalRefusal(old.key, old.entry.resource, path)}
			}
		}
	}
	return nil
}

func hookRemovalRefusal(key outputKey, resource, path string) diag.Diagnostic {
	return diag.Diagnostic{
		Code:     diag.CodeHookRemoveUnowned,
		Severity: diag.SeverityError,
		Target:   string(key.target),
		Resource: resource,
		Message:  "Cannot safely remove hook: a native hook may remain active without clean ownership proof",
		Source:   diag.SourceRef{File: path},
		Hint:     "Review the named native file and deliberately remove the hook, or explicitly repair ownership before retrying; configuration was not changed",
	}
}
