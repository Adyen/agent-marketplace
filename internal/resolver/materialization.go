package resolver

import (
	"strings"
	"sync"
	"unicode"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

// SharedPathDisplacement records a resource that lost a native path on one
// target to a resource of another kind with the same installed name, ignoring
// letter case, such as a command and a skill on a target that installs
// commands as skills.
type SharedPathDisplacement struct {
	Target       hcc.Target
	MachineScope bool
	Winner       ResolvedArtifact
	Displaced    ResolvedArtifact
	// DisplacedIndex is the displaced resource's index in the projection input.
	DisplacedIndex int
}

// Paths returns the scope-neutral native paths of the winner and the displaced
// resource. They differ only in letter case when the names do.
func (d SharedPathDisplacement) Paths() (winner, displaced string) {
	winner, _ = targetmeta.NativeResourcePath(d.Target, d.Winner.ResourceKind(), d.Winner.Name)
	displaced, _ = targetmeta.NativeResourcePath(d.Target, d.Displaced.ResourceKind(), d.Displaced.Name)
	return winner, displaced
}

// Sources returns the collision labels of the winning and displaced sources.
func (d SharedPathDisplacement) Sources() (winner, displaced string) {
	return collisionSourceLabel(skillOccurrence{resolved: d.Winner, url: materializationURL(d.Winner)}, false),
		collisionSourceLabel(skillOccurrence{resolved: d.Displaced, url: materializationURL(d.Displaced)}, false)
}

// ResourceKind returns the kind of the loaded resource, or the catalog entry's
// kind when no resource is loaded.
func (r *ResolvedArtifact) ResourceKind() ir.ResourceKind {
	if r.Resource != nil {
		return r.Resource.Kind()
	}
	return r.Kind
}

// sharedPathKinds maps, per target, every resource kind whose native path
// pattern another kind shares to the first such kind, for example both a
// command and a skill to skill on targets that install commands as skills.
// Kinds with a path of their own are absent. It is derived once from the
// target layouts, the topology authority.
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
			out[definition.Target][canonical] = canonical
		}
	}
	return out
})

func materializationURL(a ResolvedArtifact) string {
	if a.IsMarketplace {
		return a.MarketplaceURL
	}
	return a.RegistryURL
}

// defaultMaterializationTargets is the read-only target list of an artifact
// without harnesses; projected artifacts receive their own copy.
var defaultMaterializationTargets = []hcc.Target{hcc.TargetClaude}

func MaterializationWinners(resolved []ResolvedArtifact) []ResolvedArtifact {
	winners, _ := materializationProjection(resolved, true)
	return winners
}

// SharedPathDisplacements returns the displacements MaterializationProjection
// reports, without building the winners. It returns nil at once when no
// resource targets a harness on which kinds share a path.
func SharedPathDisplacements(resolved []ResolvedArtifact) []SharedPathDisplacement {
	shared := sharedPathKinds()
	for _, a := range resolved {
		targets := a.Harnesses
		if targets == nil {
			targets = defaultMaterializationTargets
		}
		for _, target := range targets {
			if shared[target] != nil {
				_, displaced := materializationProjection(resolved, false)
				return displaced
			}
		}
	}
	return nil
}

// MaterializationProjection keeps, per target and install scope, one resource
// for each native output path. Resources compete when they share a path: on
// most targets that means the same kind and installed name, and on targets
// that install commands as skills also a command and a skill of the same name.
// Across kinds, names that differ only in letter case share a path too, as
// they do on case-insensitive filesystems; within one kind such names are left
// to the case-only collision refusal. It returns the winners and every
// resource displaced by one of another kind; displacements by the same kind
// are already reported as name collisions.
func MaterializationProjection(resolved []ResolvedArtifact) ([]ResolvedArtifact, []SharedPathDisplacement) {
	return materializationProjection(resolved, true)
}

func materializationProjection(resolved []ResolvedArtifact, wantWinners bool) ([]ResolvedArtifact, []SharedPathDisplacement) {
	type key struct {
		kind    ir.ResourceKind
		name    string
		target  hcc.Target
		machine bool
	}
	type placement struct {
		index  int
		target hcc.Target
	}
	shared := sharedPathKinds()
	targetsOf := func(a *ResolvedArtifact) []hcc.Target {
		if a.Harnesses == nil {
			return defaultMaterializationTargets
		}
		return a.Harnesses
	}
	// slotKind returns the kind whose path the resource takes on the target,
	// and whether that path is shared with another kind there.
	slotKind := func(kind ir.ResourceKind, t hcc.Target) (ir.ResourceKind, bool) {
		if canonical, ok := shared[t][kind]; ok {
			return canonical, true
		}
		return kind, false
	}
	exactKey := func(a *ResolvedArtifact, t hcc.Target) (key, bool) {
		slot, isShared := slotKind(a.ResourceKind(), t)
		return key{slot, a.Name, t, a.Source.IsMachineScope()}, isShared
	}
	competes := func(a *ResolvedArtifact) bool {
		return !a.Unresolved && !HasAdditiveSources(a.ResourceKind())
	}
	occurrence := func(i int) skillOccurrence {
		return skillOccurrence{resolved: resolved[i], url: materializationURL(resolved[i]), order: i}
	}
	winners := make(map[key]int, len(resolved))
	caseVariants := false
	for i := range resolved {
		a := &resolved[i]
		if !competes(a) {
			continue
		}
		upper := strings.IndexFunc(a.Name, unicode.IsUpper) >= 0
		for _, target := range targetsOf(a) {
			k, isShared := exactKey(a, target)
			caseVariants = caseVariants || isShared && upper
			if prior, ok := winners[k]; ok {
				winners[k] = pickCollisionWinner([]skillOccurrence{occurrence(prior), occurrence(i)}).order
			} else {
				winners[k] = i
			}
		}
	}
	// Exact-name winners on a shared path whose names differ only in letter
	// case compete across kinds. The kind of the best member keeps the path;
	// members of other kinds are displaced by it. Such a group needs a name
	// with an upper-case letter, so the pass is skipped without one.
	var caseLoser map[placement]int
	if caseVariants {
		// foldedKey returns the shared-path key with the name folded, or false
		// when the resource does not win an exact-name shared path.
		foldedKey := func(i int, a *ResolvedArtifact, target hcc.Target, folded string) (key, bool) {
			k, isShared := exactKey(a, target)
			if !isShared || winners[k] != i {
				return key{}, false
			}
			k.name = folded
			return k, true
		}
		// Only folded names of names with an upper-case letter can group more
		// than one exact name, so collect those first.
		candidates := make(map[key]bool)
		for i := range resolved {
			a := &resolved[i]
			if !competes(a) || strings.IndexFunc(a.Name, unicode.IsUpper) < 0 {
				continue
			}
			folded := strings.ToLower(a.Name)
			for _, target := range targetsOf(a) {
				if k, ok := foldedKey(i, a, target, folded); ok {
					candidates[k] = true
				}
			}
		}
		groups := make(map[key][]int, len(candidates))
		var order []key
		for i := range resolved {
			a := &resolved[i]
			if !competes(a) {
				continue
			}
			folded := a.Name
			if strings.IndexFunc(folded, unicode.IsUpper) >= 0 {
				folded = strings.ToLower(folded)
			}
			for _, target := range targetsOf(a) {
				k, ok := foldedKey(i, a, target, folded)
				if !ok || !candidates[k] {
					continue
				}
				if _, seen := groups[k]; !seen {
					order = append(order, k)
				}
				groups[k] = append(groups[k], i)
			}
		}
		caseLoser = make(map[placement]int)
		for _, k := range order {
			members := groups[k]
			if len(members) < 2 {
				continue
			}
			occurrences := make([]skillOccurrence, 0, len(members))
			for _, m := range members {
				occurrences = append(occurrences, occurrence(m))
			}
			best := pickCollisionWinner(occurrences).order
			for _, m := range members {
				if resolved[m].ResourceKind() != resolved[best].ResourceKind() {
					caseLoser[placement{m, k.target}] = best
				}
			}
		}
	}
	var out []ResolvedArtifact
	if wantWinners {
		out = make([]ResolvedArtifact, 0, len(resolved))
	}
	var displaced []SharedPathDisplacement
	for i := range resolved {
		a := &resolved[i]
		if a.Unresolved {
			continue
		}
		targets := targetsOf(a)
		kind := a.ResourceKind()
		additive := HasAdditiveSources(kind)
		kept := func(target hcc.Target) bool {
			k, _ := exactKey(a, target)
			if winners[k] != i {
				return false
			}
			_, lost := caseLoser[placement{i, target}]
			return !lost
		}
		count := 0
		for _, target := range targets {
			if additive || kept(target) {
				count++
				continue
			}
			k, _ := exactKey(a, target)
			winner := winners[k]
			if best, lost := caseLoser[placement{i, target}]; lost && winner == i {
				winner = best
			}
			if resolved[winner].ResourceKind() != kind {
				displaced = append(displaced, SharedPathDisplacement{Target: target, MachineScope: a.Source.IsMachineScope(), Winner: resolved[winner], Displaced: *a, DisplacedIndex: i})
			}
		}
		if count == 0 || !wantWinners {
			continue
		}
		switch {
		case count != len(targets):
			selected := make([]hcc.Target, 0, count)
			for _, target := range targets {
				if kept(target) {
					selected = append(selected, target)
				}
			}
			targets = selected
		case a.Harnesses == nil:
			targets = []hcc.Target{hcc.TargetClaude}
		}
		winner := *a
		winner.Harnesses = targets
		out = append(out, winner)
	}
	return out, displaced
}
