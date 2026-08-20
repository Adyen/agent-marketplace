package tui

import (
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// Selection identity is the resource (or section), not the numeric cursor
// index: a snapshot rebuild that reorders or inserts rows must keep the
// same resource selected, and the open detail view must keep showing the
// same resource across rebuilds and resizes.

func identityTestModel(t *testing.T, names ...string) Model {
	t.Helper()
	m := NewModel(t.TempDir(), t.TempDir())
	m.loading = false
	m.driftAssessment = driftReady
	m.width, m.height = 100, 30
	var rows []row
	for _, name := range names {
		rows = append(rows, mkRow(name, withGlobal, withRegistry("registry.git", "registry")))
	}
	m.rows = rows
	m.filteredRows = rows
	m.rebuildFlatItems()
	return m
}

func selectRowByName(t *testing.T, m Model, name string) Model {
	t.Helper()
	for i, item := range m.flatItems {
		if item.Kind == flatArtifact && item.Row.Artifact.Name == name {
			m.cursor = i
			return m
		}
	}
	t.Fatalf("row %q not found in flat items", name)
	return m
}

func selectedRowName(t *testing.T, m Model) string {
	t.Helper()
	r, ok := m.selectedRow()
	if !ok {
		t.Fatalf("cursor does not select a row (cursor=%d items=%d)", m.cursor, len(m.flatItems))
	}
	return r.Artifact.Name
}

func TestSnapshotRebuildPreservesSelectedResourceIdentity(t *testing.T) {
	m := identityTestModel(t, "alpha", "bravo", "charlie")
	m = selectRowByName(t, m, "bravo")

	// An accepted snapshot that inserts a row before the selection must not
	// shift the selection onto a different resource.
	rebuilt := identityTestModel(t, "inserted", "alpha", "bravo", "charlie")
	generation := m.beginRequest(requestSnapshot)
	updated, _ := m.Update(snapshotLoadedMsg{
		generation: generation,
		rows:       rebuilt.rows,
		mergedCfg:  &config.Config{},
	})
	m = updated.(Model)
	if got := selectedRowName(t, m); got != "bravo" {
		t.Fatalf("snapshot rebuild moved the selection from bravo to %q", got)
	}
}

func TestSnapshotRebuildPreservesSelectedSectionIdentity(t *testing.T) {
	m := identityTestModel(t, "alpha", "bravo")
	// Put the cursor on the scope:global section header (index 0).
	m.cursor = 0
	if _, ok := m.selectedSectionPath(); !ok {
		t.Fatal("fixture cursor is not on a section header")
	}

	generation := m.beginRequest(requestSnapshot)
	updated, _ := m.Update(snapshotLoadedMsg{
		generation: generation,
		rows: []row{
			mkRow("orphan", withAvailable),
			mkRow("alpha", withGlobal, withRegistry("registry.git", "registry")),
		},
		mergedCfg: &config.Config{},
	})
	m = updated.(Model)
	path, ok := m.selectedSectionPath()
	if !ok || path != "scope:global" {
		t.Fatalf("snapshot rebuild lost the selected section identity: %q ok=%t", path, ok)
	}
}

func TestSnapshotRebuildClampsWhenSelectedResourceDisappears(t *testing.T) {
	m := identityTestModel(t, "alpha", "bravo")
	m = selectRowByName(t, m, "bravo")

	generation := m.beginRequest(requestSnapshot)
	updated, _ := m.Update(snapshotLoadedMsg{
		generation: generation,
		rows:       []row{mkRow("other", withGlobal, withRegistry("registry.git", "registry"))},
		mergedCfg:  &config.Config{},
	})
	m = updated.(Model)
	if m.cursor < 0 || m.cursor >= len(m.flatItems) {
		t.Fatalf("cursor out of range after selected resource disappeared: %d of %d", m.cursor, len(m.flatItems))
	}
	if got := selectedRowName(t, m); got == "bravo" {
		t.Fatal("removed resource is still selected")
	}
}

func TestDetailPaneIdentitySurvivesSnapshotRebuildAndResize(t *testing.T) {
	m := identityTestModel(t, "alpha", "bravo", "charlie")
	m = selectRowByName(t, m, "bravo")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.pane != paneDetail {
		t.Fatalf("enter did not open the detail pane: %v", m.pane)
	}
	detailKey := m.detailKey
	if detailKey == "" {
		t.Fatal("opening the detail pane did not record the open resource identity")
	}

	// Accepted snapshot rebuild with rows reordered and one inserted.
	rebuilt := identityTestModel(t, "inserted", "charlie", "alpha", "bravo")
	generation := m.beginRequest(requestSnapshot)
	updated, _ = m.Update(snapshotLoadedMsg{
		generation: generation,
		rows:       rebuilt.rows,
		mergedCfg:  &config.Config{},
	})
	m = updated.(Model)
	if m.pane != paneDetail {
		t.Fatalf("snapshot rebuild closed the detail pane: %v", m.pane)
	}
	if m.detailKey != detailKey {
		t.Fatalf("snapshot rebuild changed the open detail identity: %q → %q", detailKey, m.detailKey)
	}

	// Resize below minimum and back: the detail workflow must recover with
	// the same resource identity.
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	m = updated.(Model)
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = updated.(Model)
	if m.pane != paneDetail || m.detailKey != detailKey {
		t.Fatalf("resize lost the detail workflow: pane=%v identity=%q", m.pane, m.detailKey)
	}

	// Leaving the detail pane returns to the list with the same resource
	// still selected.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.pane != paneList {
		t.Fatalf("esc did not return to the list: %v", m.pane)
	}
	if got := selectedRowName(t, m); got != "bravo" {
		t.Fatalf("esc from detail selected %q, want bravo", got)
	}
}

func orphanRow(name, path string) row {
	r := row{}
	r.Artifact.Name = name
	r.Artifact.Path = path
	r.DriftItem = &materialize.DriftItem{Name: name, Reason: materialize.DriftOrphaned, Path: path, Target: "factory"}
	return r
}

func TestSnapshotRebuildPreservesOrphanOutputIdentity(t *testing.T) {
	m := identityTestModel(t, "alpha")
	orphan := orphanRow("stale-output", "/repo/.factory/skills/stale-output")
	m.rows = append(m.rows, orphan)
	m.filteredRows = m.rows
	m.rebuildFlatItems()
	m = selectRowByName(t, m, "stale-output")
	if key := m.selectionKey(); key == "" {
		t.Fatal("orphan output row has no stable identity")
	}

	// A second orphan with the same display name but a different output
	// path is a different identity and must not capture the selection.
	reordered := []row{
		orphanRow("stale-output", "/repo/.claude/skills/stale-output"),
		mkRow("alpha", withGlobal, withRegistry("registry.git", "registry")),
		orphan,
	}
	generation := m.beginRequest(requestSnapshot)
	updated, _ := m.Update(snapshotLoadedMsg{generation: generation, rows: reordered, mergedCfg: &config.Config{}})
	m = updated.(Model)
	r, ok := m.selectedRow()
	if !ok || r.Artifact.Path != orphan.Artifact.Path {
		t.Fatalf("rebuild did not restore the exact orphan output identity: %+v", r.Artifact)
	}
}

func TestDetailIdentityFallsBackWhenResourceDisappears(t *testing.T) {
	m := identityTestModel(t, "alpha", "bravo")
	m = selectRowByName(t, m, "bravo")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.pane != paneDetail {
		t.Fatal("fixture: detail pane not open")
	}

	generation := m.beginRequest(requestSnapshot)
	updated, _ = m.Update(snapshotLoadedMsg{
		generation: generation,
		rows:       []row{mkRow("alpha", withGlobal, withRegistry("registry.git", "registry"))},
		mergedCfg:  &config.Config{},
	})
	m = updated.(Model)
	if m.pane != paneDetail {
		t.Fatalf("rebuild closed the detail pane: %v", m.pane)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.cursor < 0 || m.cursor >= len(m.flatItems) {
		t.Fatalf("cursor out of range after the detail resource disappeared: %d of %d", m.cursor, len(m.flatItems))
	}
	if !strings.Contains(ansi.Strip(m.View()), "alpha") {
		t.Fatal("list did not recover after the detail resource disappeared")
	}
}
