package materialize

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"

	"github.com/Adyen/agent-marketplace/internal/harness"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

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
			files = collectHookSupportFiles(source.path, w.target, source.harness)
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
	primary := filepath.Base(sourcePath)
	excluded := map[string]bool{primary: true}
	info, ok := harness.TargetInfoFor(hcc.Target(sourceHarness))
	if !ok {
		return excluded
	}
	files := append(slices.Clone(info.AggregateFiles.Project), info.AggregateFiles.User...)
	for _, source := range files {
		if filepath.Base(source) != primary {
			continue
		}
		for _, sibling := range files {
			if filepath.Dir(source) == filepath.Dir(sibling) {
				excluded[filepath.Base(sibling)] = true
			}
		}
	}
	return excluded
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
