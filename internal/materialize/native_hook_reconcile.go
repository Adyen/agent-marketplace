package materialize

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"

	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

type nativeHookAggregateDeclaration struct {
	partition targetmeta.NativePartition
	aggregate targetmeta.NativeAggregate
	file      targetmeta.NativeAggregateFile
}

func nativeHookAggregateForScope(target targetmeta.Target, machineScope bool) (nativeHookAggregateDeclaration, error) {
	layout, ok := targetmeta.LookupNativeLayout(target)
	if !ok {
		return nativeHookAggregateDeclaration{}, fmt.Errorf("target %s declares no native layout", target)
	}
	var declaration nativeHookAggregateDeclaration
	found := false
	for _, partition := range layout.Partitions {
		for _, aggregate := range partition.Aggregates {
			if aggregate.Kind != ir.KindHook {
				continue
			}
			if found {
				return nativeHookAggregateDeclaration{}, fmt.Errorf("target %s declares more than one hook aggregate", target)
			}
			file := aggregate.Project
			if machineScope {
				file = aggregate.User
			}
			declaration = nativeHookAggregateDeclaration{partition: partition, aggregate: aggregate, file: file}
			found = true
		}
	}
	if !found {
		return nativeHookAggregateDeclaration{}, fmt.Errorf("target %s declares no hook aggregate", target)
	}
	if declaration.file.Path == "" {
		return nativeHookAggregateDeclaration{}, fmt.Errorf("target %s declares an empty hook aggregate path", target)
	}
	return declaration, nil
}

// hookUsesResourceFiles reports whether target's native layout installs each
// hook as its own resource file instead of declaring a shared hook aggregate.
// Such hooks are ordinary per-resource artifacts: the aggregate ownership,
// support-directory, and review pipeline does not apply to them, and their
// compilers receive no installed support directory.
func hookUsesResourceFiles(target hcc.Target) bool {
	layout, ok := targetmeta.LookupNativeLayout(targetmeta.Target(target))
	if !ok {
		return false
	}
	resourcePath := false
	for _, partition := range layout.Partitions {
		for _, aggregate := range partition.Aggregates {
			if aggregate.Kind == ir.KindHook {
				return false
			}
		}
		if _, ok := partition.ResourcePaths[ir.KindHook]; ok {
			resourcePath = true
		}
	}
	return resourcePath
}

// resourceFileHookSupportDir returns the per-hook support root, relative to
// the target root, of a target whose hooks are resource files, or "" when the
// target declares none.
func resourceFileHookSupportDir(target hcc.Target) string {
	if !hookUsesResourceFiles(target) {
		return ""
	}
	layout, _ := targetmeta.LookupNativeLayout(targetmeta.Target(target))
	for _, partition := range layout.Partitions {
		if partition.HookSupportDir != "" {
			return path.Join(partition.Prefix, partition.HookSupportDir)
		}
	}
	return ""
}

// resourceFileHookSupportPlan reports whether a materialization plan
// publishes repository-scope support of resource-file hooks, which is
// installed as portable regular copies in every materialization mode.
func resourceFileHookSupportPlan(key outputKey, reportPrefix string) bool {
	dir := resourceFileHookSupportDir(key.target)
	return !key.machineScope && dir != "" && reportPrefix == componentName(dir)
}

func addExistingNativeHookOutputKeys(repoRoot string, outputs map[outputKey]bool) {
	for _, target := range hcc.Targets() {
		for _, machineScope := range []bool{false, true} {
			key := outputKey{target: target, machineScope: machineScope}
			if outputs[key] || !nativeHookOutputPresent(key, repoRoot) {
				continue
			}
			if !machineScope {
				machineKey := outputKey{target: target, machineScope: true}
				repoDestination, repoFile, repoErr := nativeHookJSONLayout(key, repoRoot)
				machineDestination, machineFile, machineErr := nativeHookJSONLayout(machineKey, repoRoot)
				if repoErr == nil && machineErr == nil &&
					sameOutputDir(repoDestination.Root, machineDestination.Root) && repoFile.rel == machineFile.rel {
					continue
				}
			}
			outputs[key] = true
		}
	}
}

func nativeHookOutputPresent(key outputKey, repoRoot string) bool {
	destination, file, err := nativeHookJSONLayout(key, repoRoot)
	if err != nil {
		return false
	}
	store, err := ownership.Open(destination.Root)
	if err != nil {
		return false
	}
	proof, err := store.Proof(file.rel)
	if err != nil {
		return false
	}
	if len(proof.HookSatisfactions()) > 0 {
		return true
	}
	for _, id := range proof.OwnedIDs() {
		if id.Kind == ownership.KindHook || (id.Kind == ownership.KindMcpServer && id.Container == "mcpServers" && nativeHookSharesMCPRoute(key, repoRoot)) {
			return true
		}
	}
	return false
}

func nativeHookEvents(data []byte, file ownedJSONFileSpec) (map[string][]json.RawMessage, error) {
	doc, err := jsonc.Parse(file.rel, data)
	if err != nil {
		return nil, fmt.Errorf("parse compiled native hook fragment")
	}
	if err := validateOwnedJSONEnvelope(doc, file, false); err != nil {
		return nil, fmt.Errorf("compiled native hook envelope differs from its declaration")
	}
	container, _, err := ownedJSONContainerNode(doc, file)
	if err != nil || container == nil {
		return nil, fmt.Errorf("compiled native hook container is invalid")
	}
	events := make(map[string][]json.RawMessage, len(container.Keys()))
	for _, event := range container.Keys() {
		array, ok := container.Member(event)
		if !ok || array.Kind() != jsonc.KindArray {
			return nil, fmt.Errorf("compiled native hook event is not an array")
		}
		for i := 0; i < array.Len(); i++ {
			rule, _ := array.Element(i)
			if err := validateOwnedJSONHookRule(file.hookShape, rule); err != nil {
				return nil, fmt.Errorf("compiled native hook entry has an invalid declared shape")
			}
			events[event] = append(events[event], json.RawMessage(rule.Raw()))
		}
	}
	return events, nil
}

func nativeHookEntriesForArtifacts(key outputKey, repoRoot, resource, sourceDigest string, artifacts []artifact.Artifact) ([]ownedJSONEntry, error) {
	if sourceDigest == "" {
		return nil, fmt.Errorf("captured hook source proof is unavailable")
	}
	_, file, err := nativeHookJSONLayout(key, repoRoot)
	if err != nil {
		return nil, err
	}
	expected, ok := targetmeta.NativeAggregateFileForScope(targetmeta.Target(key.target), ir.KindHook, key.machineScope)
	if !ok || expected.Path == "" {
		return nil, fmt.Errorf("target %s has no native hook aggregate for this scope", key.target)
	}
	var entries []ownedJSONEntry
	for _, compiled := range artifacts {
		policy, policyErr := compiled.Policy()
		if policyErr != nil || compiled.Path != expected.Path || !compiled.Fragment || !compiled.ExternalReconciliationRequired || !policy.IsExternal() ||
			compiled.ManagedByField != "" || compiled.ManagedByValue != "" || compiled.MergeWithExisting || compiled.Mode.Perm() != 0o600 {
			return nil, fmt.Errorf("compiled native hook fragment does not match its descriptor policy")
		}
		events, err := nativeHookEvents(compiled.Bytes, file)
		if err != nil {
			return nil, err
		}
		contributions, err := ownedJSONHookEntries(file.rel, events, resource)
		if err != nil {
			return nil, err
		}
		for i := range contributions {
			contributions[i].sourceDigest = sourceDigest
		}
		entries = append(entries, contributions...)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("compiled native hook output contains no event entries")
	}
	return entries, nil
}

func nativeHookJSONLayout(key outputKey, repoRoot string) (harness.Destination, ownedJSONFileSpec, error) {
	declaration, err := nativeHookAggregateForScope(targetmeta.Target(key.target), key.machineScope)
	if err != nil {
		return harness.Destination{}, ownedJSONFileSpec{}, err
	}
	artifactPath := path.Join(declaration.partition.Prefix, declaration.file.Path)
	destinations, err := harness.ResolveDestinations(key.target, key.machineScope, repoRoot)
	if err != nil {
		return harness.Destination{}, ownedJSONFileSpec{}, err
	}
	var destination harness.Destination
	var rel string
	matches := 0
	for _, candidate := range destinations {
		if len(candidate.AggregateRoutes) != 0 {
			continue
		}
		routed, routeErr := candidate.RouteArtifact(artifactPath)
		if routeErr != nil || routed != declaration.file.Path || candidate.Root == "" {
			continue
		}
		destination = candidate
		rel = routed
		matches++
	}
	if matches != 1 {
		return harness.Destination{}, ownedJSONFileSpec{}, fmt.Errorf("target %s has no unique native hook destination", key.target)
	}
	return destination, ownedJSONFileSpec{
		rel:                   rel,
		container:             declaration.file.Member,
		envelope:              declaration.file.Envelope,
		hookShape:             declaration.aggregate.Shape,
		legacyMarkedFlatHooks: declaration.aggregate.LegacyMarkedFlatHooks || declaration.file.LegacyMarkedFlatHooks,
	}, nil
}

func nativeHookSelectionAuthority(result compilePhaseResult, keys []outputKey) (map[string]bool, bool) {
	union := make(map[string]bool)
	for _, key := range keys {
		if result.unknownHookSelection[key] || result.nativeMCPIncomplete[key] {
			return nil, false
		}
		for name := range result.selectedHookNames[key] {
			union[name] = true
		}
	}
	return union, true
}

func defaultNativeHookReconcileOptions() ownedJSONReconcileOptions {
	return ownedJSONReconcileOptions{
		sessionName:        "reconcile-native-hooks",
		writeCode:          "materialize.write.error",
		malformedCode:      diag.CodeHookNativeToleranceRefused,
		shapeCode:          diag.CodeHookNativeToleranceRefused,
		satisfiedCode:      diag.CodeMaterializeHookSatisfiedByUnmanaged,
		prunePostponedCode: "materialize.write.error",
	}
}

func nativeHookReconcileOptionsFor(handler ownedAggregateHandler) ownedJSONReconcileOptions {
	if adapter, ok := handler.(nativeHookReconcileOptionsProvider); ok {
		return adapter.nativeHookReconcileOptions()
	}
	return defaultNativeHookReconcileOptions()
}

func reconcileNativeHookJSON(ctx context.Context, report *Report, key outputKey, repoRoot string, work *ownedJSONAggregateWork, options ownedJSONReconcileOptions, dryRun bool) *Report {
	if report == nil {
		report = &Report{}
	}
	if report.PerTarget == nil {
		report.PerTarget = make(map[hcc.Target]TargetReport)
	}
	destination, file, err := nativeHookJSONLayout(key, repoRoot)
	if err != nil {
		return nativeHookDestinationError(report, key)
	}
	root, err := ownership.CanonicalRoot(destination.Root)
	if err != nil {
		return nativeHookDestinationError(report, key)
	}
	if info, statErr := os.Stat(root); statErr == nil && !info.IsDir() || statErr != nil && !os.IsNotExist(statErr) {
		return nativeHookDestinationError(report, key)
	}
	destination.Root = root
	options.rootID = destination.ID
	declaration, err := nativeHookAggregateForScope(targetmeta.Target(key.target), key.machineScope)
	if err != nil {
		return nativeHookDestinationError(report, key)
	}
	if declaration.file.PreserveLegacyHandwritten {
		for _, legacyRel := range declaration.file.Legacy {
			options.legacyHandwrittenFiles = append(options.legacyHandwrittenFiles, path.Join(declaration.partition.Prefix, legacyRel))
		}
	}
	file.rawValueDigest = options.rawValueDigest
	return reconcileOwnedJSONAggregates(ctx, report, key, destination.Root, []ownedJSONFileSpec{file}, work, options, dryRun)
}

func nativeHookSharesAggregateRoute(key outputKey, repoRoot string) ([]harness.AggregateRoute, bool) {
	hookDestination, hookFile, err := nativeHookJSONLayout(key, repoRoot)
	if err != nil {
		return nil, false
	}
	declaration, err := nativeHookAggregateForScope(targetmeta.Target(key.target), key.machineScope)
	if err != nil {
		return nil, false
	}
	declaredFiles := map[string]bool{hookFile.rel: true}
	for _, legacyRel := range declaration.file.Legacy {
		declaredFiles[path.Join(declaration.partition.Prefix, legacyRel)] = true
	}
	destinations, err := harness.ResolveDestinations(key.target, key.machineScope, repoRoot)
	if err != nil {
		return nil, false
	}
	for _, destination := range destinations {
		if len(destination.AggregateRoutes) > 0 && sameOutputDir(destination.Root, hookDestination.Root) && declaredFiles[destination.AggregateFile] {
			return destination.AggregateRoutes, true
		}
	}
	return nil, false
}

func nativeHookCanSkipGenericAggregateRoute(key outputKey, repoRoot string) bool {
	_, shared := nativeHookSharesAggregateRoute(key, repoRoot)
	return shared
}

func nativeHookSharesMCPRoute(key outputKey, repoRoot string) bool {
	routes, shared := nativeHookSharesAggregateRoute(key, repoRoot)
	if !shared {
		return false
	}
	for _, route := range routes {
		if route.Container == "mcpServers" {
			return true
		}
	}
	return false
}

func nativeHookDestinationError(report *Report, key outputKey) *Report {
	report.Diags = append(report.Diags, diag.Diagnostic{
		Code:     "materialize.destination.error",
		Severity: diag.SeverityError,
		Target:   string(key.target),
		Message:  "native hook destination could not be resolved safely; aggregate was left unchanged",
	})
	return report
}
