package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

func v4ExclusionDecisions(actions ...string) V4ResourceGroup {
	var group V4ResourceGroup
	for _, action := range actions {
		op, locator, _ := strings.Cut(action, ":")
		if op == "exclude" {
			group.Decisions = append(group.Decisions, V4SelectionDecision{Action: V4SelectionExclude, Exclusion: locator})
		} else {
			group.Decisions = append(group.Decisions, V4SelectionDecision{
				Action: V4SelectionExplicit, Locator: locator,
				Request: &V4ResourceRequest{Locator: locator, Required: op == "required"},
			})
		}
	}
	return group
}

func TestV4EffectiveExclusionsPreserveDecisionSemantics(t *testing.T) {
	for _, tc := range []struct {
		name    string
		actions []string
		want    []string
	}{
		{"empty", nil, nil},
		{"include-only", []string{"include:skill/a"}, nil},
		{"exclude-only", []string{"exclude:skill/a"}, []string{"skill/a"}},
		{"excluded", []string{"include:skill/a", "exclude:skill/a"}, []string{"skill/a"}},
		{"reincluded", []string{"exclude:skill/a", "include:skill/a"}, nil},
		{"required-survives", []string{"required:skill/a", "exclude:skill/a"}, nil},
		{"later-options-replace", []string{"required:skill/a", "include:skill/a", "exclude:skill/a"}, []string{"skill/a"}},
		{"first-exclusion-order", []string{"exclude:skill/b", "exclude:skill/a", "exclude:skill/b"}, []string{"skill/b", "skill/a"}},
		{"wildcard-retained", []string{"exclude:*", "include:skill/a"}, []string{"*"}},
		{"kind-wildcard-retained", []string{"exclude:skill/*", "include:skill/a"}, []string{"skill/*"}},
		{"reinclusion-then-exclusion", []string{"exclude:skill/a", "include:skill/a", "exclude:skill/a"}, []string{"skill/a"}},
		{"kind-is-distinct", []string{"exclude:command/a", "include:skill/a"}, []string{"command/a"}},
		{"registry-is-distinct", []string{"exclude:other/skill/a", "include:tools/skill/a"}, []string{"other/skill/a"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			group := v4ExclusionDecisions(tc.actions...)
			before, _ := json.Marshal(group)
			for _, requests := range [][]V4ResourceRequest{nil, effectiveV4Requests(group)} {
				if got := effectiveV4Exclusions(group, requests); !reflect.DeepEqual(got, tc.want) {
					t.Fatalf("exclusions = %#v, want %#v", got, tc.want)
				}
			}
			if got := replayV4Exclusions(group); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("baseline exclusions = %#v, want %#v", got, tc.want)
			}
			after, _ := json.Marshal(group)
			if !bytes.Equal(before, after) {
				t.Fatal("exclusion adaptation mutated decisions")
			}
		})
	}
}

func TestV4EffectiveExclusionsAvoidRepeatedSnapshots(t *testing.T) {
	group := v4ExactExclusionFixture(t, 64).Groups["g"]
	var got []string
	allocations := testing.AllocsPerRun(3, func() { got = effectiveV4Exclusions(group, nil) })
	if len(got) != 64 {
		t.Fatalf("exclusions = %d, want 64", len(got))
	}
	budget := float64(2 * len(group.Decisions))
	if allocations > budget {
		t.Fatalf("allocations = %v, want <=%v without per-exclusion request snapshots", allocations, budget)
	}
}

func TestV4ActiveExclusionAdaptationPreservesSelections(t *testing.T) {
	for _, count := range []int{1, 64, 1024} {
		t.Run(fmt.Sprintf("active=%d", count), func(t *testing.T) {
			cfg := v4ActiveExclusionFixture(t, count)
			before, _ := json.Marshal(cfg)
			out, err := cfg.ToLegacyConfig(repoLayer)
			if err != nil || len(out.Registries) != 1 {
				t.Fatalf("adaptation failed: %v", err)
			}
			want := make([]string, count)
			for i := range count {
				want[i] = fmt.Sprintf("item-%04d", i)
			}
			if !reflect.DeepEqual(out.Registries[0].Skills.Include, want) || !reflect.DeepEqual(out.Registries[0].Skills.Exclude, []string{"unselected"}) {
				t.Fatal("active selections, order or exact exclusion changed")
			}
			after, _ := json.Marshal(cfg)
			if !bytes.Equal(before, after) {
				t.Fatal("active-request adaptation mutated configuration")
			}
		})
	}
}

func TestV4EffectiveExclusionsMatchReplay(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		data := rapid.SliceOfN(rapid.Byte(), 0, 96).Draw(t, "decisions")
		locators := []string{"skill/a", "command/a", "tools/skill/a", "other/skill/b"}
		exclusions := append(append([]string(nil), locators...), "*", "skill/*", "tools/*", "tools/skill/*")
		var group V4ResourceGroup
		for _, value := range data {
			if value%3 == 0 {
				group.Decisions = append(group.Decisions, V4SelectionDecision{Action: V4SelectionExclude, Exclusion: exclusions[int(value>>2)%len(exclusions)]})
			} else {
				locator := locators[int(value>>2)%len(locators)]
				group.Decisions = append(group.Decisions, V4SelectionDecision{Action: V4SelectionExplicit, Locator: locator, Request: &V4ResourceRequest{Locator: locator, Required: value%3 == 2}})
			}
		}
		before, _ := json.Marshal(group)
		want := replayV4Exclusions(group)
		for _, requests := range [][]V4ResourceRequest{nil, effectiveV4Requests(group)} {
			got := effectiveV4Exclusions(group, requests)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("decisions=%#v: got %#v, want %#v", group.Decisions, got, want)
			}
		}
		after, _ := json.Marshal(group)
		if !bytes.Equal(before, after) {
			t.Fatal("exclusion adaptation mutated decisions")
		}
	})
}

// replayV4Exclusions pins the pre-R53 implementation as an oracle.
func replayV4Exclusions(group V4ResourceGroup) []string {
	seen := map[string]bool{}
	var out []string
	for _, decision := range group.Decisions {
		if decision.Action != V4SelectionExclude || seen[decision.Exclusion] {
			continue
		}
		// An exact exclusion superseded by a later include is no longer active.
		_, resourceName := legacyV4ExclusionKindName(decision.Exclusion)
		if resourceName != "*" {
			reincluded := false
			for _, request := range effectiveV4Requests(group) {
				if request.Locator == decision.Exclusion {
					reincluded = true
					break
				}
			}
			if reincluded {
				seen[decision.Exclusion] = true
				continue
			}
		}
		seen[decision.Exclusion] = true
		out = append(out, decision.Exclusion)
	}
	return out
}
