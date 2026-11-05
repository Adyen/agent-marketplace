package resolver

import (
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/Adyen/agent-marketplace/internal/config"
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
	winner, _ = targetmeta.NativeResourcePath(d.Target, d.Winner.ResourceKind(), d.Winner.MaterializedName())
	displaced, _ = targetmeta.NativeResourcePath(d.Target, d.Displaced.ResourceKind(), d.Displaced.MaterializedName())
	return winner, displaced
}

// Sources returns the collision labels of the winning and displaced sources.
func (d SharedPathDisplacement) Sources() (winner, displaced string) {
	return collisionSourceLabel(skillOccurrence{resolved: d.Winner, url: materializationURL(d.Winner)}, false),
		collisionSourceLabel(skillOccurrence{resolved: d.Displaced, url: materializationURL(d.Displaced)}, false)
}

// MaterializedName returns the name the resource installs under: the
// installed name when one is set, otherwise the resource name.
func (r *ResolvedArtifact) MaterializedName() string {
	if r.InstalledName != "" {
		return r.InstalledName
	}
	return r.Name
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

// nativePathKinds records, per target, the resource kinds that install at a
// native path of their own name, such as skills/<name>/SKILL.md. Names of
// such resources that differ only in letter case name one file on
// case-insensitive filesystems.
var nativePathKinds = sync.OnceValue(func() map[hcc.Target]map[ir.ResourceKind]bool {
	out := make(map[hcc.Target]map[ir.ResourceKind]bool)
	for _, definition := range targetmeta.Definitions() {
		for kind := ir.KindSkill; kind <= ir.KindRule; kind++ {
			if _, ok := targetmeta.NativeResourcePattern(definition.Target, kind); ok {
				if out[definition.Target] == nil {
					out[definition.Target] = make(map[ir.ResourceKind]bool)
				}
				out[definition.Target][kind] = true
			}
		}
	}
	return out
})

// hasUpper reports whether s contains an upper-case letter, with a byte loop
// for the ASCII names resource validation allows.
func hasUpper(s string) bool {
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c >= 'A' && c <= 'Z':
			return true
		case c >= utf8.RuneSelf:
			return strings.IndexFunc(s[i:], unicode.IsUpper) >= 0
		}
	}
	return false
}

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
// resource targets a harness on which kinds share a path and no resource name
// contains an upper-case letter.
func SharedPathDisplacements(resolved []ResolvedArtifact) []SharedPathDisplacement {
	shared := sharedPathKinds()
	for i := range resolved {
		a := &resolved[i]
		if hasUpper(a.MaterializedName()) {
			_, displaced := materializationProjection(resolved, false)
			return displaced
		}
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
// Names that differ only in letter case share a path too, as they do on
// case-insensitive filesystems: across kinds the best member's kind keeps it,
// and within one kind a required member keeps it when it outranks the others
// by layer; otherwise those names are left to the case-only collision refusal.
// It returns the winners and every displaced resource, except those displaced
// by an equal name of the same kind, which are already reported as name
// collisions.
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
	pathKinds := nativePathKinds()
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
		return key{slot, a.MaterializedName(), t, a.Source.IsMachineScope()}, isShared
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
		upper := hasUpper(a.MaterializedName())
		for _, target := range targetsOf(a) {
			k, _ := exactKey(a, target)
			caseVariants = caseVariants || upper && pathKinds[target][a.ResourceKind()]
			if prior, ok := winners[k]; ok {
				winners[k] = pickCollisionWinner([]skillOccurrence{occurrence(prior), occurrence(i)}).order
			} else {
				winners[k] = i
			}
		}
	}
	// Exact-name winners on a native path whose names differ only in letter
	// case compete. Across kinds, the kind of the best member keeps the path
	// and members of other kinds are displaced by it. Within that kind, a
	// required best member that outranks every other member by layer keeps the
	// path; without one, the case-only refusal handles the group. Such a group
	// needs a name with an upper-case letter, so the pass is skipped without
	// one.
	var caseLoser map[placement]int
	if caseVariants {
		// foldedKey returns the native-path key with the name folded, or false
		// when the resource does not win an exact-name native path.
		foldedKey := func(i int, a *ResolvedArtifact, target hcc.Target, folded string) (key, bool) {
			k, _ := exactKey(a, target)
			if !pathKinds[target][a.ResourceKind()] || winners[k] != i {
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
			if !competes(a) || !hasUpper(a.MaterializedName()) {
				continue
			}
			folded := strings.ToLower(a.MaterializedName())
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
			folded := a.MaterializedName()
			if hasUpper(folded) {
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
			bestKind := resolved[best].ResourceKind()
			requiredWins := resolved[best].Required
			for _, m := range members {
				if m != best && resolved[m].ResourceKind() == bestKind && resolved[m].Required &&
					config.CompareEntryPrecedence(resolved[best].Source, resolved[best].FilePrecedence, resolved[m].Source, resolved[m].FilePrecedence) <= 0 {
					requiredWins = false
				}
			}
			for _, m := range members {
				if m != best && (resolved[m].ResourceKind() != bestKind || requiredWins) {
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
			best, caseLost := caseLoser[placement{i, target}]
			if caseLost && winner == i {
				winner = best
			}
			if caseLost || resolved[winner].ResourceKind() != kind {
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
