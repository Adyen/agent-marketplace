package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestV4ExclusionKindNamePreservesPrivateInputs(t *testing.T) {
	for _, tc := range []struct {
		value, kind, name string
	}{
		{"", "", "*"},
		{"*", "*", "*"},
		{"skill", "skill", "*"},
		{"skill/*", "skill", "*"},
		{"skill/item", "skill", "item"},
		{"skill/", "skill", ""},
		{"/", "", ""},
		{"/*", "", "*"},
		{"/item", "", "item"},
		{"//", "", ""},
		{"//item", "", ""},
		{"skill//item", "", ""},
		{"tools/skill/item", "", ""},
		{"tools/skill/*", "tools", "skill"},
		{"tools/skill/item/*", "", ""},
		{"skill/*/*", "skill", "*"},
		{"skill/**", "skill", "**"},
		{"Skill/Item", "Skill", "Item"},
		{" skill / item ", " skill ", " item "},
		{"skill/\x00item", "skill", "\x00item"},
		{"\xff/\xfe", "\xff", "\xfe"},
	} {
		t.Run(fmt.Sprintf("%q", tc.value), func(t *testing.T) {
			kind, name := v4ExclusionKindName(tc.value)
			if kind != tc.kind || name != tc.name {
				t.Fatalf("parse %q = (%q, %q), want (%q, %q)", tc.value, kind, name, tc.kind, tc.name)
			}
		})
	}
}

// Preserve the old split-based behavior even for private, malformed byte
// strings; validation remains the responsibility of the existing callers.
func FuzzV4ExclusionKindNameMatchesSplit(f *testing.F) {
	for _, value := range []string{"", "*", "skill/*", "skill/item", "tools/skill/*", "tools/skill/item", "skill/*/*", "///", "\xff/\x00"} {
		f.Add(value)
	}
	f.Fuzz(func(t *testing.T, value string) {
		kind, name := v4ExclusionKindName(value)
		wantKind, wantName := legacyV4ExclusionKindName(value)
		if kind != wantKind || name != wantName {
			t.Fatalf("parse %q = (%q, %q), want (%q, %q)", value, kind, name, wantKind, wantName)
		}
	})
}

// Freeze the pre-slice algorithm independently of the production parser.
// The exclusion replay oracle also uses this so a parser bug cannot affect
// both production output and its expected result.
func legacyV4ExclusionKindName(value string) (string, string) {
	parts := strings.Split(strings.TrimSuffix(value, "/*"), "/")
	if len(parts) == 1 {
		return parts[0], "*"
	}
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", ""
}

func TestV4ExclusionKindNameReachesRuntimeSelectors(t *testing.T) {
	for _, tc := range []struct {
		name string
		kind ir.ResourceKind
	}{
		{"skill", ir.KindSkill},
		{"command", ir.KindCommand},
		{"subagent", ir.KindSubagent},
		{"hook", ir.KindHook},
		{"mcp", ir.KindMcpServer},
		{"collection", ir.KindCollection},
	} {
		for _, wildcard := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/wildcard=%t", tc.name, wildcard), func(t *testing.T) {
				other, otherKind := "command", ir.KindCommand
				if tc.kind == ir.KindCommand {
					other, otherKind = "skill", ir.KindSkill
				}
				excluded := "drop"
				if wildcard {
					excluded = "*"
				}
				cfg, err := ParseV4([]byte(fmt.Sprintf(
					"apiVersion: agent-marketplace/v4\nsources: {g: {git: https://example.test/tools.git, ref: main}}\nresources:\n  g:\n    include: [%s/keep, %s/drop, %s/kept]\n    exclude: ['%s/%s']\n",
					tc.name, tc.name, other, tc.name, excluded)))
				if err != nil {
					t.Fatal(err)
				}
				before, err := json.Marshal(cfg)
				if err != nil {
					t.Fatal(err)
				}
				out, err := cfg.ToLegacyConfig(repoLayer)
				if err != nil || out == nil || len(out.Registries) != 1 {
					t.Fatalf("runtime adaptation failed: %v", err)
				}
				entry := out.Registries[0]
				selector := entry.SelectorForKindPtr(tc.kind)
				control := entry.SelectorForKindPtr(otherKind)
				if selector == nil || control == nil || entry.ExcludeAll ||
					!reflect.DeepEqual(control.Include, []string{"kept"}) ||
					len(control.Exclude) != 0 || control.ExcludeAll {
					t.Fatal("kind selection or the unrelated retained control changed")
				}
				var includes, excludes []string
				if !wildcard {
					includes, excludes = []string{"keep"}, []string{"drop"}
				}
				if !reflect.DeepEqual(selector.Include, includes) ||
					!reflect.DeepEqual(selector.Exclude, excludes) || selector.ExcludeAll != wildcard {
					t.Fatalf("selectors = %#v, want includes=%#v excludes=%#v wildcard=%t", selector, includes, excludes, wildcard)
				}
				after, err := json.Marshal(cfg)
				if err != nil || !bytes.Equal(before, after) {
					t.Fatalf("runtime adaptation mutated authored configuration: %v", err)
				}
			})
		}
	}
}

func TestV4ExclusionKindNameReachesMarketplace(t *testing.T) {
	for _, count := range []int{1, 64, 1024} {
		t.Run(fmt.Sprintf("exclusions=%d", count), func(t *testing.T) {
			cfg := v4MarketplaceExclusionKindFixture(t, count)
			before, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			out, err := cfg.ToLegacyConfig(userLayer)
			if err != nil {
				t.Fatal(err)
			}
			checkV4MarketplaceExclusionKindResult(t, out, count)
			after, err := json.Marshal(cfg)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("Marketplace adaptation mutated authored input: %v", err)
			}
		})
	}
}
