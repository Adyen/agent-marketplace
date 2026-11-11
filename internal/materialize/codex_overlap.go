package materialize

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/harness"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
	"golang.org/x/text/unicode/norm"
)

type codexDestinationBinding struct {
	key         outputKey
	destination harness.Destination
}

type codexManagedPath struct {
	partition string
	id        string
	path      string
	location  string
}

// preflightCodexDestinations resolves active Codex scopes and coalesces desired
// state for physical partitions shared by repository and machine scope. It is
// deliberately called before executeMaterializePlans performs its first mkdir.
func preflightCodexDestinations(result *compilePhaseResult, repoRoot string, modes config.MaterializationModes) (map[string]bool, *diag.Diagnostic) {
	codex := hcc.Target(targetmeta.TargetCodex)
	hasCodexOutput := false
	for key := range result.allOutputs {
		if key.target == codex {
			hasCodexOutput = true
			break
		}
	}
	if !hasCodexOutput {
		return nil, nil
	}

	slog.Debug("Codex destination overlap preflight start")
	resolved := make(map[bool][]harness.Destination, 2)
	for _, machineScope := range []bool{false, true} {
		key := outputKey{target: codex, machineScope: machineScope}
		if !result.allOutputs[key] {
			continue
		}
		destinations, err := harness.ResolveDestinations(codex, machineScope, repoRoot)
		if err != nil {
			d := diag.Diagnostic{
				Code:     "materialize.destination.error",
				Severity: diag.SeverityError,
				Target:   string(codex),
				Message:  fmt.Sprintf("resolve Codex %s destinations before materialization: %v", codexScopeName(machineScope), err),
				Hint:     "fix the repository, HOME, or CODEX_HOME path and retry; no destination was changed",
			}
			slog.Error("Codex destination overlap preflight failed", "scope", codexScopeName(machineScope), "code", d.Code, "error", err)
			return nil, &d
		}
		resolved[machineScope] = destinations
	}

	bindings := make([]codexDestinationBinding, 0, 4)
	for _, machineScope := range []bool{false, true} {
		key := outputKey{target: codex, machineScope: machineScope}
		if !result.allOutputs[key] {
			continue
		}
		for _, destination := range resolved[machineScope] {
			bindings = append(bindings, codexDestinationBinding{key: key, destination: destination})
		}
	}
	if d := validateCodexManagedPathIntersections(bindings); d != nil {
		slog.Error("Codex destination overlap preflight refused managed-path intersection", "code", d.Code, "error", d.Message)
		return nil, d
	}
	if d := mergeCodexArtifactsBeforePreflight(result); d != nil {
		slog.Error("Codex destination overlap preflight could not merge artifacts", "code", d.Code, "error", d.Message)
		return nil, d
	}

	byPartition := make(map[string][]codexDestinationBinding, len(bindings))
	for _, binding := range bindings {
		partition := codexPartitionKey(binding.destination.Root, binding.destination.Prefix)
		byPartition[partition] = append(byPartition[partition], binding)
	}
	coalesced := make(map[string]bool)
	for partition, group := range byPartition {
		if len(group) < 2 {
			continue
		}
		projectMode := modes.For(string(hcc.TargetCodex), false)
		machineMode := modes.For(string(hcc.TargetCodex), true)
		if projectMode != machineMode {
			d := codexDestinationDiagnostic(
				fmt.Sprintf("Codex destination %q is selected by repository and machine scopes with incompatible materialization modes (%s and %s)", group[0].destination.Root, projectMode, machineMode),
				"use the same materialization mode for both scopes or move CODEX_HOME/the repository so their managed destinations do not overlap",
			)
			slog.Error("Codex destination overlap preflight refused incompatible ownership intent", "code", d.Code, "destination", group[0].destination.ID, "project_mode", projectMode, "machine_mode", machineMode)
			return nil, &d
		}
		if d := coalesceCodexPartition(result, group); d != nil {
			slog.Error("Codex destination overlap preflight refused conflicting writes", "code", d.Code, "destination", group[0].destination.ID, "error", d.Message)
			return nil, d
		}
		coalesced[partition] = true
		slog.Debug("Codex destination plans coalesced", "root", group[0].destination.Root, "prefix", group[0].destination.Prefix, "plans", len(group))
	}
	slog.Debug("Codex destination overlap preflight complete", "destinations", len(bindings), "coalesced_partitions", len(coalesced))
	return coalesced, nil
}

func codexScopeName(machineScope bool) string {
	if machineScope {
		return "machine"
	}
	return "repository"
}

func codexPartitionKey(root, prefix string) string {
	return filepath.Clean(root) + "\x00" + filepath.ToSlash(filepath.Clean(prefix))
}

func codexPlanPartitionKey(plan materializePlan) string {
	prefix, _, _ := strings.Cut(filepath.ToSlash(plan.reportPrefix), "/")
	return codexPartitionKey(plan.root, prefix)
}

func codexDestinationDiagnostic(message, hint string) diag.Diagnostic {
	return diag.Diagnostic{
		Code:     diag.CodeMaterializeDestinationOverlap,
		Severity: diag.SeverityError,
		Target:   string(targetmeta.TargetCodex),
		Message:  message,
		Hint:     hint,
	}
}

func validateCodexManagedPathIntersections(bindings []codexDestinationBinding) *diag.Diagnostic {
	managed := make([]codexManagedPath, 0, len(bindings)*3)
	physicalRoots, err := codexPhysicalRoots(bindings)
	if err != nil {
		return &diag.Diagnostic{
			Code:     "materialize.destination.error",
			Severity: diag.SeverityError,
			Target:   string(targetmeta.TargetCodex),
			Message:  fmt.Sprintf("inspect Codex destination locations before materialization: %v", err),
			Hint:     "fix the repository, HOME, or CODEX_HOME path and retry; no destination was changed",
		}
	}
	for _, binding := range bindings {
		partition := codexPartitionKey(binding.destination.Root, binding.destination.Prefix)
		physicalRoot := physicalRoots[filepath.Clean(binding.destination.Root)]
		components := binding.destination.Info.Components
		aggregates := binding.destination.Info.AggregateFilesForScope(binding.key.machineScope)
		rels := make([]string, 0, len(components)+len(aggregates))
		rels = append(append(rels, components...), aggregates...)
		for _, rel := range rels {
			managed = append(managed, codexManagedPath{
				partition: partition,
				id:        binding.destination.ID,
				path:      filepath.Join(binding.destination.Root, filepath.FromSlash(rel)),
				location:  strings.ToLower(norm.NFC.String(filepath.Join(physicalRoot, filepath.FromSlash(rel)))),
			})
		}
	}

	for i := range managed {
		for j := i + 1; j < len(managed); j++ {
			a, b := managed[i], managed[j]
			if a.partition == b.partition {
				continue
			}
			if a.path == b.path {
				d := codexDestinationDiagnostic(
					fmt.Sprintf("Codex destinations %q and %q both manage %q but are not the same native partition", a.id, b.id, a.path),
					"move CODEX_HOME or the repository so the managed paths are disjoint",
				)
				return &d
			}
			if pathIsAncestor(a.path, b.path) || pathIsAncestor(b.path, a.path) {
				d := codexDestinationDiagnostic(
					fmt.Sprintf("Codex managed paths intersect: %q (%s) and %q (%s) have an ancestor/descendant relationship", a.path, a.id, b.path, b.id),
					"move CODEX_HOME or the repository so no managed component contains another Codex managed path",
				)
				return &d
			}
			if a.location == b.location || pathIsAncestor(a.location, b.location) || pathIsAncestor(b.location, a.location) {
				d := codexDestinationDiagnostic(
					fmt.Sprintf("Codex managed paths %q (%s) and %q (%s) are spelled differently but name the same location on case- or normalization-insensitive filesystems", a.path, a.id, b.path, b.id),
					"spell CODEX_HOME with the same letter case as the repository's .codex directory, or move CODEX_HOME or the repository so the managed paths are disjoint",
				)
				return &d
			}
		}
	}
	return nil
}

func codexPhysicalRoots(bindings []codexDestinationBinding) (map[string]string, error) {
	roots := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		roots = append(roots, binding.destination.Root)
	}
	return destinationPhysicalRoots(roots)
}

func destinationPhysicalRoots(roots []string) (map[string]string, error) {
	type seenRoot struct {
		path string
		info os.FileInfo
	}
	physical := make(map[string]string, len(roots))
	var seen []seenRoot
	for _, candidate := range roots {
		root := filepath.Clean(candidate)
		if _, done := physical[root]; done {
			continue
		}
		physical[root] = root
		existing, suffix := root, ""
		info, err := os.Stat(existing)
		for errors.Is(err, fs.ErrNotExist) && filepath.Dir(existing) != existing {
			suffix = filepath.Join(filepath.Base(existing), suffix)
			existing = filepath.Dir(existing)
			info, err = os.Stat(existing)
		}
		if err != nil {
			return nil, err
		}
		representative := existing
		matched := false
		for _, prior := range seen {
			if os.SameFile(prior.info, info) {
				representative = prior.path
				matched = true
				break
			}
		}
		if !matched {
			seen = append(seen, seenRoot{path: existing, info: info})
		}
		physical[root] = filepath.Join(representative, suffix)
	}
	return physical, nil
}

func pathIsAncestor(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil || rel == "." || rel == ".." {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func mergeCodexArtifactsBeforePreflight(result *compilePhaseResult) *diag.Diagnostic {
	for key := range result.allOutputs {
		if key.target != hcc.Target(targetmeta.TargetCodex) {
			continue
		}
		tw := result.byTarget[key]
		if tw == nil || len(tw.artifacts) == 0 {
			continue
		}
		merged, err := artifact.MergeFragments(tw.artifacts)
		if err != nil {
			d := diag.Diagnostic{
				Code:     "materialize.merge.error",
				Severity: diag.SeverityError,
				Target:   string(targetmeta.TargetCodex),
				Message:  fmt.Sprintf("merge Codex artifacts during destination preflight: %v", err),
			}
			return &d
		}
		tw.artifacts = merged
	}
	return nil
}

func coalesceCodexPartition(result *compilePhaseResult, group []codexDestinationBinding) *diag.Diagnostic {
	if d := validateCodexDesiredLeafIntersections(result, group); d != nil {
		return d
	}
	if d := coalesceCodexArtifacts(result, group); d != nil {
		return d
	}
	if d := coalesceCodexSkillLinks(result, group); d != nil {
		return d
	}
	coalesceCodexArtifactMeta(result, group)
	coalesceCodexBlockedComponents(result, group)
	if !group[0].destination.Info.OwnershipReconciledAggregates {
		return nil
	}
	if d := coalesceCodexHooks(result, group); d != nil {
		return d
	}
	coalesceCodexNativeHooks(result, group)
	return coalesceCodexMCP(result, group)
}

func validateCodexDesiredLeafIntersections(result *compilePhaseResult, group []codexDestinationBinding) *diag.Diagnostic {
	leaves := make(map[outputKey]map[string]string, 2)
	for _, binding := range group {
		if leaves[binding.key] == nil {
			leaves[binding.key] = make(map[string]string)
		}
		if tw := result.byTarget[binding.key]; tw != nil {
			for _, candidate := range tw.artifacts {
				rel, ok := codexArtifactRel(binding.destination, candidate.Path)
				if ok {
					leaves[binding.key][filepath.Join(binding.destination.Root, filepath.FromSlash(rel))] = "file"
				}
			}
		}
		for installDirRel, linkGroup := range result.skillLinks[binding.key] {
			for _, link := range linkGroup.links {
				logical := filepath.ToSlash(filepath.Join(installDirRel, link.name))
				rel, ok := codexArtifactRel(binding.destination, logical)
				if ok {
					leaves[binding.key][filepath.Join(binding.destination.Root, filepath.FromSlash(rel))] = "link"
				}
			}
		}
	}
	for key, own := range leaves {
		other := leaves[outputKey{target: key.target, machineScope: !key.machineScope}]
		for path, kind := range own {
			if otherKind, exists := other[path]; exists && otherKind != kind {
				d := codexDestinationDiagnostic(
					fmt.Sprintf("Codex destination write %q is both a file and a link across repository and machine plans", path),
					"rename or remove one resource so each managed path has one compatible output kind",
				)
				return &d
			}
			for parent := filepath.Dir(path); parent != group[0].destination.Root && parent != filepath.Dir(parent); parent = filepath.Dir(parent) {
				if _, exists := other[parent]; !exists {
					continue
				}
				d := codexDestinationDiagnostic(
					fmt.Sprintf("Codex destination writes intersect: %q contains %q", parent, path),
					"rename or remove one resource so managed output paths are disjoint",
				)
				return &d
			}
		}
	}
	return nil
}

func codexArtifactRel(destination harness.Destination, path string) (string, bool) {
	prefix := strings.TrimSuffix(filepath.ToSlash(destination.Prefix), "/") + "/"
	if !strings.HasPrefix(filepath.ToSlash(path), prefix) {
		return "", false
	}
	rel, err := destination.RouteArtifact(path)
	return rel, err == nil
}

func coalesceCodexArtifacts(result *compilePhaseResult, group []codexDestinationBinding) *diag.Diagnostic {
	destination := group[0].destination
	byPath := make(map[string]artifact.Artifact)
	for _, binding := range group {
		tw := result.byTarget[binding.key]
		if tw == nil {
			continue
		}
		for _, candidate := range tw.artifacts {
			rel, ok := codexArtifactRel(binding.destination, candidate.Path)
			if !ok {
				continue
			}
			if previous, exists := byPath[rel]; exists {
				if !codexArtifactsCompatible(previous, candidate) {
					d := codexDestinationDiagnostic(
						fmt.Sprintf("Codex repository and machine plans produce conflicting bytes or ownership intent for %q", filepath.Join(destination.Root, filepath.FromSlash(rel))),
						"make the two resource definitions byte-identical, remove one selection, or separate their destinations",
					)
					return &d
				}
				continue
			}
			byPath[rel] = candidate
		}
	}
	merged := make([]artifact.Artifact, 0, len(byPath))
	for _, candidate := range byPath {
		merged = append(merged, candidate)
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].Path < merged[j].Path })
	for _, binding := range group {
		tw := result.byTarget[binding.key]
		if tw == nil {
			tw = &targetWork{}
			result.byTarget[binding.key] = tw
		}
		kept := tw.artifacts[:0]
		for _, candidate := range tw.artifacts {
			if _, ok := codexArtifactRel(binding.destination, candidate.Path); !ok {
				kept = append(kept, candidate)
			}
		}
		tw.artifacts = append(kept, merged...)
	}
	return nil
}

func codexArtifactsCompatible(a, b artifact.Artifact) bool {
	return a.Path == b.Path &&
		bytes.Equal(a.Bytes, b.Bytes) &&
		a.Mode == b.Mode &&
		a.Fragment == b.Fragment &&
		a.FragmentKey == b.FragmentKey &&
		a.FragmentOrderKey == b.FragmentOrderKey &&
		a.MergeWithExisting == b.MergeWithExisting &&
		a.ExternalReconciliationRequired == b.ExternalReconciliationRequired &&
		a.ManagedByField == b.ManagedByField &&
		a.ManagedByValue == b.ManagedByValue &&
		a.DedupeFragmentEntries == b.DedupeFragmentEntries
}

func coalesceCodexSkillLinks(result *compilePhaseResult, group []codexDestinationBinding) *diag.Diagnostic {
	destination := group[0].destination
	mergedGroups := make(map[string]*skillLinkGroup)
	seen := make(map[string]skillLink)
	for _, binding := range group {
		for installDirRel, sourceGroup := range result.skillLinks[binding.key] {
			logicalProbe := filepath.ToSlash(filepath.Join(installDirRel, "probe"))
			if _, ok := codexArtifactRel(binding.destination, logicalProbe); !ok {
				continue
			}
			mergedGroup := mergedGroups[installDirRel]
			if mergedGroup == nil {
				mergedGroup = &skillLinkGroup{installDirRel: sourceGroup.installDirRel, reportPrefix: sourceGroup.reportPrefix}
				mergedGroups[installDirRel] = mergedGroup
			}
			for _, link := range sourceGroup.links {
				identity := filepath.ToSlash(filepath.Join(installDirRel, link.name))
				if previous, exists := seen[identity]; exists {
					if !codexSkillLinksCompatible(previous, link) {
						rel, _ := destination.RouteArtifact(identity)
						d := codexDestinationDiagnostic(
							fmt.Sprintf("Codex repository and machine plans produce conflicting link ownership intent for %q", filepath.Join(destination.Root, filepath.FromSlash(rel))),
							"make the two resources refer to the same compiled source, remove one selection, or separate their destinations",
						)
						return &d
					}
					continue
				}
				seen[identity] = link
				mergedGroup.links = append(mergedGroup.links, link)
			}
		}
	}
	for _, binding := range group {
		if result.skillLinks[binding.key] == nil {
			result.skillLinks[binding.key] = make(map[string]*skillLinkGroup)
		}
		for installDirRel := range result.skillLinks[binding.key] {
			logicalProbe := filepath.ToSlash(filepath.Join(installDirRel, "probe"))
			if _, ok := codexArtifactRel(binding.destination, logicalProbe); ok {
				delete(result.skillLinks[binding.key], installDirRel)
			}
		}
		for installDirRel, source := range mergedGroups {
			clone := *source
			clone.links = append([]skillLink(nil), source.links...)
			result.skillLinks[binding.key][installDirRel] = &clone
		}
	}
	return nil
}

func codexSkillLinksCompatible(a, b skillLink) bool {
	if a.name != b.name || a.target != b.target || a.cacheDir != b.cacheDir || a.sourceDir != b.sourceDir ||
		a.kind != b.kind || a.resourceName != b.resourceName || a.registryURL != b.registryURL || a.registryPath != b.registryPath ||
		len(a.artifacts) != len(b.artifacts) {
		return false
	}
	for i := range a.artifacts {
		if !codexArtifactsCompatible(a.artifacts[i], b.artifacts[i]) {
			return false
		}
	}
	return true
}

func coalesceCodexArtifactMeta(result *compilePhaseResult, group []codexDestinationBinding) {
	merged := make(map[string]skillLink)
	for _, binding := range group {
		for path, meta := range result.artifactMeta[binding.key] {
			if _, ok := codexArtifactRel(binding.destination, path); ok {
				if _, exists := merged[path]; !exists {
					merged[path] = meta
				}
			}
		}
	}
	for _, binding := range group {
		if result.artifactMeta[binding.key] == nil {
			result.artifactMeta[binding.key] = make(map[string]skillLink)
		}
		for path := range result.artifactMeta[binding.key] {
			if _, ok := codexArtifactRel(binding.destination, path); ok {
				delete(result.artifactMeta[binding.key], path)
			}
		}
		for path, meta := range merged {
			result.artifactMeta[binding.key][path] = meta
		}
	}
}

func coalesceCodexBlockedComponents(result *compilePhaseResult, group []codexDestinationBinding) {
	merged := make(map[string]bool)
	for _, binding := range group {
		for component := range result.blocked.componentsFor(binding.key) {
			merged[component] = true
		}
	}
	for _, binding := range group {
		if len(merged) == 0 {
			continue
		}
		clone := make(map[string]bool, len(merged))
		for component := range merged {
			clone[component] = true
		}
		result.blocked[binding.key] = clone
	}
}

func coalesceCodexHooks(result *compilePhaseResult, group []codexDestinationBinding) *diag.Diagnostic {
	merged := &ownedJSONAggregateWork{}
	seen := make(map[string]ownedJSONEntry)
	hasWork := false
	for _, binding := range group {
		state, _ := result.ownedState[binding.key].(*codexOwnedAggregateState)
		if state == nil || state.hooks == nil {
			continue
		}
		work := state.hooks
		hasWork = true
		for rel := range work.incomplete {
			merged.markIncomplete(rel)
		}
		for _, entry := range work.entries {
			identity := entry.rel + "\x00" + ownedAggregateIDKey(entry.id())
			if previous, exists := seen[identity]; exists {
				if !codexHookEntriesCompatible(previous, entry) {
					d := codexDestinationDiagnostic(
						fmt.Sprintf("Codex repository and machine plans have incompatible ownership intent for hook entry in %q", filepath.Join(group[0].destination.Root, entry.rel)),
						"select one hook owner or make both definitions byte-identical with the same resource identity",
					)
					return &d
				}
				continue
			}
			seen[identity] = entry
			merged.add(entry)
		}
	}
	if !hasWork {
		merged = nil
	}
	for _, binding := range group {
		state, _ := result.ownedState[binding.key].(*codexOwnedAggregateState)
		if state == nil {
			state = &codexOwnedAggregateState{}
			result.ownedState[binding.key] = state
		}
		state.hooks = merged
	}
	return nil
}

func coalesceCodexNativeHooks(result *compilePhaseResult, group []codexDestinationBinding) {
	var merged *ownedJSONAggregateWork
	for _, binding := range group {
		work := result.nativeHooks[binding.key]
		if work == nil {
			continue
		}
		if merged == nil {
			merged = &ownedJSONAggregateWork{}
		}
		for rel, incomplete := range work.incomplete {
			if incomplete {
				merged.markIncomplete(rel)
			}
		}
		for _, entry := range work.entries {
			merged.add(entry)
		}
	}
	if merged == nil {
		return
	}
	for _, binding := range group {
		result.nativeHooks[binding.key] = merged
	}
}

func codexHookEntriesCompatible(a, b ownedJSONEntry) bool {
	return a.rel == b.rel && a.kind == b.kind && a.container == b.container && a.key == b.key &&
		a.identity == b.identity && a.resource == b.resource && bytes.Equal(a.raw, b.raw)
}

func coalesceCodexMCP(result *compilePhaseResult, group []codexDestinationBinding) *diag.Diagnostic {
	merged := make([]codexMCPEntry, 0)
	seen := make(map[string]codexMCPEntry)
	blocked := false
	for _, binding := range group {
		state, _ := result.ownedState[binding.key].(*codexOwnedAggregateState)
		if state == nil {
			state = &codexOwnedAggregateState{}
			result.ownedState[binding.key] = state
		}
		blocked = blocked || state.blocked
		local := make(map[string]bool)
		for _, entry := range state.mcps {
			if local[entry.name] {
				d := diag.Diagnostic{
					Code:     diag.CodeMaterializeCodexDuplicateIdentity,
					Severity: diag.SeverityError,
					Target:   string(targetmeta.TargetCodex),
					Resource: entry.resource,
					Message:  fmt.Sprintf("Codex destination plan contains more than one MCP server named %q; neither definition was written", entry.name),
					Hint:     "remove the duplicate MCP selection before materializing this shared destination",
				}
				return &d
			}
			local[entry.name] = true
			if previous, exists := seen[entry.name]; exists {
				if !codexMCPEntriesCompatible(previous, entry) {
					d := codexDestinationDiagnostic(
						fmt.Sprintf("Codex repository and machine plans produce conflicting MCP server %q in %q", entry.name, filepath.Join(group[0].destination.Root, codexMCPFile)),
						"make the MCP definitions byte-identical with the same resource owner, remove one selection, or separate their destinations",
					)
					return &d
				}
				continue
			}
			seen[entry.name] = entry
			merged = append(merged, entry)
		}
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].name < merged[j].name })
	for _, binding := range group {
		state, _ := result.ownedState[binding.key].(*codexOwnedAggregateState)
		if state == nil {
			state = &codexOwnedAggregateState{}
			result.ownedState[binding.key] = state
		}
		state.mcps = append([]codexMCPEntry(nil), merged...)
		state.blocked = blocked
	}
	return nil
}

func codexMCPEntriesCompatible(a, b codexMCPEntry) bool {
	return a.name == b.name && a.resource == b.resource && a.valueDigest == b.valueDigest && bytes.Equal(a.raw, b.raw)
}
