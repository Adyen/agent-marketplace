package tui

import (
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func BenchmarkViewGroupings(b *testing.B) {
	b.Setenv("XDG_STATE_HOME", b.TempDir())
	rows := buildBenchRows(500)
	kinds := []ir.ResourceKind{ir.KindSkill, ir.KindCommand, ir.KindSubagent, ir.KindHook, ir.KindMcpServer, ir.KindCollection, ir.KindRule}
	for i := range rows {
		rows[i].Artifact.Kind = kinds[i%len(kinds)]
		rows[i].RegistryPath = []string{"frontend", "backend", "platform"}[i%3]
	}
	for _, c := range []struct {
		name string
		view viewSettings
	}{
		{"status", viewSettings{GroupBy: groupingAxis("status"), ThenBy: groupingAxis("none")}},
		{"source_kind", viewSettings{GroupBy: groupingAxis("source"), ThenBy: groupingAxis("kind")}},
		{"kind_source", viewSettings{GroupBy: groupingAxis("kind"), ThenBy: groupingAxis("source")}},
	} {
		b.Run(c.name, func(b *testing.B) {
			m := NewModel("/repo", "/home")
			m.rows = append([]row(nil), rows...)
			m.viewSettings = c.view
			m.recomputeView()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				m.recomputeView()
			}
		})
	}
}
