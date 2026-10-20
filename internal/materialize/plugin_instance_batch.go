package materialize

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/packagegen"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

type pluginInstanceBatchWork struct {
	plan    *pluginInstancePlan
	ref     ownership.PluginGenerationRef
	removed bool
}

type pluginInstanceBatchGroup struct {
	key            outputKey
	repoRoot       string
	batch          *pluginInstanceBatch
	portable       *portableState
	outDir         string
	destinations   []harness.Destination
	nativeHooks    *ownedJSONAggregateWork
	nativeMCP      []ownedJSONEntry
	nativeHook     bool
	markerUpdates  map[string][]pluginOutputPlan
	markerRemovals map[string][]ownership.PluginOutputRef
	aggregateState ownedAggregateState
	aggregateSet   bool
	handler        ownedAggregateHandler
	ownerInstances map[string]bool
}

func mergePluginAggregateState(dst *ownedAggregateState, src ownedAggregateState, key outputKey) {
	if dst.values == nil {
		dst.values = make(map[outputKey]any)
	}
	switch source := src.get(key).(type) {
	case *ownedJSONAggregateWork:
		current, _ := dst.values[key].(*ownedJSONAggregateWork)
		dst.values[key] = mergeOwnedJSONAggregateWork(current, source)
	case *codexOwnedAggregateState:
		current, _ := dst.values[key].(*codexOwnedAggregateState)
		if current == nil {
			current = &codexOwnedAggregateState{}
		}
		current.hooks = mergeOwnedJSONAggregateWork(current.hooks, source.hooks)
		current.mcps = append(current.mcps, source.mcps...)
		current.blocked = current.blocked || source.blocked
		dst.values[key] = current
	case nil:
	default:
		dst.values[key] = source
	}
}

func reconcilePluginInstances(ctx context.Context, repoRoot string, resolved []resolver.ResolvedArtifact, compiled compilePhaseResult, report *Report, namespaceBlocked map[string]bool, dryRun bool, _ config.MaterializationModes, fault dependencytxn.FaultHook) {
	canonicalRoot, err := ownership.CanonicalRoot(repoRoot)
	if err != nil {
		report.Diags = append(report.Diags, pluginBatchDiagnostic("", err))
		return
	}
	activeScopes := map[string]bool{"machine": true, "repo:" + ownership.RootKey(canonicalRoot): true}
	activeByOutput := make(map[outputKey]map[string]bool)
	selected := make(map[string]bool)
	blocked := make(map[string]bool)
	for _, resource := range resolved {
		pkg := resource.PluginPackage
		if pkg == nil || len(resource.Harnesses) == 0 {
			continue
		}
		scope := pluginGenerationScope(resource, repoRoot)
		for _, target := range resolver.HarnessTargets(resource.Harnesses) {
			generation := pkg.TargetGenerations[string(target)]
			targetScope := generation.Scope
			if targetScope == "" {
				targetScope = scope
			}
			instanceKey := generation.InstanceKey
			if instanceKey == "" {
				instanceKey = packagegen.InstanceKey(pkg.Name, target, targetScope)
			}
			key := outputKey{target: target, machineScope: resource.Source.IsMachineScope()}
			if activeByOutput[key] == nil {
				activeByOutput[key] = make(map[string]bool)
			}
			activeByOutput[key][instanceKey] = true
			selected[instanceKey] = true
			identity := pluginInstanceIdentity(resource, target)
			if namespaceBlocked[identity] || compiled.pluginBlocked[identity] || generation.GenerationError != "" ||
				len(pkg.DecodeFailures) > 0 || generation.GenerationKey == "" {
				blocked[instanceKey] = true
			}
		}
	}
	var work []pluginInstanceBatchWork
	for instanceKey, plan := range compiled.plugins {
		if blocked[instanceKey] || plan == nil {
			continue
		}
		work = append(work, pluginInstanceBatchWork{plan: plan, ref: plan.ref})
	}
	priorRefs, err := ownership.ReadPluginInstanceStates()
	if err != nil {
		report.Diags = append(report.Diags, pluginBatchDiagnostic("", err))
		return
	}
	for _, ref := range priorRefs {
		if !activeScopes[ref.Scope] || selected[ref.InstanceKey] {
			continue
		}
		if compiled.scopeFilter != nil && (ref.Scope == "machine") != *compiled.scopeFilter {
			continue
		}
		target := hcc.Target(ref.Target)
		key := outputKey{target: target, machineScope: ref.Scope == "machine"}
		work = append(work, pluginInstanceBatchWork{
			plan: &pluginInstancePlan{key: key, ref: ref, removed: true},
			ref:  ref, removed: true,
		})
	}
	sort.Slice(work, func(i, j int) bool {
		if work[i].ref.Name != work[j].ref.Name {
			return work[i].ref.Name < work[j].ref.Name
		}
		if work[i].ref.Target != work[j].ref.Target {
			return work[i].ref.Target < work[j].ref.Target
		}
		if work[i].ref.Scope != work[j].ref.Scope {
			return work[i].ref.Scope < work[j].ref.Scope
		}
		return work[i].ref.InstanceKey < work[j].ref.InstanceKey
	})
	for _, item := range work {
		report = reconcilePluginInstance(ctx, repoRoot, item, activeByOutput[item.plan.key], compiled, report, dryRun, fault)
	}
	sort.Strings(report.RetainedPluginData)
}

func reconcilePluginInstance(ctx context.Context, repoRoot string, item pluginInstanceBatchWork, activeInstances map[string]bool, compiled compilePhaseResult, report *Report, dryRun bool, fault dependencytxn.FaultHook) *Report {
	plan := item.plan
	key := plan.key
	if item.removed {
		key = outputKey{target: hcc.Target(item.ref.Target), machineScope: item.ref.Scope == "machine"}
		plan.key = key
	}
	outDir, destinations, err := pluginOutputDestinations(key, repoRoot)
	if err != nil {
		report.Diags = append(report.Diags, pluginBatchDiagnostic(item.ref.Name, err))
		return report
	}
	portable, err := loadPortableState(repoRoot, key.machineScope)
	if err != nil {
		report.Diags = append(report.Diags, pluginBatchDiagnostic(item.ref.Name, err))
		return report
	}
	batch := newPluginInstanceBatch(item.ref.InstanceKey, activeInstances)
	batch.ownerInstances[item.ref.InstanceKey] = true
	if fault != nil {
		batch.commitOptions = append(batch.commitOptions, dependencytxn.WithFaultHook(fault))
	}
	group := &pluginInstanceBatchGroup{
		key: key, repoRoot: repoRoot, batch: batch, portable: portable, outDir: outDir,
		destinations: destinations, ownerInstances: batch.ownerInstances,
		markerUpdates: make(map[string][]pluginOutputPlan), markerRemovals: make(map[string][]ownership.PluginOutputRef),
	}
	prior, statePath, stateGeneration, stateMode, err := ownership.ReadPluginState(item.ref.InstanceKey)
	if err != nil {
		report.Diags = append(report.Diags, pluginBatchDiagnostic(item.ref.Name, err))
		return report
	}
	var outputPlans []pluginOutputPlan
	var outputRefs []ownership.PluginOutputRef
	if !item.removed {
		outputPlans, outputRefs, err = planPluginArtifacts(key, outDir, repoRoot, item.ref.InstanceKey, plan.artifacts, prior, portable, dryRun)
		if err != nil {
			report.Diags = append(report.Diags, pluginBatchDiagnostic(item.ref.Name, err))
			return report
		}
		for _, output := range outputPlans {
			if output.ref.MarkerField != "" {
				group.markerUpdates[filepath.Clean(output.participant.Path)] = append(
					group.markerUpdates[filepath.Clean(output.participant.Path)], output,
				)
			} else if err := batch.addParticipant(output.participant); err != nil {
				report.Diags = append(report.Diags, pluginBatchDiagnostic(item.ref.Name, err))
				return report
			}
			if output.changed && output.ref.MarkerField == "" {
				batch.addAggregateReport(key, output.rootID, filepath.Dir(output.participant.Path), filepath.Base(output.participant.Path), false)
			}
		}
		for _, output := range outputRefs {
			if output.ManifestPath != "" {
				portable.addPlugin(key.target, output.ManifestPath, output.Digest, item.ref.InstanceKey)
			}
		}
	}
	desiredPaths := make(map[string]bool, len(outputRefs))
	for _, output := range outputRefs {
		desiredPaths[filepath.Clean(output.Path)] = true
	}
	for _, previous := range prior.Outputs {
		if desiredPaths[filepath.Clean(previous.Path)] && !item.removed {
			continue
		}
		if previous.MarkerField != "" {
			group.markerRemovals[filepath.Clean(previous.Path)] = append(group.markerRemovals[filepath.Clean(previous.Path)], previous)
			if previous.ManifestPath != "" {
				portable.remove(key.target, previous.ManifestPath)
			}
			continue
		}
		if err := addPluginOutputRemoval(ctx, batch, key, outDir, previous, portable, dryRun); err != nil {
			report.Diags = append(report.Diags, pluginBatchDiagnostic(item.ref.Name, err))
			return report
		}
		if previous.ManifestPath != "" {
			portable.remove(key.target, previous.ManifestPath)
		}
	}
	if item.removed {
		if err := batch.addParticipant(dependencytxn.DesiredFile{
			Path: statePath, Expected: stateGeneration, ExpectedMode: stateMode, Delete: true,
		}); err != nil {
			report.Diags = append(report.Diags, pluginBatchDiagnostic(item.ref.Name, err))
			return report
		}
	} else {
		stateValue := ownership.PluginState{Version: 1, Instance: item.ref, Outputs: outputRefs, Members: plan.members}
		encoded, err := ownership.EncodePluginState(stateValue)
		if err != nil {
			report.Diags = append(report.Diags, pluginBatchDiagnostic(item.ref.Name, err))
			return report
		}
		if err := batch.addParticipant(dependencytxn.DesiredFile{
			Path: statePath, Expected: stateGeneration, ExpectedMode: stateMode, Bytes: encoded, Mode: 0o600,
		}); err != nil {
			report.Diags = append(report.Diags, pluginBatchDiagnostic(item.ref.Name, err))
			return report
		}
	}

	handler := plan.handler
	if handler == nil {
		if adapter, ok := materializeBindingCatalog()[string(key.target)]; ok {
			handler = adapter.handler
		}
	}
	members := plan.members
	if item.removed {
		members = prior.Members
	}
	hasHook, hasMCP := false, false
	for _, member := range members {
		hasHook = hasHook || member.Kind == ir.KindHook.String()
		hasMCP = hasMCP || member.Kind == ir.KindMcpServer.String()
	}
	nativeMCP := plan.mcpEntries
	if item.removed {
		nativeMCP = nil
	}
	if nativeHookSharesMCPRoute(key, repoRoot) && hasMCP && (len(nativeMCP) > 0 || item.removed) {
		if plan.hooks == nil {
			plan.hooks = &ownedJSONAggregateWork{}
		}
		hasHook = true
	}
	if hasHook {
		group.nativeHooks = plan.hooks
		if group.nativeHooks == nil {
			group.nativeHooks = &ownedJSONAggregateWork{}
		}
		group.nativeMCP = append(group.nativeMCP, nativeMCP...)
		group.nativeHook = true
		group.handler = handler
	}
	if handler != nil && (plan.aggregateSet || hasMCP) {
		group.aggregateSet = true
		group.handler = handler
		mergePluginAggregateState(&group.aggregateState, plan.aggregate, key)
	}
	if err := applyPluginMarkerPlans(report, group, dryRun); err != nil {
		report.Diags = append(report.Diags, pluginBatchDiagnostic(item.ref.Name, err))
		return report
	}
	if group.nativeHook {
		options := nativeHookReconcileOptionsFor(group.handler)
		options.batch = batch
		options.activePluginInstances = activeInstances
		options.pluginInstanceKeys = batch.ownerInstances
		options.selectedHookResources = compiled.selectedHookNames[key]
		var mcpDiagnostics []diag.Diagnostic
		options.nativeHookFinalize = nativeHookActivationFinalizer(key, repoRoot, group.nativeMCP, false, func() {}, &mcpDiagnostics)
		diagnosticStart := len(report.Diags)
		report = reconcileNativeHookJSON(ctx, report, key, repoRoot, group.nativeHooks, options, dryRun)
		if pluginDiagnosticsContainError(report.Diags[diagnosticStart:]) {
			return report
		}
	}
	if group.handler != nil && group.aggregateSet {
		group.aggregateState.nativeHooks = map[outputKey]bool{key: true}
		group.aggregateState.batches = map[outputKey]*pluginInstanceBatch{key: batch}
		diagnosticStart := len(report.Diags)
		report = group.handler.reconcile(ctx, report, group.aggregateState, key, outDir, destinations, dryRun)
		if pluginDiagnosticsContainError(report.Diags[diagnosticStart:]) {
			return report
		}
	}
	if !dryRun {
		manifest, hasManifest, err := portable.manifestParticipant(outDir)
		if err != nil {
			report.Diags = append(report.Diags, pluginBatchDiagnostic(item.ref.Name, err))
			return report
		}
		if hasManifest {
			if err := batch.addParticipant(manifest); err != nil {
				report.Diags = append(report.Diags, pluginBatchDiagnostic(item.ref.Name, err))
				return report
			}
			batch.addManifestCommit(nativeHookManifestCommit{state: portable, participant: manifest})
		}
		if _, err := batch.commit(ctx); err != nil {
			report.Diags = append(report.Diags, pluginBatchDiagnostic(item.ref.Name, err))
			return report
		}
	}
	if item.removed {
		if stateRoot, stateErr := packagegen.StateRoot(); stateErr == nil {
			dataPath := filepath.Join(stateRoot, "plugin-data", item.ref.InstanceKey)
			if info, statErr := os.Lstat(dataPath); statErr == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
				report.RetainedPluginData = append(report.RetainedPluginData, dataPath)
			}
		}
	}
	batch.appendReports(report)
	return report
}

func pluginDiagnosticsContainError(diagnostics []diag.Diagnostic) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.IsError() {
			return true
		}
	}
	return false
}

func applyPluginMarkerPlans(report *Report, group *pluginInstanceBatchGroup, dryRun bool) error {
	paths := make(map[string]bool, len(group.markerUpdates)+len(group.markerRemovals))
	for path := range group.markerUpdates {
		paths[path] = true
	}
	for path := range group.markerRemovals {
		paths[path] = true
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	for _, path := range ordered {
		if !pluginOutputPathAllowed(group.key, group.repoRoot, path) {
			return fmt.Errorf("%w: plugin marker output path escapes its target scope", dependencytxn.ErrUnsafePath)
		}
		updates := group.markerUpdates[path]
		removals := group.markerRemovals[path]
		var reportRoot, reportRel, rootID string
		var original []byte
		var expected dependencytxn.Generation
		var expectedMode os.FileMode
		var linkTarget string
		var mode os.FileMode
		if len(updates) > 0 {
			first := updates[0]
			reportRoot, reportRel, rootID = first.outDir, first.reportPath, first.rootID
			original = append([]byte(nil), first.original...)
			expected = first.participant.Expected
			expectedMode = first.participant.ExpectedMode
			linkTarget = first.participant.ExpectedLinkTarget
			mode = first.participant.Mode
			for _, update := range updates[1:] {
				if update.participant.Expected != expected || update.participant.ExpectedMode != expectedMode ||
					update.participant.ExpectedLinkTarget != linkTarget || update.ref.MarkerField != first.ref.MarkerField {
					return fmt.Errorf("%w: plugin marker aggregate changed during planning: %s", dependencytxn.ErrGenerationConflict, path)
				}
			}
		} else {
			reportRoot, reportRel = filepath.Dir(path), filepath.Base(path)
			var err error
			original, expected, expectedMode, linkTarget, err = readPluginOutput(path)
			if err != nil {
				return err
			}
			mode = expectedMode
			if linkTarget != "" {
				if !isManagedSymlink(path) {
					return fmt.Errorf("%w: unmanaged plugin aggregate symlink: %s", dependencytxn.ErrUnsafePath, path)
				}
				original, _, mode, err = dependencytxn.ReadSnapshot(linkTarget)
				if err != nil {
					return err
				}
			}
		}
		if !expected.Exists && len(updates) == 0 {
			continue
		}
		next := append([]byte(nil), original...)
		sort.Slice(removals, func(i, j int) bool { return removals[i].MarkerValue < removals[j].MarkerValue })
		for _, removal := range removals {
			empty, err := emptyPluginMarkerDocument(removal)
			if err != nil {
				return err
			}
			next, _, err = artifact.MergeWithExistingJSON(next, empty, removal.MarkerField, removal.MarkerValue)
			if err != nil {
				return err
			}
		}
		sort.Slice(updates, func(i, j int) bool { return updates[i].ref.MarkerValue < updates[j].ref.MarkerValue })
		for _, update := range updates {
			var err error
			next, _, err = artifact.MergeWithExistingJSON(next, update.participant.Bytes, update.ref.MarkerField, update.ref.MarkerValue)
			if err != nil {
				return err
			}
		}
		if bytes.Equal(original, next) && linkTarget == "" {
			continue
		}
		participant := dependencytxn.DesiredFile{
			Path: path, Expected: expected, ExpectedMode: expectedMode, ExpectedLinkTarget: linkTarget,
			Bytes: next, Mode: mode.Perm(),
		}
		if err := group.batch.addParticipant(participant); err != nil {
			return err
		}
		rel := filepath.ToSlash(reportRel)
		if dryRun {
			recordOwnedAggregateWrite(report, group.key, rootID, reportRoot, rel)
		} else {
			group.batch.addAggregateReport(group.key, rootID, reportRoot, rel, false)
		}
	}
	return nil
}

func pluginBatchDiagnostic(name string, err error) diag.Diagnostic {
	return diag.Diagnostic{
		Code: diag.CodePluginGenerationPublish, Severity: diag.SeverityError,
		Resource: "plugin/" + name, Message: fmt.Sprintf("plugin instance batch could not be committed: %v", err),
	}
}

func addPluginOutputRemoval(ctx context.Context, batch *pluginInstanceBatch, key outputKey, outDir string, ref ownership.PluginOutputRef, portable *portableState, dryRun bool) error {
	rel, err := filepath.Rel(outDir, ref.Path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("%w: plugin output path escapes its target root", dependencytxn.ErrUnsafePath)
	}
	current, expected, mode, linkTarget, err := readPluginOutput(ref.Path)
	if err != nil {
		return err
	}
	expectedMode := mode
	if !expected.Exists {
		return nil
	}
	if ref.MarkerField != "" {
		if linkTarget != "" {
			if !isManagedSymlink(ref.Path) {
				return fmt.Errorf("%w: unmanaged plugin aggregate symlink", dependencytxn.ErrUnsafePath)
			}
			current, _, mode, err = dependencytxn.ReadSnapshot(linkTarget)
			if err != nil {
				return err
			}
		}
		empty, err := emptyPluginMarkerDocument(ref)
		if err != nil {
			return err
		}
		next, _, err := artifact.MergeWithExistingJSON(current, empty, ref.MarkerField, ref.MarkerValue)
		if err != nil {
			return err
		}
		if bytes.Equal(current, next) {
			return nil
		}
		if !dryRun {
			if err := batch.addParticipant(dependencytxn.DesiredFile{
				Path: ref.Path, Expected: expected, ExpectedMode: expectedMode, ExpectedLinkTarget: linkTarget, Bytes: next, Mode: mode.Perm(),
			}); err != nil {
				return err
			}
			batch.addAggregateReport(key, codexRootIDForPrefix(key, filepath.ToSlash(rel)), outDir, filepath.ToSlash(rel), false)
		}
		return nil
	}
	if linkTarget != "" {
		if ref.LinkTarget == "" || ref.LinkTarget != linkTarget {
			return fmt.Errorf("%w: plugin output link changed: %s", dependencytxn.ErrGenerationConflict, ref.Path)
		}
	} else if portableArtifactDigest(artifact.Artifact{Bytes: current, Mode: mode}) != ref.Digest ||
		uint32(mode.Perm()) != ref.Mode {
		return fmt.Errorf("%w: plugin-owned output changed: %s", dependencytxn.ErrGenerationConflict, ref.Path)
	}
	if err := batch.addParticipant(dependencytxn.DesiredFile{
		Path: ref.Path, Expected: expected, ExpectedMode: mode, ExpectedLinkTarget: linkTarget, Delete: true,
	}); err != nil {
		return err
	}
	if ref.ManifestPath != "" {
		portable.remove(key.target, ref.ManifestPath)
	}
	if !dryRun {
		batch.addAggregateReport(key, codexRootIDForPrefix(key, ref.ManifestPath), outDir, filepath.ToSlash(rel), true)
	}
	return nil
}
