package harness

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/ownership"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

var resolvedLayoutProviders = map[hcc.Target]resolvedLayoutProvider{
	hcc.Target(targetmeta.TargetCodex): resolveCodexDestinations,
}

func resolveCodexDestinations(machineScope bool, repoRoot, homeDir string) ([]Destination, error) {
	layout, ok := targetmeta.LookupNativeLayout(targetmeta.TargetCodex)
	if !ok {
		return nil, fmt.Errorf("codex native layout is not registered")
	}

	scope := "repo-"
	base := repoRoot
	if machineScope {
		scope = "machine-"
		base = homeDir
	}
	destinations := make([]Destination, 0, len(layout.Partitions))
	for _, partition := range layout.Partitions {
		root := filepath.Join(base, partition.Prefix)
		if machineScope && partition.MachineRootEnv != "" {
			if configured := os.Getenv(partition.MachineRootEnv); configured != "" {
				if !filepath.IsAbs(configured) {
					return nil, fmt.Errorf("%s must be absolute", partition.MachineRootEnv)
				}
				root = configured
			}
		}
		root, err := canonicalCodexRoot(root)
		if err != nil {
			return nil, err
		}
		destinations = append(destinations, Destination{
			ID:                   scope + partition.ID,
			Root:                 root,
			Prefix:               partition.Prefix,
			InstallationEvidence: partition.InstallationEvidence,
			Info:                 codexTargetInfo(partition),
		})
	}
	if !machineScope {
		canonicalRepo, err := canonicalCodexRoot(repoRoot)
		if err != nil {
			return nil, err
		}
		for _, destination := range destinations {
			if !isStrictlyWithin(canonicalRepo, destination.Root) {
				return nil, fmt.Errorf("codex repository destinations must remain inside the repository root")
			}
		}
	}
	return destinations, nil
}

func isStrictlyWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func canonicalCodexRoot(path string) (string, error) {
	root, err := ownership.CanonicalRoot(path)
	if err != nil {
		return "", fmt.Errorf("resolve Codex destination %q: %w", path, err)
	}
	return root, nil
}

func codexTargetInfo(partition targetmeta.NativePartition) TargetInfo {
	var files AggregateFileSet
	appendAggregateFiles(&files, "", partition.Aggregates)
	var resourceDirs map[ir.ResourceKind]string
	for kind, pattern := range partition.ResourcePaths {
		// A directory-shaped pattern (the %s placeholder is a directory
		// element) materializes as a resource directory, so it contributes
		// no flat ResourceDirs entry — the same predicate the validator
		// enforces against SkillDirKinds.
		if targetmeta.IsDirShapedPattern(pattern) {
			continue
		}
		if resourceDirs == nil {
			resourceDirs = make(map[ir.ResourceKind]string)
		}
		resourceDirs[kind] = path.Dir(pattern)
	}
	return TargetInfo{
		Components:                    partition.Components,
		PlanStrategy:                  PlanSubtree,
		ResourceDirs:                  resourceDirs,
		SkillPathPattern:              partition.ResourcePaths[ir.KindSkill],
		SkillDirKinds:                 partition.SkillDirKinds,
		AggregateFiles:                files,
		OwnershipReconciledAggregates: len(files.Project) > 0,
	}
}

func codexVirtualTargetInfo() (TargetInfo, error) {
	layout, ok := targetmeta.LookupNativeLayout(targetmeta.TargetCodex)
	if !ok {
		return TargetInfo{}, fmt.Errorf("codex native layout is not registered")
	}
	info := TargetInfo{
		PlanStrategy:                  PlanSubtree,
		SkillDirKinds:                 make(map[ir.ResourceKind]bool),
		OwnershipReconciledAggregates: true,
	}
	for _, partition := range layout.Partitions {
		info.Components = append(info.Components, partition.Prefix)
		for kind, pattern := range partition.ResourcePaths {
			if kind == ir.KindSkill {
				info.SkillPathPattern = path.Join(partition.Prefix, pattern)
			}
		}
		for kind, directory := range partition.SkillDirKinds {
			info.SkillDirKinds[kind] = directory
		}
		appendAggregateFiles(&info.AggregateFiles, partition.Prefix, partition.Aggregates)
	}
	return info, nil
}
