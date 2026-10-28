package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Adyen/agent-marketplace/internal/config"
)

// isolatedPrefsHome points XDG_STATE_HOME at a fresh temp dir and returns a
// fresh home dir so preference tests never touch developer state.
func isolatedPrefsHome(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	return t.TempDir()
}

func prefsFile(t *testing.T) string {
	t.Helper()
	root := os.Getenv("XDG_STATE_HOME")
	if root == "" {
		t.Fatalf("test must isolate XDG_STATE_HOME")
	}
	return filepath.Join(root, "agent-marketplace", "tui.json")
}

func newViewModel(t *testing.T, rows []row) Model {
	t.Helper()
	home := isolatedPrefsHome(t)
	m := NewModel("/repo", home)
	m.loading = false
	m.width, m.height = 80, 24
	m.rows = rows
	m.filteredRows = m.rows
	m.recomputeView()
	return m
}

// spec: tui-interactions/accept-and-discard-live-grouping-preview
func TestViewSettingsKeyOpensModalWithLivePreview(t *testing.T) {
	m := newViewModel(t, groupingTestRows())

	mi, _ := m.Update(keyPress('v'))
	m = mi.(Model)
	if m.pane != paneViewSettings {
		t.Fatalf("v should open view settings, pane=%v", m.pane)
	}

	// Move to the primary field and change it to Source: the list behind the
	// modal previews immediately.
	mi, _ = m.Update(keyNamed("right"))
	m = mi.(Model)
	if m.viewSettings.GroupBy != axisSource {
		t.Fatalf("primary preview = %q, want source", m.viewSettings.GroupBy)
	}
	foundSource := false
	for _, it := range m.flatItems {
		if it.Kind == flatSection && strings.HasPrefix(it.SectionPath, "src:") {
			foundSource = true
		}
	}
	if !foundSource {
		t.Fatalf("live preview should regroup into source sections, got %v", sectionPaths(m.flatItems))
	}

	// Accept with Enter: the changed view is retained and persisted.
	mi, _ = m.Update(keyNamed("enter"))
	m = mi.(Model)
	if m.pane != paneList {
		t.Fatalf("enter should close the modal, pane=%v", m.pane)
	}
	if m.viewSettings.GroupBy != axisSource {
		t.Fatalf("accepted settings = %+v, want source primary", m.viewSettings)
	}
	if _, err := os.Stat(prefsFile(t)); err != nil {
		t.Fatalf("accepted change should write preferences: %v", err)
	}
}

// spec: tui-interactions/accept-and-discard-live-grouping-preview
func TestViewSettingsEscRestoresWithoutWrite(t *testing.T) {
	m := newViewModel(t, groupingTestRows())

	mi, _ := m.Update(keyPress('v'))
	m = mi.(Model)
	mi, _ = m.Update(keyNamed("right"))
	m = mi.(Model)
	if m.viewSettings.GroupBy == axisStatus {
		t.Fatalf("preview should have changed the grouping")
	}
	mi, _ = m.Update(keyNamed("esc"))
	m = mi.(Model)
	if m.pane != paneList {
		t.Fatalf("esc should close the modal, pane=%v", m.pane)
	}
	if m.viewSettings.GroupBy != axisStatus || m.viewSettings.ThenBy != axisNone {
		t.Fatalf("esc should restore defaults, got %+v", m.viewSettings)
	}
	foundStatus := false
	for _, it := range m.flatItems {
		if it.Kind == flatSection && strings.HasPrefix(it.SectionPath, "status:") {
			foundStatus = true
		}
	}
	if !foundStatus {
		t.Fatalf("esc should restore the status list, got %v", sectionPaths(m.flatItems))
	}
	if _, err := os.Stat(prefsFile(t)); !os.IsNotExist(err) {
		t.Fatalf("esc must not write preferences, stat err=%v", err)
	}
}

// spec: tui-interactions/accept-and-discard-live-grouping-preview
func TestViewSettingsFieldNavigationAndAxisValidity(t *testing.T) {
	m := newViewModel(t, groupingTestRows())
	mi, _ := m.Update(keyPress('v'))
	m = mi.(Model)

	// Tab switches to the secondary field; choose Kind.
	mi, _ = m.Update(keyNamed("tab"))
	m = mi.(Model)
	mi, _ = m.Update(keyNamed("right"))
	m = mi.(Model)
	if m.viewSettings.ThenBy != axisKind {
		t.Fatalf("secondary field = %q, want kind", m.viewSettings.ThenBy)
	}

	// Primary change to Kind makes the Kind secondary invalid: it resets to None.
	mi, _ = m.Update(keyNamed("shift+tab"))
	m = mi.(Model)
	mi, _ = m.Update(keyNamed("right"))
	m = mi.(Model)
	mi, _ = m.Update(keyNamed("right"))
	m = mi.(Model)
	if m.viewSettings.GroupBy != axisKind {
		t.Fatalf("primary = %q, want kind", m.viewSettings.GroupBy)
	}
	if m.viewSettings.ThenBy == axisKind || m.viewSettings.ThenBy == axisStatus {
		t.Fatalf("secondary must exclude primary and status, got %q", m.viewSettings.ThenBy)
	}
	// Status must never be offered as secondary.
	mi, _ = m.Update(keyNamed("tab"))
	m = mi.(Model)
	for range 4 {
		mi, _ = m.Update(keyNamed("right"))
		m = mi.(Model)
		if m.viewSettings.ThenBy == axisStatus || m.viewSettings.ThenBy == m.viewSettings.GroupBy {
			t.Fatalf("invalid secondary axis %q for primary %q", m.viewSettings.ThenBy, m.viewSettings.GroupBy)
		}
	}
}

// spec: tui-interactions/accept-and-discard-live-grouping-preview
func TestViewSettingsPaletteEntryOpensModal(t *testing.T) {
	m := newViewModel(t, groupingTestRows())
	ids := map[paletteActionID]bool{}
	for _, a := range paletteActions() {
		ids[a.id] = true
	}
	if !ids[actionViewSettings] {
		t.Fatalf("palette missing view-settings action")
	}
	mi, _ := m.dispatchAction(actionViewSettings)
	m = mi.(Model)
	if m.pane != paneViewSettings {
		t.Fatalf("palette dispatch should open view settings, pane=%v", m.pane)
	}
}

// spec: tui-feedback/help-and-palette-reflect-section-context
func TestFormerTabKeysInactiveUnderGroupings(t *testing.T) {
	for _, combo := range []viewSettings{
		{GroupBy: axisStatus, ThenBy: axisNone},
		{GroupBy: axisSource, ThenBy: axisKind},
		{GroupBy: axisKind, ThenBy: axisSource},
	} {
		m := newViewModel(t, groupingTestRows())
		m.viewSettings = combo
		m.recomputeView()
		before := len(m.flatItems)
		cursor := m.cursor
		for _, k := range []string{"1", "2", "3", "tab", "shift+tab"} {
			mi, _ := m.Update(keyNamed(k))
			m = mi.(Model)
		}
		if len(m.flatItems) != before || m.cursor != cursor {
			t.Fatalf("%v: former tab keys mutated list state", combo)
		}
		if m.pane != paneList {
			t.Fatalf("%v: former tab keys left pane=%v", combo, m.pane)
		}
	}
}

// spec: tui-startup/confirmed-preferences-survive-restart-without-config-writes
func TestViewPreferencesRoundTrip(t *testing.T) {
	home := isolatedPrefsHome(t)
	repo := t.TempDir()
	// Seed authored config and acquisition lock bytes; an accepted preference
	// change must leave them byte-identical.
	sentinels := map[string][]byte{}
	for _, p := range []string{
		filepath.Join(repo, ".agents", "config.yaml"),
		filepath.Join(repo, ".agents", "config.local.yaml"),
		filepath.Join(home, ".agents", "config.yaml"),
		filepath.Join(repo, ".agents", "lock.yaml"),
	} {
		sentinel := []byte("# sentinel " + p + "\n")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, sentinel, 0o600); err != nil {
			t.Fatal(err)
		}
		sentinels[p] = sentinel
	}
	m := NewModel(repo, home)
	m.loading = false
	m.rows = groupingTestRows()
	m.recomputeView()

	mi, _ := m.Update(keyPress('v'))
	m = mi.(Model)
	mi, _ = m.Update(keyNamed("right"))
	m = mi.(Model)
	mi, _ = m.Update(keyNamed("enter"))
	m = mi.(Model)

	data, err := os.ReadFile(prefsFile(t))
	if err != nil {
		t.Fatalf("preference file missing after accept: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("preference file is not valid JSON: %v", err)
	}
	if decoded["version"] != float64(1) {
		t.Fatalf("version = %v", decoded["version"])
	}
	if strings.Contains(string(data), "fold") {
		t.Fatalf("fold state must not persist: %s", data)
	}

	// Restart: a fresh model loads the saved grouping.
	m2 := NewModel(repo, home)
	if m2.viewSettings.GroupBy != axisSource {
		t.Fatalf("restarted model grouping = %+v", m2.viewSettings)
	}
	// Authored config and acquisition locks stay byte-identical.
	for p, want := range sentinels {
		got, err := os.ReadFile(p)
		if err != nil || string(got) != string(want) {
			t.Fatalf("preference persistence touched authored config/lock %s: %q %v", p, got, err)
		}
	}
}

// spec: tui-startup/missing-invalid-unreadable-and-failed-save-preference-files
func TestLoadViewPreferencesFallbacks(t *testing.T) {
	t.Run("missing file uses defaults silently", func(t *testing.T) {
		isolatedPrefsHome(t)
		got, warn := loadViewPreferences(t.TempDir())
		if warn != "" {
			t.Fatalf("missing file should not warn, got %q", warn)
		}
		if got.GroupBy != axisStatus || got.ThenBy != axisNone {
			t.Fatalf("defaults = %+v", got)
		}
	})

	t.Run("home fallback when XDG unset", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("XDG_STATE_HOME", "")
		path := filepath.Join(home, ".local", "state", "agent-marketplace", "tui.json")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(`{"version":1,"view":{"groupBy":"source","thenBy":"kind"}}`), 0o600); err != nil {
			t.Fatal(err)
		}
		got, warn := loadViewPreferences(home)
		if warn != "" {
			t.Fatalf("home-fallback load warned: %q", warn)
		}
		if got.GroupBy != axisSource || got.ThenBy != axisKind {
			t.Fatalf("home-fallback preferences = %+v", got)
		}
	})

	for _, tc := range []struct {
		name    string
		content string
	}{
		{"malformed json", "{not json"},
		{"unsupported version", `{"version":2,"view":{"groupBy":"kind","thenBy":"none"}}`},
		{"invalid axis", `{"version":1,"view":{"groupBy":"color","thenBy":"none"}}`},
		{"status secondary rejected", `{"version":1,"view":{"groupBy":"source","thenBy":"status"}}`},
		{"same axis rejected", `{"version":1,"view":{"groupBy":"kind","thenBy":"kind"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolatedPrefsHome(t)
			path := prefsFile(t)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			got, warn := loadViewPreferences(t.TempDir())
			if warn == "" {
				t.Fatalf("bad file should produce a warning")
			}
			if got.GroupBy != axisStatus || got.ThenBy != axisNone {
				t.Fatalf("bad file should fall back to defaults, got %+v", got)
			}
			// The bad file is left untouched.
			data, _ := os.ReadFile(path)
			if string(data) != tc.content {
				t.Fatalf("bad file was rewritten")
			}
		})
	}

	t.Run("nonregular file warns without blocking", func(t *testing.T) {
		isolatedPrefsHome(t)
		path := prefsFile(t)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := syscall.Mkfifo(path, 0o600); err != nil {
			t.Skipf("fifo unsupported: %v", err)
		}
		done := make(chan string, 1)
		go func() {
			_, warn := loadViewPreferences(t.TempDir())
			done <- warn
		}()
		select {
		case warn := <-done:
			if warn == "" {
				t.Fatalf("nonregular preference file should warn")
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("loadViewPreferences blocked on a FIFO")
		}
	})

	t.Run("unreadable file warns", func(t *testing.T) {
		isolatedPrefsHome(t)
		path := prefsFile(t)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err) // a directory where the file belongs fails reads
		}
		got, warn := loadViewPreferences(t.TempDir())
		if warn == "" {
			t.Fatalf("unreadable file should warn")
		}
		if got.GroupBy != axisStatus {
			t.Fatalf("defaults = %+v", got)
		}
	})
}

// spec: tui-startup/missing-invalid-unreadable-and-failed-save-preference-files
func TestViewSettingsEscAndUnchangedAcceptPreserveBadFile(t *testing.T) {
	m := newViewModel(t, groupingTestRows())
	path := prefsFile(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{"{corrupt", `{"version":2,"view":{"groupBy":"kind","thenBy":"none"}}`} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		for _, key := range []tea.KeyPressMsg{keyNamed("esc"), keyNamed("enter")} {
			mi, _ := m.Update(keyPress('v'))
			m = mi.(Model)
			mi, _ = m.Update(key)
			m = mi.(Model)
			if m.pane != paneList {
				t.Fatalf("content %q key %v: modal must close, pane=%v", content, key, m.pane)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != content {
				t.Fatalf("content %q key %v: preference file changed to %q err=%v", content, key, data, err)
			}
		}
	}
}

// spec: tui-startup/missing-invalid-unreadable-and-failed-save-preference-files
func TestViewSettingsUnchangedAcceptWritesNothing(t *testing.T) {
	m := newViewModel(t, groupingTestRows())
	mi, _ := m.Update(keyPress('v'))
	m = mi.(Model)
	mi, _ = m.Update(keyNamed("enter")) // accept without changing anything
	if mi.(Model).pane != paneList {
		t.Fatalf("accept must close the modal")
	}
	if _, err := os.Stat(prefsFile(t)); !os.IsNotExist(err) {
		t.Fatalf("unchanged acceptance must write nothing")
	}
}

// spec: tui-startup/missing-invalid-unreadable-and-failed-save-preference-files
func TestViewPreferencesSaveFailurePreservesFile(t *testing.T) {
	isolatedPrefsHome(t)
	path := prefsFile(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte(`{"version":1,"view":{"groupBy":"kind","thenBy":"none"}}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	// Make the directory unwritable so the atomic temp+rename cannot proceed.
	if err := os.Chmod(filepath.Dir(path), 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Dir(path), 0o755) })

	err := saveViewPreferences(t.TempDir(), viewSettings{GroupBy: axisSource, ThenBy: axisNone})
	if err == nil {
		t.Fatalf("save should fail in a read-only directory")
	}
	data, rerr := os.ReadFile(path)
	if rerr != nil || string(data) != string(original) {
		t.Fatalf("failed save must preserve existing bytes, got %q err %v", data, rerr)
	}
}

// spec: tui-startup/missing-invalid-unreadable-and-failed-save-preference-files
func TestViewPreferencesWarningSurfacesAtStartup(t *testing.T) {
	isolatedPrefsHome(t)
	repoRoot, home := progressiveFixture(t)
	path := prefsFile(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := NewModel(repoRoot, home)
	if m.viewPrefsWarning == "" {
		t.Fatalf("corrupt preference file must record a startup warning")
	}
	m.width, m.height = 80, 24
	// Run the real Init batch and feed the warning toast through Update like
	// the Bubble Tea runtime would, then accept a snapshot so the list
	// renders.
	var toast toastMsg
	found := false
	initCmd := m.Init()
	joinStartupSnapshot(t, initCmd)
	for _, c := range initCmd().(tea.BatchMsg) {
		if c == nil {
			continue
		}
		if tm, ok := c().(toastMsg); ok && strings.Contains(tm.text, "view preferences") {
			toast, found = tm, true
		}
	}
	if !found {
		t.Fatalf("Init batch must contain the preference warning toast")
	}
	mi, _ := m.Update(toast)
	m = mi.(Model)
	mi, _ = m.Update(snapshotLoadedMsg{
		generation:      m.requests.Active(requestSnapshot),
		mergedCfg:       &config.Config{},
		driftAssessment: driftReady,
	})
	m = mi.(Model)
	if !strings.Contains(ansi.Strip(m.View().Content), "view preferences") {
		t.Fatalf("accepted snapshot + enqueued toast must render the warning")
	}
}
