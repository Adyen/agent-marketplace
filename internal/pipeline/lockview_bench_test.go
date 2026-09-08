package pipeline

import (
	"fmt"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

func BenchmarkReadOnlyGitDocumentMatches(b *testing.B) {
	registry := lockfile.GitRegistry{URL: "https://example.com/repo", Ref: "main"}
	entries := make([]config.RegistryEntry, 0, 32)
	for i := range 32 {
		root := fmt.Sprintf("skills/team-%d", i)
		registry.Roots = append(registry.Roots, lockfile.GitRoot{Path: root, SourceHarness: "factory"})
		entries = append(entries, config.RegistryEntry{URL: registry.URL, Ref: registry.Ref, Path: root, SourceHarness: "factory"})
	}
	doc := lockfile.Document{Registries: []lockfile.GitRegistry{registry}}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if !readOnlyGitDocumentMatches(doc, entries) {
			b.Fatal("matching roots rejected")
		}
	}
}
