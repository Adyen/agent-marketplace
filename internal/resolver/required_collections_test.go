package resolver

import (
	"reflect"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func requiredCollectionFixture(provider int) (*config.Config, map[string][]catalog.Entry) {
	cfg := &config.Config{Registries: []config.RegistryEntry{
		{URL: "git@example.invalid:mandatory.git", Ref: "main", Required: true, Source: config.Organization},
		{URL: "git@example.invalid:optional.git", Ref: "main", Source: config.RepoCommitted},
	}}
	setCatalogKeys(cfg)
	catalogs := map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: {
			{Kind: ir.KindSkill, Name: "mandatory-alpha"},
			{Kind: ir.KindSkill, Name: "mandatory-beta"},
		},
		cfg.Registries[1].CatalogKey: {{Kind: ir.KindSkill, Name: "optional-member"}},
	}
	member := "optional-member"
	if provider == 0 {
		member = "mandatory-alpha"
	}
	key := cfg.Registries[provider].CatalogKey
	catalogs[key] = append(catalogs[key], catalog.Entry{Kind: ir.KindCollection, Name: "shared", Resource: ir.NewCollection("shared", "1.0.0", "shared", []ir.CollectionMember{{Kind: ir.KindSkill, Name: member}}, nil, nil, ir.SourceRef{})})
	return cfg, catalogs
}

// spec: resource-selection/top-level-collection-keeps-required-auto-selection
func TestRequiredRegistrySurvivesTopLevelCollectionPropagation(t *testing.T) {
	for _, provider := range []int{0, 1} {
		t.Run([]string{"organization", "repository"}[provider], func(t *testing.T) {
			cfg, catalogs := requiredCollectionFixture(provider)
			control, errs := Resolve(cfg, catalogs, nil)
			if len(errs) != 0 || findResolved(control, "mandatory-alpha") == nil || findResolved(control, "mandatory-beta") == nil {
				t.Fatalf("control lost mandatory resources: resolved=%v errors=%v", control, errs)
			}
			cfg.Collections.Include = []string{"shared"}
			original := cloneConfigForResolve(cfg)
			resolved, errs := Resolve(cfg, catalogs, nil)
			if len(errs) != 0 {
				t.Fatalf("collection lookup failed: %v", errs)
			}
			for _, name := range []string{"mandatory-alpha", "mandatory-beta"} {
				if got := findResolved(resolved, name); got == nil || !got.Required || got.Source != config.Organization {
					t.Fatalf("required organization resource %q lost its authority: %v", name, resolved)
				}
			}
			if provider == 1 {
				if got := findResolved(resolved, "optional-member"); got == nil || got.Via != "collection:shared@1.0.0" {
					t.Fatalf("requested repository collection was not expanded: %v", resolved)
				}
			}
			if !reflect.DeepEqual(cfg, original) {
				t.Fatal("resolution mutated the authored configuration")
			}
		})
	}
}

// spec: resource-selection/top-level-collection-preserves-authored-required-subset
func TestTopLevelCollectionPreservesExplicitRequiredSelection(t *testing.T) {
	cfg, catalogs := requiredCollectionFixture(1)
	cfg.Registries[0].Skills.Include = []string{"mandatory-alpha"}
	cfg.Collections.Include = []string{"shared"}
	resolved, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 || findResolved(resolved, "mandatory-alpha") == nil || findResolved(resolved, "optional-member") == nil || findResolved(resolved, "mandatory-beta") != nil {
		t.Fatalf("authored subset or collection lookup changed: resolved=%v errors=%v", resolved, errs)
	}
}

func TestRequiredAutoSelectionStillReportsMissingTopLevelCollection(t *testing.T) {
	cfg, catalogs := requiredCollectionFixture(0)
	cfg.Registries = cfg.Registries[:1]
	cfg.Collections.Include = []string{"absent"}
	resolved, errs := Resolve(cfg, catalogs, nil)
	if findResolved(resolved, "mandatory-alpha") == nil || findResolved(resolved, "mandatory-beta") == nil {
		t.Fatalf("missing collection removed required resources: %v", resolved)
	}
	if len(errs) != 1 || !IsSelectedNotFound(errs[0]) {
		t.Fatalf("missing collection was not reported: %v", errs)
	}
}

func BenchmarkRequiredCollectionPropagation(b *testing.B) {
	cfg, catalogs := requiredCollectionFixture(1)
	cfg.Collections.Include = []string{"shared"}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = Resolve(cfg, catalogs, nil)
	}
}
