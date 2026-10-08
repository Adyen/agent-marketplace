package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
)

func searchFoldRows() []row {
	return []row{
		mkRow("deploy-alpha", withGlobal, withRegistry("sec.git", "security")),
		mkRow("deploy-beta", withGlobal, withRegistry("sec.git", "security")),
		mkRow("deploy-remote", withAvailable, withRegistry("team.git", "team")),
		mkRow("unrelated-x", withGlobal, withRegistry("infra.git", "infra")),
	}
}

func searchChildrenOf(path string) []string {
	if path == "status:enabled" {
		return []string{"deploy-alpha", "deploy-beta"}
	}
	return []string{"deploy-remote"}
}

func newSearchFoldModel(t *testing.T) Model {
	t.Helper()
	m := newTestModel()
	m.rows = searchFoldRows()
	m.filteredRows = m.rows
	m.rebuildFlatItems()
	m = pressKey(t, m, v2KeyPress('/'))
	m = asModel(t, mustUpdate(m.Update(tea.PasteMsg{Content: "deploy"})))
	m = pressKey(t, m, v2Enter())
	if m.filter != "deploy" || m.filterActive || len(m.filteredRows) != 3 {
		t.Fatalf("precondition: filter=%q active=%t rows=%d", m.filter, m.filterActive, len(m.filteredRows))
	}
	return m
}

func pressKey(t *testing.T, m Model, msg tea.KeyPressMsg) Model {
	t.Helper()
	return asModel(t, mustUpdate(m.Update(msg)))
}

func selectSearchSection(t *testing.T, m *Model, path string) {
	t.Helper()
	for i, it := range m.flatItems {
		if it.Kind == flatSection && it.SectionPath == path {
			m.cursor = i
			return
		}
	}
	t.Fatalf("section %s not found in flatItems", path)
}

func searchChildVisible(m Model, name string) bool {
	for _, it := range m.flatItems {
		if it.Kind == flatArtifact && it.Row.Artifact.Name == name {
			return true
		}
	}
	return false
}

func assertSearchFoldInvariant(t *testing.T, m Model, wantRows int) {
	t.Helper()
	if m.filter != "deploy" {
		t.Fatalf("filter changed to %q", m.filter)
	}
	if len(m.filteredRows) != wantRows {
		t.Fatalf("match count changed to %d, want %d", len(m.filteredRows), wantRows)
	}
	if m.pane != paneList {
		t.Fatalf("pane = %v, want list", m.pane)
	}
	for _, collapsed := range m.foldedSections {
		if collapsed {
			t.Fatalf("browsing fold state mutated during search: %v", m.foldedSections)
		}
	}
}

func assertSectionCollapsed(t *testing.T, m Model, path string) {
	t.Helper()
	found := false
	for _, it := range m.flatItems {
		if it.Kind == flatSection && it.SectionPath == path {
			found = true
			if !it.Collapsed {
				t.Fatalf("%s header should report Collapsed", path)
			}
		}
	}
	if !found {
		t.Fatalf("collapsed section %s header should still be listed", path)
	}
	for _, name := range searchChildrenOf(path) {
		if searchChildVisible(m, name) {
			t.Fatalf("collapsed %s should hide %s", path, name)
		}
	}
}

func assertSectionExpanded(t *testing.T, m Model, path string) {
	t.Helper()
	for _, it := range m.flatItems {
		if it.SectionPath == path && it.Collapsed {
			t.Fatalf("%s header should not report Collapsed", path)
		}
	}
	for _, name := range searchChildrenOf(path) {
		if !searchChildVisible(m, name) {
			t.Fatalf("expanded %s should show %s", path, name)
		}
	}
}

// spec: tui-navigation/fold-a-category-in-applied-search
func TestSearchFoldControls(t *testing.T) {
	tests := []struct {
		name   string
		target string
		fold   []tea.KeyPressMsg
		unfold []tea.KeyPressMsg
		folded []string
	}{
		{"enter toggles twice", "status:enabled",
			[]tea.KeyPressMsg{keyNamed("enter")}, []tea.KeyPressMsg{keyNamed("enter")},
			[]string{"status:enabled"}},
		{"space toggles twice", "status:available",
			[]tea.KeyPressMsg{keyNamed("space")}, []tea.KeyPressMsg{keyNamed("space")},
			[]string{"status:available"}},
		{"za toggles twice", "status:enabled",
			[]tea.KeyPressMsg{keyPress('z'), keyPress('a')}, []tea.KeyPressMsg{keyPress('z'), keyPress('a')},
			[]string{"status:enabled"}},
		{"h then l", "status:available",
			[]tea.KeyPressMsg{keyPress('h')}, []tea.KeyPressMsg{keyPress('l')},
			[]string{"status:available"}},
		{"left then right", "status:enabled",
			[]tea.KeyPressMsg{keyNamed("left")}, []tea.KeyPressMsg{keyNamed("right")},
			[]string{"status:enabled"}},
		{"zM collapses all then zR expands all", "status:enabled",
			[]tea.KeyPressMsg{keyPress('z'), keyPress('M')}, []tea.KeyPressMsg{keyPress('z'), keyPress('R')},
			[]string{"status:enabled", "status:available"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := newSearchFoldModel(t)
			selectSearchSection(t, &m, tc.target)
			for _, msg := range tc.fold {
				m = pressKey(t, m, msg)
			}
			for _, path := range tc.folded {
				assertSectionCollapsed(t, m, path)
			}
			if len(tc.folded) == 1 {
				if it := m.flatItems[m.cursor]; it.Kind != flatSection || it.SectionPath != tc.folded[0] {
					t.Fatalf("header-local fold moved selection off %s", tc.folded[0])
				}
			}
			assertSearchFoldInvariant(t, m, 3)
			for _, msg := range tc.unfold {
				m = pressKey(t, m, msg)
			}
			for _, path := range tc.folded {
				assertSectionExpanded(t, m, path)
			}
			assertSearchFoldInvariant(t, m, 3)
		})
	}

	t.Run("h on a matching resource folds its section and selects the header", func(t *testing.T) {
		m := newSearchFoldModel(t)
		for i, it := range m.flatItems {
			if it.Kind == flatArtifact && it.Row.Artifact.Name == "deploy-alpha" {
				m.cursor = i
			}
		}
		m = pressKey(t, m, keyPress('h'))
		assertSectionCollapsed(t, m, "status:enabled")
		if it := m.flatItems[m.cursor]; it.Kind != flatSection || it.SectionPath != "status:enabled" {
			t.Fatalf("h on a row should select its section header, got %+v", it)
		}
		assertSearchFoldInvariant(t, m, 3)
	})
}

// spec: tui-navigation/change-query-reveals-previously-folded-matches
func TestSearchFoldQueryLifecycle(t *testing.T) {
	m := newTestModel()
	m.rows = searchFoldRows()
	m.filteredRows = m.rows
	m.foldedSections["status:enabled"] = true
	m.rebuildFlatItems()
	if searchChildVisible(m, "deploy-alpha") {
		t.Fatalf("precondition: browsing fold should hide deploy-alpha")
	}

	m = pressKey(t, m, v2KeyPress('/'))
	m = asModel(t, mustUpdate(m.Update(tea.PasteMsg{Content: "deploy"})))
	m = pressKey(t, m, v2Enter())
	assertSectionExpanded(t, m, "status:enabled")
	assertSectionExpanded(t, m, "status:available")

	selectSearchSection(t, &m, "status:available")
	m = pressKey(t, m, keyNamed("enter"))
	assertSectionCollapsed(t, m, "status:available")

	m = pressKey(t, m, v2KeyPress('/'))
	m = pressKey(t, m, keyNamed("left"))
	m = pressKey(t, m, v2Enter())
	assertSectionCollapsed(t, m, "status:available")
	assertSectionExpanded(t, m, "status:enabled")

	m = pressKey(t, m, v2KeyPress('/'))
	m = pressKey(t, m, keyNamed("backspace"))
	m = pressKey(t, m, v2Enter())
	if m.filter != "deplo" {
		t.Fatalf("filter = %q, want deplo", m.filter)
	}
	assertSectionExpanded(t, m, "status:available")

	selectSearchSection(t, &m, "status:available")
	m = pressKey(t, m, keyNamed("enter"))
	m = pressKey(t, m, keyPress('z'))
	m = pressKey(t, m, keyPress('R'))
	assertSectionExpanded(t, m, "status:available")
	m = pressKey(t, m, keyNamed("esc"))
	if m.filter != "" {
		t.Fatalf("filter = %q, want cleared", m.filter)
	}
	assertSectionCollapsed(t, m, "status:enabled")
	assertSectionExpanded(t, m, "status:available")
}

// spec: tui-navigation/refresh-retains-search-folds-and-header-selection
func TestSearchFoldRefresh(t *testing.T) {
	snapshot := func(t *testing.T, m Model, extra row) Model {
		t.Helper()
		return asModel(t, mustUpdate(m.Update(snapshotLoadedMsg{
			generation: m.requests.active(requestSnapshot),
			rows:       append(searchFoldRows(), extra),
		})))
	}
	discovery := func(t *testing.T, m Model) Model {
		t.Helper()
		m.authoritativeRows = m.rows
		generation := m.beginRequest(requestDiscovery)
		outcome := marketplace.DiscoveryOutcome{
			Endpoint: config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market", Alias: "public"},
			Items:    []mpapi.ResourceListItem{{RegistryID: "registry", Kind: "skill", Name: "deploy-extra", Availability: mpapi.AvailabilityAvailable}},
			Total:    1,
		}
		return asModel(t, mustUpdate(m.Update(discoveryLoadedMsg{generation: generation, query: "deploy", outcomes: []marketplace.DiscoveryOutcome{outcome}, done: true})))
	}
	tests := []struct {
		name         string
		folded       string
		refresh      func(t *testing.T, m Model) Model
		visibleExtra bool
	}{
		{"snapshot keeps first enabled header folded and selected", "status:enabled",
			func(t *testing.T, m Model) Model {
				return snapshot(t, m, mkRow("deploy-extra", withAvailable, withRegistry("team.git", "team")))
			}, true},
		{"snapshot keeps later available header folded and selected", "status:available",
			func(t *testing.T, m Model) Model {
				return snapshot(t, m, mkRow("deploy-extra", withGlobal, withRegistry("sec.git", "security")))
			}, true},
		{"discovery keeps first enabled header folded and selected", "status:enabled",
			discovery, true},
		{"discovery keeps later available header folded and selected", "status:available",
			discovery, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := newSearchFoldModel(t)
			selectSearchSection(t, &m, tc.folded)
			m = pressKey(t, m, keyNamed("enter"))
			selected := m.selectionKey()
			m = tc.refresh(t, m)
			assertSearchFoldInvariant(t, m, 4)
			if got := m.selectionKey(); got != selected {
				t.Fatalf("refresh moved selection from %q to %q", selected, got)
			}
			if it := m.flatItems[m.cursor]; it.Kind != flatSection || it.SectionPath != tc.folded {
				t.Fatalf("refresh should keep %s selected, got %+v", tc.folded, it)
			}
			assertSectionCollapsed(t, m, tc.folded)
			if tc.folded == "status:enabled" {
				assertSectionExpanded(t, m, "status:available")
			} else {
				assertSectionExpanded(t, m, "status:enabled")
			}
			if tc.visibleExtra && !searchChildVisible(m, "deploy-extra") {
				t.Fatalf("refresh should show new matching row deploy-extra in the expanded section")
			}
		})
	}
}

// spec: tui-navigation/empty-search-results-tolerate-folding
func TestSearchFoldEmptyResults(t *testing.T) {
	m := newTestModel()
	m.rows = searchFoldRows()
	m.filteredRows = m.rows
	m.foldedSections["status:available"] = true
	m.rebuildFlatItems()

	m = pressKey(t, m, v2KeyPress('/'))
	m = asModel(t, mustUpdate(m.Update(tea.PasteMsg{Content: "zzz"})))
	m = pressKey(t, m, v2Enter())
	if len(m.flatItems) != 0 {
		t.Fatalf("precondition: no-match query should list no items, got %d", len(m.flatItems))
	}

	for _, msg := range []tea.KeyPressMsg{
		keyNamed("enter"), keyNamed("space"), keyPress('h'), keyPress('l'),
		keyPress('z'), keyPress('a'), keyPress('z'), keyPress('M'), keyPress('z'), keyPress('R'),
	} {
		m = pressKey(t, m, msg)
	}
	if m.pane != paneList || m.cursor != 0 || len(m.flatItems) != 0 {
		t.Fatalf("fold controls on empty results: pane=%v cursor=%d items=%d", m.pane, m.cursor, len(m.flatItems))
	}
	if !m.foldedSections["status:available"] {
		t.Fatalf("empty search folds must not touch browsing fold state")
	}

	m = pressKey(t, m, v2KeyPress('/'))
	for i := 0; i < 3; i++ {
		m = pressKey(t, m, keyNamed("backspace"))
	}
	m = asModel(t, mustUpdate(m.Update(tea.PasteMsg{Content: "deploy"})))
	m = pressKey(t, m, v2Enter())
	assertSectionExpanded(t, m, "status:enabled")
	assertSectionExpanded(t, m, "status:available")

	m = pressKey(t, m, keyNamed("esc"))
	assertSectionExpanded(t, m, "status:enabled")
	assertSectionCollapsed(t, m, "status:available")
}
