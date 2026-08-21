package harness

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

var resolvedLayoutProviders = map[hcc.Target]resolvedLayoutProvider{
	hcc.Target(targetmeta.TargetCodex): resolveCodexDestinations,
}

func resolveCodexDestinations(machineScope bool, repoRoot string) ([]Destination, error) {
	layout, ok := targetmeta.LookupNativeLayout(targetmeta.TargetCodex)
	if !ok {
		return nil, fmt.Errorf("codex native layout is not registered")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home directory: %w", err)
	}

	scope := "repo-"
	base := repoRoot
	if machineScope {
		scope = "machine-"
		base = home
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
		root, err = canonicalRoot(root)
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
	return destinations, nil
}

func codexTargetInfo(partition targetmeta.NativePartition) TargetInfo {
	aggregates := make([]string, 0, len(partition.Aggregates))
	for _, aggregate := range partition.Aggregates {
		aggregates = append(aggregates, aggregate.Path)
	}
	slices.Sort(aggregates)
	var resourceDirs map[ir.ResourceKind]string
	for kind, pattern := range partition.ResourcePaths {
		if kind == ir.KindSkill || kind == ir.KindCommand {
			continue
		}
		if resourceDirs == nil {
			resourceDirs = make(map[ir.ResourceKind]string)
		}
		resourceDirs[kind] = path.Dir(pattern)
	}
	return TargetInfo{
		Components:       partition.Components,
		PlanStrategy:     PlanSubtree,
		ResourceDirs:     resourceDirs,
		SkillPathPattern: partition.ResourcePaths[ir.KindSkill],
		SkillDirKinds:    partition.SkillDirKinds,
		AggregateFiles: AggregateFileSet{
			Project: slices.Clone(aggregates),
			User:    aggregates,
		},
		OwnershipReconciledAggregates: len(aggregates) > 0,
	}
}
