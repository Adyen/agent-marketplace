package hookpolicy

import (
	"fmt"
	"slices"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

type HookLoadRisk struct {
	Loader target.Target
	Loaded target.Target
}

func DuplicateExecutionRisks(selected []target.Target) ([]HookLoadRisk, error) {
	if len(selected) == 0 {
		return nil, nil
	}
	registry, err := shipped()
	if err != nil {
		return nil, fmt.Errorf("shipped hook catalogs are invalid: %w", err)
	}
	selectedSet := make(map[target.Target]struct{}, len(selected))
	for _, harness := range selected {
		selectedSet[harness] = struct{}{}
	}
	ordered := make([]target.Target, 0, len(selectedSet))
	for harness := range selectedSet {
		ordered = append(ordered, harness)
	}
	slices.Sort(ordered)
	for _, harness := range ordered {
		if _, ok := registry.catalogs[harness]; !ok {
			return nil, fmt.Errorf("no shipped hook catalog for target %s", harness)
		}
	}

	seenPairs := make(map[[2]target.Target]struct{})
	var risks []HookLoadRisk
	for _, loader := range ordered {
		for _, loadedName := range registry.catalogs[loader].LoadsHooksOf {
			loaded := target.Target(loadedName)
			if _, ok := selectedSet[loaded]; !ok {
				continue
			}
			first, second := loader, loaded
			if second < first {
				first, second = second, first
			}
			pair := [2]target.Target{first, second}
			if _, ok := seenPairs[pair]; ok {
				continue
			}
			seenPairs[pair] = struct{}{}
			risks = append(risks, HookLoadRisk{Loader: loader, Loaded: loaded})
		}
	}
	slices.SortFunc(risks, func(a, b HookLoadRisk) int {
		if order := strings.Compare(string(a.Loader), string(b.Loader)); order != 0 {
			return order
		}
		return strings.Compare(string(a.Loaded), string(b.Loaded))
	})
	return risks, nil
}
