package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func keyPress(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

func keyNamed(name string) tea.KeyPressMsg {
	switch name {
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEsc}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case " ", "space":
		return tea.KeyPressMsg{Code: tea.KeySpace}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	}
	return tea.KeyPressMsg{Code: rune((name)[0]), Text: string(name)}
}

func newTestModel() Model {
	m := NewModel("/repo", "/home")
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
	m.cursor = 0 // status:enabled section header

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
	if !m.currentFoldState()["status:enabled"] {
		t.Fatalf("za on status:enabled should collapse it")
	}
}

func TestZMprefixCollapsesAll(t *testing.T) {
	m := newTestModel()
	mi, _ := m.Update(keyPress('z'))
	m = mi.(Model)
	mi, _ = m.Update(keyPress('M'))
	m = mi.(Model)

	fs := m.currentFoldState()
	if !fs["status:enabled"] {
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
	m.cursor = 0 // status:enabled

	mi, _ := m.Update(keyPress('h'))
	m = mi.(Model)
	if !m.currentFoldState()["status:enabled"] {
		t.Fatalf("h should collapse status:enabled")
	}

	mi, _ = m.Update(keyPress('l'))
	m = mi.(Model)
	if m.currentFoldState()["status:enabled"] {
		t.Fatalf("l should expand status:enabled")
	}
}

func TestEnterOnSectionHeaderTogglesFold(t *testing.T) {
	m := newTestModel()
	m.cursor = 0 // status:enabled

	mi, _ := m.Update(keyNamed("enter"))
	m = mi.(Model)
	if !m.currentFoldState()["status:enabled"] {
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
	if !m.currentFoldState()["status:enabled"] {
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
