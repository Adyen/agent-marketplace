package resolver

import (
	"sync"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

// SharedPathDisplacement records a resource that lost a native path on one
// target to a resource of another kind with the same installed name, such as a
// command and a skill on a target that installs commands as skills.
type SharedPathDisplacement struct {
	Target       hcc.Target
	MachineScope bool
	Winner       ResolvedArtifact
	Displaced    ResolvedArtifact
}

// Path returns the scope-neutral native path the two resources share.
func (d SharedPathDisplacement) Path() string {
	path, _ := targetmeta.NativeResourcePath(d.Target, resolvedKind(d.Displaced), d.Displaced.Name)
	return path
}

// Sources returns the collision labels of the winning and displaced sources.
func (d SharedPathDisplacement) Sources() (winner, displaced string) {
	return collisionSourceLabel(skillOccurrence{resolved: d.Winner, url: materializationURL(d.Winner)}, false),
		collisionSourceLabel(skillOccurrence{resolved: d.Displaced, url: materializationURL(d.Displaced)}, false)
}

// sharedPathKinds maps, per target, each resource kind that installs at the
// same native path pattern as an earlier kind to that earlier kind, for
// example a command to a skill on targets that install commands as skills.
// It is derived once from the target layouts, the topology authority.
var sharedPathKinds = sync.OnceValue(func() map[hcc.Target]map[ir.ResourceKind]ir.ResourceKind {
	out := make(map[hcc.Target]map[ir.ResourceKind]ir.ResourceKind)
	for _, definition := range targetmeta.Definitions() {
		first := make(map[string]ir.ResourceKind)
		for kind := ir.KindSkill; kind <= ir.KindRule; kind++ {
			pattern, ok := targetmeta.NativeResourcePattern(definition.Target, kind)
			if !ok {
				continue
			}
			canonical, seen := first[pattern]
			if !seen {
				first[pattern] = kind
				continue
			}
			if out[definition.Target] == nil {
				out[definition.Target] = make(map[ir.ResourceKind]ir.ResourceKind)
			}
			out[definition.Target][kind] = canonical
		}
	}
	return out
})

func resolvedKind(a ResolvedArtifact) ir.ResourceKind {
	if a.Resource != nil {
		return a.Resource.Kind()
	}
	return a.Kind
}

func materializationURL(a ResolvedArtifact) string {
	if a.IsMarketplace {
		return a.MarketplaceURL
	}
	return a.RegistryURL
}

func MaterializationWinners(resolved []ResolvedArtifact) []ResolvedArtifact {
	winners, _ := MaterializationProjection(resolved)
	return winners
}

// MaterializationProjection keeps, per target and install scope, one resource
// for each native output path. Resources compete when they share a path: on
// most targets that means the same kind and installed name, and on targets
// that install commands as skills also a command and a skill of the same name.
// It returns the winners and every resource displaced by one of another kind;
// displacements by the same kind are already reported as name collisions.
func MaterializationProjection(resolved []ResolvedArtifact) ([]ResolvedArtifact, []SharedPathDisplacement) {
	type key struct {
		kind    ir.ResourceKind
		name    string
		target  hcc.Target
		machine bool
	}
	shared := sharedPathKinds()
	targetsOf := func(a ResolvedArtifact) []hcc.Target {
		if a.Harnesses == nil {
			return []hcc.Target{hcc.TargetClaude}
		}
		return a.Harnesses
	}
	keyOf := func(a ResolvedArtifact, t hcc.Target) key {
		kind := resolvedKind(a)
		if canonical, ok := shared[t][kind]; ok {
			kind = canonical
		}
		return key{kind, a.Name, t, a.Source.IsMachineScope()}
	}
	winners := make(map[key]int, len(resolved))
	for i, a := range resolved {
		if a.Unresolved || HasAdditiveSources(resolvedKind(a)) {
			continue
		}
		for _, target := range targetsOf(a) {
			k := keyOf(a, target)
			if prior, ok := winners[k]; ok {
				winner := pickCollisionWinner([]skillOccurrence{{resolved: resolved[prior], url: materializationURL(resolved[prior]), order: prior}, {resolved: a, url: materializationURL(a), order: i}})
				winners[k] = winner.order
			} else {
				winners[k] = i
			}
		}
	}
	out := make([]ResolvedArtifact, 0, len(resolved))
	var displaced []SharedPathDisplacement
	for i, a := range resolved {
		if a.Unresolved {
			continue
		}
		targets := targetsOf(a)
		kind := resolvedKind(a)
		additive := HasAdditiveSources(kind)
		count := 0
		for _, target := range targets {
			if additive {
				count++
				continue
			}
			winner := winners[keyOf(a, target)]
			if winner == i {
				count++
			} else if resolvedKind(resolved[winner]) != kind {
				displaced = append(displaced, SharedPathDisplacement{Target: target, MachineScope: a.Source.IsMachineScope(), Winner: resolved[winner], Displaced: a})
			}
		}
		if count == 0 {
			continue
		}
		if count != len(targets) {
			selected := make([]hcc.Target, 0, count)
			for _, target := range targets {
				if winners[keyOf(a, target)] == i {
					selected = append(selected, target)
				}
			}
			targets = selected
		}
		a.Harnesses = targets
		out = append(out, a)
	}
	return out, displaced
}
