package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// AC4: scope switching and Space selection inside the harness Huh
// multiselect never write config; only Enter submits a non-empty
// selection through the guarded save.

func TestHarnessFormSpaceSelectsWithoutWriting(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	configPath := filepath.Join(tmpHome, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte("apiVersion: agent-marketplace/v4\nharnesses:\n  - factory\nsources: {}\nresources: {}\n")
	if err := os.WriteFile(configPath, original, 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewModel("/repo", tmpHome)
	m.globalHarnesses = []string{"factory"}
	m.openHarnessModal()
	if m.harnessModal.form == nil {
		t.Fatal("harness modal must own a Huh form")
	}

	mi, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = mi.(Model)
	if cmd != nil {
		t.Fatal("cursor movement must not emit a write command")
	}
	mi, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	m = mi.(Model)
	if cmd != nil {
		t.Fatal("space selection must not emit a write command")
	}
	if !m.harnessModal.selected["claude"] {
		t.Fatalf("space should select claude in the form, got %#v", m.harnessModal.selected)
	}

	mi, cmd = m.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	m = mi.(Model)
	if cmd != nil {
		t.Fatal("scope switch must not emit a write command")
	}
	if m.harnessModal.scope != harnessScopeProject {
		t.Fatal("'p' should switch the form to project scope")
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(original) {
		t.Fatalf("config written during form interaction:\n%s", data)
	}
}

func TestHarnessFormPreservesUnknownIDs(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.globalHarnesses = []string{"factory", "mystery-harness"}
	m.openHarnessModal()
	// Toggle factory off through the form; the unknown ID must survive.
	mi, _ := m.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	m = mi.(Model)
	saved := selectedHarnessIDs(m.harnessModal.selected)
	found := false
	for _, id := range saved {
		if id == "mystery-harness" {
			found = true
		}
	}
	if !found {
		t.Fatalf("unknown harness ID lost through form editing: %#v", saved)
	}
}

func TestHarnessFormEscCancelsWithoutWrite(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.globalHarnesses = []string{"factory"}
	m.openHarnessModal()
	mi, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = mi.(Model)
	if m.pane != paneList {
		t.Fatal("esc should cancel the harness form")
	}
	if cmd != nil {
		t.Fatal("cancel must not emit a write command")
	}
}

func TestToggleFormEnterDispatchesHighlightedAction(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.pane = paneToggleModal
	m.modalSkill = mkRow("audit")
	m.ensureToggleForm()
	mi, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = mi.(Model)
	if cmd == nil {
		t.Fatal("enter on the toggle form should dispatch the highlighted action")
	}
	if m.pane != paneList {
		t.Fatalf("toggle action should close the modal, got pane=%v", m.pane)
	}
}

func TestEditConfigFormEnterDispatchesProjectEditor(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.pane = paneEditConfigModal
	m.ensureEditConfigForm()
	// Move highlight to project, then submit through the form.
	mi, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = mi.(Model)
	mi, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = mi.(Model)
	if cmd == nil {
		t.Fatal("enter on the edit-config form should dispatch the editor")
	}
	if m.pane != paneList {
		t.Fatalf("editor dispatch should close the modal, got pane=%v", m.pane)
	}
}

func TestSyncQuitFormConfirmAndDecline(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.pane = paneConfirm
	m.confirmMsg = "1 pending changes not synced. Sync+quit? y/n/esc"
	m.ensureSyncQuitForm()
	mi, _ := m.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	m = mi.(Model)
	if m.pane != paneList {
		t.Fatal("'n' through the confirm form should close the pane")
	}
	if m.quitAfterSync {
		t.Fatal("declined sync+quit must not arm quitAfterSync")
	}
}
