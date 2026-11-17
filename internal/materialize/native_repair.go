package materialize

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

type nativeRepairFileKey struct {
	root      string
	rel       string
	container string
}

// nativeRepairMCPArtifact is one compiled MCP fragment; markerValue overrides
// the policy marker for plugin-owned fragments, matching sync's plugin marker.
type nativeRepairMCPArtifact struct {
	output      artifact.Artifact
	markerValue string
}

type nativeRepairDesiredFile struct {
	spec    ownedJSONFileSpec
	entries []ownedJSONEntry
	invalid bool
}

func nativeAggregateRepairPreview(ctx context.Context, repoRoot string, resolved []resolver.ResolvedArtifact, target hcc.Target) (*RepairPreview, error) {
	compiled, _ := compileRepairTarget(repoRoot, resolved, target)
	preview := &RepairPreview{Diags: append([]diag.Diagnostic(nil), compiled.allDiags...)}
	if target == hcc.TargetClaude {
		if d := preflightClaudeDestinations(&compiled, repoRoot); d != nil {
			preview.Diags = append(preview.Diags, *d)
		}
	}
	diagnosticKeys := make([]outputKey, 0, len(compiled.byTarget))
	for key, work := range compiled.byTarget {
		if key.target == target && work != nil {
			diagnosticKeys = append(diagnosticKeys, key)
		}
	}
	sort.Slice(diagnosticKeys, func(i, j int) bool {
		return !diagnosticKeys[i].machineScope && diagnosticKeys[j].machineScope
	})
	for _, key := range diagnosticKeys {
		preview.Diags = append(preview.Diags, compiled.byTarget[key].diags...)
	}
	coalescence := geminiAggregateCoalescence{}
	if target == hcc.TargetGemini {
		report := &Report{}
		var err error
		coalescence, err = coalesceGeminiNativeAggregates(&compiled, repoRoot, report)
		preview.Diags = append(preview.Diags, report.Diags...)
		if err != nil {
			preview.Diags = append(preview.Diags, diag.Diagnostic{
				Code:     diag.CodeMaterializeDestinationOverlap,
				Severity: diag.SeverityError,
				Target:   string(target),
				Message:  "repository and machine Gemini selections conflict in a shared native aggregate",
			})
			return preview, nil
		}
	}

	keys := make([]outputKey, 0, len(compiled.allOutputs))
	for key := range compiled.allOutputs {
		if key.target == target && !(coalescence.isRepo(key)) {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		return !keys[i].machineScope && keys[j].machineScope
	})

	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		destination, hookSpec, err := nativeHookJSONLayout(key, repoRoot)
		if err != nil {
			preview.Diags = append(preview.Diags, nativeRepairDiagnostic(key.target, "native aggregate destination could not be resolved safely"))
			continue
		}
		files := make(map[nativeRepairFileKey]*nativeRepairDesiredFile)
		fileFor := func(root string, spec ownedJSONFileSpec) *nativeRepairDesiredFile {
			fileKey := nativeRepairFileKey{root: root, rel: spec.rel, container: spec.container}
			file := files[fileKey]
			if file == nil {
				file = &nativeRepairDesiredFile{spec: spec}
				files[fileKey] = file
			}
			return file
		}
		add := func(root string, spec ownedJSONFileSpec, entry ownedJSONEntry) {
			file := fileFor(root, spec)
			for _, prior := range file.entries {
				if prior.id() != entry.id() {
					continue
				}
				if !bytes.Equal(prior.raw, entry.raw) {
					file.invalid = true
				}
				return
			}
			file.entries = append(file.entries, entry)
		}

		var pluginPlans []*pluginInstancePlan
		for _, plan := range compiled.plugins {
			if plan.key != key && (!coalescence.isOwner(key) || !coalescence.includes(plan.key)) {
				continue
			}
			pluginPlans = append(pluginPlans, plan)
		}
		sort.Slice(pluginPlans, func(i, j int) bool {
			return pluginPlans[i].ref.InstanceKey < pluginPlans[j].ref.InstanceKey
		})

		fileFor(destination.Root, hookSpec)
		hookWork := compiled.nativeHooks[key]
		for _, plan := range pluginPlans {
			hookWork = mergeOwnedJSONAggregateWork(hookWork, plan.hooks)
		}
		if hookWork != nil {
			for _, entry := range hookWork.entries {
				add(destination.Root, hookSpec, entry)
			}
		}

		mcpDeclaration, hasMCP := targetmeta.NativeAggregateFileForScope(targetmeta.Target(target), ir.KindMcpServer, key.machineScope)
		if hasMCP {
			if root, rel, container, ok := nativeMCPEntryDestination(key, repoRoot, destination, hookSpec, mcpDeclaration.Path); ok {
				fileFor(root, ownedJSONFileSpec{rel: rel, container: container, keyed: true})
			}
		}
		for _, entry := range compiled.nativeMCPEntries[key] {
			root, rel, container, ok := nativeMCPEntryDestination(key, repoRoot, destination, hookSpec, entry.rel)
			if !ok {
				preview.Diags = append(preview.Diags, nativeRepairDiagnostic(target, "compiled MCP output could not be routed to its declared aggregate"))
				continue
			}
			spec := ownedJSONFileSpec{rel: rel, container: container, keyed: true}
			entry.rel, entry.container = rel, container
			add(root, spec, entry)
		}

		if hasMCP {
			destinations, destinationErr := harness.ResolveDestinations(target, key.machineScope, repoRoot)
			if destinationErr != nil {
				preview.Diags = append(preview.Diags, nativeRepairDiagnostic(target, "native MCP destinations could not be resolved safely"))
				continue
			}
			var artifacts []nativeRepairMCPArtifact
			if work := compiled.byTarget[key]; work != nil {
				for _, output := range work.artifacts {
					artifacts = append(artifacts, nativeRepairMCPArtifact{output: output})
				}
			}
			for _, plan := range pluginPlans {
				for _, output := range plan.artifacts {
					artifacts = append(artifacts, nativeRepairMCPArtifact{output: output, markerValue: pluginMarkerValue(plan.ref.InstanceKey)})
				}
			}
			for _, source := range artifacts {
				output := source.output
				root, rel, container, routed := nativeMCPArtifactDestination(destinations, mcpDeclaration, output.Path)
				if !routed {
					continue
				}
				policy, policyErr := output.Policy()
				if policyErr != nil {
					preview.Diags = append(preview.Diags, nativeRepairDiagnostic(target, "compiled MCP fragment policy could not be validated"))
					fileFor(root, ownedJSONFileSpec{rel: rel, container: container, keyed: true}).invalid = true
					continue
				}
				markerField, markerValue := "", ""
				if policy.Mode == artifact.ReconciliationMarker {
					markerField, markerValue = policy.MarkerField, policy.MarkerValue
					if source.markerValue != "" {
						markerValue = source.markerValue
					}
				}
				entries, decodeErr := nativeMCPFragmentEntries(rel, container, output.Bytes, markerField, markerValue)
				if decodeErr != nil {
					preview.Diags = append(preview.Diags, nativeRepairDiagnostic(target, "compiled MCP fragment could not be decoded safely"))
					fileFor(root, ownedJSONFileSpec{rel: rel, container: container, keyed: true}).invalid = true
					continue
				}
				for _, entry := range entries {
					add(root, ownedJSONFileSpec{rel: rel, container: container, keyed: true}, entry)
				}
			}
		}

		fileKeys := make([]nativeRepairFileKey, 0, len(files))
		for fileKey := range files {
			fileKeys = append(fileKeys, fileKey)
		}
		sort.Slice(fileKeys, func(i, j int) bool {
			if fileKeys[i].root != fileKeys[j].root {
				return fileKeys[i].root < fileKeys[j].root
			}
			if fileKeys[i].rel != fileKeys[j].rel {
				return fileKeys[i].rel < fileKeys[j].rel
			}
			return fileKeys[i].container < fileKeys[j].container
		})
		for _, fileKey := range fileKeys {
			file := files[fileKey]
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if file.invalid || nativeRepairFileIncomplete(compiled, key, target, file) {
				continue
			}
			path := filepath.Join(fileKey.root, fileKey.rel)
			if _, err := os.Lstat(path); err != nil {
				if !os.IsNotExist(err) {
					preview.Diags = append(preview.Diags, nativeRepairDiagnostic(target, "native aggregate could not be inspected safely"))
				}
				continue
			}
			store, err := ownership.Open(fileKey.root, ownership.WithLockObserver(observeRepairLock))
			if err != nil {
				preview.Diags = append(preview.Diags, nativeRepairDiagnostic(target, "native aggregate ownership could not be inspected safely"))
				continue
			}
			options := defaultNativeHookReconcileOptions()
			filePreview, diagnostics := previewOwnedJSONAggregateFile(store, key, fileKey.root, file.spec, options, true, file.entries)
			preview.Diags = append(preview.Diags, diagnostics...)
			if filePreview != nil {
				preview.Files = append(preview.Files, filePreview)
			}
		}
	}
	return preview, nil
}

func nativeMCPEntryDestination(key outputKey, repoRoot string, hookDestination harness.Destination, hookSpec ownedJSONFileSpec, entryRel string) (string, string, string, bool) {
	if key.target == hcc.TargetGemini &&
		(entryRel == hookSpec.rel || filepath.Base(entryRel) == filepath.Base(hookSpec.rel)) {
		return hookDestination.Root, hookSpec.rel, "mcpServers", true
	}
	declaration, ok := targetmeta.NativeAggregateFileForScope(targetmeta.Target(key.target), ir.KindMcpServer, key.machineScope)
	if !ok {
		return "", "", "", false
	}
	destinations, err := harness.ResolveDestinations(key.target, key.machineScope, repoRoot)
	if err != nil {
		return "", "", "", false
	}
	for _, destination := range destinations {
		if len(destination.AggregateRoutes) > 0 {
			for _, route := range destination.AggregateRoutes {
				if route.Container == declaration.Member &&
					(destination.AggregateFile == entryRel || filepath.Base(route.ArtifactPath) == filepath.Base(declaration.Path) && entryRel == declaration.Path) {
					return destination.Root, destination.AggregateFile, route.Container, true
				}
			}
		}
	}
	for _, destination := range destinations {
		if len(destination.AggregateRoutes) > 0 {
			continue
		}
		if declaration.Path == entryRel {
			return destination.Root, declaration.Path, declaration.Member, true
		}
	}
	return "", "", "", false
}

func nativeMCPArtifactDestination(destinations []harness.Destination, declaration targetmeta.NativeAggregateFile, artifactPath string) (string, string, string, bool) {
	for _, destination := range destinations {
		if len(destination.AggregateRoutes) > 0 {
			for _, route := range destination.AggregateRoutes {
				if route.Container == declaration.Member &&
					(route.ArtifactPath == artifactPath || filepath.Base(route.ArtifactPath) == filepath.Base(artifactPath) &&
						filepath.Base(route.ArtifactPath) == filepath.Base(declaration.Path)) {
					return destination.Root, destination.AggregateFile, route.Container, true
				}
			}
		}
	}
	for _, destination := range destinations {
		if len(destination.AggregateRoutes) > 0 {
			continue
		}
		rel, err := destination.RouteArtifact(artifactPath)
		if err == nil && rel == declaration.Path {
			return destination.Root, rel, declaration.Member, true
		}
		if destination.Info.PlanStrategy == harness.PlanWindsurf && strings.HasPrefix(artifactPath, windsurfDir+"/") &&
			strings.TrimPrefix(artifactPath, windsurfDir+"/") == declaration.Path {
			return destination.Root, declaration.Path, declaration.Member, true
		}
	}
	return "", "", "", false
}

func nativeMCPFragmentEntries(rel, container string, raw []byte, markerField, markerValue string) ([]ownedJSONEntry, error) {
	var fragment map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fragment); err != nil {
		return nil, err
	}
	var servers map[string]json.RawMessage
	if err := json.Unmarshal(fragment[container], &servers); err != nil {
		return nil, err
	}
	entries := make([]ownedJSONEntry, 0, len(servers))
	for _, name := range sortedKeys(servers) {
		canonical, err := canonicalOwnedJSON(servers[name])
		if err != nil {
			return nil, err
		}
		if markerField != "" {
			var value map[string]json.RawMessage
			if err := json.Unmarshal(canonical, &value); err != nil {
				return nil, err
			}
			marker, err := json.Marshal(markerValue)
			if err != nil {
				return nil, err
			}
			value[markerField] = marker
			canonical, err = json.Marshal(value)
			if err != nil {
				return nil, err
			}
		}
		entries = append(entries, ownedJSONEntry{
			rel:       rel,
			kind:      ownership.KindMcpServer,
			container: container,
			key:       name,
			identity:  ownership.McpIdentityDigest(name),
			raw:       canonical,
			resource:  name,
		})
	}
	return entries, nil
}

func nativeRepairFileIncomplete(compiled compilePhaseResult, key outputKey, target hcc.Target, file *nativeRepairDesiredFile) bool {
	if file.spec.hookShape != "" {
		if compiled.unknownHookSelection[key] {
			return true
		}
		if hooks := compiled.nativeHooks[key]; hooks != nil && hooks.incomplete[file.spec.rel] {
			return true
		}
		blocked := compiled.blocked.componentsFor(key)
		return blocked[blockedAllComponents] || blocked[resourceInstallDirRel(target, ir.KindHook)]
	}
	if compiled.nativeMCPIncomplete[key] || len(compiled.nativeMCPConflicts[key]) > 0 {
		return true
	}
	blocked := compiled.blocked.componentsFor(key)
	return blocked[blockedAllComponents] ||
		blocked[resourceInstallDirRel(target, ir.KindMcpServer)]
}

func nativeRepairDiagnostic(target hcc.Target, message string) diag.Diagnostic {
	return diag.Diagnostic{Code: "materialize.destination.error", Severity: diag.SeverityError, Target: string(target), Message: message}
}
