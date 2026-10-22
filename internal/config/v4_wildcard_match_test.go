package config

import (
	"strings"
	"testing"

	"pgregory.net/rapid"
)

func TestV4WildcardMatchingPreservesRawShapes(t *testing.T) {
	values := []string{
		"", "*", "/", "/*", "skill", "skill/", "skill/*", "skill/a",
		"command/a", "tools/*", "tools/skill/*", "tools/skill/a",
		"tools/command/a", "other/skill/a", "tools-extra/skill/a",
		"tools//a", "tools/skill/", "/skill/a", "//", "tools/skill/a/extra",
		"tools/skill/*/extra", "tools/*/*", "*/*", "skill/\xc3\xa9", "skill/\xff",
	}
	for _, selector := range values {
		for _, locator := range values {
			got := v4ExclusionMatches(selector, locator)
			want := selectorMatchesV4Locator(selector, locator)
			if got != want {
				t.Fatalf("match(%q, %q) = %t, want %t", selector, locator, got, want)
			}
		}
	}
}

func TestV4WildcardMatchingMatchesTokenOracle(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		component := rapid.SampledFrom([]string{"", "skill", "command", "tools", "tools-extra", "a", "*", "\xc3\xa9", "\xff"})
		locator := strings.Join(rapid.SliceOfN(component, 0, 5).Draw(t, "locator"), "/")
		selector := strings.Join(rapid.SliceOfN(component, 0, 4).Draw(t, "selector"), "/")
		if rapid.Bool().Draw(t, "terminal-wildcard") {
			selector += "/*"
		}
		if got, want := v4ExclusionMatches(selector, locator), selectorMatchesV4Locator(selector, locator); got != want {
			t.Fatalf("match(%q, %q) = %t, want %t", selector, locator, got, want)
		}
	})
}

func FuzzV4WildcardMatching(f *testing.F) {
	for _, seed := range [][2]string{
		{"skill/*", "skill/a"}, {"tools/*", "tools/skill/a"},
		{"tools/skill/*", "tools/skill/a"}, {"tools/*", "tools-extra/skill/a"},
		{"tools/*", "tools/skill/a/extra"}, {"/*", "/"},
		{"*/*", "*/a/b"}, {"tools/*/*", "tools/*/*"},
		{"", ""}, {"skill/*", "skill/\xff"},
	} {
		f.Add(seed[0], seed[1])
	}
	f.Fuzz(func(t *testing.T, selector, locator string) {
		if len(selector)+len(locator) > 1024 {
			return
		}
		if got, want := v4ExclusionMatches(selector, locator), selectorMatchesV4Locator(selector, locator); got != want {
			t.Fatalf("match(%q, %q) = %t, want %t", selector, locator, got, want)
		}
	})
}
