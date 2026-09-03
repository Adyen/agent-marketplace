package lockfile

import (
	"fmt"
	"testing"
)

func BenchmarkPlanUnchanged(b *testing.B) {
	commit, err := ParseGitOID(sha1a)
	if err != nil {
		b.Fatal(err)
	}
	doc := Document{APIVersion: APIVersionV1, Scope: ScopeRepo}
	requirements := Requirements{Scope: ScopeRepo}
	for i := 0; i < 100; i++ {
		url := fmt.Sprintf("https://git.example.com/team/repo-%03d", i)
		rootPath := fmt.Sprintf("resources/%03d", i)
		doc.Registries = append(doc.Registries, GitRegistry{URL: url, Ref: "main", Commit: commit, Roots: []GitRoot{{Path: rootPath, SourceHarness: "claude", RevisionSchema: "v1", RevisionDigest: Digest(sha256a)}}})
		requirements.Git = append(requirements.Git, GitRequirement{URL: url, Ref: "main", Roots: []GitRootRequirement{{Path: rootPath, SourceHarness: "claude"}}})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		plan, err := Plan(requirements, &doc, OperationSync)
		if err != nil {
			b.Fatal(err)
		}
		if plan.Changed() || len(plan.Retained) != 100 {
			b.Fatalf("unexpected plan: %#v", plan)
		}
	}
}

func BenchmarkNormalizeCollectionGraph(b *testing.B) {
	digest := Digest(sha256a)
	root := MarketplaceCollection{SourceID: "sets", Name: "base", Revision: digest, Direct: true}
	resources := make([]MarketplaceResource, 32)
	for i := range resources {
		name := fmt.Sprintf("leaf-%02d", i)
		root.Members = append(root.Members, CollectionEdge{SourceID: "src", Kind: "skill", Name: name, Revision: digest})
		resources[i] = marketplaceTestResource("src", "skill", name, digest, false)
		resources[i].SelectedBy = []CollectionEdge{{SourceID: root.SourceID, Name: root.Name, Revision: root.Revision}}
	}
	doc := Document{APIVersion: APIVersionV1, Scope: ScopeRepo, Marketplaces: []Marketplace{{
		URL: "https://market.example", InstanceID: "mp", Collections: []MarketplaceCollection{root}, Resources: resources,
	}}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Normalize(doc); err != nil {
			b.Fatal(err)
		}
	}
}
