package materialize

import (
	"strings"

	"github.com/Adyen/agent-marketplace/internal/harness"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

// blockedAllComponents is the wildcard component. It records "a resource was
// skipped and we cannot attribute it to a component", which is the only case
// that may still suppress pruning for a whole output root.
const blockedAllComponents = "*"

var codexNativePrefixes = func() []string {
	layout, _ := targetmeta.LookupNativeLayout(targetmeta.TargetCodex)
	prefixes := make([]string, 0, len(layout.Partitions))
	for _, partition := range layout.Partitions {
		prefixes = append(prefixes, partition.Prefix)
	}
	return prefixes
}()

// blockedOutputs records, per output key, WHICH output components must not be
// pruned this run because a resource that would have written into them was
// skipped this run.
//
// Pruning is suppressed because a resource skipped this run is indistinguishable
// from one that was removed from configuration: a stale-file scan would delete
// output the previous clean run wrote. That argument is only valid for the part
// of the output the skipped resource would have written, which is why this is a
// SET OF COMPONENTS and not a bool.
//
// Bead agent-marketplace-5exj.20 introduced the component granularity, as the
// prerequisite for escalating the Devin hook and MCP entry-skip diagnostics to
// errors: with an output-key-wide bool, one unrepresentable hook would have
// stopped `.devin/skills/` and `.devin/agents/` from pruning as well. Devin
// hooks and MCP servers write to shared aggregate FILES, not to component
// directories, so they now block no component here and are prune-suppressed
// per file by ownedJSONAggregateWork.incomplete instead.
type blockedOutputs map[outputKey]map[string]bool

// mark records components for an output key. Marking zero components still
// registers the key: a resource really was skipped for that output, it just
// cannot cost any component its pruning.
func (b blockedOutputs) mark(key outputKey, components ...string) {
	set := b[key]
	if set == nil {
		set = make(map[string]bool, len(components))
		b[key] = set
	}
	for _, component := range components {
		set[component] = true
	}
}

// blockAll suppresses pruning for the entire output root. It is reserved for
// skips that cannot be attributed to a component — a resolved artifact with no
// IR resource has no kind to map.
func (b blockedOutputs) blockAll(key outputKey) { b.mark(key, blockedAllComponents) }

// blockResource suppresses pruning for exactly the components the skipped
// resource would have written into.
func (b blockedOutputs) blockResource(key outputKey, w skillTarget) {
	b.mark(key, blockedComponentsFor(key, w)...)
}

// componentsFor returns the blocked component set for one output key.
func (b blockedOutputs) componentsFor(key outputKey) map[string]bool { return b[key] }

// componentBlocked reports whether one component's pruning is suppressed.
// The wildcard blocks every component, including the unnamed root plan.
func componentBlocked(blocked map[string]bool, component string) bool {
	if len(blocked) == 0 {
		return false
	}
	return blocked[blockedAllComponents] || blocked[componentName(component)]
}

// anyBlocked reports whether ANY component of this output key is blocked. It
// gates the root-wide manifest prune passes, which walk a target's whole
// recorded state rather than one component directory and therefore cannot be
// narrowed the same way.
func anyBlocked(blocked map[string]bool) bool { return len(blocked) > 0 }

// componentName reduces an install-dir-relative path to the component name the
// prune bookkeeping uses: the first path segment, with target-native virtual
// root prefixes removed so routed and unrouted component names agree.
func componentName(rel string) string {
	rel = strings.TrimPrefix(rel, windsurfDir+"/")
	for _, prefix := range codexNativePrefixes {
		rel = strings.TrimPrefix(rel, strings.TrimSuffix(prefix, "/")+"/")
	}
	component, _, _ := strings.Cut(rel, "/")
	return component
}

// blockedComponentsFor maps a skipped resource to the output components it
// would have written into.
//
// It returns nil — block nothing — when the resource's whole output is a shared
// aggregate FILE rather than a component directory. The target's aggregate
// handler suppresses pruning for that exact file, which is strictly narrower
// than any component would be.
//
// It falls back to the wildcard whenever the mapping is not known, so an
// unmapped kind keeps today's fail-closed behaviour rather than silently
// enabling a prune.
func blockedComponentsFor(key outputKey, w skillTarget) []string {
	if w.skill.Resource == nil {
		return []string{blockedAllComponents}
	}
	info, ok := harness.TargetInfoMap[key.target]
	if !ok {
		return []string{blockedAllComponents}
	}
	kind := w.skill.Resource.Kind()
	if !info.ResourceSupportedForScope(kind, key.machineScope) {
		return nil
	}
	if handler := ownedAggregateHandlerFor(key.target); handler != nil && len(handler.aggregateRels(kind, key.machineScope)) > 0 {
		return nil
	}
	if info.UsesSkillDir(kind) {
		return []string{info.ComponentDirForScope(skillComponent(info), key.machineScope)}
	}
	if dir := info.ResourceInstallDir(kind); dir != "" {
		return []string{info.ComponentDirForScope(componentName(dir), key.machineScope)}
	}
	return []string{blockedAllComponents}
}

// skillComponent is the component directory a target's skill DIRECTORIES live
// in, read from the target's own skill path pattern so it cannot drift from
// what the compilers emit. "skills" is the fallback for the one target that
// declares no pattern (Claude).
func skillComponent(info harness.TargetInfo) string {
	if info.SkillPathPattern == "" {
		return "skills"
	}
	return componentName(info.SkillPathPattern)
}
