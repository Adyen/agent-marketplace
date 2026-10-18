package resolver

import (
	"context"
	"slices"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

var (
	repositoryTargets = []hcc.Target{hcc.TargetClaude}
	machineTargets    = []hcc.Target{hcc.TargetFactory}
)

// recordingMarketplaceResolver returns fixed artifacts and records the target
// set the generic resolver asked for.
type recordingMarketplaceResolver struct {
	artifacts []ResolvedArtifact
	targets   *[]hcc.Target
}

func (r recordingMarketplaceResolver) Resolve(_ context.Context, _ *config.Config, targets []hcc.Target, _ MarketplaceResolveMode) ([]ResolvedArtifact, []error) {
	*r.targets = slices.Clone(targets)
	out := make([]ResolvedArtifact, len(r.artifacts))
	for i, artifact := range r.artifacts {
		artifact.Harnesses = slices.Clone(targets)
		out[i] = artifact
	}
	return out, nil
}

func harnessesByName(t *testing.T, resolved []ResolvedArtifact) map[string][]hcc.Target {
	t.Helper()
	out := make(map[string][]hcc.Target, len(resolved))
	for _, artifact := range resolved {
		if _, duplicate := out[artifact.Name]; duplicate {
			t.Fatalf("duplicate resolved name %q in %#v", artifact.Name, resolved)
		}
		out[artifact.Name] = artifact.Harnesses
	}
	return out
}

func requireHarnesses(t *testing.T, got map[string][]hcc.Target, name string, want []hcc.Target) {
	t.Helper()
	harnesses, ok := got[name]
	if !ok {
		t.Fatalf("%q not resolved; got %v", name, got)
	}
	if !slices.Equal(harnesses, want) {
		t.Fatalf("%q harnesses = %v, want %v", name, harnesses, want)
	}
}

func TestResolveTargetHarnessesPerSelectingScope(t *testing.T) {
	const machineURL, repoURL = "git@example.com:machine.git", "git@example.com:repo.git"

	t.Run("direct selections and collection members", func(t *testing.T) {
		cfg := &config.Config{Registries: []config.RegistryEntry{
			{URL: machineURL, Ref: "main", Source: config.UserPersonal, Skills: ks("machine-skill"), Collections: ks("machine-bundle")},
			{URL: machineURL, Ref: "org", Source: config.Organization, Skills: ks("org-skill")},
			{URL: repoURL, Ref: "main", Source: config.RepoCommitted, Skills: ks("repo-skill"), Collections: ks("repo-bundle")},
		}}
		setCatalogKeys(cfg)
		machineBundle := ir.NewCollection("machine-bundle", "", "", []ir.CollectionMember{{Name: "machine-member", Kind: ir.KindSkill}}, nil, nil, ir.SourceRef{})
		repoBundle := ir.NewCollection("repo-bundle", "", "", []ir.CollectionMember{{Name: "repo-member", Kind: ir.KindSkill}}, nil, nil, ir.SourceRef{})
		catalogs := map[string][]catalog.Entry{
			cfg.Registries[0].CatalogKey: {
				{Name: "machine-skill", Path: "/cache/machine/machine-skill"},
				{Kind: ir.KindCollection, Name: "machine-bundle", Resource: machineBundle},
				{Name: "machine-member", Path: "/cache/machine/machine-member"},
			},
			cfg.Registries[1].CatalogKey: {{Name: "org-skill", Path: "/cache/machine/org-skill"}},
			cfg.Registries[2].CatalogKey: {
				{Name: "repo-skill", Path: "/cache/repo/repo-skill"},
				{Kind: ir.KindCollection, Name: "repo-bundle", Resource: repoBundle},
				{Name: "repo-member", Path: "/cache/repo/repo-member"},
			},
		}
		resolved, errs := Resolve(cfg, catalogs, nil, WithTargetHarnesses(repositoryTargets), WithMachineTargetHarnesses(machineTargets))
		if len(errs) != 0 {
			t.Fatalf("Resolve errors = %v", errs)
		}
		got := harnessesByName(t, resolved)
		for _, name := range []string{"machine-skill", "org-skill", "machine-member"} {
			requireHarnesses(t, got, name, machineTargets)
		}
		for _, name := range []string{"repo-skill", "repo-member"} {
			requireHarnesses(t, got, name, repositoryTargets)
		}

		// Without a machine set, machine-scope selections keep the
		// repository set, as before.
		resolved, errs = Resolve(cfg, catalogs, nil, WithTargetHarnesses(repositoryTargets))
		if len(errs) != 0 {
			t.Fatalf("Resolve errors without machine targets = %v", errs)
		}
		got = harnessesByName(t, resolved)
		for _, name := range []string{"machine-skill", "org-skill", "machine-member", "repo-skill", "repo-member"} {
			requireHarnesses(t, got, name, repositoryTargets)
		}
	})

	t.Run("local selections use the repository set", func(t *testing.T) {
		cfg := &config.Config{Registries: []config.RegistryEntry{
			{URL: machineURL, Ref: "main", Source: config.UserPersonal},
			{URL: repoURL, Ref: "main", Source: config.RepoCommitted},
		}}
		setCatalogKeys(cfg)
		catalogs := map[string][]catalog.Entry{
			cfg.Registries[0].CatalogKey: {{Name: "local-machine", Path: "/cache/machine/local-machine"}},
			cfg.Registries[1].CatalogKey: {{Name: "local-repo", Path: "/cache/repo/local-repo"}},
		}
		local := &config.LocalConfig{Skills: ks("local-machine", "local-repo")}
		resolved, errs := Resolve(cfg, catalogs, local, WithTargetHarnesses(repositoryTargets), WithMachineTargetHarnesses(machineTargets))
		if len(errs) != 0 {
			t.Fatalf("Resolve errors = %v", errs)
		}
		got := harnessesByName(t, resolved)
		requireHarnesses(t, got, "local-machine", repositoryTargets)
		requireHarnesses(t, got, "local-repo", repositoryTargets)
	})

	t.Run("collection members follow the layer that selected the collection", func(t *testing.T) {
		// A repository collection whose member is bound to a user-layer
		// registry keeps the repository set instead of being dropped by an
		// empty intersection of the two scopes' sets.
		cfg := &config.Config{Registries: []config.RegistryEntry{
			{URL: machineURL, Ref: "main", Source: config.UserPersonal},
			{URL: repoURL, Ref: "main", Source: config.RepoCommitted, Collections: ks("repo-bundle")},
		}}
		setCatalogKeys(cfg)
		bundle := ir.NewCollection("repo-bundle", "", "", []ir.CollectionMember{{Name: "bound-member", Kind: ir.KindSkill, Registry: machineURL}}, nil, nil, ir.SourceRef{})
		catalogs := map[string][]catalog.Entry{
			cfg.Registries[0].CatalogKey: {{Name: "bound-member", Path: "/cache/machine/bound-member"}},
			cfg.Registries[1].CatalogKey: {{Kind: ir.KindCollection, Name: "repo-bundle", Resource: bundle}},
		}
		resolved, errs := Resolve(cfg, catalogs, nil, WithTargetHarnesses(repositoryTargets), WithMachineTargetHarnesses(machineTargets))
		if len(errs) != 0 {
			t.Fatalf("Resolve errors = %v", errs)
		}
		requireHarnesses(t, harnessesByName(t, resolved), "bound-member", repositoryTargets)
	})

	t.Run("marketplace pins are narrowed to their owning layer's set", func(t *testing.T) {
		var requested []hcc.Target
		mp := recordingMarketplaceResolver{targets: &requested, artifacts: []ResolvedArtifact{
			{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "mp-machine"}, CatalogKey: "machine-pin", Source: config.UserPersonal, IsMarketplace: true, MarketplaceURL: "https://market.example"},
			{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "mp-repo"}, CatalogKey: "repo-pin", Source: config.RepoCommitted, IsMarketplace: true, MarketplaceURL: "https://market.example"},
		}}
		cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{{URL: "https://market.example", InstanceID: "configured"}}}
		resolved, errs := Resolve(cfg, nil, nil, WithTargetHarnesses(repositoryTargets), WithMachineTargetHarnesses(machineTargets), WithMarketplaceResolver(mp))
		if len(errs) != 0 {
			t.Fatalf("Resolve errors = %v", errs)
		}
		if want := []hcc.Target{hcc.TargetClaude, hcc.TargetFactory}; !slices.Equal(requested, want) {
			t.Fatalf("Marketplace resolver received %v, want every scope's targets %v", requested, want)
		}
		got := harnessesByName(t, resolved)
		requireHarnesses(t, got, "mp-machine", machineTargets)
		requireHarnesses(t, got, "mp-repo", repositoryTargets)
	})
}
