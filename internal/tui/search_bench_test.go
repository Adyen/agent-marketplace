package tui

import (
	"fmt"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

// buildBenchRows returns n synthetic rows spread across several registries,
// each row carrying realistic name/tag/URL strings so applyFilter has
// representative work to do per keystroke.
func buildBenchRows(n int) []row {
	registries := []struct{ url, name string }{
		{"git@example.com:security.git", "security"},
		{"git@example.com:infra.git", "infra"},
		{"git@example.com:platform.git", "platform"},
		{"git@example.com:team-frontend.git", "team-frontend"},
		{"git@example.com:team-backend.git", "team-backend"},
	}
	tagPools := [][]string{
		{"security", "audit", "compliance"},
		{"k8s", "helm", "infra"},
		{"platform", "observability", "metrics"},
		{"frontend", "react", "a11y"},
		{"backend", "go", "grpc"},
	}

	rows := make([]row, 0, n)
	for i := 0; i < n; i++ {
		reg := registries[i%len(registries)]
		tags := tagPools[i%len(tagPools)]
		name := fmt.Sprintf("skill-%s-%04d", reg.name, i)
		rows = append(rows, row{
			Artifact: resolver.ResolvedArtifact{
				Entry: catalog.Entry{
					Name: name,
					Tags: tags,
					Path: "/cache/" + name,
				},
				RegistryURL: reg.url,
			},
			RegistryURL:    reg.url,
			RegistryName:   reg.name,
			EnabledProject: i%3 == 0,
			IsAvailable:    i%3 != 0,
		})
	}
	return rows
}

// BenchmarkApplyFilter measures the per-keystroke cost of filtering the row
// list.
func BenchmarkApplyFilter(b *testing.B) {
	rows := buildBenchRows(500)

	cases := []struct {
		name   string
		filter string
	}{
		{"no_match", "zzz-nomatch"},
		{"partial_name", "security"},
		{"partial_tag", "helm"},
		{"match_all", "skill"},
	}

	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			m := NewModel("/repo", "/home")
			m.rows = append([]row(nil), rows...)
			m.filter = c.filter
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = m.applyFilter()
			}
		})
	}
}

func BenchmarkFuzzyRankedView(b *testing.B) {
	rows := buildBenchRows(500)

	cases := []struct {
		name   string
		filter string
	}{
		{"broad_match", "skill"},
		{"narrow_match", "helm"},
	}

	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			m := NewModel("/repo", "/home")
			m.rows = append([]row(nil), rows...)
			m.filter = c.filter
			m.recomputeView()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				m.recomputeView()
			}
		})
	}
}

// BenchmarkApplyFilterColdAndWarm covers cold and warm searchable cache,
// no-match, tag-match, registry-match, and broad-match cases.
func BenchmarkApplyFilterColdAndWarm(b *testing.B) {
	cases := []struct {
		name    string
		n       int
		filter  string
		preWarm bool
	}{
		{"cold_no_match_1000", 1000, "zzz-nomatch", false},
		{"cold_broad_match_1000", 1000, "skill", false},
		{"cold_tag_match_1000", 1000, "k8s", false},
		{"cold_registry_match_1000", 1000, "infra", false},
		{"warm_no_match_1000", 1000, "zzz-nomatch", true},
		{"warm_broad_match_1000", 1000, "skill", true},
		{"warm_tag_match_1000", 1000, "k8s", true},
	}

	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			rows := buildBenchRows(c.n)
			m := NewModel("/repo", "/home")
			m.rows = append([]row(nil), rows...)

			if c.preWarm {
				// Pre-warm the searchable cache by running applyFilter once.
				m.filter = "warmup"
				_ = m.applyFilter()
			}

			m.filter = c.filter
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if !c.preWarm {
					b.StopTimer()
					for j := range m.rows {
						m.rows[j].searchableLower = ""
					}
					b.StartTimer()
				}
				_ = m.applyFilter()
			}
		})
	}
}
