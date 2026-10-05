package tui

// AC4: every interactive modal owns an embedded Huh v2 form that carries
// its option state. These checks use only stdlib and preexisting
// in-package helpers so the file can be copied alone to BASE_SHA: it
// fails to compile there because the modal states have no `form` field.

import (
	"testing"
)

func TestHarnessModalCarriesHuhForm(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.globalHarnesses = []string{"factory"}
	m.openHarnessModal()
	if m.harnessModal.form == nil {
		t.Fatal("harness modal must own a Huh form")
	}
}

func TestInitModalCarriesHuhForm(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.pane = paneInitModal
	m.initModal = initModalState{selected: map[string]bool{"factory": true}}
	m.ensureInitForm()
	if m.initModal.form == nil {
		t.Fatal("init modal must own a Huh form")
	}
}

func TestToggleModalCarriesHuhForm(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.pane = paneToggleModal
	m.ensureToggleForm()
	if m.toggleForm == nil {
		t.Fatal("toggle modal must own a Huh form")
	}
}

func TestMigrationModalCarriesHuhForm(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.pane = paneMigrationModal
	m.migrationPrompt = migrationPrompt{Configs: []migrationConfig{{Label: "Project", Path: "/repo/.agents/config.yaml"}}}
	m.ensureMigrationForm()
	if m.migrationForm == nil {
		t.Fatal("migration modal must own a Huh form")
	}
}

func TestEditConfigModalCarriesHuhForm(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.pane = paneEditConfigModal
	m.ensureEditConfigForm()
	if m.editConfigForm == nil {
		t.Fatal("edit-config modal must own a Huh form")
	}
}

func TestSyncQuitConfirmCarriesHuhForm(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.pane = paneConfirm
	m.confirmMsg = "1 pending changes not synced. Sync+quit? y/n/esc"
	m.ensureSyncQuitForm()
	if m.syncQuitForm == nil {
		t.Fatal("sync+quit confirm must own a Huh form")
	}
}
