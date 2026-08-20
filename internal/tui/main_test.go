package tui

import "testing"

// seedTestTerminalSize gives a freshly constructed model a supported
// terminal size. NewModel intentionally starts with unknown dimensions
// (below minimum), which gates all interaction, so tests that drive keys,
// mouse, or rendering seed a size first.
func seedTestTerminalSize(m *Model) { m.width, m.height = 80, 24 }

func modelWithSelectedRow(t testing.TB, r row) Model {
	t.Helper()
	m := NewModel(t.TempDir(), t.TempDir())
	seedTestTerminalSize(&m)
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
