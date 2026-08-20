package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func keyPress(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func keyNamed(name string) tea.KeyMsg {
	switch name {
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case " ", "space":
		return tea.KeyMsg{Type: tea.KeySpace}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)}
}

func newTestModel() Model {
	m := NewModel("/repo", "/home")
	// The model starts with unknown dimensions (below minimum) by design;
	// interaction tests seed a supported size explicitly.
	seedTestTerminalSize(&m)
	m.rows = []row{
		mkRow("a", withGlobal, withRegistry("sec.git", "security")),
		mkRow("b", withProject, withRegistry("infra.git", "infra")),
	}
	m.filteredRows = m.rows
	m.rebuildFlatItems()
	return m
}

func TestZAprefixTogglesFoldOnCurrentSection(t *testing.T) {
	m := newTestModel()
	m.cursor = 0 // scope:global section header

	// Press 'z' then 'a' → toggle current section fold.
	mi, _ := m.Update(keyPress('z'))
	m = mi.(Model)
	if !m.zPending {
		t.Fatalf("expected zPending after pressing z")
	}

	mi, _ = m.Update(keyPress('a'))
	m = mi.(Model)
	if m.zPending {
		t.Fatalf("expected zPending cleared after completing za")
	}
	if !m.currentFoldState()["scope:global"] {
		t.Fatalf("za on scope:global should collapse it")
	}
}

func TestZMprefixCollapsesAll(t *testing.T) {
	m := newTestModel()
	mi, _ := m.Update(keyPress('z'))
	m = mi.(Model)
	mi, _ = m.Update(keyPress('M'))
	m = mi.(Model)

	fs := m.currentFoldState()
	if !fs["scope:global"] || !fs["scope:project"] {
		t.Fatalf("zM should collapse all top-level sections, got %v", fs)
	}
}

func TestZRprefixExpandsAll(t *testing.T) {
	m := newTestModel()
	// First collapse everything.
	m.collapseAllSections()
	m.rebuildFlatItems()

	mi, _ := m.Update(keyPress('z'))
	m = mi.(Model)
	mi, _ = m.Update(keyPress('R'))
	m = mi.(Model)

	fs := m.currentFoldState()
	for path, collapsed := range fs {
		if collapsed {
			t.Fatalf("zR should expand all sections, %s still collapsed", path)
		}
	}
}

func TestHandLCollapseAndExpandCurrentSection(t *testing.T) {
	m := newTestModel()
	m.cursor = 0 // scope:global

	mi, _ := m.Update(keyPress('h'))
	m = mi.(Model)
	if !m.currentFoldState()["scope:global"] {
		t.Fatalf("h should collapse scope:global")
	}

	mi, _ = m.Update(keyPress('l'))
	m = mi.(Model)
	if m.currentFoldState()["scope:global"] {
		t.Fatalf("l should expand scope:global")
	}
}

func TestEnterOnSectionHeaderTogglesFold(t *testing.T) {
	m := newTestModel()
	m.cursor = 0 // scope:global

	mi, _ := m.Update(keyNamed("enter"))
	m = mi.(Model)
	if !m.currentFoldState()["scope:global"] {
		t.Fatalf("enter on section header should collapse it")
	}
	if m.pane != paneList {
		t.Fatalf("enter on section header should stay on list pane, got %v", m.pane)
	}
}

func TestSpaceOnSectionHeaderTogglesFold(t *testing.T) {
	m := newTestModel()
	m.cursor = 0

	mi, _ := m.Update(keyNamed("space"))
	m = mi.(Model)
	if !m.currentFoldState()["scope:global"] {
		t.Fatalf("space on section header should toggle fold")
	}
}

func TestZPrefixCancelsOnNonMatchingKey(t *testing.T) {
	m := newTestModel()
	mi, _ := m.Update(keyPress('z'))
	m = mi.(Model)
	if !m.zPending {
		t.Fatalf("expected zPending after z")
	}
	// Press 'x' — unrelated; zPending should clear without side effects.
	mi, _ = m.Update(keyPress('x'))
	m = mi.(Model)
	if m.zPending {
		t.Fatalf("zPending should clear after a non-matching key")
	}
	if len(m.currentFoldState()) != 0 {
		t.Fatalf("fold state should not change from z+x, got %v", m.currentFoldState())
	}
}
