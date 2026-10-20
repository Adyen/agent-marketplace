package resolver

import (
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func HarnessTargets(targets []hcc.Target) []hcc.Target {
	if targets == nil {
		return []hcc.Target{hcc.TargetClaude}
	}
	return targets
}

func MaterializationWinners(resolved []ResolvedArtifact) []ResolvedArtifact {
	type key struct {
		kind    ir.ResourceKind
		name    string
		target  hcc.Target
		machine bool
	}
	targetsOf := func(a ResolvedArtifact) []hcc.Target { return HarnessTargets(a.Harnesses) }
	urlOf := func(a ResolvedArtifact) string {
		if a.IsMarketplace {
			return a.MarketplaceURL
		}
		return a.RegistryURL
	}
	keyOf := func(a ResolvedArtifact, t hcc.Target) key {
		kind := a.Kind
		if a.Resource != nil {
			kind = a.Resource.Kind()
		}
		return key{kind, a.Name, t, a.Source.IsMachineScope()}
	}
	winners := make(map[key]int, len(resolved))
	for i, a := range resolved {
		if a.Unresolved {
			continue
		}
		for _, target := range targetsOf(a) {
			k := keyOf(a, target)
			if HasAdditiveSources(k.kind) {
				continue
			}
			if prior, ok := winners[k]; ok {
				winner := pickCollisionWinner([]skillOccurrence{{resolved: resolved[prior], url: urlOf(resolved[prior]), order: prior}, {resolved: a, url: urlOf(a), order: i}})
				winners[k] = winner.order
			} else {
				winners[k] = i
			}
		}
	}
	out := make([]ResolvedArtifact, 0, len(resolved))
	for i, a := range resolved {
		if a.Unresolved {
			continue
		}
		targets := targetsOf(a)
		count := 0
		for _, target := range targets {
			k := keyOf(a, target)
			if HasAdditiveSources(k.kind) || winners[k] == i {
				count++
			}
		}
		if count == 0 {
			continue
		}
		if count != len(targets) {
			selected := make([]hcc.Target, 0, count)
			for _, target := range targets {
				k := keyOf(a, target)
				if HasAdditiveSources(k.kind) || winners[k] == i {
					selected = append(selected, target)
				}
			}
			targets = selected
		}
		a.Harnesses = targets
		out = append(out, a)
	}
	return out
}
