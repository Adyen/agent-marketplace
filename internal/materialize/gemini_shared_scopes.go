package materialize

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"

	"github.com/Adyen/agent-marketplace/internal/harness"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
)

func coalesceGeminiNativeAggregates(result *compilePhaseResult, repoRoot string) (bool, error) {
	repo := outputKey{target: hcc.TargetGemini}
	machine := outputKey{target: hcc.TargetGemini, machineScope: true}
	if !result.allOutputs[repo] && !result.allOutputs[machine] {
		return false, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false, err
	}
	if !sameOutputDir(repoRoot, home) {
		return false, nil
	}
	if result.scopeFilter != nil {
		return false, fmt.Errorf("a scope-restricted operation cannot isolate a shared Gemini root")
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
					return false, fmt.Errorf("decode Gemini native fragment %s: %w", a.Path, err)
				}
				var servers map[string]json.RawMessage
				if rawServers, exists := fragment[container]; exists && len(rawServers) > 0 {
					if err := json.Unmarshal(rawServers, &servers); err != nil {
						return false, fmt.Errorf("decode Gemini %s in %s: %w", container, a.Path, err)
					}
				}
				for name, raw := range servers {
					canonical, err := canonicalOwnedJSON(raw)
					if err != nil {
						return false, err
					}
					if prior, ok := seen[name]; ok && !bytes.Equal(prior, canonical) {
						return false, fmt.Errorf("shared Gemini MCP identity %q has conflicting desired values", name)
					}
					seen[name] = canonical
				}
			}
			if key == repo {
				shared = append(shared, a)
			}
		}
	}
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
	return true, nil
}
