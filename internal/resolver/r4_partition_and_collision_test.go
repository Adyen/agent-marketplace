package resolver

import (
	"errors"
	"slices"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/skill"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestCollectionMemberMissNotSuppressedAcrossTargetPartitions(t *testing.T) {
	direct := config.RegistryEntry{
		URL:       "git@example.com:direct.git",
		Ref:       "main",
		Source:    config.RepoCommitted,
		Skills:    ks("shared"),
		Harnesses: []string{string(hcc.TargetClaude)},
	}
	collectionOwner := config.RegistryEntry{
		URL:         "git@example.com:collections.git",
		Ref:         "main",
		Source:      config.RepoCommitted,
		Collections: ks("tools"),
		Harnesses:   []string{string(hcc.TargetFactory)},
	}
	cfg := &config.Config{Registries: []config.RegistryEntry{direct, collectionOwner}}
	setCatalogKeys(cfg)

	collection := ir.NewCollection("tools", "", "", []ir.CollectionMember{
		{Name: "shared", Kind: ir.KindSkill},
	}, nil, nil, ir.SourceRef{})
	catalogs := map[string][]catalog.Entry{
		ck(direct.URL, direct.Ref): {
			{Kind: ir.KindSkill, Name: "shared", Resource: ir.NewSkill("shared", "Claude", "", nil, nil, ir.SourceRef{})},
		},
		ck(collectionOwner.URL, collectionOwner.Ref): {
			{Kind: ir.KindCollection, Name: "tools", Resource: collection},
		},
	}

	resolved, errs := Resolve(cfg, catalogs, nil)
	if len(resolved) != 1 || resolved[0].Name != "shared" || resolved[0].Via != "" {
		t.Fatalf("resolved = %#v, want only direct Claude artifact", resolved)
	}
	if !harnessesEqual(resolved[0].Harnesses, []hcc.Target{hcc.TargetClaude}) {
		t.Fatalf("direct artifact harnesses = %v, want Claude", resolved[0].Harnesses)
	}

	var miss *SelectedNotFoundError
	for _, err := range errs {
		if errors.As(err, &miss) {
			break
		}
	}
	if miss == nil || miss.Kind != ir.KindSkill || miss.Name != "shared" || miss.Via == "" {
		t.Fatalf("errors = %v, want missing Factory collection member diagnostic", errs)
	}
}

func TestDirectArtifactSuppressesSamePartitionCollectionMissingMember(t *testing.T) {
	const (
		directURL = "git@example.com:direct.git"
		memberURL = "git@example.com:member.git"
	)
	cfg := &config.Config{Registries: []config.RegistryEntry{
		{URL: directURL, Ref: "main", Source: config.RepoCommitted, Skills: ks("shared"), Harnesses: []string{string(hcc.TargetClaude)}},
		{URL: "git@example.com:collections.git", Ref: "main", Source: config.RepoCommitted, Collections: ks("tools"), Harnesses: []string{string(hcc.TargetClaude)}},
		{URL: memberURL, Ref: "main", Path: "skills", Source: config.RepoCommitted, Harnesses: []string{string(hcc.TargetClaude)}},
	}}
	setCatalogKeys(cfg)
	collection := ir.NewCollection("tools", "", "", []ir.CollectionMember{{
		Name: "shared", Kind: ir.KindSkill, Registry: memberURL, Path: "skills",
	}}, nil, nil, ir.SourceRef{})
	catalogs := map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: {{Kind: ir.KindSkill, Name: "shared"}},
		cfg.Registries[1].CatalogKey: {{Kind: ir.KindCollection, Name: "tools", Resource: collection}},
		cfg.Registries[2].CatalogKey: {},
	}

	resolved, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("same-partition direct winner should suppress collection miss, got %v", errs)
	}
	if len(resolved) != 1 || resolved[0].Name != "shared" || resolved[0].Via != "" {
		t.Fatalf("resolved = %#v, want only direct shared artifact", resolved)
	}
}

func TestDirectArtifactSuppressesSamePartitionCollectionMalformedMember(t *testing.T) {
	const (
		directURL = "git@example.com:direct-malformed.git"
		memberURL = "git@example.com:member-malformed.git"
	)
	cfg := &config.Config{Registries: []config.RegistryEntry{
		{URL: directURL, Ref: "main", Source: config.RepoCommitted, Skills: ks("shared"), Harnesses: []string{string(hcc.TargetClaude)}},
		{URL: "git@example.com:collections-malformed.git", Ref: "main", Source: config.RepoCommitted, Collections: ks("tools"), Harnesses: []string{string(hcc.TargetClaude)}},
		{URL: memberURL, Ref: "main", Path: "skills", Source: config.RepoCommitted, Harnesses: []string{string(hcc.TargetClaude)}},
	}}
	setCatalogKeys(cfg)
	collection := ir.NewCollection("tools", "", "", []ir.CollectionMember{{
		Name: "shared", Kind: ir.KindSkill, Registry: memberURL, Path: "skills",
	}}, nil, nil, ir.SourceRef{})
	catalogs := map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: {{Kind: ir.KindSkill, Name: "shared"}},
		cfg.Registries[1].CatalogKey: {{Kind: ir.KindCollection, Name: "tools", Resource: collection}},
		cfg.Registries[2].CatalogKey: {},
	}
	malformed := malformedFixture(cfg.Registries[2].CatalogKey, skill.MalformedSkill{
		DirName: "shared", Path: "/cache/member-malformed/skills/shared", Error: errors.New("invalid frontmatter"),
	})

	resolved, errs := Resolve(cfg, catalogs, nil, WithMalformed(malformed))
	if len(errs) != 0 {
		t.Fatalf("same-partition direct winner should suppress collection malformed diagnostic, got %v", errs)
	}
	if len(resolved) != 1 || resolved[0].Name != "shared" || resolved[0].Via != "" {
		t.Fatalf("resolved = %#v, want only direct shared artifact", resolved)
	}
}

func TestDeferredCollectionMissRetainedAcrossTargetPartitions(t *testing.T) {
	const (
		missingURL = "git@example.com:missing-target-collection.git"
		foundURL   = "git@example.com:found-target-collection.git"
	)
	cfg := &config.Config{
		Collections: ks("tools"),
		Registries: []config.RegistryEntry{
			{URL: missingURL, Ref: "main", Source: config.RepoCommitted, Harnesses: []string{string(hcc.TargetClaude)}},
			{URL: foundURL, Ref: "main", Source: config.RepoCommitted, Harnesses: []string{string(hcc.TargetFactory)}},
		},
	}
	setCatalogKeys(cfg)
	collection := ir.NewCollection("tools", "", "", nil, nil, nil, ir.SourceRef{})
	catalogs := map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: {},
		cfg.Registries[1].CatalogKey: {{Kind: ir.KindCollection, Name: "tools", Resource: collection}},
	}

	_, errs := Resolve(cfg, catalogs, nil, WithTargetHarnesses([]hcc.Target{hcc.TargetClaude, hcc.TargetFactory}))
	var miss *SelectedNotFoundError
	for _, err := range errs {
		if errors.As(err, &miss) && miss.Kind == ir.KindCollection && miss.Name == "tools" {
			return
		}
	}
	t.Fatalf("collection miss in Claude partition was suppressed by Factory collection: %v", errs)
}

func TestDeferredCollectionMissRetainedAcrossScopePartitions(t *testing.T) {
	const (
		missingURL = "git@example.com:missing-scope-collection.git"
		foundURL   = "git@example.com:found-scope-collection.git"
	)
	cfg := &config.Config{
		Collections: ks("tools"),
		Registries: []config.RegistryEntry{
			{URL: missingURL, Ref: "main", Source: config.RepoCommitted, Harnesses: []string{string(hcc.TargetClaude)}},
			{URL: foundURL, Ref: "main", Source: config.UserPersonal, Harnesses: []string{string(hcc.TargetClaude)}},
		},
	}
	setCatalogKeys(cfg)
	collection := ir.NewCollection("tools", "", "", nil, nil, nil, ir.SourceRef{})
	catalogs := map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: {},
		cfg.Registries[1].CatalogKey: {{Kind: ir.KindCollection, Name: "tools", Resource: collection}},
	}

	_, errs := Resolve(cfg, catalogs, nil, WithTargetHarnesses([]hcc.Target{hcc.TargetClaude}))
	var miss *SelectedNotFoundError
	for _, err := range errs {
		if errors.As(err, &miss) && miss.Kind == ir.KindCollection && miss.Name == "tools" {
			return
		}
	}
	t.Fatalf("collection miss in repo partition was suppressed by machine collection: %v", errs)
}

func TestCollisionRunnersUpAreOrderIndependent(t *testing.T) {
	build := func(reverse bool) (*config.Config, map[string][]catalog.Entry) {
		registries := []config.RegistryEntry{
			{URL: "git@example.com:c.git", Ref: "main", Source: config.RepoCommitted, Skills: ks("shared"), Harnesses: []string{string(hcc.TargetClaude)}},
			{URL: "git@example.com:a.git", Ref: "main", Source: config.RepoCommitted, Skills: ks("shared"), Harnesses: []string{string(hcc.TargetClaude)}},
			{URL: "git@example.com:b.git", Ref: "main", Source: config.RepoCommitted, Skills: ks("shared"), Harnesses: []string{string(hcc.TargetClaude)}},
		}
		if reverse {
			slices.Reverse(registries)
		}
		cfg := &config.Config{Registries: registries}
		setCatalogKeys(cfg)
		catalogs := make(map[string][]catalog.Entry, len(registries))
		for _, entry := range registries {
			entries := []catalog.Entry{
				{
					Kind:     ir.KindSkill,
					Name:     "shared",
					Resource: ir.NewSkill("shared", "collision", "", nil, nil, ir.SourceRef{}),
				},
				{Kind: ir.KindSkill, Name: "catalog-order-control"},
			}
			if reverse {
				slices.Reverse(entries)
			}
			catalogs[entry.CatalogKey] = entries
		}
		return cfg, catalogs
	}

	want := []string{"git@example.com:b", "git@example.com:c"}
	for _, reverse := range []bool{false, true} {
		cfg, catalogs := build(reverse)
		_, errs := Resolve(cfg, catalogs, nil)
		var warning *NameCollisionWarning
		for _, err := range errs {
			if errors.As(err, &warning) {
				break
			}
		}
		if warning == nil {
			t.Fatalf("reverse=%v errors = %v, want collision warning", reverse, errs)
		}
		if warning.Winner != "git@example.com:a" {
			t.Fatalf("reverse=%v winner = %q, want deterministic URL winner", reverse, warning.Winner)
		}
		if !slices.Equal(warning.RunnersUp, want) {
			t.Fatalf("reverse=%v runners-up = %v, want %v", reverse, warning.RunnersUp, want)
		}
	}
}
