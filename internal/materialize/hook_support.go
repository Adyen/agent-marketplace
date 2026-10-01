package materialize

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/harness"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// mirroredHookForTarget points plugin-root assets at the routed installation,
// not the disposable source staging directory or a harness-specific token.
func mirroredHookForTarget(w skillTarget, repoRoot string) (ir.Resource, error) {
	hook, ok := w.skill.Resource.(*ir.Hook)
	if !ok || !w.skill.IsMarketplace || len(hook.SupportFiles) == 0 {
		return w.skill.Resource, nil
	}
	// Aggregate-only targets have no native support-file component. Keep the
	// verified archive-keyed cache as the execution root rather than inventing
	// an unowned native directory or leaving a plugin-root token unresolved.
	if resourceInstallDirRel(w.target, ir.KindHook) == "" {
		return hookWithSupportRoot(hook, materializationSourcePath(w.skill)), nil
	}
	destinations, err := harness.ResolveDestinations(w.target, w.machineScope, repoRoot)
	if err != nil {
		return nil, err
	}
	supportPath := resourceInstallDirRel(w.target, ir.KindHook) + "/__support_path__"
	for _, destination := range destinations {
		relative, err := destination.RouteArtifact(supportPath)
		if err != nil {
			continue
		}
		if destination.Info.PlanStrategy == harness.PlanWindsurf && w.machineScope {
			relative = strings.TrimPrefix(relative, windsurfDir+"/")
		}
		dir := filepath.Dir(filepath.Join(destination.Root, filepath.FromSlash(relative)))
		return hookWithSupportRoot(hook, dir), nil
	}
	return nil, fmt.Errorf("no hook support destination for %s", w.target)
}

func hookWithSupportRoot(hook *ir.Hook, dir string) *ir.Hook {
	clone := *hook
	for _, token := range []string{"${CLAUDE_PLUGIN_ROOT}", "${DROID_PLUGIN_ROOT}"} {
		clone.Command = strings.ReplaceAll(clone.Command, token+"/hooks/", filepath.ToSlash(dir)+"/")
	}
	return &clone
}

type hookSupportOutput struct {
	output outputKey
	path   string
}

type hookSupportSource struct {
	path    string
	target  hcc.Target
	harness string
}

type hookSupportClaim struct {
	artifact  artifact.Artifact
	producers []int
	conflict  bool
}

func prepareHookSupport(work []skillTarget) (map[int][]artifact.Artifact, map[int][]string) {
	support := make(map[int][]artifact.Artifact)
	cached := make(map[hookSupportSource][]artifact.Artifact)
	claims := make(map[hookSupportOutput]*hookSupportClaim)
	var keys []hookSupportOutput
	for i, w := range work {
		if w.skill.Resource == nil || w.skill.Resource.Kind() != ir.KindHook {
			continue
		}
		source := hookSupportSource{path: materializationSourcePath(w.skill), target: w.target, harness: w.skill.SourceHarness}
		files, exists := cached[source]
		if !exists {
			if hook, ok := w.skill.Resource.(*ir.Hook); ok && w.skill.IsMarketplace && len(hook.SupportFiles) > 0 {
				names := make([]string, 0, len(hook.SupportFiles))
				for name := range hook.SupportFiles {
					names = append(names, name)
				}
				sort.Strings(names)
				files = hookSupportArtifacts(source.path, names, w.target)
			} else {
				files = collectHookSupportFiles(source.path, w.target, source.harness)
			}
			cached[source] = files
		}
		support[i] = files
		for _, file := range files {
			key := hookSupportOutput{output: outputKey{target: w.target, machineScope: w.machineScope}, path: file.Path}
			claim := claims[key]
			if claim == nil {
				claim = &hookSupportClaim{artifact: file}
				claims[key] = claim
				keys = append(keys, key)
			}
			if !bytes.Equal(claim.artifact.Bytes, file.Bytes) || claim.artifact.Mode.Perm() != file.Mode.Perm() {
				claim.conflict = true
			}
			claim.producers = append(claim.producers, i)
		}
	}
	conflicts := make(map[int][]string)
	for _, key := range keys {
		claim := claims[key]
		if !claim.conflict {
			continue
		}
		for _, i := range claim.producers {
			conflicts[i] = append(conflicts[i], key.path)
		}
	}
	return support, conflicts
}

func hookSupportExclusions(sourcePath, sourceHarness string) map[string]bool {
	return catalog.HookSupportExclusions(sourcePath, sourceHarness)
}

func retainBlockedHookArtifacts(outDir string, artifacts []artifact.Artifact) ([]artifact.Artifact, error) {
	result := slices.Clone(artifacts)
	for i, a := range result {
		if err := a.Validate(); err != nil {
			return nil, err
		}
		var desired map[string]json.RawMessage
		if err := json.Unmarshal(a.Bytes, &desired); err != nil {
			return nil, err
		}
		if len(desired) == 0 {
			continue
		}
		_, nested := desired["hooks"]
		if !nested {
			arrays := true
			for _, value := range desired {
				if trimmed := bytes.TrimSpace(value); len(trimmed) == 0 || trimmed[0] != '[' {
					arrays = false
					break
				}
			}
			if !arrays {
				continue
			}
		}
		file := filepath.Join(outDir, filepath.FromSlash(a.Path))
		existing, err := os.ReadFile(file)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		doc, err := jsonc.Parse(file, existing)
		if err != nil {
			return nil, err
		}
		if doc.Root().Kind() != jsonc.KindObject {
			return nil, fmt.Errorf("hook aggregate must be a JSON object: %s", file)
		}
		rawDesired := a.Bytes
		if !nested {
			wrapped, err := json.Marshal(map[string]json.RawMessage{"hooks": doc.Root().Canonical()})
			if err != nil {
				return nil, err
			}
			doc, err = jsonc.Parse(file, wrapped)
			if err != nil {
				return nil, err
			}
			desired = map[string]json.RawMessage{"hooks": a.Bytes}
			rawDesired, err = json.Marshal(desired)
			if err != nil {
				return nil, err
			}
		}
		retained, err := retainNativeManagedEntries(doc, desired, rawDesired)
		if err != nil {
			return nil, err
		}
		if !nested {
			var unwrapped map[string]json.RawMessage
			if err := json.Unmarshal(retained, &unwrapped); err != nil {
				return nil, err
			}
			retained = unwrapped["hooks"]
		}
		result[i].Bytes = retained
	}
	return result, nil
}
