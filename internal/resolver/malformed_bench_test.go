package resolver

import (
	"errors"
	"fmt"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/skill"
)

// BenchmarkResolveMalformedCorrelation isolates the work WithMalformed adds to
// the resolver hot path. The existing BenchmarkResolve* cases cannot see it:
// they pass no malformed set and every selection hits the catalog, so neither
// the miss branch nor skill.MatchMalformed is ever reached.
//
// Sub-benchmarks:
//
//	none    — no malformed set at all (the overwhelmingly common case)
//	absent  — a malformed set present, every miss a genuine absence, so each
//	          miss pays a full scan of the registry's malformed entries
//	matched — every miss correlates on the first candidate
func BenchmarkResolveMalformedCorrelation(b *testing.B) {
	benchmarkResolveMalformedCorrelation(b, 10, 50, 10, 20)
}

// BenchmarkResolveMalformedCorrelationWorstCase is the pathological-but-real
// shape: a large org config (30 registry entries), a stale-heavy config (50
// unresolved selections per entry) and a badly broken registry (200 dropped
// resources per entry). Correlation is O(misses × malformed) per registry, so
// this is 300k candidate comparisons per Resolve — the point at which an
// allocation per comparison would be unacceptable.
func BenchmarkResolveMalformedCorrelationWorstCase(b *testing.B) {
	benchmarkResolveMalformedCorrelation(b, 30, 50, 50, 200)
}

func benchmarkResolveMalformedCorrelation(b *testing.B, registries, perReg, misses, malPerReg int) {
	build := func(missName func(r, i int) string) (*config.Config, map[string][]catalog.Entry, map[string][]skill.MalformedSkill) {
		cfg := &config.Config{}
		catalogs := map[string][]catalog.Entry{}
		malformed := map[string][]skill.MalformedSkill{}
		for r := 0; r < registries; r++ {
			url := fmt.Sprintf("git@example.com:reg-%02d.git", r)
			ref := "main"
			selections := make([]string, 0, perReg+misses)
			entries := make([]catalog.Entry, 0, perReg)
			for s := 0; s < perReg; s++ {
				name := fmt.Sprintf("reg%02d-skill-%03d", r, s)
				entries = append(entries, catalog.Entry{Name: name, Path: "/cache/" + name})
				selections = append(selections, name)
			}
			for m := 0; m < misses; m++ {
				selections = append(selections, missName(r, m))
			}
			cfg.Registries = append(cfg.Registries, config.RegistryEntry{
				URL: url, Ref: ref, Source: config.RepoCommitted,
				Skills: config.KindSelector{Include: selections},
			})
			key := cache.CatalogKeyForEntry(url, ref, "")
			catalogs[key] = entries

			broken := make([]skill.MalformedSkill, 0, malPerReg)
			for m := 0; m < malPerReg; m++ {
				name := fmt.Sprintf("reg%02d-broken-%03d", r, m)
				broken = append(broken, skill.MalformedSkill{
					DirName: name,
					Path:    "/cache/skills/" + name,
					Error:   errors.New("yaml: did not find expected ',' or ']'"),
				})
			}
			malformed[key] = broken
		}
		setCatalogKeys(cfg)
		return cfg, catalogs, malformed
	}

	absentName := func(r, i int) string { return fmt.Sprintf("reg%02d-gone-%03d", r, i) }
	brokenName := func(r, i int) string { return fmt.Sprintf("reg%02d-broken-%03d", r, i) }

	b.Run("none", func(b *testing.B) {
		cfg, catalogs, _ := build(absentName)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = Resolve(cfg, catalogs, nil)
		}
	})

	b.Run("absent", func(b *testing.B) {
		cfg, catalogs, malformed := build(absentName)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = Resolve(cfg, catalogs, nil, WithMalformed(malformed))
		}
	})

	b.Run("matched", func(b *testing.B) {
		cfg, catalogs, malformed := build(brokenName)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = Resolve(cfg, catalogs, nil, WithMalformed(malformed))
		}
	})
}
