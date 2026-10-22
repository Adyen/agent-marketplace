package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

func TestV4RebuildRequestsPreservesDecisions(t *testing.T) {
	for _, tc := range []struct {
		name    string
		actions []string
		want    []string
	}{
		{"empty", nil, nil},
		{"exact", []string{"include:skill/a", "include:command/a", "exclude:skill/a"}, []string{"command/a"}},
		{"required", []string{"required:skill/a", "exclude:skill/a"}, []string{"skill/a"}},
		{"replacement-is-optional", []string{"required:skill/a", "include:skill/a", "exclude:skill/a"}, nil},
		{"reincluded", []string{"include:skill/a", "exclude:skill/a", "include:skill/a"}, []string{"skill/a"}},
		{"future-request", []string{"exclude:skill/a", "include:skill/a"}, []string{"skill/a"}},
		{"missing", []string{"include:skill/a", "exclude:skill/missing"}, []string{"skill/a"}},
		{"global", []string{"include:skill/a", "required:command/a", "exclude:*"}, []string{"command/a"}},
		{"kind", []string{"include:skill/a", "include:command/a", "exclude:skill/*"}, []string{"command/a"}},
		{"registry", []string{"include:tools/skill/a", "include:other/skill/a", "exclude:tools/*"}, []string{"other/skill/a"}},
		{"registry-kind", []string{"include:tools/skill/a", "include:tools/command/a", "exclude:tools/skill/*"}, []string{"tools/command/a"}},
		{"required-kind", []string{"required:skill/a", "include:command/a", "exclude:skill/*"}, []string{"command/a", "skill/a"}},
		{"required-registry", []string{"required:tools/skill/a", "include:tools/command/a", "exclude:tools/*"}, []string{"tools/skill/a"}},
		{"required-registry-kind", []string{"required:tools/skill/a", "include:tools/command/a", "exclude:tools/skill/*"}, []string{"tools/command/a", "tools/skill/a"}},
		{"required-replaced-before-wildcard", []string{"required:tools/skill/a", "include:tools/skill/a", "exclude:tools/skill/*"}, nil},
		{"replacement-required-before-wildcard", []string{"include:tools/skill/a", "required:tools/skill/a", "exclude:tools/skill/*"}, []string{"tools/skill/a"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			group := v4ExclusionDecisions(tc.actions...)
			cfg := &V4Config{Groups: map[string]V4ResourceGroup{"g": group}}
			cfg.rebuildRequests()
			var got []string
			for _, request := range cfg.Requests {
				got = append(got, request.Locator)
			}
			if !reflect.DeepEqual(got, tc.want) || !reflect.DeepEqual(cfg.Decisions, group.Decisions) {
				t.Fatalf("requests = %v, want %v; decisions = %#v", got, tc.want, cfg.Decisions)
			}
		})
	}
}

func TestV4RebuildRequestsKeepsUnseenFallback(t *testing.T) {
	group := v4ExclusionDecisions("exclude:skill/fallback")
	group.Include = []V4ResourceRequest{{Locator: "skill/fallback", Options: V4ResourceOptions{As: "kept", HasAs: true}}}
	cfg := &V4Config{Groups: map[string]V4ResourceGroup{"g": group}}
	cfg.rebuildRequests()
	if !reflect.DeepEqual(cfg.Requests, group.Include) {
		t.Fatalf("unseen include-only request = %#v, want %#v", cfg.Requests, group.Include)
	}
}

func TestV4RebuildRequestsMatchesPerLocatorInterpretation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		locators := []string{"skill/a", "command/a", "tools/skill/a", "tools/command/a", "other/skill/b"}
		exclusions := append(append([]string(nil), locators...), "*", "skill/*", "tools/*", "tools/skill/*", "skill/missing")
		cfg := &V4Config{Groups: map[string]V4ResourceGroup{}}
		for _, name := range []string{"z", "a"} {
			group := V4ResourceGroup{SourceName: name}
			count := rapid.IntRange(0, 48).Draw(t, name+"-count")
			for i := range count {
				provenance := V4Provenance{File: name + ".yaml", Line: i + 1, Precedence: i}
				if rapid.Bool().Draw(t, "exclude") {
					group.Decisions = append(group.Decisions, V4SelectionDecision{
						SourceName: name, Action: V4SelectionExclude,
						Exclusion: rapid.SampledFrom(exclusions).Draw(t, "selector"), Provenance: provenance,
					})
					continue
				}
				locator := rapid.SampledFrom(locators).Draw(t, "locator")
				request := V4ResourceRequest{
					SourceName: name, Locator: locator, Required: rapid.Bool().Draw(t, "required"),
					Options:   V4ResourceOptions{As: fmt.Sprintf("alias-%d", i), HasAs: true},
					Harnesses: []string{"codex"}, HasHarnesses: true,
					Reason: V4SelectionExplicit, Provenance: []V4Provenance{provenance},
				}
				group.Include = append(group.Include, request)
				group.Decisions = append(group.Decisions, V4SelectionDecision{
					SourceName: name, Action: V4SelectionExplicit, Locator: locator,
					Request: &request, Provenance: provenance,
				})
			}
			// Include-only fallbacks must not be suppressed by earlier decisions.
			group.Include = append(group.Include, V4ResourceRequest{SourceName: name, Locator: "skill/missing"})
			cfg.Groups[name] = group
		}
		before, err := json.Marshal(cfg.Groups)
		if err != nil {
			t.Fatal(err)
		}
		wantRequests, wantDecisions := interpretV4RequestsByLocator(cfg.Groups)
		for range 2 {
			cfg.rebuildRequests()
			if !reflect.DeepEqual(cfg.Requests, wantRequests) || !reflect.DeepEqual(cfg.Decisions, wantDecisions) {
				t.Fatalf("requests = %#v, want %#v; decisions = %#v, want %#v", cfg.Requests, wantRequests, cfg.Decisions, wantDecisions)
			}
		}
		after, err := json.Marshal(cfg.Groups)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("request rebuilding mutated input groups: %v", err)
		}
	})
}

func TestV4RebuildRequestsThroughIncludeTree(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root, fragment := filepath.Join(dir, "config.yaml"), filepath.Join(dir, "fragment.yaml")
	child := []byte("sources: {g: {git: https://example.test/tools.git, ref: main}}\nresources:\n  g:\n    include: [skill/again, skill/from-child, command/again]\n    exclude: [skill/again]\n")
	parent := []byte("apiVersion: agent-marketplace/v4\ninclude: [fragment.yaml]\nresources:\n  g:\n    include: [skill/again]\n    exclude: [command/again]\n")
	for file, data := range map[string][]byte{fragment: child, root: parent} {
		if err := os.WriteFile(file, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := LoadV4ConfigFromPath(root)
	if err != nil {
		t.Fatal(err)
	}
	var locators []string
	for _, request := range cfg.Requests {
		locators = append(locators, request.Locator)
		if len(request.Provenance) == 0 {
			t.Fatalf("request %q lost provenance", request.Locator)
		}
		wantFile := fragment
		if request.Locator == "skill/again" {
			wantFile = root
		}
		if got := request.Provenance[len(request.Provenance)-1].File; got != wantFile {
			t.Fatalf("request %q provenance = %q, want %q", request.Locator, got, wantFile)
		}
	}
	if !reflect.DeepEqual(locators, []string{"skill/again", "skill/from-child"}) || len(cfg.Decisions) != 6 {
		t.Fatalf("requests = %v; decisions = %#v", locators, cfg.Decisions)
	}
	legacy, err := cfg.ToLegacyConfig(repoLayer)
	if err != nil {
		t.Fatal(err)
	}
	if len(legacy.Registries) != 1 || !reflect.DeepEqual(legacy.Registries[0].Skills.Include, []string{"again", "from-child"}) {
		t.Fatalf("runtime adapter changed selections: %#v", legacy.Registries)
	}
	for file, want := range map[string][]byte{fragment: child, root: parent} {
		got, err := os.ReadFile(file)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("configuration read changed authored file %q: %v", file, err)
		}
	}
}

// Interpret one locator's history at a time, independently of the production
// selected-request map and its exclusion scan.
func interpretV4RequestsByLocator(groups map[string]V4ResourceGroup) ([]V4ResourceRequest, []V4SelectionDecision) {
	var requests []V4ResourceRequest
	var decisions []V4SelectionDecision
	names := make([]string, 0, len(groups))
	for name := range groups {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		group := groups[name]
		decisions = append(decisions, group.Decisions...)
		locators := map[string]bool{}
		for _, r := range group.Include {
			locators[r.Locator] = true
		}
		for _, d := range group.Decisions {
			if d.Action != V4SelectionExclude && d.Request != nil {
				locators[d.Locator] = true
			}
		}
		keys := make([]string, 0, len(locators))
		for locator := range locators {
			keys = append(keys, locator)
		}
		sort.Strings(keys)
		for _, locator := range keys {
			var selected *V4ResourceRequest
			excluded := false
			for _, d := range group.Decisions {
				if d.Action == V4SelectionExclude {
					if selected != nil && !selected.Required && selectorMatchesV4Locator(d.Exclusion, locator) {
						excluded = true
					}
				} else if d.Request != nil && d.Locator == locator {
					r := *d.Request
					selected, excluded = &r, false
				}
			}
			if selected == nil {
				for _, r := range group.Include {
					if r.Locator == locator {
						selected = &r
						break
					}
				}
			}
			if selected != nil && !excluded {
				requests = append(requests, *selected)
			}
		}
	}
	return requests, decisions
}

func selectorMatchesV4Locator(selector, locator string) bool {
	if selector == "*" || selector == locator {
		return true
	}
	pattern, parts := strings.Split(selector, "/"), strings.Split(locator, "/")
	if len(parts) < 2 || len(parts) > 3 || pattern[len(pattern)-1] != "*" {
		return false
	}
	switch len(pattern) {
	case 2:
		return pattern[0] == parts[0]
	case 3:
		return len(parts) == 3 && pattern[0] == parts[0] && pattern[1] == parts[1]
	default:
		return false
	}
}
