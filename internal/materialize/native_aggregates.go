package materialize

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/harness"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
)

func materializeNativeAggregates(ctx context.Context, report *Report, key outputKey, repoRoot string, artifacts, preview []artifact.Artifact, blocked map[string]bool, dryRun bool, boundaries ...*destinationBoundary) []artifact.Artifact {
	if len(harness.NativeAggregateInstalls(key.target)) == 0 {
		return artifacts
	}
	destinations, err := harness.ResolveDestinations(key.target, key.machineScope, repoRoot)
	if err != nil {
		report.Diags = append(report.Diags, ownedAggregateCodedDiag(key, "", "", err, "materialize.destination.error"))
		return nil
	}
	remaining := append([]artifact.Artifact(nil), artifacts...)
	for _, destination := range destinations {
		if len(destination.AggregateRoutes) == 0 {
			continue
		}
		desired := make(map[string]json.RawMessage)
		var routeErr error
		for _, route := range destination.AggregateRoutes {
			desired[route.Container] = json.RawMessage(`{}`)
			kept := remaining[:0]
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
		previewDesired := make(map[string]json.RawMessage)
		for _, route := range destination.AggregateRoutes {
			for _, a := range preview {
				if a.Path != route.ArtifactPath {
					continue
				}
				var fragment map[string]json.RawMessage
				if err := json.Unmarshal(a.Bytes, &fragment); err != nil {
					continue
				}
				if value, ok := fragment[route.Container]; ok {
					previewDesired[route.Container] = value
				}
			}
		}
		rel := destination.Info.AggregateFilesForScope(key.machineScope)[0]
		if routeErr == nil {
			var tr TargetReport
			if key.target == hcc.TargetGemini {
				var diagnostics []diag.Diagnostic
				tr, diagnostics, routeErr = syncGeminiNativeAggregate(ctx, key, destination, desired, previewDesired, dryRun, !componentBlocked(blocked, ""), boundaries...)
				report.Diags = append(report.Diags, diagnostics...)
			} else {
				pruneStale := !componentBlocked(blocked, "")
				if slices.ContainsFunc(destination.AggregateRoutes, func(route harness.AggregateRoute) bool { return route.Container == "hooks" }) {
					pruneStale = pruneStale && !componentBlocked(blocked, resourceInstallDirRel(key.target, ir.KindHook))
				}
				tr, routeErr = syncNativeAggregate(key, destination, rel, desired, previewDesired, dryRun, pruneStale, boundaries...)
			}
			if routeErr == nil && len(tr.Written)+len(tr.Skipped)+len(tr.Removed) > 0 {
				report.PerRoot = append(report.PerRoot, tr)
				report.PerTarget[key.target] = mergeTargetReports(report.PerTarget[key.target], tr)
			}
		}
		if routeErr != nil {
			slog.Error("native aggregate preserved after refusal", "target", key.target, "destination", destination.ID, "file", rel)
			report.Diags = append(report.Diags, ownedAggregateCodedDiag(key, destination.Root, rel, routeErr, "materialize.write.error"))
		}
	}
	return remaining
}

func syncNativeAggregate(key outputKey, destination harness.Destination, rel string, desired, preview map[string]json.RawMessage, dryRun, pruneStaleEntries bool, boundaries ...*destinationBoundary) (TargetReport, error) {
	var boundary *destinationBoundary
	if len(boundaries) > 0 {
		boundary = boundaries[0]
	}
	tr := TargetReport{Target: key.target, MachineScope: key.machineScope, RootID: destination.ID, OutDir: destination.Root}
	if !hasNativeDesiredEntries(desired) {
		if !pruneStaleEntries {
			return tr, nil
		}
		hasEntries, err := nativeAggregateHasManagedEntries(key.target, destination, boundary)
		if err != nil {
			return tr, err
		}
		if !hasEntries {
			return tr, nil
		}
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
	existing, err := readNativeCommandFile(file, info, boundary)
	absent := nativeAggregateInventoryAbsent(err)
	if err != nil && !absent {
		return tr, err
	}
	if absent {
		existing = []byte("{}\n")
	}
	merged, err := mergeNativeAggregate(existing, desired, preview, pruneStaleEntries)
	if err != nil {
		return tr, err
	}
	if bytes.Equal(existing, merged) {
		tr.Skipped = []string{rel}
		return tr, nil
	}
	if !dryRun {
		if err := publishPortableBytes(file, merged, mode, info, boundary); err != nil {
			return tr, err
		}
	}
	slog.Debug("native aggregate reconciled", "target", key.target, "destination", destination.ID, "file", rel, "dry_run", dryRun)
	tr.Written = []string{rel}
	return tr, nil
}

func mergeNativeAggregate(existing []byte, desired, preview map[string]json.RawMessage, pruneStaleEntries bool) ([]byte, error) {
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
	if len(preview) > 0 {
		rawDesired, err = augmentDesiredWithPreviewManaged(doc, desired, preview, rawDesired)
		if err != nil {
			return nil, err
		}
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

func augmentDesiredWithPreviewManaged(doc *jsonc.Document, desired, preview map[string]json.RawMessage, rawDesired []byte) ([]byte, error) {
	var desiredDoc map[string]json.RawMessage
	if err := json.Unmarshal(rawDesired, &desiredDoc); err != nil {
		return nil, err
	}
	changed := false
	for _, container := range sortedKeys(preview) {
		existing, ok := doc.Root().Member(container)
		if !ok || existing.Kind() != jsonc.KindObject {
			continue
		}
		var prevMembers map[string]json.RawMessage
		if err := json.Unmarshal(preview[container], &prevMembers); err != nil || prevMembers == nil {
			continue
		}
		wantMembers := make(map[string]json.RawMessage)
		if err := json.Unmarshal(desiredDoc[container], &wantMembers); err != nil || wantMembers == nil {
			wantMembers = make(map[string]json.RawMessage)
		}
		containerChanged := false
		for _, member := range sortedKeys(prevMembers) {
			node, ok := existing.Member(member)
			if !ok {
				continue
			}
			var prevArray []json.RawMessage
			if err := json.Unmarshal(prevMembers[member], &prevArray); err == nil && prevArray != nil {
				if node.Kind() != jsonc.KindArray {
					continue
				}
				var wantArray []json.RawMessage
				_ = json.Unmarshal(wantMembers[member], &wantArray)
				wantCanon := make(map[string]bool, len(wantArray))
				for _, elem := range wantArray {
					wantCanon[canonicalMemberKey(elem)] = true
				}
				prevCanon := make(map[string]bool, len(prevArray))
				for _, elem := range prevArray {
					prevCanon[canonicalMemberKey(elem)] = true
				}
				memberChanged := false
				for i := 0; i < node.Len(); i++ {
					elem, _ := node.Element(i)
					canon := canonicalMemberKey(elem.Canonical())
					if isManagedNode(elem) && prevCanon[canon] && !wantCanon[canon] {
						wantArray = append(wantArray, elem.Canonical())
						wantCanon[canon] = true
						memberChanged = true
					}
				}
				if !memberChanged {
					continue
				}
				raw, err := json.Marshal(wantArray)
				if err != nil {
					return nil, err
				}
				wantMembers[member] = raw
				containerChanged = true
				continue
			}
			if _, wanted := wantMembers[member]; wanted || !isManagedNode(node) {
				continue
			}
			wantMembers[member] = node.Canonical()
			containerChanged = true
		}
		if !containerChanged {
			continue
		}
		changed = true
		raw, err := json.Marshal(wantMembers)
		if err != nil {
			return nil, err
		}
		desiredDoc[container] = raw
		desired[container] = raw
	}
	if !changed {
		return rawDesired, nil
	}
	return json.Marshal(desiredDoc)
}

func canonicalMemberKey(raw json.RawMessage) string {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return string(raw)
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return string(raw)
	}
	return string(canonical)
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

func nativeAggregateInventoryAbsent(err error) bool {
	return errors.Is(err, fs.ErrNotExist) &&
		!errors.Is(err, fsboundary.ErrUnsafePath) &&
		!errors.Is(err, fsboundary.ErrUnsafeFile) &&
		!errors.Is(err, fsboundary.ErrGenerationConflict)
}

func nativeAggregateHasManagedEntries(target hcc.Target, destination harness.Destination, boundary *destinationBoundary) (bool, error) {
	if target == hcc.TargetGemini && geminiNativeHasOwnedMCP(destination) {
		return true, nil
	}
	rel := destination.AggregateFile
	if rel == "" {
		return false, nil
	}
	if err := harness.ValidateRelPath(rel); err != nil {
		return false, err
	}
	if boundary == nil {
		if _, err := validatePortableOutputDir(destination.Root, filepath.Dir(rel)); err != nil {
			return false, err
		}
	}
	file := filepath.Join(destination.Root, rel)
	info, err := os.Lstat(file)
	if err != nil && !nativeAggregateInventoryAbsent(err) {
		return false, err
	}
	if info != nil && !info.Mode().IsRegular() {
		return false, fsboundary.ErrUnsafeFile
	}
	raw, err := readNativeCommandFile(file, info, boundary)
	if err != nil {
		if nativeAggregateInventoryAbsent(err) && info == nil {
			return false, nil
		}
		return false, err
	}
	if !bytes.Contains(raw, []byte(managedByField)) {
		return false, nil
	}
	doc, err := jsonc.Parse("", raw)
	if err != nil {
		return false, nil
	}
	for _, route := range destination.AggregateRoutes {
		if target == hcc.TargetGemini && route.Container == "mcpServers" {
			continue
		}
		entries, err := nativeManagedMembers(doc.Root(), route.Container)
		if err != nil {
			return false, err
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
	legacy := info.PreservedLegacyDirs.Project
	if machineScope {
		legacy = info.PreservedLegacyDirs.User
	}
	for _, rel := range legacy {
		if protected == nil {
			protected = make(map[string]bool)
		}
		rel = trimArtifactRootPrefix(rel, info.ArtifactRootPrefix)
		protected[strings.TrimSuffix(rel, "/")+"/"] = true
	}
	if info.PlanStrategy == harness.PlanWindsurf && len(protected) > 0 {
		normalized := make(map[string]bool, len(protected))
		for rel := range protected {
			normalized[strings.TrimPrefix(rel, ".windsurf/")] = true
		}
		protected = normalized
	}
	return protected
}

func addExistingNativeAggregateKeys(repoRoot string, outputs map[outputKey]bool) {
	for _, target := range hcc.Targets() {
		if len(harness.NativeAggregateInstalls(target)) == 0 {
			continue
		}
		for _, machine := range []bool{false, true} {
			key := outputKey{target: target, machineScope: machine}
			if outputs[key] {
				continue
			}
			destinations, err := harness.ResolveDestinations(target, machine, repoRoot)
			if err != nil {
				outputs[key] = true
				continue
			}
			for _, destination := range destinations {
				if len(destination.AggregateRoutes) == 0 {
					continue
				}
				home, err := os.UserHomeDir()
				if err != nil {
					outputs[key] = true
					break
				}
				if !machine && sameOutputDir(repoRoot, home) && target == hcc.TargetGemini {
					continue
				}
				boundary := destinationBoundary{machineScope: machine, repoRoot: repoRoot, homeDir: home, roots: []string{destination.Root}}
				hasEntries, err := nativeAggregateHasManagedEntries(key.target, destination, &boundary)
				if hasEntries || err != nil {
					outputs[key] = true
					break
				}
			}
		}
	}
}
