package tui

import (
	"fmt"
	"strings"
	"testing"
)

// BenchmarkSourceToggleAtCursor measures resolving the x key against 2000
// rows from 20 sources, on a resource row and a filtered single-source header.
func BenchmarkSourceToggleAtCursor(b *testing.B) {
	rows := make([]row, 0, 2000)
	for source := 0; source < 20; source++ {
		url := fmt.Sprintf("/repo/plugins/source-%02d", source)
		name := fmt.Sprintf("source-%02d", source)
		for resource := 0; resource < 100; resource++ {
			rows = append(rows, mkRow(fmt.Sprintf("resource-%02d-%03d", source, resource), withProject, withRegistry(url, name)))
		}
	}
	m := sourceToggleTestModel(rows)
	for _, c := range []struct {
		name   string
		cursor func(testing.TB, *Model)
	}{
		{"resource_row", func(b testing.TB, m *Model) { cursorOnRow(b, m, "resource-10-050") }},
		{"group_header", func(b testing.TB, m *Model) {
			m.filteredRows = rows[1000:1100]
			m.rebuildFlatItems()
			cursorOnSection(b, m, "Enabled")
		}},
	} {
		b.Run(c.name, func(b *testing.B) {
			c.cursor(b, &m)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if toggle, refusal := m.sourceToggleAtCursor(); refusal != "" || toggle.resources != 100 {
					b.Fatalf("toggle = %+v, refusal %q", toggle, refusal)
				}
			}
		})
	}
}

func BenchmarkGroupToggleFor(b *testing.B) {
	rows := make([]row, 0, 2000)
	for source := 0; source < 20; source++ {
		url := fmt.Sprintf("/repo/plugins/source-%02d", source)
		name := fmt.Sprintf("source-%02d", source)
		for resource := 0; resource < 100; resource++ {
			rows = append(rows, mkRow(fmt.Sprintf("resource-%02d-%03d", source, resource), withProject, withRegistry(url, name)))
		}
	}
	m := sourceToggleTestModel(rows)
	target := rows[1050]
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		toggle, refusal := m.groupToggleFor(target, scopeProject)
		if refusal != "" || toggle.enable || toggle.resources != 100 {
			b.Fatalf("toggle = %+v, refusal %q", toggle, refusal)
		}
	}
}

func BenchmarkOpenGroupToggleModal(b *testing.B) {
	rows := make([]row, 2000)
	for i := range rows {
		rows[i] = mkRow(fmt.Sprintf("resource-%04d", i), withProject,
			withRegistry(fmt.Sprintf("/repo/plugins/source-%02d", i/100), fmt.Sprintf("source-%02d", i/100)))
	}
	m := sourceToggleTestModel(rows)
	cursorOnRow(b, &m, "resource-1050")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if refusal := m.openGroupToggleModal(); refusal != "" {
			b.Fatal(refusal)
		}
		m.pane = paneList
	}
}

func BenchmarkRenderGroupToggleModal(b *testing.B) {
	rows := make([]row, 0, 2000)
	for source := 0; source < 20; source++ {
		url := fmt.Sprintf("/repo/plugins/source-%02d", source)
		name := fmt.Sprintf("source-%02d", source)
		for resource := 0; resource < 100; resource++ {
			rows = append(rows, mkRow(fmt.Sprintf("resource-%02d-%03d", source, resource), withProject, withRegistry(url, name)))
		}
	}
	m := sourceToggleTestModel(rows)
	m.modalSource = rows[1050]
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		output := renderGroupToggleModal(m)
		if !strings.Contains(output, `Source "source-10"`) {
			b.Fatal("group modal omitted source title")
		}
	}
}
