package collection

import (
	"fmt"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

// BenchmarkExpand exercises a realistic nested-collection workload:
// a root collection that includes two children, each carrying N skills,
// against a merged config with a handful of registries. Expand is on the
// sync/status hot path, so any regression here is user-visible latency.
func BenchmarkExpand(b *testing.B) {
	const skillsPerCollection = 32

	makeSkills := func(prefix string) []CollectionSkill {
		out := make([]CollectionSkill, 0, skillsPerCollection)
		for i := 0; i < skillsPerCollection; i++ {
			out = append(out, CollectionSkill{Name: fmt.Sprintf("%s-skill-%d", prefix, i)})
		}
		return out
	}

	child1 := Collection{
		Name:              "child-1",
		Skills:            makeSkills("c1"),
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	child2 := Collection{
		Name:              "child-2",
		Skills:            makeSkills("c2"),
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	root := Collection{
		Name:              "root",
		Skills:            makeSkills("root"),
		Includes:          []CollectionInclude{{Name: "child-1"}, {Name: "child-2"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}

	available := []Collection{root, child1, child2}

	existing := []config.RegistryEntry{
		{URL: "git@example.com:core.git", Ref: "main", Source: config.RepoCommitted, CatalogKey: "/cache/core", Skills: config.KindSelector{Include: []string{"root-skill-0"}}},
		{URL: "git@example.com:other.git", Ref: "stable", Source: config.UserPersonal, CatalogKey: "/cache/other"},
	}

	refs := []string{"root"}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, errs := Expand(refs, available, existing)
		if len(errs) != 0 {
			b.Fatalf("unexpected errors: %v", errs)
		}
	}
}

// BenchmarkExpandSameRegistryDifferentPaths exercises the path-scoped Expand
// hot path: a collection that references skills from two different
// subdirectories of the same registry. This guards performance regressions
// for the new (url, ref, path) key logic.
func BenchmarkExpandSameRegistryDifferentPaths(b *testing.B) {
	col := Collection{
		Name:              "mixed",
		Skills:            []CollectionSkill{{Name: "factory-skill"}, {Name: "claude-skill", Path: "skills"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
		SourcePath:        ".factory/skills",
	}

	available := []Collection{col}

	existing := []config.RegistryEntry{
		{URL: "git@example.com:core.git", Ref: "main", Path: ".factory/skills", Source: config.RepoCommitted, CatalogKey: "/cache/core:factory"},
		{URL: "git@example.com:core.git", Ref: "main", Path: "skills", Source: config.UserPersonal, CatalogKey: "/cache/core:claude"},
	}

	refs := []string{"mixed"}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, errs := Expand(refs, available, existing); len(errs) != 0 {
			b.Fatalf("Expand() errors = %v", errs)
		}
	}
}

func BenchmarkExpandMultiKindResources(b *testing.B) {
	col := Collection{
		Name:    "security",
		Version: "1.0",
		Resources: []CollectionEntry{
			{Name: "threat-model"},
			{Name: "security-scan", Kind: "command"},
			{Name: "pre-commit-lint", Kind: "hook"},
			{Name: "sentry", Kind: "mcp"},
			{Name: "reviewer", Kind: "subagent"},
		},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	existing := []config.RegistryEntry{{
		URL:    "git@example.com:core.git",
		Ref:    "main",
		Source: config.RepoCommitted,
	}}
	refs := []string{"security"}
	cols := []Collection{col}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		res, errs := Expand(refs, cols, existing)
		if len(errs) != 0 {
			b.Fatalf("unexpected errors: %v", errs)
		}
		if len(res.SyntheticEntries) != 1 {
			b.Fatalf("unexpected synthetic entry count: %d", len(res.SyntheticEntries))
		}
	}
}

func BenchmarkExpandWithIncludes(b *testing.B) {
	child := Collection{
		Name:              "child",
		Skills:            []CollectionSkill{{Name: "inner-skill"}},
		Resources:         []CollectionEntry{{Name: "inner-hook", Kind: "hook"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	parent := Collection{
		Name:              "parent",
		Skills:            []CollectionSkill{{Name: "outer-skill"}},
		Resources:         []CollectionEntry{{Name: "outer-command", Kind: "command"}},
		Includes:          []CollectionInclude{{Name: "child"}},
		SourceRegistryURL: "git@example.com:core.git",
		SourceRef:         "main",
	}
	existing := []config.RegistryEntry{{
		URL:    "git@example.com:core.git",
		Ref:    "main",
		Source: config.RepoCommitted,
	}}
	refs := []string{"parent"}
	cols := []Collection{parent, child}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		res, errs := Expand(refs, cols, existing)
		if len(errs) != 0 {
			b.Fatalf("unexpected errors: %v", errs)
		}
		if len(res.SyntheticEntries) != 1 {
			b.Fatalf("unexpected synthetic entry count: %d", len(res.SyntheticEntries))
		}
	}
}
