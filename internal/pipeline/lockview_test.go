package pipeline

import (
	"fmt"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

func TestReadOnlyGitRootMatching(t *testing.T) {
	var largeRoots []lockfile.GitRoot
	var largeWants []lockfile.GitRootRequirement
	for i := range 64 {
		path := fmt.Sprintf("skills/team-%d", i)
		largeRoots = append(largeRoots, lockfile.GitRoot{Path: path, SourceHarness: "factory"})
		largeWants = append(largeWants, lockfile.GitRootRequirement{Path: path, SourceHarness: "factory"})
	}
	for _, tc := range []struct {
		name  string
		roots []lockfile.GitRoot
		wants []lockfile.GitRootRequirement
		match bool
	}{
		{"explicit", []lockfile.GitRoot{{Path: ".", SourceHarness: "factory"}}, []lockfile.GitRootRequirement{{Path: ".", SourceHarness: "factory"}}, true},
		{"auto-first", []lockfile.GitRoot{{Path: ".", SourceHarness: "factory"}}, []lockfile.GitRootRequirement{{}, {SourceHarness: "factory"}}, true},
		{"explicit-first", []lockfile.GitRoot{{Path: ".", SourceHarness: "factory"}}, []lockfile.GitRootRequirement{{SourceHarness: "factory"}, {}}, true},
		{"missing-root", []lockfile.GitRoot{{Path: ".", SourceHarness: "factory"}}, []lockfile.GitRootRequirement{{}, {Path: "missing"}}, false},
		{"missing-explicit-harness", []lockfile.GitRoot{{Path: ".", SourceHarness: "factory"}, {Path: ".", SourceHarness: "claude"}}, []lockfile.GitRootRequirement{{}, {SourceHarness: "devin"}}, false},
		{"extra-root", []lockfile.GitRoot{{Path: ".", SourceHarness: "factory"}, {Path: "extra", SourceHarness: "factory"}}, []lockfile.GitRootRequirement{{}}, false},
		{"extra-harness", []lockfile.GitRoot{{Path: ".", SourceHarness: "factory"}, {Path: ".", SourceHarness: "claude"}}, []lockfile.GitRootRequirement{{}, {SourceHarness: "factory"}}, false},
		{"normalized-path", []lockfile.GitRoot{{Path: "skills", SourceHarness: "factory"}}, []lockfile.GitRootRequirement{{Path: "./skills/"}}, true},
		{"both-explicit-reversed", []lockfile.GitRoot{{Path: ".", SourceHarness: "factory"}, {Path: ".", SourceHarness: "claude"}}, []lockfile.GitRootRequirement{{}, {SourceHarness: "claude"}, {SourceHarness: "factory"}}, true},
		{"implicit-does-not-select-every-harness", []lockfile.GitRoot{{Path: ".", SourceHarness: "factory"}, {Path: ".", SourceHarness: "claude"}}, []lockfile.GitRootRequirement{{}, {}}, false},
		{"invalid-after-explicit", []lockfile.GitRoot{{Path: ".", SourceHarness: "factory"}}, []lockfile.GitRootRequirement{{SourceHarness: "factory"}, {Path: "a/../b"}}, false},
		{"indexed-fresh", largeRoots, largeWants, true},
		{"indexed-extra-root-is-stale", largeRoots, largeWants[:63], false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := lockfile.Document{Registries: []lockfile.GitRegistry{{URL: "https://example.com/repo", Ref: "main", Roots: tc.roots}}}
			entries := make([]config.RegistryEntry, 0, len(tc.wants))
			for _, want := range tc.wants {
				entries = append(entries, config.RegistryEntry{URL: "https://example.com/repo.git", Ref: "main", Path: want.Path, SourceHarness: want.SourceHarness})
			}
			if got := readOnlyGitDocumentMatches(doc, entries); got != tc.match {
				t.Fatalf("match = %v, want %v", got, tc.match)
			}
		})
	}
}
