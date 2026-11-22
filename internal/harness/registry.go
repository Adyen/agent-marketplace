package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"

	"github.com/Adyen/agent-marketplace/internal/ownership"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

const (
	AdapterDevinOwnedJSON     = "devin-owned-json"
	AdapterCodexOwnedJSONTOML = "codex-owned-json-toml"
	AdapterDevinRepair        = "devin-repair"
	AdapterCodexRepair        = "codex-repair"
	AdapterOpenCodeOwnedJSON  = "opencode-owned-json"
	AdapterOpenCodeRepair     = "opencode-repair"
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

// ApplicationTarget combines one compiled target's delivery declaration with
// its existing installation metadata.
type ApplicationTarget struct {
	InstallationAdapter
	ID            string
	acceptedKinds []ir.ResourceKind
}

func NewApplicationTarget(id string, kinds []ir.ResourceKind) ApplicationTarget {
	return ApplicationTarget{ID: id, acceptedKinds: slices.Clone(kinds)}
}

func (t ApplicationTarget) Accepts(kind ir.ResourceKind) bool {
	return slices.Contains(t.acceptedKinds, kind)
}

func (t ApplicationTarget) RequireAccepted(kind ir.ResourceKind) error {
	if !t.Accepts(kind) {
		return fmt.Errorf("harness: target %q does not accept resource kind %q", t.ID, kind)
	}
	return nil
}

func (t ApplicationTarget) Adapter() (InstallationAdapter, bool) {
	return t.InstallationAdapter, t.hasAdapter()
}

func (t ApplicationTarget) hasAdapter() bool {
	return t.resolve != nil
}

func (t ApplicationTarget) Info() TargetInfo {
	return t.InstallationAdapter.Info()
}

// InstallationAdapterRegistry composes all application targets and retains
// the adapters for targets with native-layout installation.
type InstallationAdapterRegistry struct {
	targets []ApplicationTarget
	byID    map[string]ApplicationTarget
}

// isInstallationAdapterTarget identifies the migrated targets whose metadata
// and roots must never be answered by the legacy TargetInfoMap/InstallRoots.
// Keep this small, explicit set beside the composition root: adding a target
// here requires a complete adapter below, rather than creating a partial
// fallback.
func isInstallationAdapterTarget(target hcc.Target) bool {
	return target == hcc.TargetDevin || target == hcc.TargetCodex || target == hcc.TargetOpenCode
}

// IsInstallationAdapterTarget reports whether target must be resolved through
// the validated application adapter registry rather than legacy roots.
func IsInstallationAdapterTarget(target hcc.Target) bool {
	return isInstallationAdapterTarget(target)
}

var (
	installationAdapterRegistry     InstallationAdapterRegistry
	installationAdapterRegistryErr  error
	installationAdapterRegistryOnce sync.Once
)

// NewInstallationAdapterRegistry validates native-layout descriptors and
// composes compiled definitions with legacy and native-layout installation
// metadata.
func NewInstallationAdapterRegistry() (InstallationAdapterRegistry, error) {
	installationAdapterRegistryOnce.Do(func() {
		installationAdapterRegistry, installationAdapterRegistryErr = newInstallationAdapterRegistry(targetmeta.ValidateNativeLayouts)
	})
	if installationAdapterRegistryErr != nil {
		return InstallationAdapterRegistry{}, installationAdapterRegistryErr
	}
	return bindMachineRoots(installationAdapterRegistry, os.UserHomeDir), nil
}

func newInstallationAdapterRegistry(validateLayouts func() error) (InstallationAdapterRegistry, error) {
	if err := validateLayouts(); err != nil {
		return InstallationAdapterRegistry{}, fmt.Errorf("validate native installation layouts: %w", err)
	}
	definitions := targetmeta.Definitions()
	for _, definition := range definitions {
		if err := targetmeta.ValidateDefinition(definition); err != nil {
			return InstallationAdapterRegistry{}, err
		}
	}
	legacyInfo, err := MakeTargetInfo()
	if err != nil {
		return InstallationAdapterRegistry{}, err
	}
	roots := make(map[hcc.Target]Roots, 2)
	for _, target := range []targetmeta.Target{targetmeta.TargetDevin, targetmeta.TargetCodex, targetmeta.TargetOpenCode} {
		definition, ok := targetmeta.Lookup(string(target))
		if !ok {
			return InstallationAdapterRegistry{}, fmt.Errorf("harness: %s target is not registered", target)
		}
		if definition.PartitionedRoots {
			continue
		}
		roots[hcc.Target(target)] = Roots{Repo: definition.RepoRoot}
	}
	adapters := make(map[hcc.Target]InstallationAdapter, 3)
	for _, target := range []targetmeta.Target{targetmeta.TargetDevin, targetmeta.TargetCodex, targetmeta.TargetOpenCode} {
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
			info.SkillPathPattern = mustNativeResourcePattern(target, ir.KindSkill)
		}
		if err != nil {
			return InstallationAdapterRegistry{}, err
		}
		if root, ok := roots[hcc.Target(target)]; ok {
			info.Roots = root
		}
		t := hcc.Target(target)
		adapter := InstallationAdapter{target: t, layout: layout, info: info}
		switch target {
		case targetmeta.TargetDevin:
			adapter.aggregateKey = AdapterDevinOwnedJSON
			adapter.repairKey = AdapterDevinRepair
		case targetmeta.TargetCodex:
			adapter.aggregateKey = AdapterCodexOwnedJSONTOML
			adapter.repairKey = AdapterCodexRepair
		default:
			adapter.aggregateKey = AdapterOpenCodeOwnedJSON
			adapter.repairKey = AdapterOpenCodeRepair
		}
		adapter.resolve = func(machineScope bool, repoRoot, homeDir string) ([]Destination, error) {
			if t == hcc.TargetCodex {
				return resolveCodexDestinations(machineScope, repoRoot, homeDir)
			}
			if t == hcc.TargetOpenCode {
				return resolveOpenCodeDestinations(adapter, machineScope, repoRoot, homeDir)
			}
			return resolveSingleRootAdapterDestinations(adapter, machineScope, repoRoot, homeDir)
		}
		adapters[t] = adapter
	}
	targets := make([]ApplicationTarget, 0, len(definitions))
	for _, definition := range definitions {
		target := hcc.Target(definition.Target)
		appTarget := ApplicationTarget{ID: string(definition.Target), acceptedKinds: definition.AcceptedKinds}
		if adapter, ok := adapters[target]; ok {
			appTarget.InstallationAdapter = adapter
		} else {
			info, ok := legacyInfo[target]
			if !ok {
				return InstallationAdapterRegistry{}, fmt.Errorf("harness: target %q has no installation metadata", target)
			}
			appTarget.InstallationAdapter = InstallationAdapter{info: info}
		}
		targets = append(targets, appTarget)
	}
	return newApplicationTargetRegistry(targets), nil
}

func newApplicationTargetRegistry(targets []ApplicationTarget) InstallationAdapterRegistry {
	registry := InstallationAdapterRegistry{
		targets: make([]ApplicationTarget, len(targets)),
		byID:    make(map[string]ApplicationTarget, len(targets)),
	}
	for i, target := range targets {
		registry.targets[i] = target
		registry.byID[target.ID] = target
	}
	return registry
}

func bindMachineRoots(registry InstallationAdapterRegistry, resolveHome func() (string, error)) InstallationAdapterRegistry {
	home, err := resolveHome()
	if err != nil || home == "" {
		return registry
	}
	definition, ok := targetmeta.Lookup(string(targetmeta.TargetDevin))
	if !ok {
		return registry
	}
	devin, ok := registry.Lookup(string(targetmeta.TargetDevin))
	if !ok || !devin.hasAdapter() {
		return registry
	}
	targets := registry.Targets()
	adapter := devin.InstallationAdapter
	info := adapter.info
	info.Roots.Machine = filepath.Join(home, definition.UserRoot)
	adapter.info = info
	for i := range targets {
		if targets[i].ID == string(targetmeta.TargetDevin) {
			targets[i].InstallationAdapter = adapter
		}
	}
	if openCode, ok := registry.Lookup(string(targetmeta.TargetOpenCode)); ok && openCode.hasAdapter() {
		adapter := openCode.InstallationAdapter
		info := adapter.info
		if root, err := openCodeMachineRoot(home); err == nil {
			info.Roots.Machine = root
		}
		adapter.info = info
		for i := range targets {
			if targets[i].ID == string(targetmeta.TargetOpenCode) {
				targets[i].InstallationAdapter = adapter
			}
		}
	}
	return newApplicationTargetRegistry(targets)
}

func (r InstallationAdapterRegistry) Lookup(id string) (ApplicationTarget, bool) {
	target, ok := r.byID[id]
	return target, ok
}

func (r InstallationAdapterRegistry) Targets() []ApplicationTarget {
	return slices.Clone(r.targets)
}

func (r InstallationAdapterRegistry) LookupAdapter(target hcc.Target) (InstallationAdapter, bool) {
	appTarget, ok := r.Lookup(string(target))
	if !ok {
		return InstallationAdapter{}, false
	}
	return appTarget.Adapter()
}

func (r InstallationAdapterRegistry) AdapterTargets() []hcc.Target {
	out := make([]hcc.Target, 0, len(r.targets))
	for _, target := range r.targets {
		if target.hasAdapter() {
			out = append(out, hcc.Target(target.ID))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func (r InstallationAdapterRegistry) legacyTargetInfoMap() map[hcc.Target]TargetInfo {
	out := make(map[hcc.Target]TargetInfo)
	for _, target := range r.targets {
		if !target.hasAdapter() {
			out[hcc.Target(target.ID)] = target.Info()
		}
	}
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
		appTarget, ok := registry.Lookup(string(target))
		if !ok || !appTarget.hasAdapter() {
			return TargetInfo{}, false
		}
		return appTarget.Info(), true
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
	if !machineScope {
		canonicalRepo, err := ownership.CanonicalRoot(repoRoot)
		if err != nil {
			return nil, err
		}
		resolvedRoot, err := ownership.CanonicalRoot(canonical)
		if err != nil {
			return nil, err
		}
		if !isStrictlyWithin(canonicalRepo, resolvedRoot) {
			return nil, fmt.Errorf("%s repository destination escapes the repository root: %w", adapter.target, os.ErrPermission)
		}
		info, statErr := os.Lstat(canonical)
		if statErr != nil && !os.IsNotExist(statErr) {
			return nil, fmt.Errorf("inspect %s repository destination: %w", adapter.target, statErr)
		}
		if statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			if _, err := filepath.EvalSymlinks(canonical); err != nil {
				return nil, fmt.Errorf("%s repository destination symlink cannot be verified: %w: %w", adapter.target, os.ErrPermission, err)
			}
		}
	}
	if err := ValidateDestinationRoot(machineScope, repoRoot, homeDir, canonical); err != nil {
		return nil, fmt.Errorf("harness adapter destination root: %w", err)
	}
	return nativeAggregateDestinations(adapter.target, machineScope, repoRoot, homeDir,
		Destination{ID: "default", Root: canonical, InstallationEvidence: true, Info: adapter.info})
}
