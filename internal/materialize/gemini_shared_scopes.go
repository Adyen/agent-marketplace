package materialize

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/harness"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
)

type geminiAggregateCoalescence struct {
	coalesced bool
	repo      outputKey
	machine   outputKey
}

func (c geminiAggregateCoalescence) includes(key outputKey) bool {
	return c.coalesced && (key == c.repo || key == c.machine)
}

func (c geminiAggregateCoalescence) isRepo(key outputKey) bool {
	return c.coalesced && key == c.repo
}

func (c geminiAggregateCoalescence) isOwner(key outputKey) bool {
	return c.coalesced && key == c.machine
}

func (c geminiAggregateCoalescence) supportKeys(key outputKey) []outputKey {
	if !c.includes(key) || !c.isOwner(key) {
		return []outputKey{key}
	}
	return []outputKey{key, c.repo}
}

func (c geminiAggregateCoalescence) supportPathsForKey(key outputKey, paths map[outputKey]map[string]bool) map[string]bool {
	if !c.includes(key) {
		return paths[key]
	}
	combined := make(map[string]bool, len(paths[c.repo])+len(paths[c.machine]))
	for _, supportKey := range []outputKey{c.repo, c.machine} {
		for path := range paths[supportKey] {
			combined[path] = true
		}
	}
	if len(combined) == 0 {
		return nil
	}
	return combined
}

func (c geminiAggregateCoalescence) supportTargetsForPlan(key outputKey, plan materializePlan, publication nativeHookSupportPublication, hookSupportPaths map[string]bool) map[string]string {
	targets := make(map[string]string)
	add := func(supportPlan *nativeHookSupportPlanPublication) {
		if supportPlan == nil {
			return
		}
		for path, target := range supportPlan.supportTargets {
			targets[path] = target
		}
	}
	if !c.includes(key) {
		add(publication.plans[nativeHookSupportPlanKeyFor(plan)])
	} else {
		for supportKey, supportPlan := range publication.plans {
			for path, target := range supportPlan.supportTargets {
				rebased, ok := rebaseGeminiSupportTargetPath(supportKey.outDir, plan.outDir, path)
				if ok {
					targets[rebased] = target
				}
			}
		}
	}
	prefix := filepath.ToSlash(plan.reportPrefix)
	for supportPath := range hookSupportPaths {
		supportPath = filepath.ToSlash(supportPath)
		if prefix != "" && prefix != "." {
			if !strings.HasPrefix(supportPath, prefix+"/") {
				continue
			}
			supportPath = strings.TrimPrefix(supportPath, prefix+"/")
		}
		if supportPath != "" {
			if _, exists := targets[supportPath]; !exists {
				targets[supportPath] = ""
			}
		}
	}
	if len(targets) == 0 {
		return nil
	}
	return targets
}

func rebaseGeminiSupportTargetPath(sourceRoot, targetRoot, supportRel string) (string, bool) {
	if sameOutputDir(sourceRoot, targetRoot) {
		return filepath.ToSlash(supportRel), true
	}
	under := func(path string) bool {
		return path != ".." && !filepath.IsAbs(path) && !strings.HasPrefix(path, ".."+string(filepath.Separator))
	}
	if rootRel, err := filepath.Rel(targetRoot, sourceRoot); err == nil && under(rootRel) {
		return filepath.ToSlash(filepath.Join(rootRel, filepath.FromSlash(supportRel))), true
	}
	relative, err := filepath.Rel(sourceRoot, targetRoot)
	if err != nil || !under(relative) {
		return "", false
	}
	prefix := filepath.ToSlash(relative)
	path := filepath.ToSlash(supportRel)
	if prefix == "." {
		return path, true
	}
	if path == prefix {
		return ".", true
	}
	if strings.HasPrefix(path, prefix+"/") {
		return strings.TrimPrefix(path, prefix+"/"), true
	}
	return "", false
}

func coalesceGeminiNativeAggregates(result *compilePhaseResult, repoRoot string, report *Report) (geminiAggregateCoalescence, error) {
	coalescence := geminiAggregateCoalescence{
		repo:    outputKey{target: hcc.TargetGemini},
		machine: outputKey{target: hcc.TargetGemini, machineScope: true},
	}
	repo, machine := coalescence.repo, coalescence.machine
	if !result.allOutputs[repo] && !result.allOutputs[machine] {
		return coalescence, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return coalescence, err
	}
	if !sameOutputDir(repoRoot, home) {
		return coalescence, nil
	}
	if result.scopeFilter != nil {
		return coalescence, fmt.Errorf("a scope-restricted operation cannot isolate a shared Gemini root")
	}
	if roots, ok := harness.MakeInstallRoots()[hcc.TargetGemini]; ok {
		for _, key := range []outputKey{machine, repo} {
			work := result.byTarget[key]
			if work == nil {
				continue
			}
			outDir := roots.For(key.machineScope, repoRoot)
			remaining, diagnostics, blocked := preflightHookSupportDestinations(result, key, repoRoot, outDir, work.artifacts, result.skillLinks[key], nil)
			work.artifacts = remaining
			report.Diags = append(report.Diags, diagnostics...)
			for _, resource := range blocked {
				if !slices.Contains(report.BlockedResources, resource) {
					report.BlockedResources = append(report.BlockedResources, resource)
				}
			}
		}
	}
	routes := make(map[string]string)
	for _, install := range harness.NativeAggregateInstalls(hcc.TargetGemini) {
		for _, route := range install.Routes {
			routes[route.ArtifactPath] = route.Container
		}
	}
	seen := make(map[string][]byte)
	var shared, kept []artifact.Artifact
	for _, key := range []outputKey{machine, repo} {
		work := result.byTarget[key]
		if work == nil {
			continue
		}
		for _, a := range work.artifacts {
			container, native := routes[a.Path]
			if !native {
				if key == repo {
					kept = append(kept, a)
				}
				continue
			}
			if container == "mcpServers" {
				var fragment map[string]json.RawMessage
				if err := json.Unmarshal(a.Bytes, &fragment); err != nil {
					return coalescence, fmt.Errorf("decode Gemini native fragment %s: %w", a.Path, err)
				}
				var servers map[string]json.RawMessage
				if rawServers, exists := fragment[container]; exists && len(rawServers) > 0 {
					if err := json.Unmarshal(rawServers, &servers); err != nil {
						return coalescence, fmt.Errorf("decode Gemini %s in %s: %w", container, a.Path, err)
					}
				}
				for name, raw := range servers {
					canonical, err := canonicalOwnedJSON(raw)
					if err != nil {
						return coalescence, err
					}
					if prior, ok := seen[name]; ok && !bytes.Equal(prior, canonical) {
						return coalescence, fmt.Errorf("shared Gemini MCP identity %q has conflicting desired values", name)
					}
					seen[name] = canonical
				}
			}
			if key == repo {
				shared = append(shared, a)
			}
		}
	}
	nativeMCPByID := make(map[string]ownedJSONEntry)
	for _, key := range []outputKey{machine, repo} {
		for _, entry := range result.nativeMCPEntries[key] {
			if prior, ok := seen[entry.key]; ok && !bytes.Equal(prior, entry.raw) {
				return coalescence, fmt.Errorf("shared Gemini MCP identity %q has conflicting desired values", entry.key)
			}
			identity := entry.rel + "\x00" + ownedAggregateIDKey(entry.id())
			if prior, ok := nativeMCPByID[identity]; ok {
				if !bytes.Equal(prior.raw, entry.raw) {
					return coalescence, fmt.Errorf("shared Gemini MCP identity %q has conflicting desired values", entry.key)
				}
				continue
			}
			nativeMCPByID[identity] = entry
		}
	}
	if len(nativeMCPByID) > 0 {
		entries := make([]ownedJSONEntry, 0, len(nativeMCPByID))
		for _, identity := range sortedKeys(nativeMCPByID) {
			entries = append(entries, nativeMCPByID[identity])
		}
		result.nativeMCPEntries[machine] = entries
	} else {
		delete(result.nativeMCPEntries, machine)
	}
	delete(result.nativeMCPEntries, repo)
	mergedHooks := mergeOwnedJSONAggregateWork(result.nativeHooks[machine], result.nativeHooks[repo])
	if mergedHooks == nil {
		delete(result.nativeHooks, machine)
	} else {
		result.nativeHooks[machine] = mergedHooks
	}
	delete(result.nativeHooks, repo)
	if result.nativeMCPConflicts[repo] != nil {
		if result.nativeMCPConflicts[machine] == nil {
			result.nativeMCPConflicts[machine] = make(map[string]bool)
		}
		for key := range result.nativeMCPConflicts[repo] {
			result.nativeMCPConflicts[machine][key] = true
		}
	}
	delete(result.nativeMCPConflicts, repo)
	if result.nativeMCPIncomplete[repo] {
		result.nativeMCPIncomplete[machine] = true
	}
	delete(result.nativeMCPIncomplete, repo)
	mergeResources := func(destination, source []BlockedResource) []BlockedResource {
		for _, resource := range source {
			if !slices.Contains(destination, resource) {
				destination = append(destination, resource)
			}
		}
		return destination
	}
	result.hookResources[machine] = mergeResources(result.hookResources[machine], result.hookResources[repo])
	delete(result.hookResources, repo)
	result.mcpResources[machine] = mergeResources(result.mcpResources[machine], result.mcpResources[repo])
	delete(result.mcpResources, repo)
	if work := result.byTarget[repo]; work != nil {
		work.artifacts = kept
	}
	// An extra native pass must not prune the repository's ordinary output,
	// including when existing-root discovery revives the machine key later.
	if result.byTarget[machine] == nil && result.allOutputs[repo] && !anyBlocked(result.blocked[machine]) {
		result.byTarget[machine] = &targetWork{nativeOnly: true}
	}
	if len(shared) > 0 {
		if result.byTarget[machine] == nil {
			result.byTarget[machine] = &targetWork{}
		}
		result.byTarget[machine].artifacts = append(result.byTarget[machine].artifacts, shared...)
	}
	result.allOutputs[machine] = true
	if anyBlocked(result.blocked[repo]) || anyBlocked(result.blocked[machine]) {
		result.blocked.blockAll(machine)
	}
	coalescence.coalesced = true
	return coalescence, nil
}
