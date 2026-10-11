package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Adyen/agent-marketplace/internal/config"
)

// v2KeyPress builds a v2 key press for a printable rune.
func v2KeyPress(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

func v2Space() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeySpace} }
func v2Enter() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyEnter} }

func asModel(t *testing.T, tm tea.Model) Model {
	t.Helper()
	m, ok := tm.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want Model", tm)
	}
	return m
}

// spec: tui-interactions/filtered-folded-descendants-and-skipped-rows
func TestV2SpaceOnSectionHeaderOpensBulkModal(t *testing.T) {
	m := newTestModel()
	m.cursor = 0 // status:enabled section header

	m = asModel(t, mustUpdate(m.Update(v2Space())))
	if m.pane != paneBulkToggle {
		t.Fatalf("v2 Space press on a section header must open the bulk toggle modal, got pane %v", m.pane)
	}
	if m.currentFoldState()["status:enabled"] {
		t.Fatalf("v2 Space on a section header must not fold it")
	}
}

func TestV2SpaceOnRowOpensToggleModal(t *testing.T) {
	m := modelWithSelectedRow(t, mkRow("audit", withGlobal, withRegistry("sec.git", "security")))

	m = asModel(t, mustUpdate(m.Update(v2Space())))
	if m.pane != paneToggleModal {
		t.Fatalf("v2 Space press on a row must open the toggle modal, got pane %v", m.pane)
	}
	if m.modalSkill.Artifact.Name != "audit" {
		t.Fatalf("toggle modal captured %q, want audit", m.modalSkill.Artifact.Name)
	}
}

func TestV2HarnessModalSpaceSelectsWithoutSaving(t *testing.T) {
	m := newTestModel()
	m.globalHarnesses = []string{"claude"}
	m.openHarnessModal()
	if m.pane != paneHarnessModal {
		t.Fatalf("precondition: harness modal not open")
	}

	m.harnessModal.cursor = 0
	for i, opt := range knownHarnessOptions {
		if !m.harnessModal.selected[opt.ID] {
			m.harnessModal.cursor = i
			break
		}
	}
	target := knownHarnessOptions[m.harnessModal.cursor].ID
	m = asModel(t, mustUpdate(m.Update(v2Space())))
	if !m.harnessModal.selected[target] {
		t.Fatalf("v2 Space press must select harness %q", target)
	}
	if m.pane != paneHarnessModal {
		t.Fatalf("Space must keep the harness modal open, got pane %v", m.pane)
	}

	m = asModel(t, mustUpdate(m.Update(v2Enter())))
	if m.pane == paneHarnessModal {
		t.Fatalf("Enter must submit and close the harness modal")
	}
}

// spec: tui-navigation/paste-in-search
func TestV2BracketedPasteReachesFocusedSearch(t *testing.T) {
	m := newTestModel()
	m.rows = []row{
		mkRow("audit", withGlobal, withRegistry("sec.git", "security")),
		mkRow("b", withProject, withRegistry("infra.git", "infra")),
	}
	m.filteredRows = m.rows
	m.rebuildFlatItems()
	m = asModel(t, mustUpdate(m.Update(v2KeyPress('/'))))
	if !m.filterActive {
		t.Fatalf("precondition: search input not focused after /")
	}

	m = asModel(t, mustUpdate(m.Update(tea.PasteMsg{Content: "audit"})))
	if m.filter != "audit" {
		t.Fatalf("bracketed paste must fill the focused search input, got %q", m.filter)
	}
	if len(m.filteredRows) != 1 || m.filteredRows[0].Artifact.Name != "audit" {
		t.Fatalf("pasted query must filter local rows, got %v", m.filteredRows)
	}
}

// spec: tui-navigation/paste-in-search
func TestV2BracketedPasteSchedulesRemoteSearchDebounce(t *testing.T) {
	m := newTestModel()
	m.mergedCfg = &config.Config{Marketplaces: []config.MarketplaceEntry{{URL: "https://market.example", InstanceID: "market"}}}
	m = asModel(t, mustUpdate(m.Update(v2KeyPress('/'))))

	m = asModel(t, mustUpdate(m.Update(tea.PasteMsg{Content: "audit"})))
	if m.requests.active(requestDiscovery) == 0 {
		t.Fatalf("pasted query must begin a debounced remote discovery request")
	}
	if !m.discoveryLoading {
		t.Fatalf("remote search debounce must mark discovery loading")
	}
}

// spec: tui-navigation/paste-outside-search
func TestV2PasteSuspendedInModal(t *testing.T) {
	m := newTestModel()
	m.mergedCfg = &config.Config{Marketplaces: []config.MarketplaceEntry{{URL: "https://market.example", InstanceID: "market"}}}
	m = asModel(t, mustUpdate(m.Update(v2KeyPress('/'))))
	m = asModel(t, mustUpdate(m.Update(tea.PasteMsg{Content: "audit"})))

	m.pane = paneToggleModal
	m = asModel(t, mustUpdate(m.Update(tea.PasteMsg{Content: "q"})))
	if m.filter != "audit" {
		t.Fatalf("paste while a modal is open must be suspended, filter changed to %q", m.filter)
	}
	if m.searchInput.Value() != "audit" {
		t.Fatalf("suspended paste must not reach the search input, got %q", m.searchInput.Value())
	}
}

// spec: tui-navigation/paste-outside-search
func TestV2PasteSuspendedBelowMinimumSize(t *testing.T) {
	m := newTestModel()
	m = asModel(t, mustUpdate(m.Update(v2KeyPress('/'))))
	m.width = minimumTerminalWidth - 1
	m.height = minimumTerminalHeight - 1

	m = asModel(t, mustUpdate(m.Update(tea.PasteMsg{Content: "audit"})))
	if m.filter != "" {
		t.Fatalf("paste below the minimum terminal size must be suspended, got filter %q", m.filter)
	}
}

// spec: tui-navigation/paste-in-search
func TestV2PasteNeverInvokesActionShortcuts(t *testing.T) {
	m := newTestModel()
	m = asModel(t, mustUpdate(m.Update(v2KeyPress('/'))))

	m = asModel(t, mustUpdate(m.Update(tea.PasteMsg{Content: "q"})))
	if m.filter != "q" {
		t.Fatalf("pasted 'q' must be search text, got filter %q", m.filter)
	}

	m = asModel(t, mustUpdate(m.Update(v2Enter()))) // leave search, keep filter
	m = asModel(t, mustUpdate(m.Update(tea.PasteMsg{Content: "sH?"})))
	if m.filter != "q" {
		t.Fatalf("unfocused paste must not alter the filter, got %q", m.filter)
	}
	if m.pane != paneList {
		t.Fatalf("unfocused paste must not open panes, got %v", m.pane)
	}
}

func mustUpdate(tm tea.Model, _ tea.Cmd) tea.Model { return tm }
