package resolver

import (
	"slices"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestWithSelectedCollectionsCapturesSelectedRoots(t *testing.T) {
	col := ir.NewCollection("tools", "", "", []ir.CollectionMember{
		{Name: "member-skill", Kind: ir.KindSkill},
	}, nil, nil, ir.SourceRef{})
	cfg := &config.Config{
		Registries: []config.RegistryEntry{{
			URL:         "git@example.com:repo.git",
			Ref:         "main",
			Source:      config.RepoCommitted,
			Collections: ks("tools"),
		}},
	}
	setCatalogKeys(cfg)
	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:repo.git", "main"): {
			{Kind: ir.KindCollection, Name: "tools", Resource: col, Path: "/cache/repo/collections/tools"},
			{Name: "member-skill", Path: "/cache/repo/member-skill"},
		},
	}

	var selected []ResolvedArtifact
	got, errs := Resolve(cfg, catalogs, nil, WithSelectedCollections(&selected))
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	if len(selected) != 1 || selected[0].Kind != ir.KindCollection || selected[0].Name != "tools" {
		t.Fatalf("expected the selected collection root, got %+v", selected)
	}
	if !slices.ContainsFunc(got, func(a ResolvedArtifact) bool { return a.Name == "member-skill" && a.Via != "" }) {
		t.Fatalf("expected member-skill to remain expanded with a Via annotation, got %+v", got)
	}
	if slices.ContainsFunc(got, func(a ResolvedArtifact) bool { return a.Kind == ir.KindCollection }) {
		t.Fatalf("collection root must not appear in Resolve output")
	}

	withoutSink, _ := Resolve(cfg, catalogs, nil)
	if len(withoutSink) != len(got) {
		t.Fatalf("WithSelectedCollections changed Resolve output: %d vs %d", len(got), len(withoutSink))
	}
}

func TestWithSelectedCollectionsOmitsExcludedRoots(t *testing.T) {
	col := ir.NewCollection("tools", "", "", []ir.CollectionMember{
		{Name: "member-skill", Kind: ir.KindSkill},
	}, nil, nil, ir.SourceRef{})
	cfg := &config.Config{
		Registries: []config.RegistryEntry{{
			URL:         "git@example.com:repo.git",
			Ref:         "main",
			Source:      config.RepoCommitted,
			Collections: ks("tools"),
		}},
	}
	setCatalogKeys(cfg)
	catalogs := map[string][]catalog.Entry{
		ck("git@example.com:repo.git", "main"): {
			{Kind: ir.KindCollection, Name: "tools", Resource: col, Path: "/cache/repo/collections/tools"},
			{Name: "member-skill", Path: "/cache/repo/member-skill"},
		},
	}
	localCfg := &config.LocalConfig{Collections: config.KindSelector{Exclude: []string{"tools"}}}

	var control []ResolvedArtifact
	Resolve(cfg, catalogs, nil, WithSelectedCollections(&control))
	if len(control) != 1 || control[0].Kind != ir.KindCollection || control[0].Name != "tools" {
		t.Fatalf("positive control: expected one selected collection root tools, got %+v", control)
	}

	var selected []ResolvedArtifact
	Resolve(cfg, catalogs, localCfg, WithSelectedCollections(&selected))
	if len(selected) != 0 {
		t.Fatalf("excluded collection must not be reported as selected, got %+v", selected)
	}
}
