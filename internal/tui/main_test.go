package tui

import "testing"

func modelWithSelectedRow(t testing.TB, r row) Model {
	t.Helper()
	m := NewModel(t.TempDir(), t.TempDir())
	m.loading = false
	m.rows = []row{r}
	m.filteredRows = m.rows
	m.rebuildFlatItems()
	for i := range m.flatItems {
		if m.flatItems[i].Kind == flatArtifact {
			m.cursor = i
			break
		}
	}
	return m
}
