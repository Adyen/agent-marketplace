package resolver

import (
	"testing"

	"pgregory.net/rapid"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
)

var requiresPropNames = []string{"a", "b", "c", "d", "e", "f"}

type requiresPropFixture struct {
	cfg      *config.Config
	catalogs map[string][]catalog.Entry
	direct   map[string]bool
	excluded map[string]bool
	reaches  map[string]bool
}

func requiresPropDraw(t *rapid.T) *requiresPropFixture {
	names := requiresPropNames
	edges := map[string][]string{}
	for _, name := range names {
		if !rapid.Bool().Draw(t, "has-requires-"+name) {
			continue
		}
		count := rapid.IntRange(1, 2).Draw(t, "edge-count-"+name)
		for i := 0; i < count; i++ {
			target := rapid.SampledFrom(names).Draw(t, "edge-"+name)
			if target != name {
				edges[name] = append(edges[name], "skill:"+target)
			}
		}
	}
	direct := map[string]bool{}
	for _, name := range names {
		if rapid.Bool().Draw(t, "include-"+name) {
			direct[name] = true
		}
	}
	excluded := map[string]bool{}
	for _, name := range names {
		if rapid.Bool().Draw(t, "exclude-"+name) {
			excluded[name] = true
		}
	}
	var includes, excludes []string
	for _, name := range names {
		if direct[name] {
			includes = append(includes, name)
		}
		if excluded[name] {
			excludes = append(excludes, name)
		}
	}
	expand := rapid.Bool().Draw(t, "expand")
	cfg := &config.Config{
		Requires: &config.RequiresConfig{Expand: &expand},
		Registries: []config.RegistryEntry{{
			URL:    "git@example.com:skills.git",
			Ref:    "main",
			Skills: kse(includes, excludes),
		}},
	}
	setCatalogKeys(cfg)
	var entries []catalog.Entry
	for _, name := range names {
		reqs := make([]any, len(edges[name]))
		for i, e := range edges[name] {
			reqs[i] = e
		}
		resource := requiringSkill(name, reqs...)
		entries = append(entries, catalog.Entry{Kind: resource.Kind(), Name: name, Resource: resource})
	}
	effective := map[string]bool{}
	if len(includes) == 0 {
		cfg.Registries[0].Skills.Include = names
		for _, name := range names {
			effective[name] = true
		}
	} else {
		for name := range direct {
			effective[name] = true
		}
	}
	fx := &requiresPropFixture{
		cfg:      cfg,
		catalogs: requiresCatalogs(entries...),
		direct:   effective,
		excluded: excluded,
		reaches:  map[string]bool{},
	}
	queue := []string{}
	for name := range effective {
		queue = append(queue, name)
		fx.reaches[name] = true
	}
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if excluded[name] {
			continue
		}
		for _, edge := range edges[name] {
			target := edge[len("skill:"):]
			if !fx.reaches[target] {
				fx.reaches[target] = true
				queue = append(queue, target)
			}
		}
	}
	return fx
}

func requiresPropNamesList(arts []ResolvedArtifact) map[string]bool {
	out := map[string]bool{}
	for _, a := range arts {
		out[a.Name] = true
	}
	return out
}

func requiresPropReachable(t *rapid.T, f *requiresPropFixture, all []ResolvedArtifact) {
	t.Helper()
	byName := map[string]ResolvedArtifact{}
	for _, a := range all {
		byName[a.Name] = a
	}
	for _, a := range all {
		if f.direct[a.Name] {
			continue
		}
		seen := map[string]bool{}
		queue := []ResolvedArtifact{a}
		reached := false
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			for _, ref := range cur.RequiredBy {
				if f.direct[ref.Name] {
					reached = true
					continue
				}
				if seen[ref.Name] {
					continue
				}
				seen[ref.Name] = true
				if next, ok := byName[ref.Name]; ok {
					queue = append(queue, next)
				}
			}
		}
		if !reached {
			t.Fatalf("implied %q is not reachable from a direct selection: %#v", a.Name, a.RequiredBy)
		}
	}
}

// spec: resource-prerequisites/cycles-and-competing-implied-candidates-resolve-deterministically
// spec: resource-prerequisites/advisory-preview-is-idempotent
func TestRequiresExpansionProperties(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		fx := requiresPropDraw(t)
		expand := *fx.cfg.Requires.Expand

		var preview []ResolvedArtifact
		resolved, _ := Resolve(fx.cfg, fx.catalogs, nil, WithImpliedPreview(&preview))
		names := requiresPropNamesList(resolved)

		var preview2 []ResolvedArtifact
		resolved2, _ := Resolve(fx.cfg, fx.catalogs, nil, WithImpliedPreview(&preview2))
		names2 := requiresPropNamesList(resolved2)
		if len(names) != len(names2) {
			t.Fatalf("non-idempotent selection: %v vs %v", names, names2)
		}
		for name := range names2 {
			if !names[name] {
				t.Fatalf("non-idempotent selection: %v vs %v", names, names2)
			}
		}

		for name := range names {
			if fx.excluded[name] {
				t.Fatalf("excluded resource %q was selected", name)
			}
		}

		if expand {
			for name := range fx.reaches {
				if fx.excluded[name] {
					continue
				}
				if !names[name] {
					t.Fatalf("reachable resource %q missing from enabled closure %v", name, names)
				}
			}
			for name := range names {
				if !fx.reaches[name] {
					t.Fatalf("unreachable resource %q in enabled closure", name)
				}
			}
			requiresPropReachable(t, fx, resolved)
		} else {
			for name := range names {
				if !fx.direct[name] {
					t.Fatalf("advisory mode selected implied resource %q", name)
				}
			}
			for name := range fx.direct {
				if !fx.excluded[name] && !names[name] {
					t.Fatalf("advisory mode dropped direct selection %q", name)
				}
			}
			previewNames := requiresPropNamesList(preview)
			for _, a := range preview {
				if names[a.Name] || !fx.reaches[a.Name] || fx.excluded[a.Name] {
					t.Fatalf("unexpected preview artifact %#v", a.Name)
				}
				if len(a.RequiredBy) == 0 {
					t.Fatalf("preview artifact %q lacks provenance", a.Name)
				}
			}
			for name := range fx.reaches {
				if fx.direct[name] || fx.excluded[name] {
					continue
				}
				if !previewNames[name] {
					t.Fatalf("reachable implied resource %q missing from preview %v", name, previewNames)
				}
			}
		}
	})
}
