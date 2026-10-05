package tui

import (
	"fmt"
	"testing"
)

// paletteBenchModel builds a realistic loaded list for palette hot paths.
func paletteBenchModel() Model {
	m := newTestModel()
	m.loading = false
	rows := make([]row, 0, 200)
	for i := 0; i < 200; i++ {
		rows = append(rows, mkRow(fmt.Sprintf("bench-resource-%03d", i),
			withGlobal, withRegistry("bench.git", "bench")))
	}
	m.rows = rows
	m.recomputeView()
	m.width = 120
	m.height = 40
	return m
}

func BenchmarkPaletteFilter(b *testing.B) {
	m := paletteBenchModel()
	m.openPalette()
	m.palette.input.SetValue("syn")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.recomputePaletteMatches()
	}
}

func BenchmarkPaletteEligibility(b *testing.B) {
	m := paletteBenchModel()
	actions := paletteActions()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, a := range actions {
			_ = m.actionEligible(a.id)
		}
	}
}

func BenchmarkPaletteRender(b *testing.B) {
	m := paletteBenchModel()
	m.openPalette()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.viewContent()
	}
}
