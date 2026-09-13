package materialize

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/Adyen/agent-marketplace/internal/harness"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
)

func planNativeAggregates(key outputKey, destinations []harness.Destination, artifacts []artifact.Artifact, blocked map[string]bool) ([]artifact.Artifact, []nativeAggregatePlan, error) {
	if len(harness.NativeAggregateInstalls(key.target)) == 0 {
		return artifacts, nil, nil
	}
	remaining := append([]artifact.Artifact(nil), artifacts...)
	var plans []nativeAggregatePlan
	for _, destination := range destinations {
		if len(destination.AggregateRoutes) == 0 {
			continue
		}
		desired := make(map[string]json.RawMessage)
		var routeErr error
		for _, route := range destination.AggregateRoutes {
			desired[route.Container] = json.RawMessage(`{}`)
			kept := make([]artifact.Artifact, 0, len(remaining))
			for _, a := range remaining {
				if a.Path != route.ArtifactPath {
					kept = append(kept, a)
					continue
				}
				var fragment map[string]json.RawMessage
				if err := json.Unmarshal(a.Bytes, &fragment); err != nil {
					routeErr = err
					continue
				}
				value, ok := fragment[route.Container]
				if !ok {
					routeErr = fmt.Errorf("missing native aggregate container %s", route.Container)
					continue
				}
				desired[route.Container] = value
			}
			remaining = kept
		}
		rels := destination.Info.AggregateFilesForScope(key.machineScope)
		if routeErr != nil {
			return nil, nil, routeErr
		}
		if len(rels) == 0 {
			return nil, nil, fmt.Errorf("native aggregate destination %q has no aggregate file", destination.ID)
		}
		plans = append(plans, nativeAggregatePlan{
			destination:       destination,
			rel:               rels[0],
			desired:           desired,
			pruneStaleEntries: !componentBlocked(blocked, ""),
		})
	}
	return remaining, plans, nil
}

func executeNativeAggregatePlans(ctx context.Context, report *Report, key outputKey, plans []nativeAggregatePlan, dryRun bool) {
	for _, plan := range plans {
		var tr TargetReport
		var routeErr error
		if key.target == hcc.TargetGemini {
			var diagnostics []diag.Diagnostic
			tr, diagnostics, routeErr = syncGeminiNativeAggregate(ctx, key, plan.destination, plan.desired, dryRun, plan.pruneStaleEntries)
			report.Diags = append(report.Diags, diagnostics...)
		} else {
			tr, routeErr = syncNativeAggregate(key, plan.destination, plan.rel, plan.desired, dryRun, plan.pruneStaleEntries)
		}
		if routeErr == nil && len(tr.Written)+len(tr.Skipped)+len(tr.Removed) > 0 {
			report.PerRoot = append(report.PerRoot, tr)
			report.PerTarget[key.target] = mergeTargetReports(report.PerTarget[key.target], tr)
		}
		if routeErr != nil {
			slog.Error("native aggregate preserved after refusal", "target", key.target, "destination", plan.destination.ID, "file", plan.rel)
			report.Diags = append(report.Diags, ownedAggregateCodedDiag(key, plan.destination.Root, plan.rel, routeErr, "materialize.write.error"))
		}
	}
}

func syncNativeAggregate(key outputKey, destination harness.Destination, rel string, desired map[string]json.RawMessage, dryRun, pruneStaleEntries bool) (TargetReport, error) {
	tr := TargetReport{Target: key.target, MachineScope: key.machineScope, RootID: destination.ID, OutDir: destination.Root}
	hasDesired := hasNativeDesiredEntries(desired)
	if !hasDesired && pruneStaleEntries {
		managed, err := nativeAggregateHasManagedEntries(key.target, destination)
		if err != nil {
			return tr, err
		}
		if !managed {
			return tr, nil
		}
	}
	if !hasDesired && !pruneStaleEntries {
		return tr, nil
	}
	if err := harness.ValidateRelPath(rel); err != nil {
		return tr, err
	}
	if _, err := validatePortableOutputDir(destination.Root, filepath.Dir(rel)); err != nil {
		return tr, err
	}
	file := filepath.Join(destination.Root, rel)
	mode := os.FileMode(0o600)
	info, err := os.Lstat(file)
	if err == nil {
		if !info.Mode().IsRegular() {
			return tr, fmt.Errorf("native aggregate must be a regular file")
		}
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return tr, err
	}
	existing, err := os.ReadFile(file)
	absent := os.IsNotExist(err)
	if err != nil && !absent {
		return tr, err
	}
	if absent {
		existing = []byte("{}\n")
	}
	merged, err := mergeNativeAggregate(existing, desired, pruneStaleEntries)
	if err != nil {
		return tr, err
	}
	if bytes.Equal(existing, merged) {
		tr.Skipped = []string{rel}
		return tr, nil
	}
	if !dryRun {
		if _, err := ensurePortableOutputDir(destination.Root, filepath.Dir(rel)); err != nil {
			return tr, err
		}
		current, err := os.ReadFile(file)
		if (absent && !os.IsNotExist(err)) || (!absent && (err != nil || !bytes.Equal(current, existing))) {
			return tr, fmt.Errorf("native aggregate changed during reconciliation")
		}
		currentInfo, statErr := os.Lstat(file)
		if absent {
			if !os.IsNotExist(statErr) {
				return tr, fmt.Errorf("native aggregate changed during reconciliation")
			}
		} else if statErr != nil || !currentInfo.Mode().IsRegular() {
			return tr, fmt.Errorf("native aggregate changed during reconciliation")
		}
		if err := atomicWriteFile(file, merged, mode); err != nil {
			return tr, err
		}
	}
	slog.Debug("native aggregate reconciled", "target", key.target, "destination", destination.ID, "file", rel, "dry_run", dryRun)
	tr.Written = []string{rel}
	return tr, nil
}

func mergeNativeAggregate(existing []byte, desired map[string]json.RawMessage, pruneStaleEntries bool) ([]byte, error) {
	doc, err := jsonc.Parse("", existing)
	if err != nil {
		return nil, err
	}
	if doc.Root().Kind() != jsonc.KindObject {
		return nil, fmt.Errorf("native configuration must be a JSON object")
	}
	scoped := make(map[string]json.RawMessage, len(desired))
	for _, container := range sortedKeys(desired) {
		node, ok := doc.Root().Member(container)
		if !ok {
			continue
		}
		if node.Kind() != jsonc.KindObject {
			return nil, fmt.Errorf("native container %s must be a JSON object", container)
		}
		if container == "hooks" {
			for _, event := range node.Keys() {
				rules, _ := node.Member(event)
				if rules.Kind() != jsonc.KindArray {
					return nil, fmt.Errorf("native hook event must be an array")
				}
				for i := 0; i < rules.Len(); i++ {
					rule, _ := rules.Element(i)
					if rule.Kind() != jsonc.KindObject {
						return nil, fmt.Errorf("native hook rule must be an object")
					}
				}
			}
		}
		if container == "mcpServers" {
			var servers map[string]json.RawMessage
			if err := json.Unmarshal(desired[container], &servers); err != nil {
				return nil, err
			}
			for _, name := range node.Keys() {
				entry, _ := node.Member(name)
				if entry.Kind() != jsonc.KindObject {
					return nil, fmt.Errorf("native MCP entry must be a JSON object")
				}
				if _, wanted := servers[name]; wanted && !isManagedNode(entry) {
					return nil, diag.NewCodedError("materialize.collision.user-owned", diag.SourceRef{}, fmt.Errorf("preserved unmanaged MCP server %q", name))
				}
			}
		}
		scoped[container] = node.Raw()
	}
	var subset bytes.Buffer
	subset.WriteByte('{')
	for i, key := range sortedKeys(scoped) {
		if i > 0 {
			subset.WriteByte(',')
		}
		encoded, _ := json.Marshal(key)
		subset.Write(encoded)
		subset.WriteByte(':')
		subset.Write(scoped[key])
	}
	subset.WriteByte('}')
	rawExisting := subset.Bytes()
	rawDesired, err := json.Marshal(desired)
	if err != nil {
		return nil, err
	}
	if !pruneStaleEntries {
		rawDesired, err = retainNativeManagedEntries(doc, desired, rawDesired)
		if err != nil {
			return nil, err
		}
	}
	merged, _, err := artifact.MergeWithExistingJSON(rawExisting, rawDesired, managedByField, managedByValue)
	if err != nil {
		return nil, err
	}
	mergedDoc, err := jsonc.Parse("", merged)
	if err != nil {
		return nil, err
	}
	for _, container := range sortedKeys(desired) {
		next, ok := mergedDoc.Root().Member(container)
		if !ok {
			continue
		}
		prior, exists := doc.Root().Member(container)
		if exists && bytes.Equal(prior.Raw(), next.Raw()) {
			continue
		}
		if !exists && len(next.Keys()) == 0 {
			continue
		}
		doc.SetMember(doc.Root(), container, next.Raw())
	}
	return doc.Bytes(), nil
}

func hasNativeDesiredEntries(desired map[string]json.RawMessage) bool {
	for _, raw := range desired {
		if !bytes.Equal(bytes.TrimSpace(raw), []byte("{}")) {
			return true
		}
	}
	return false
}

func nativeManagedMembers(root *jsonc.Node, container string) (map[string]json.RawMessage, error) {
	node, ok := root.Member(container)
	if !ok || node.Kind() != jsonc.KindObject {
		return nil, nil
	}
	retained := make(map[string]json.RawMessage)
	for _, key := range node.Keys() {
		entry, _ := node.Member(key)
		if container == "mcpServers" {
			if isManagedNode(entry) {
				retained[key] = entry.Canonical()
			}
			continue
		}
		if container != "hooks" || entry.Kind() != jsonc.KindArray {
			continue
		}
		var rules []json.RawMessage
		for i := 0; i < entry.Len(); i++ {
			rule, _ := entry.Element(i)
			if isManagedNode(rule) {
				rules = append(rules, rule.Canonical())
			}
		}
		if len(rules) > 0 {
			raw, err := json.Marshal(rules)
			if err != nil {
				return nil, err
			}
			retained[key] = raw
		}
	}
	return retained, nil
}

func retainNativeManagedEntries(doc *jsonc.Document, desired map[string]json.RawMessage, rawDesired []byte) ([]byte, error) {
	retained := make(map[string]map[string]json.RawMessage)
	for _, container := range sortedKeys(desired) {
		entries, err := nativeManagedMembers(doc.Root(), container)
		if err != nil {
			return nil, err
		}
		if container == "mcpServers" {
			var wanted map[string]json.RawMessage
			if err := json.Unmarshal(desired[container], &wanted); err != nil {
				return nil, err
			}
			for key := range wanted {
				delete(entries, key)
			}
		}
		if len(entries) > 0 {
			retained[container] = entries
		}
	}
	if len(retained) == 0 {
		return rawDesired, nil
	}
	rawRetained, err := json.Marshal(retained)
	if err != nil {
		return nil, err
	}
	markedDesired, _, err := artifact.MergeWithExistingJSON(nil, rawDesired, managedByField, managedByValue)
	if err != nil {
		return nil, err
	}
	merged, err := artifact.MergeFragments([]artifact.Artifact{
		{Path: "native.json", Bytes: rawRetained, Fragment: true, DedupeFragmentEntries: true},
		{Path: "native.json", Bytes: markedDesired, Fragment: true, DedupeFragmentEntries: true},
	})
	if err != nil {
		return nil, err
	}
	return merged[0].Bytes, nil
}

func nativeAggregateHasManagedEntries(target hcc.Target, destination harness.Destination) (bool, error) {
	if target == hcc.TargetGemini {
		owned, err := geminiNativeHasOwnedMCP(destination)
		if err != nil {
			return false, fmt.Errorf("inspect Gemini native aggregate ownership: %w", err)
		}
		if owned {
			return true, nil
		}
	}
	rel := destination.AggregateFile
	if rel == "" {
		return false, nil
	}
	if err := harness.ValidateRelPath(rel); err != nil {
		return false, fmt.Errorf("validate native aggregate path %q: %w", rel, err)
	}
	file := filepath.Join(destination.Root, rel)
	info, err := os.Lstat(file)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("stat native aggregate %q: %w", file, err)
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("native aggregate %q must be a regular file", file)
	}
	if _, err := validatePortableOutputDir(destination.Root, filepath.Dir(rel)); err != nil {
		return false, fmt.Errorf("validate native aggregate directory %q: %w", destination.Root, err)
	}
	raw, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read native aggregate %q: %w", file, err)
	}
	managedRoute := false
	for _, route := range destination.AggregateRoutes {
		if target == hcc.TargetGemini && route.Container == "mcpServers" {
			continue
		}
		containerPos := bytes.Index(raw, []byte(`"`+route.Container+`"`))
		if containerPos >= 0 && bytes.Contains(raw[containerPos:], []byte(managedByField)) {
			managedRoute = true
			break
		}
	}
	if !managedRoute {
		return false, nil
	}
	doc, err := jsonc.Parse("", raw)
	if err != nil {
		return false, fmt.Errorf("parse native aggregate %q: %w", file, err)
	}
	for _, route := range destination.AggregateRoutes {
		if target == hcc.TargetGemini && route.Container == "mcpServers" {
			continue
		}
		entries, err := nativeManagedMembers(doc.Root(), route.Container)
		if err != nil {
			return false, fmt.Errorf("inspect managed entries in native aggregate %q: %w", file, err)
		}
		if len(entries) > 0 {
			return true, nil
		}
	}
	return false, nil
}

func nativeAggregatePruneFence(target hcc.Target, machineScope bool) map[string]bool {
	protected := aggregateProtectedPaths(target, machineScope)
	info, _ := harness.TargetInfoFor(target)
	for _, install := range harness.NativeAggregateInstalls(target) {
		if protected == nil {
			protected = make(map[string]bool)
		}
		for _, route := range install.Routes {
			protected[trimArtifactRootPrefix(route.ArtifactPath, info.ArtifactRootPrefix)] = true
		}
		if !install.ScopeRoot {
			protected[install.Path(machineScope)] = true
		}
	}
	return protected
}

func addExistingNativeAggregateKeys(repoRoot string, outputs map[outputKey]bool) error {
	for _, target := range hcc.Targets() {
		if len(harness.NativeAggregateInstalls(target)) == 0 {
			continue
		}
		for _, machine := range []bool{false, true} {
			key := outputKey{target: target, machineScope: machine}
			destinations, err := harness.ResolveDestinations(target, machine, repoRoot)
			if err != nil {
				return fmt.Errorf("resolve %s %s native aggregate destinations: %w", scopeName(machine), target, err)
			}
			if outputs[key] {
				continue
			}
			for _, destination := range destinations {
				if len(destination.AggregateRoutes) == 0 {
					continue
				}
				if !machine {
					home, _ := os.UserHomeDir()
					if sameOutputDir(repoRoot, home) && target == hcc.TargetGemini {
						continue
					}
				}
				hasManaged, err := nativeAggregateHasManagedEntries(key.target, destination)
				if err != nil {
					return fmt.Errorf("inspect %s %s native aggregate destination %q: %w", scopeName(machine), target, destination.Root, err)
				}
				if hasManaged {
					outputs[key] = true
				}
			}
		}
	}
	return nil
}
