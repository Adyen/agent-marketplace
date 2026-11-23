package materialize

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
)

// applyImpliedPreview folds the advisory implied-preview compile result into
// the run: its blocked components suppress pruning in the actual run, and its
// fragments are merged once. Preview compilation diagnostics and blocked
// resources are not selected-resource failures. It returns the
// output keys whose preview fragments could not be merged; those targets are
// skipped so nothing a preview still needs is pruned.
func applyImpliedPreview(compileResult *compilePhaseResult, preview *compilePhaseResult, report *Report) map[outputKey]error {
	if preview == nil {
		return nil
	}
	actualAggregates := compileResult.ownedAggregateState()
	coalesceOpenCodePreview(compileResult.openCodeAggregateScope, preview)
	// A preview hook whose selection is unknown makes the native hook
	// selection authority incomplete, so installed hooks are not pruned.
	for key, unknown := range preview.unknownHookSelection {
		if !unknown {
			continue
		}
		if compileResult.unknownHookSelection == nil {
			compileResult.unknownHookSelection = make(map[outputKey]bool)
		}
		compileResult.unknownHookSelection[key] = true
	}
	// A refused preview hook leaves its native file incomplete in the run too,
	// so the installed entry is retained instead of pruned.
	for key, previewWork := range preview.nativeHooks {
		if previewWork == nil || compileResult.nativeHooks[key] == nil {
			continue
		}
		for rel, incomplete := range previewWork.incomplete {
			if incomplete {
				compileResult.nativeHooks[key].markIncomplete(rel)
			}
		}
	}
	// Previewed hook names join the selection authority so their installed
	// entries are retained, exactly as for a selected hook that is blocked.
	for key, names := range preview.selectedHookNames {
		if compileResult.selectedHookNames == nil {
			compileResult.selectedHookNames = make(map[outputKey]map[string]bool)
		}
		if compileResult.selectedHookNames[key] == nil {
			compileResult.selectedHookNames[key] = make(map[string]bool)
		}
		for name := range names {
			compileResult.selectedHookNames[key][name] = true
		}
	}
	for key, components := range preview.blocked {
		for component := range components {
			compileResult.blocked.mark(key, component)
		}
		if components[blockedAllComponents] {
			if binding, hasBinding, bindingErr := materializeAdapterFor(key.target); bindingErr == nil && hasBinding {
				binding.handler.markIncomplete(actualAggregates, key, ir.KindHook)
				binding.handler.markIncomplete(actualAggregates, key, ir.KindMcpServer)
			}
		}
	}
	for _, blockedResource := range preview.blockedResources {
		binding, hasBinding, bindingErr := materializeAdapterFor(blockedResource.Target)
		if bindingErr != nil || !hasBinding {
			continue
		}
		binding.handler.markIncomplete(actualAggregates, outputKey{target: blockedResource.Target, machineScope: blockedResource.MachineScope}, blockedResource.Kind)
	}
	mergeErrs := make(map[outputKey]error)
	for key, tw := range preview.byTarget {
		merged, mergeErr := artifact.MergeFragments(tw.artifacts)
		if mergeErr != nil {
			mergeErrs[key] = mergeErr
			report.Diags = append(report.Diags, diag.Diagnostic{
				Code:     "materialize.merge.error",
				Severity: diag.SeverityError,
				Target:   string(key.target),
				Message:  fmt.Sprintf("merge preview fragments for target %s: %v", key.target, mergeErr),
			})
			continue
		}
		tw.artifacts = merged
	}
	return mergeErrs
}

// coalesceOpenCodePreview moves preview OpenCode work onto the scope that
// reconciles a shared OPENCODE_CONFIG aggregate, so retention sees entries
// implied from either scope.
func coalesceOpenCodePreview(owningScope *bool, preview *compilePhaseResult) {
	if owningScope == nil || preview == nil {
		return
	}
	from := outputKey{target: hcc.TargetOpenCode, machineScope: !*owningScope}
	to := outputKey{target: hcc.TargetOpenCode, machineScope: *owningScope}
	moved, _ := preview.ownedState[from].(*ownedJSONAggregateWork)
	if moved == nil {
		return
	}
	existing, _ := preview.ownedState[to].(*ownedJSONAggregateWork)
	preview.ownedState[to] = mergeOwnedJSONAggregateWork(existing, moved)
	delete(preview.ownedState, from)
}

func previewAggregateState(preview *compilePhaseResult) ownedAggregateState {
	if preview == nil {
		return ownedAggregateState{}
	}
	return preview.ownedAggregateState()
}

func previewArtifacts(key outputKey, preview *compilePhaseResult) []artifact.Artifact {
	if preview == nil {
		return nil
	}
	tw := preview.byTarget[key]
	if tw == nil {
		return nil
	}
	return tw.artifacts
}

func previewNativeMCPEntries(key outputKey, preview *compilePhaseResult) []ownedJSONEntry {
	if preview == nil {
		return nil
	}
	return preview.nativeMCPEntries[key]
}

// previewKeepPaths names the report-relative regular outputs an admitted
// preview would produce; existing managed outputs at these paths are retained.
func previewKeepPaths(key outputKey, outDir string, preview *compilePhaseResult) map[string]bool {
	keep := make(map[string]bool)
	if preview == nil {
		return keep
	}
	plans := materializePlans(key.target, outDir, previewArtifacts(key, preview), linkedComponents(preview.skillLinks[key]), key.machineScope)
	for _, plan := range plans {
		regular, _ := splitMergeArtifacts(plan.artifacts)
		for _, a := range regular {
			keep[filepath.ToSlash(filepath.Join(plan.reportPrefix, a.Path))] = true
		}
	}
	for _, group := range preview.skillLinks[key] {
		for _, link := range group.links {
			keep[filepath.ToSlash(filepath.Join(group.reportPrefix, link.name))] = true
		}
	}
	return keep
}

// previewMergeBytes maps each absolute merge-file path to the preview's
// fragment for it.
func previewMergeBytes(key outputKey, outDir string, preview *compilePhaseResult) map[string][]byte {
	out := make(map[string][]byte)
	if preview == nil {
		return out
	}
	for _, plan := range materializePlans(key.target, outDir, previewArtifacts(key, preview), linkedComponents(preview.skillLinks[key]), key.machineScope) {
		_, mergeArtifacts := splitMergeArtifacts(plan.artifacts)
		for _, a := range mergeArtifacts {
			out[filepath.Join(plan.outDir, filepath.FromSlash(a.Path))] = a.Bytes
		}
	}
	return out
}

func retainedPreviewMergeBytes(existing, desired, preview []byte) ([]byte, error) {
	doc, err := jsonc.Parse("", existing)
	if err != nil || doc.Root().Kind() != jsonc.KindObject {
		return desired, nil
	}
	var desiredDoc, previewDoc map[string]json.RawMessage
	if err := json.Unmarshal(desired, &desiredDoc); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(preview, &previewDoc); err != nil {
		return desired, nil
	}
	return augmentDesiredWithPreviewManaged(doc, desiredDoc, previewDoc, desired)
}

// canonicalManagedElemKey identifies an array element as it appears once
// installed, i.e. carrying the managed-by marker.
func canonicalManagedElemKey(raw json.RawMessage) string {
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		return canonicalMemberKey(raw)
	}
	value[managedByField] = managedByValue
	canonical, err := json.Marshal(value)
	if err != nil {
		return canonicalMemberKey(raw)
	}
	return string(canonical)
}

func previewKeepIdentities(preview []byte) map[string]bool {
	if len(preview) == 0 {
		return nil
	}
	doc, err := jsonc.Parse("", preview)
	if err != nil || doc.Root().Kind() != jsonc.KindObject {
		return nil
	}
	keep := make(map[string]bool)
	root := doc.Root()
	for _, container := range root.Keys() {
		node, ok := root.Member(container)
		if !ok {
			continue
		}
		switch node.Kind() {
		case jsonc.KindObject:
			for _, name := range node.Keys() {
				keep[container+"\x00key\x00"+name] = true
			}
		case jsonc.KindArray:
			for i := 0; i < node.Len(); i++ {
				if elem, ok := node.Element(i); ok {
					keep[container+"\x00elem\x00"+canonicalManagedElemKey(elem.Canonical())] = true
				}
			}
		case jsonc.KindLiteral:
		}
	}
	return keep
}
