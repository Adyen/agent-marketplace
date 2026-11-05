package main

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

func setupGitRequirementsFixture(tb testing.TB) (string, lockfile.GitRegistry) {
	tb.Helper()
	repo := setupLocalGitSyncFixture(tb, 1)
	if output, err := executeCommand("sync"); err != nil {
		tb.Fatalf("seed exact lock: %v\n%s", err, output)
	}
	doc, err := lockfile.Read(filepath.Join(repo, ".agents", "lock.yaml"))
	if err != nil || len(doc.Registries) != 1 || len(doc.Registries[0].Roots) != 1 {
		tb.Fatalf("seed registry inventory: %v, doc=%+v", err, doc)
	}
	root := doc.Registries[0].Roots[0]
	if root.Path != "." || root.SourceHarness != "claude" || root.RevisionSchema != "v4" || len(root.Resources) != 1 || root.Resources[0].Kind != "skill" || root.Resources[0].Name != "review-0" || len(root.Resources[0].SupportFiles) != 1 {
		tb.Fatalf("unexpected independently seeded inventory: %+v", root)
	}
	if _, ok := root.Resources[0].SupportFiles["scripts/review.sh"]; !ok {
		tb.Fatalf("missing support file: %+v", root.Resources[0])
	}
	return repo, doc.Registries[0]
}

// Include local Git processes, ref resolution, exact-cache checks and source
// builds on disk. Exclude fixture creation/seed sync, compilation and network.
func BenchmarkAcquireRepeatedGitRequirementsLocalDiskNoNetwork(b *testing.B) {
	for _, tc := range []struct {
		name     string
		count    int
		policies []string
		roots    int
	}{
		{"implicit-1", 1, []string{""}, 1},
		{"implicit-16", 16, []string{""}, 1},
		{"implicit-128", 128, []string{""}, 1},
		{"implicit-512", 512, []string{""}, 1},
		{"explicit-128", 128, []string{"claude"}, 1},
		{"distinct-policies-2", 2, []string{"claude", "factory"}, 2},
	} {
		b.Run(tc.name, func(b *testing.B) {
			_, seeded := setupGitRequirementsFixture(b)
			entries := make([]config.RegistryEntry, tc.count)
			for i := range entries {
				entries[i] = config.RegistryEntry{URL: seeded.URL, Ref: seeded.Ref, SourceHarness: tc.policies[i%len(tc.policies)]}
			}
			first, err := acquireLockedRegistry(context.Background(), seeded.URL, seeded.URL, seeded.Ref, entries)
			if err != nil || len(first.Roots) != tc.roots || first.Commit != seeded.Commit {
				b.Fatalf("preflight acquisition: %v, registry=%+v", err, first)
			}
			if tc.roots == 1 && !reflect.DeepEqual(first, seeded) {
				b.Fatalf("changed complete seeded inventory: %+v", first)
			}
			b.ReportAllocs()
			for b.Loop() {
				registry, err := acquireLockedRegistry(context.Background(), seeded.URL, seeded.URL, seeded.Ref, entries)
				if err != nil || len(registry.Roots) != tc.roots || registry.Commit != seeded.Commit {
					b.Fatalf("acquisition: %v, registry=%+v", err, registry)
				}
			}
		})
	}
}
