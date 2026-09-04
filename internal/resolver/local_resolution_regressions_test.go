package resolver

import (
	"reflect"
	"sync"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestResolveLocalSelectionChoosesCompatibleOwnerBeforeAuthority(t *testing.T) {
	cfg := &config.Config{Registries: []config.RegistryEntry{
		{URL: "git@example.com:incompatible.git", Ref: "main", Source: config.UserPersonal, Harnesses: []string{"claude"}},
		{URL: "git@example.com:compatible.git", Ref: "main", Source: config.RepoCommitted, Harnesses: []string{"factory"}},
	}}
	setCatalogKeys(cfg)
	catalogs := map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: {{Kind: ir.KindSkill, Name: "shared"}},
		cfg.Registries[1].CatalogKey: {{Kind: ir.KindSkill, Name: "shared"}},
	}

	resolved, errs := Resolve(cfg, catalogs, &config.LocalConfig{Skills: ks("shared")}, WithTargetHarnesses([]hcc.Target{"factory"}))
	if len(resolved) != 1 || resolved[0].Name != "shared" {
		t.Fatalf("expected one locally selected resource, got %#v (errs=%v)", resolved, errs)
	}
	if resolved[0].RegistryURL != "git@example.com:compatible.git" || resolved[0].RegistryPath != "" {
		t.Fatalf("selected incompatible/first owner instead of compatible owner: %#v", resolved[0])
	}
	if !reflect.DeepEqual(resolved[0].Harnesses, []hcc.Target{"factory"}) {
		t.Fatalf("effective harnesses = %#v, want [factory]", resolved[0].Harnesses)
	}
}

func TestResolveLocalSelectionCannotBypassHigherLayerExclude(t *testing.T) {
	cfg := &config.Config{Registries: []config.RegistryEntry{
		{URL: "git@example.com:lower.git", Ref: "main", Source: config.RepoCommitted},
		{URL: "git@example.com:higher.git", Ref: "main", Source: config.RepoPersonal, Skills: config.KindSelector{Exclude: []string{"blocked"}}},
	}}
	setCatalogKeys(cfg)
	catalogs := map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: {{Kind: ir.KindSkill, Name: "blocked"}},
		cfg.Registries[1].CatalogKey: {},
	}

	resolved, errs := Resolve(cfg, catalogs, &config.LocalConfig{Skills: ks("blocked")})
	for _, artifact := range resolved {
		if artifact.Name == "blocked" {
			t.Fatalf("local selection reintroduced higher-layer exclusion: %#v", artifact)
		}
	}
	if len(errs) != 0 {
		t.Fatalf("higher-layer exclusion should be a no-op, got errors: %v", errs)
	}
}

func TestResolveDoesNotMutateConfigDuringCollectionPropagation(t *testing.T) {
	cfg := &config.Config{
		Collections: config.KindSelector{Include: []string{"top-level"}, Exclude: []string{"never"}},
		Registries: []config.RegistryEntry{{
			URL:         "git@example.com:collections.git",
			Ref:         "main",
			Collections: config.KindSelector{Include: []string{"entry-level"}, Exclude: []string{"entry-never"}},
		}},
	}
	setCatalogKeys(cfg)
	catalogs := map[string][]catalog.Entry{cfg.Registries[0].CatalogKey: {
		{Kind: ir.KindCollection, Name: "entry-level"},
		{Kind: ir.KindCollection, Name: "top-level"},
	}}
	beforeValue := *cfg
	beforeValue.Registries = append([]config.RegistryEntry(nil), cfg.Registries...)
	before := &beforeValue

	const runs = 20
	var wg sync.WaitGroup
	for i := 0; i < runs; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			Resolve(cfg, catalogs, nil)
		}()
	}
	wg.Wait()

	if !reflect.DeepEqual(cfg, before) {
		t.Fatalf("Resolve mutated caller config:\n got:  %#v\n want: %#v", cfg, before)
	}
}
