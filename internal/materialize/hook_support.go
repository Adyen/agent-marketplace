package materialize

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

func readMirroredHookSupport(skill resolver.ResolvedArtifact) ([]artifact.Artifact, error) {
	hook, ok := skill.Resource.(*ir.Hook)
	if !ok || !skill.IsMarketplace || len(hook.SupportFiles) == 0 {
		return nil, nil
	}
	if skill.MarketplaceAcquisition != config.MarketplaceAcquisitionMirror {
		return nil, fmt.Errorf("hook support declarations require mirror acquisition")
	}
	root := materializationSourcePath(skill)
	if root == "" {
		return nil, fmt.Errorf("hook support source is unavailable")
	}
	excluded := catalog.HookSupportExclusions(hook.Source.File, skill.SourceHarness)
	excluded["hooks.json"] = true
	names := make([]string, 0, len(hook.SupportFiles))
	for name := range hook.SupportFiles {
		names = append(names, name)
	}
	sort.Strings(names)
	var files []artifact.Artifact
	for _, name := range names {
		expected := hook.SupportFiles[name]
		if excluded[name] {
			return nil, fmt.Errorf("hook support declaration names a native definition")
		}
		data, executable, err := catalog.ReadHookSupportFile(root, name)
		if err != nil {
			return nil, err
		}
		if artifact.SupportFileDigest(artifact.SupportFile{Data: data, Executable: executable}) != expected {
			return nil, fmt.Errorf("hook support integrity mismatch")
		}
		mode := os.FileMode(0o644)
		if executable {
			mode = 0o755
		}
		files = append(files, artifact.Artifact{Path: name, Bytes: data, Mode: mode})
	}
	return files, nil
}

func mirroredHookSupportPlan(w skillTarget, repoRoot string, files []artifact.Artifact) (string, []artifact.Artifact, error) {
	layout, ok := targetmeta.LookupNativeLayout(w.target)
	if !ok {
		return "", nil, fmt.Errorf("hook support layout is unavailable")
	}
	source := sha256.Sum256([]byte(w.skill.MarketplaceInstanceID + "\x00" + w.skill.RegistryURL + "\x00" + w.skill.RegistryPath))
	var supportDir string
	for _, partition := range layout.Partitions {
		for _, aggregate := range partition.Aggregates {
			if aggregate.Kind == ir.KindHook {
				supportDir = filepath.ToSlash(filepath.Join(partition.Prefix, aggregate.SupportDir, fmt.Sprintf("%x", source), w.skill.Resource.Name()))
			}
		}
	}
	if supportDir == "" {
		return "", nil, fmt.Errorf("hook support layout is unavailable")
	}
	info, _ := harness.TargetInfoFor(w.target)
	if info.ArtifactRootPrefix != "" && !strings.HasPrefix(supportDir, info.ArtifactRootPrefix+"/") {
		supportDir = info.ArtifactRootPrefix + "/" + supportDir
	}
	if info.PlanStrategy == harness.PlanWindsurf {
		supportDir = windsurfDir + "/" + supportDir
	}
	destinations, err := harness.ResolveDestinations(w.target, w.machineScope, repoRoot)
	if err != nil {
		return "", nil, err
	}
	for _, destination := range destinations {
		relative, err := destination.RouteArtifact(supportDir + "/placeholder")
		if err != nil {
			continue
		}
		if destination.Info.PlanStrategy == harness.PlanWindsurf && w.machineScope {
			relative = strings.TrimPrefix(relative, windsurfDir+"/")
		}
		relative = strings.TrimPrefix(relative, info.ArtifactRootPrefix+"/")
		dir := filepath.Dir(filepath.Join(destination.Root, filepath.FromSlash(relative)))
		base := dir
		if !w.machineScope {
			base, err = filepath.Rel(repoRoot, dir)
			if err == nil && (base == ".." || strings.HasPrefix(base, "../")) {
				canonicalRepo, canonicalErr := filepath.EvalSymlinks(repoRoot)
				if canonicalErr != nil {
					return "", nil, fmt.Errorf("hook support root is unavailable")
				}
				base, err = filepath.Rel(canonicalRepo, dir)
			}
			if err != nil || base == ".." || strings.HasPrefix(base, "../") {
				return "", nil, fmt.Errorf("hook support is outside repository")
			}
		}
		state, err := loadPortableState(repoRoot, w.machineScope)
		if err != nil {
			return "", nil, err
		}
		for _, file := range files {
			rel := filepath.ToSlash(filepath.Join(filepath.Dir(relative), "hooks", file.Path))
			dst, err := portableOutputPath(destination.Root, rel)
			if err != nil {
				return "", nil, err
			}
			existing, err := os.Lstat(dst)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil || !existing.Mode().IsRegular() {
				return "", nil, fmt.Errorf("hook support destination is unsafe")
			}
			entryPath := filepath.ToSlash(filepath.Join(destination.Prefix, rel))
			entry, owned := state.entry(w.target, entryPath)
			data, _, err := catalog.ReadHookSupportFile(destination.Root, rel)
			current := portableArtifactDigest(artifact.Artifact{Bytes: data, Mode: existing.Mode()})
			if err != nil || !owned || portableOwnershipDecision(entry, current, portableArtifactDigest(file)) == portableOwnershipCollision {
				return "", nil, fmt.Errorf("hook support destination is not owned")
			}
		}
		for i := range files {
			files[i].Path = supportDir + "/hooks/" + files[i].Path
		}
		return filepath.ToSlash(base), files, nil
	}
	return "", nil, fmt.Errorf("hook support destination is unavailable")
}

func isHookSupportComponent(target hcc.Target, prefix string) bool {
	layout, ok := targetmeta.LookupNativeLayout(target)
	if !ok {
		return false
	}
	for _, partition := range layout.Partitions {
		for _, aggregate := range partition.Aggregates {
			if aggregate.Kind == ir.KindHook && componentName(prefix) == aggregate.SupportDir {
				return true
			}
		}
	}
	return false
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
		if w.skill.IsMarketplace && w.skill.MarketplaceAcquisition != config.MarketplaceAcquisitionIndexOnly {
			continue
		}
		source := hookSupportSource{path: materializationSourcePath(w.skill), target: w.target, harness: w.skill.SourceHarness}
		files, exists := cached[source]
		if !exists {
			if hook, ok := w.skill.Resource.(*ir.Hook); ok && w.skill.IsMarketplace && len(hook.SupportFiles) > 0 {
				continue
			}
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
