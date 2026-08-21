package resolver

import (
	"fmt"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// executableResolveFixture builds a config/catalog pair of hooks and MCP
// servers. When contested is true every registry publishes the same names, so
// every identity is refused; otherwise names are registry-unique and every
// identity installs.
func executableResolveFixture(registries, perReg int, contested bool) (*config.Config, map[string][]catalog.Entry) {
	cfg := &config.Config{}
	catalogs := map[string][]catalog.Entry{}
	for r := 0; r < registries; r++ {
		url := fmt.Sprintf("git@example.com:exec-%02d.git", r)
		ref := "main"
		cfg.Registries = append(cfg.Registries, config.RegistryEntry{
			URL:      url,
			Ref:      ref,
			Required: true,
			Source:   config.UserPersonal,
		})
		entries := make([]catalog.Entry, 0, perReg)
		for i := 0; i < perReg; i++ {
			suffix := fmt.Sprintf("reg%02d-%03d", r, i)
			if contested {
				suffix = fmt.Sprintf("%03d", i)
			}
			entries = append(entries,
				catalog.Entry{Kind: ir.KindHook, Name: "hook-" + suffix, Path: fmt.Sprintf("/cache/reg%02d/hook-%03d", r, i)},
				catalog.Entry{Kind: ir.KindMcpServer, Name: "mcp-" + suffix, Path: fmt.Sprintf("/cache/reg%02d/mcp-%03d", r, i)},
			)
		}
		catalogs[cache.CatalogKeyForEntry(url, ref, "")] = entries
	}
	setCatalogKeys(cfg)
	return cfg, catalogs
}

// BenchmarkResolveExecutables measures Resolve over catalogs made entirely of
// executable kinds, which is the shape that exercises the per-identity grouping
// in refuseAmbiguousExecutables on every sync.
func BenchmarkResolveExecutables(b *testing.B) {
	cases := []struct {
		name       string
		registries int
		perReg     int
		contested  bool
	}{
		{"5reg_100exec_unique", 5, 100, false},
		{"10reg_200exec_unique", 10, 200, false},
		{"5reg_100exec_contested", 5, 100, true},
	}

	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			cfg, catalogs := executableResolveFixture(c.registries, c.perReg, c.contested)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = Resolve(cfg, catalogs, nil)
			}
		})
	}
}

// executableCollectionFixture builds one registry per collection, each shipping
// a collection whose members are executables the same registry provides. This is
// the shape that pays for the per-source claim index in collection expansion:
// every member needs its identity computed instead of being dropped by name.
func executableCollectionFixture(registries, perCollection int) (*config.Config, map[string][]catalog.Entry) {
	cfg := &config.Config{}
	catalogs := map[string][]catalog.Entry{}
	for r := 0; r < registries; r++ {
		url := fmt.Sprintf("git@example.com:exec-pack-%02d.git", r)
		ref := "main"
		colName := fmt.Sprintf("pack-%02d", r)
		cfg.Registries = append(cfg.Registries, config.RegistryEntry{
			URL:         url,
			Ref:         ref,
			Source:      config.UserPersonal,
			Collections: config.KindSelector{Include: []string{colName}},
		})

		members := make([]ir.CollectionMember, 0, perCollection*2)
		entries := make([]catalog.Entry, 0, perCollection*2+1)
		for i := 0; i < perCollection; i++ {
			hook := fmt.Sprintf("hook-reg%02d-%03d", r, i)
			mcp := fmt.Sprintf("mcp-reg%02d-%03d", r, i)
			members = append(members,
				ir.CollectionMember{Name: hook, Kind: ir.KindHook},
				ir.CollectionMember{Name: mcp, Kind: ir.KindMcpServer},
			)
			entries = append(entries,
				catalog.Entry{Kind: ir.KindHook, Name: hook, Path: fmt.Sprintf("/cache/reg%02d/%s", r, hook)},
				catalog.Entry{Kind: ir.KindMcpServer, Name: mcp, Path: fmt.Sprintf("/cache/reg%02d/%s", r, mcp)},
			)
		}
		entries = append(entries, catalog.Entry{
			Kind:     ir.KindCollection,
			Name:     colName,
			Resource: ir.NewCollection(colName, "", "", members, nil, nil, ir.SourceRef{}),
			Path:     fmt.Sprintf("/cache/reg%02d/collections/%s", r, colName),
		})
		catalogs[cache.CatalogKeyForEntry(url, ref, "")] = entries
	}
	setCatalogKeys(cfg)
	return cfg, catalogs
}

// BenchmarkResolveExecutableCollectionMembers measures the expansion path where
// executable members are kept per source rather than deduplicated by name, so a
// regression in the claim index shows up here rather than in a bug report.
func BenchmarkResolveExecutableCollectionMembers(b *testing.B) {
	for _, c := range []struct {
		name          string
		registries    int
		perCollection int
	}{
		{"2reg_25exec", 2, 25},
		{"5reg_100exec", 5, 100},
	} {
		b.Run(c.name, func(b *testing.B) {
			cfg, catalogs := executableCollectionFixture(c.registries, c.perCollection)
			resolved, errs := Resolve(cfg, catalogs, nil)
			if len(errs) != 0 {
				b.Fatalf("fixture must resolve cleanly, got %v", errs)
			}
			if want := c.registries * c.perCollection * 2; len(resolved) != want {
				b.Fatalf("fixture resolved %d artifacts, want %d", len(resolved), want)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = Resolve(cfg, catalogs, nil)
			}
		})
	}
}

// BenchmarkRefuseAmbiguousExecutables isolates the refusal pass from the rest of
// Resolve so a regression in the grouping is attributable.
func BenchmarkRefuseAmbiguousExecutables(b *testing.B) {
	for _, c := range []struct {
		name      string
		contested bool
	}{
		{"unique", false},
		{"contested", true},
	} {
		b.Run(c.name, func(b *testing.B) {
			cfg, catalogs := executableResolveFixture(5, 100, c.contested)
			resolved, _ := Resolve(cfg, catalogs, nil)
			if c.contested {
				// A contested run resolves nothing, so rebuild the pre-refusal
				// input the pass actually sees.
				occurrences, _ := buildOccurrences(cfg, catalogs, nil, newMalformedIndex(nil), nil)
				winners, _ := pickWinners(occurrences)
				resolved = make([]ResolvedArtifact, 0, len(winners))
				for _, winner := range winners {
					resolved = append(resolved, winner.resolved)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = refuseAmbiguousExecutables(resolved)
			}
		})
	}
}
