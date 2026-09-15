package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

const (
	AdapterDevinOwnedJSON     = "devin-owned-json"
	AdapterCodexOwnedJSONTOML = "codex-owned-json-toml"
	AdapterDevinRepair        = "devin-repair"
	AdapterCodexRepair        = "codex-repair"
)

// InstallationAdapter binds a target's validated native layout to
// application-owned installation policy.
type InstallationAdapter struct {
	target       hcc.Target
	layout       targetmeta.NativeLayout
	info         TargetInfo
	aggregateKey string
	repairKey    string
	resolve      func(bool, string, string) ([]Destination, error)
}

func (a InstallationAdapter) Target() hcc.Target              { return a.target }
func (a InstallationAdapter) Layout() targetmeta.NativeLayout { return a.layout }
func (a InstallationAdapter) Info() TargetInfo                { return a.info }
func (a InstallationAdapter) AggregateHandlerKey() string     { return a.aggregateKey }
func (a InstallationAdapter) RepairKey() string               { return a.repairKey }
func (a InstallationAdapter) Resolve(machineScope bool, repoRoot, homeDir string) ([]Destination, error) {
	if a.resolve == nil {
		return nil, fmt.Errorf("harness: installation adapter %q has no destination resolver", a.target)
	}
	return a.resolve(machineScope, repoRoot, homeDir)
}

// InstallationAdapterRegistry contains the application adapters for targets
// using owned aggregate installation. It is built on demand so descriptor
// validation happens before adapter construction.
type InstallationAdapterRegistry struct {
	adapters map[hcc.Target]InstallationAdapter
}

// isInstallationAdapterTarget identifies the migrated targets whose metadata
// and roots must never be answered by the legacy TargetInfoMap/InstallRoots.
// Keep this small, explicit set beside the composition root: adding a target
// here requires a complete adapter below, rather than creating a partial
// fallback.
func isInstallationAdapterTarget(target hcc.Target) bool {
	return target == hcc.TargetDevin || target == hcc.TargetCodex
}

// IsInstallationAdapterTarget reports whether target must be resolved through
// the validated application adapter registry rather than legacy roots.
func IsInstallationAdapterTarget(target hcc.Target) bool {
	return isInstallationAdapterTarget(target)
}

// NewInstallationAdapterRegistry validates native-layout descriptors and builds
// the Devin and Codex adapters. Other targets continue to use legacy metadata.
func NewInstallationAdapterRegistry() (InstallationAdapterRegistry, error) {
	return newInstallationAdapterRegistry(targetmeta.ValidateNativeLayouts)
}

func newInstallationAdapterRegistry(validateLayouts func() error) (InstallationAdapterRegistry, error) {
	if err := validateLayouts(); err != nil {
		return InstallationAdapterRegistry{}, fmt.Errorf("validate native installation layouts: %w", err)
	}
	roots := make(map[hcc.Target]Roots, 2)
	for _, target := range []targetmeta.Target{targetmeta.TargetDevin, targetmeta.TargetCodex} {
		definition, ok := targetmeta.Lookup(string(target))
		if !ok {
			return InstallationAdapterRegistry{}, fmt.Errorf("harness: %s target is not registered", target)
		}
		if definition.PartitionedRoots {
			continue
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return InstallationAdapterRegistry{}, fmt.Errorf("resolve home directory for %s adapter: %w", target, err)
		}
		roots[hcc.Target(target)] = Roots{Repo: definition.RepoRoot, Machine: filepath.Join(home, definition.UserRoot)}
	}
	adapters := make(map[hcc.Target]InstallationAdapter, 2)
	for _, target := range []targetmeta.Target{targetmeta.TargetDevin, targetmeta.TargetCodex} {
		layout, ok := targetmeta.LookupNativeLayout(target)
		if !ok {
			return InstallationAdapterRegistry{}, fmt.Errorf("harness: %s native layout is not registered", target)
		}
		var info TargetInfo
		var err error
		if target == targetmeta.TargetCodex {
			info, err = codexVirtualTargetInfo()
		} else {
			info, err = targetInfoFromLayout(target)
			info.OwnershipReconciledAggregates = true
			info.SkillPathPattern = mustNativeResourcePattern(targetmeta.TargetDevin, ir.KindSkill)
		}
		if err != nil {
			return InstallationAdapterRegistry{}, err
		}
		if root, ok := roots[hcc.Target(target)]; ok {
			info.Roots = root
		}
		t := hcc.Target(target)
		adapter := InstallationAdapter{target: t, layout: layout, info: info}
		if target == targetmeta.TargetDevin {
			adapter.aggregateKey = AdapterDevinOwnedJSON
			adapter.repairKey = AdapterDevinRepair
		} else {
			adapter.aggregateKey = AdapterCodexOwnedJSONTOML
			adapter.repairKey = AdapterCodexRepair
		}
		adapter.resolve = func(machineScope bool, repoRoot, homeDir string) ([]Destination, error) {
			if t == hcc.TargetCodex {
				return resolveCodexDestinations(machineScope, repoRoot, homeDir)
			}
			return resolveSingleRootAdapterDestinations(adapter, machineScope, repoRoot, homeDir)
		}
		adapters[t] = adapter
	}
	return InstallationAdapterRegistry{adapters: adapters}, nil
}

func (r InstallationAdapterRegistry) Lookup(target hcc.Target) (InstallationAdapter, bool) {
	adapter, ok := r.adapters[target]
	return adapter, ok
}

func (r InstallationAdapterRegistry) Targets() []hcc.Target {
	out := make([]hcc.Target, 0, len(r.adapters))
	for target := range r.adapters {
		out = append(out, target)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// TargetInfoFor returns adapter metadata for migrated targets and legacy
// metadata for the remaining targets. A migrated target never falls back to
// legacy state: if descriptor validation or adapter construction fails it is
// unknown and callers must fail closed.
func TargetInfoFor(target hcc.Target) (TargetInfo, bool) {
	if isInstallationAdapterTarget(target) {
		registry, err := NewInstallationAdapterRegistry()
		if err != nil {
			return TargetInfo{}, false
		}
		adapter, ok := registry.Lookup(target)
		if !ok {
			return TargetInfo{}, false
		}
		return adapter.Info(), true
	}
	info, ok := TargetInfoMap[target]
	return info, ok
}

func resolveSingleRootAdapterDestinations(adapter InstallationAdapter, machineScope bool, repoRoot, homeDir string) ([]Destination, error) {
	root := adapter.info.Roots.For(machineScope, repoRoot)
	if machineScope {
		definition, ok := targetmeta.Lookup(string(adapter.target))
		if !ok {
			return nil, fmt.Errorf("unknown harness target %q", adapter.target)
		}
		root = filepath.Join(homeDir, definition.UserRoot)
	}
	canonical, err := canonicalRoot(root)
	if err != nil {
		return nil, err
	}
	return nativeAggregateDestinations(adapter.target, machineScope, repoRoot, homeDir,
		Destination{ID: "default", Root: canonical, InstallationEvidence: true, Info: adapter.info})
}
