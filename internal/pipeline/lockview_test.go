package pipeline

import (
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

func TestReadOnlyGitRootMatching(t *testing.T) {
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := lockfile.Document{Registries: []lockfile.GitRegistry{{URL: "https://example.com/repo", Ref: "main", Roots: tc.roots}}}
			entries := make([]config.RegistryEntry, 0, len(tc.wants))
			for _, want := range tc.wants {
				entries = append(entries, config.RegistryEntry{URL: "https://example.com/repo.git", Ref: "main", Path: want.Path, SourceHarness: want.SourceHarness})
			}
			if got := readOnlyGitDocumentMatches(doc, entries, nil); got != tc.match {
				t.Fatalf("match = %v, want %v", got, tc.match)
			}
		})
	}
}
