package pipeline

import (
	"fmt"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// benchCatalogs builds a deterministic catalog map for benchmarking.
// Each registry has nSkills skills, nCommands commands, and nSubagents subagents.
func benchCatalogs(registries []config.RegistryEntry, nSkills, nCommands, nSubagents int) map[string]catalog.LoadedCatalog {
	catalogs := make(map[string]catalog.LoadedCatalog, len(registries))
	for _, entry := range registries {
		var entries []catalog.Entry
		for i := 0; i < nSkills; i++ {
			entries = append(entries, catalog.Entry{
				Kind: ir.KindSkill,
				Name: fmt.Sprintf("skill-%03d", i),
			})
		}
		for i := 0; i < nCommands; i++ {
			entries = append(entries, catalog.Entry{
				Kind: ir.KindCommand,
				Name: fmt.Sprintf("cmd-%03d", i),
			})
		}
		for i := 0; i < nSubagents; i++ {
			entries = append(entries, catalog.Entry{
				Kind: ir.KindSubagent,
				Name: fmt.Sprintf("agent-%03d", i),
			})
		}
		catalogs[entry.CatalogKey] = catalog.LoadedCatalog{Entries: entries}
	}
	return catalogs
}

// benchRegistries builds nReg registry entries with deterministic CatalogKeys.
func benchRegistries(nReg int) []config.RegistryEntry {
	entries := make([]config.RegistryEntry, nReg)
	for i := 0; i < nReg; i++ {
		entries[i] = config.RegistryEntry{
			URL:        fmt.Sprintf("https://example.com/registry-%d", i),
			Ref:        "main",
			CatalogKey: fmt.Sprintf("cache-key-%d", i),
		}
	}
	return entries
}

// BenchmarkAllDiscoveredResourceNamesSingleKind benchmarks the
// allDiscoveredResourceNames path for a single kind (the pre-fix code path).
func BenchmarkAllDiscoveredResourceNamesSingleKind(b *testing.B) {
	regs := benchRegistries(1)
	catalogs := benchCatalogs(regs, 50, 50, 50)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = allDiscoveredResourceNames(regs, catalogs, "", config.RepoCommitted, ir.KindSkill)
	}
}

// BenchmarkAllDiscoveredResourceNamesAllKinds benchmarks iterating
// allDiscoveredResourceNames over every kind (the --all / bundle-mode path
// modified by this PR).
func BenchmarkAllDiscoveredResourceNamesAllKinds(b *testing.B) {
	regs := benchRegistries(1)
	catalogs := benchCatalogs(regs, 50, 50, 50)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, k := range config.AllKinds {
			_, _ = allDiscoveredResourceNames(regs, catalogs, "", config.RepoCommitted, k)
		}
	}
}

// BenchmarkAllDiscoveredResourceNamesMultiRegistry benchmarks the multi-registry
// variant with 5 registries each providing mixed kinds.
func BenchmarkAllDiscoveredResourceNamesMultiRegistry(b *testing.B) {
	regs := benchRegistries(5)
	catalogs := benchCatalogs(regs, 20, 20, 20)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, k := range config.AllKinds {
			_, _ = allDiscoveredResourceNames(regs, catalogs, "", config.RepoCommitted, k)
		}
	}
}

// BenchmarkAllDiscoveredResourceNamesLarge benchmarks with a larger dataset
// (200 resources per kind, single registry).
func BenchmarkAllDiscoveredResourceNamesLarge(b *testing.B) {
	regs := benchRegistries(1)
	catalogs := benchCatalogs(regs, 200, 200, 200)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, k := range config.AllKinds {
			_, _ = allDiscoveredResourceNames(regs, catalogs, "", config.RepoCommitted, k)
		}
	}
}
