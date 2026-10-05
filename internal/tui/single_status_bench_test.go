package tui

import "testing"

func BenchmarkSingleStatusRender(b *testing.B) {
	for _, size := range []struct {
		name          string
		width, height int
	}{{"normal", 120, 24}, {"minimum", 50, 12}} {
		b.Run(size.name, func(b *testing.B) {
			m := NewModel("/repo", "/home")
			m.width, m.height, m.loading = size.width, size.height, false
			m.rows = buildBenchRows(500)
			m.flatItems = buildStatusTree(m.rows, nil)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = renderList(m, size.height-2)
			}
		})
	}
}
