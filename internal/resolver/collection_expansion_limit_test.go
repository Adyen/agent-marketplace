package resolver

import (
	"errors"
	"fmt"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: resource-selection/collection-expansion-stays-within-the-work-limit
func TestResolveRefusesCollectionExpansionAboveWorkLimit(t *testing.T) {
	for _, tc := range []struct {
		depth   int
		refused bool
	}{{8, false}, {12, true}} {
		t.Run(fmt.Sprintf("depth %d", tc.depth), func(t *testing.T) {
			cfg, catalogs := collectionDiamond(tc.depth)
			resolved, errs := Resolve(cfg, catalogs, nil)
			refused := false
			for _, err := range errs {
				if errors.Is(err, ErrCollectionExpansionLimit) {
					refused = true
				} else if !tc.refused {
					t.Fatalf("unexpected error: %v", err)
				}
			}
			if refused != tc.refused {
				t.Fatalf("depth %d: refused=%t, want %t (errs=%v)", tc.depth, refused, tc.refused, errs)
			}
			if !tc.refused && findResolved(resolved, "leaf-skill") == nil {
				t.Fatalf("depth %d: shared leaf skill was not expanded", tc.depth)
			}
		})
	}
}

// spec: resource-selection/collection-expansion-stays-within-the-work-limit
func TestResolveSharesCollectionExpansionLimitAcrossReselection(t *testing.T) {
	// A depth-10 diamond fits one selection pass. A repository selection of
	// the required collection's leaf forces a second pass, and both passes
	// together exceed the per-resolve limit.
	diamondCfg, diamondCatalogs := collectionDiamond(10)
	required := diamondCfg.Registries[0]
	required.Source, required.Required = config.Organization, true
	cfg := &config.Config{Registries: []config.RegistryEntry{
		required,
		{URL: "git@example.com:repository.git", Ref: "main", Source: config.RepoCommitted, Skills: ks("leaf-skill")},
	}}
	setCatalogKeys(cfg)
	catalogs := map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: diamondCatalogs[diamondCfg.Registries[0].CatalogKey],
		cfg.Registries[1].CatalogKey: {{Kind: ir.KindSkill, Name: "leaf-skill"}},
	}
	_, errs := Resolve(cfg, catalogs, nil)
	for _, err := range errs {
		if errors.Is(err, ErrCollectionExpansionLimit) {
			return
		}
	}
	t.Fatalf("errs = %v, want the expansion limit shared across both selection passes", errs)
}

// spec: resource-selection/collection-expansion-stays-within-the-work-limit
func TestResolveKeepsCollectionExpansionLimitWhenReselecting(t *testing.T) {
	// The first pass admits early-skill, then exhausts the limit in a depth-11
	// diamond. The repository selection of early-skill forces a second pass,
	// which starts with the budget exhausted and must still refuse.
	diamondCfg, diamondCatalogs := collectionDiamond(11)
	entries := append([]catalog.Entry(nil), diamondCatalogs[diamondCfg.Registries[0].CatalogKey]...)
	entries[0].Resource = ir.NewCollection("root", "1.0.0", "root", []ir.CollectionMember{{Name: "early-skill", Kind: ir.KindSkill}}, []string{"a1", "b1"}, nil, ir.SourceRef{})
	entries = append(entries, catalog.Entry{Kind: ir.KindSkill, Name: "early-skill"})
	required := diamondCfg.Registries[0]
	required.Source, required.Required = config.Organization, true
	cfg := &config.Config{Registries: []config.RegistryEntry{
		required,
		{URL: "git@example.com:repository.git", Ref: "main", Source: config.RepoCommitted, Skills: ks("early-skill")},
	}}
	setCatalogKeys(cfg)
	catalogs := map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: entries,
		cfg.Registries[1].CatalogKey: {{Kind: ir.KindSkill, Name: "early-skill"}},
	}
	_, errs := Resolve(cfg, catalogs, nil)
	for _, err := range errs {
		if errors.Is(err, ErrCollectionExpansionLimit) {
			return
		}
	}
	t.Fatalf("errs = %v, want the expansion limit kept after reselection", errs)
}
