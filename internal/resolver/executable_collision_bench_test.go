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
// executable kinds, which is the shape that exercises the per-scope identity
// grouping in refuseAmbiguousExecutables on every sync.
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
