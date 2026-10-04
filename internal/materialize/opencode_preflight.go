package materialize

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
)

// A custom native override is global to OpenCode. Two independent scope plans
// cannot each prune it, so refuse that layout before publishing either plan.
func preflightOpenCodeDestinations(result *compilePhaseResult, repo string) *diag.Diagnostic {
	var destinations [2][]harness.Destination
	for index, machine := range []bool{false, true} {
		if !result.allOutputs[outputKey{target: hcc.TargetOpenCode, machineScope: machine}] {
			continue
		}
		resolved, err := harness.ResolveDestinations(hcc.TargetOpenCode, machine, repo)
		if err != nil {
			return &diag.Diagnostic{Code: "materialize.destination.error", Severity: diag.SeverityError, Target: "opencode", Message: err.Error()}
		}
		destinations[index] = resolved
	}
	if len(destinations[0]) == 0 || len(destinations[1]) == 0 {
		return nil
	}
	if destinations[0][0].Root == destinations[1][0].Root || (result.openCodeAggregateScope == nil && filepath.Join(destinations[0][1].Root, destinations[0][1].AggregateFile) == filepath.Join(destinations[1][1].Root, destinations[1][1].AggregateFile)) {
		return &diag.Diagnostic{Code: diag.CodeMaterializeDestinationOverlap, Severity: diag.SeverityError, Target: "opencode", Message: "OpenCode repository and machine selections share a native override; use separate destinations or select one scope"}
	}
	return nil
}

func preflightOpenCodeResources(result *compilePhaseResult, repo string) []diag.Diagnostic {
	var diagnostics []diag.Diagnostic
	for _, machine := range []bool{false, true} {
		key := outputKey{target: hcc.TargetOpenCode, machineScope: machine}
		if componentBlocked(result.blocked[key], "hooks") {
			result.blocked.mark(key, "plugins")
		}
		work := result.byTarget[key]
		if work == nil {
			continue
		}
		destinations, err := harness.ResolveDestinations(key.target, machine, repo)
		if err != nil {
			continue
		} // Destination preflight reports this error.
		root := destinations[0].Root
		if componentBlocked(result.blocked[key], "hooks") {
			result.blocked.mark(key, "plugins")
		}
		blockedHooks := false
		for _, candidate := range work.artifacts {
			if candidate.Path != "plugins/agent-marketplace.js" {
				continue
			}
			file, err := portableOutputPath(root, candidate.Path)
			if err == nil {
				_, err = os.Lstat(file)
				if os.IsNotExist(err) {
					continue
				}
			}
			state, stateErr := loadPortableState(repo, machine)
			owned := false
			if err == nil && stateErr == nil {
				entry, exists := state.entry(key.target, candidate.Path)
				digest, digestErr := portablePathDigest(file)
				owned = exists && digestErr == nil && digest == entry.Digest
			}
			if !owned {
				blockedHooks = true
				result.blocked.mark(key, "hooks", "plugins")
				diagnostics = append(diagnostics, diag.Diagnostic{Code: "materialize.collision.user-owned", Severity: diag.SeverityError, Target: "opencode", Message: "OpenCode hook adapter is unmanaged or modified; its declarations and existing plugin are preserved"})
			}
		}
		// Config-defined agents load over Markdown files. Preserve their identity
		// instead of installing a resource which cannot become effective.
		configAgents := map[string]bool{}
		for _, configuration := range openCodeConfigInventory(repo, root, destinations[1]) {
			if data, err := ownership.ReadAggregateFile(configuration); err == nil && data != nil {
				if document, err := jsonc.ParseWithOptions(configuration, data, jsonc.ParseOptions{AllowTrailingCommas: true}); err == nil {
					if agents, ok := document.Root().Member("agent"); ok && agents.Kind() == jsonc.KindObject {
						for _, name := range agents.Keys() {
							configAgents[name] = true
						}
					}
				}
			}
		}
		kept := make([]artifact.Artifact, 0, len(work.artifacts))
		for _, candidate := range work.artifacts {
			if blockedHooks && (strings.HasPrefix(candidate.Path, "hooks/") || strings.HasPrefix(candidate.Path, "plugins/")) {
				continue
			}
			if strings.HasPrefix(candidate.Path, "agents/") {
				name := strings.TrimSuffix(strings.TrimPrefix(candidate.Path, "agents/"), ".md")
				if configAgents[name] || openCodeBuiltInAgent(name) {
					result.blocked.mark(key, "agents")
					diagnostics = append(diagnostics, diag.Diagnostic{Code: "materialize.collision.user-owned", Severity: diag.SeverityError, Target: "opencode", Resource: name, Message: "OpenCode agent conflicts with an existing config-defined or built-in agent; resource omitted"})
					continue
				}
			}
			kept = append(kept, candidate)
		}
		work.artifacts = kept
		for dir, group := range result.skillLinks[key] {
			links := group.links[:0]
			for _, link := range group.links {
				if blockedHooks && link.kind == ir.KindHook {
					continue
				}
				name := strings.TrimSuffix(link.name, ".md")
				if link.kind == ir.KindSubagent && (configAgents[name] || openCodeBuiltInAgent(name)) {
					result.blocked.mark(key, "agents")
					diagnostics = append(diagnostics, diag.Diagnostic{Code: "materialize.collision.user-owned", Severity: diag.SeverityError, Target: "opencode", Resource: name, Message: "OpenCode agent conflicts with an existing config-defined or built-in agent; resource omitted"})
					continue
				}
				links = append(links, link)
			}
			group.links = links
			if len(links) == 0 {
				delete(result.skillLinks[key], dir)
			}
		}
	}
	return diagnostics
}

func openCodeBuiltInAgent(name string) bool {
	switch name {
	case "build", "plan", "general", "explore", "compaction", "title", "summary":
		return true
	}
	return false
}

func normalizeOpenCodeScopes(work []skillTarget, allOutputs map[outputKey]bool, repoRoot string) *diag.Diagnostic {
	if os.Getenv("OPENCODE_CONFIG_DIR") != "" || os.Getenv("OPENCODE_CONFIG") != "" {
		selected := map[outputKey]bool{}
		for _, item := range work {
			if item.target == hcc.TargetOpenCode {
				selected[outputKey{target: item.target, machineScope: item.machineScope}] = true
			}
		}
		if refusal := preflightOpenCodeDestinations(&compilePhaseResult{allOutputs: selected}, repoRoot); refusal != nil {
			return refusal
		}
		if len(selected) == 1 && os.Getenv("OPENCODE_CONFIG_DIR") != "" {
			for key := range allOutputs {
				if key.target == hcc.TargetOpenCode && !selected[key] {
					delete(allOutputs, key)
				}
			}
		}
	}

	return nil
}

// Inspect explicit native config layers, including workspace settings inherited
// beneath a later native-directory config. This is an inventory, not a tree walk.
func openCodeConfigInventory(repo, root string, aggregate harness.Destination) []string {
	files := []string{filepath.Join(aggregate.Root, aggregate.AggregateFile)}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	for _, directory := range []string{filepath.Join(base, "opencode"), repo, filepath.Join(repo, ".opencode"), root} {
		for _, name := range []string{"opencode.json", "opencode.jsonc"} {
			files = append(files, filepath.Join(directory, name))
		}
	}
	if custom := os.Getenv("OPENCODE_CONFIG"); custom != "" {
		files = append(files, custom)
	}
	return files
}

// With only a file override, resource roots remain separate. Reconcile the
// shared aggregate once while retaining the opposite root's cleanup plan.
func openCodeAggregateScope(work []skillTarget, filter *bool, outputs map[outputKey]bool) *bool {
	if os.Getenv("OPENCODE_CONFIG") == "" || os.Getenv("OPENCODE_CONFIG_DIR") != "" {
		return nil
	}
	for _, item := range work {
		if item.target == hcc.TargetOpenCode {
			machine := item.machineScope
			return &machine
		}
	}
	machine := filter != nil && *filter
	if filter == nil && !outputs[outputKey{target: hcc.TargetOpenCode}] {
		machine = outputs[outputKey{target: hcc.TargetOpenCode, machineScope: true}]
	}
	return &machine
}
