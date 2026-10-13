package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

// viewModalState is the View settings modal state: the focused field and the
// settings captured at open so Esc can restore them.
type viewModalState struct {
	field    int // 0 = primary grouping, 1 = secondary grouping
	original viewSettings
}

func axisLabel(a groupingAxis) string {
	switch a {
	case axisStatus:
		return "Status"
	case axisSource:
		return "Source"
	case axisKind:
		return "Kind"
	default:
		return "None"
	}
}

// primaryAxisChoices are the selectable primary axes in display order.
func primaryAxisChoices() []groupingAxis {
	return []groupingAxis{axisStatus, axisSource, axisKind}
}

// secondaryAxisChoices are the secondary axes valid for the given primary:
// None, Kind, Source, excluding the primary axis; Status is never secondary.
func secondaryAxisChoices(primary groupingAxis) []groupingAxis {
	var out []groupingAxis
	for _, a := range []groupingAxis{axisNone, axisKind, axisSource} {
		if a == primary {
			continue
		}
		out = append(out, a)
	}
	return out
}

func cycleChoice(current groupingAxis, choices []groupingAxis, delta int) groupingAxis {
	idx := 0
	for i, c := range choices {
		if c == current {
			idx = i
			break
		}
	}
	idx = (idx + delta + len(choices)) % len(choices)
	return choices[idx]
}

// openViewSettingsModal captures the current settings for cancellation and
// opens the modal over the live list.
func (m *Model) openViewSettingsModal() {
	m.viewModal = viewModalState{field: 0, original: m.viewSettings.normalized()}
	m.pane = paneViewSettings
}

func (m Model) handleViewSettingsKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	sel := m.viewSettings.normalized()
	switch {
	case key.Matches(msg, m.keys.Clear):
		// Restore the settings captured at open; no writes happened.
		m.applyViewSettings(m.viewModal.original)
		m.pane = paneList
		m.viewModal = viewModalState{}
		return m, nil
	case key.Matches(msg, m.keys.SearchConfirm):
		original := m.viewModal.original
		m.pane = paneList
		m.viewModal = viewModalState{}
		if sel != original {
			if err := saveViewPreferences(m.homeDir, sel); err != nil {
				return m, func() tea.Msg {
					return toastMsg{text: fmt.Sprintf("view preferences not saved: %v", err), duration: 5e9}
				}
			}
		}
		return m, nil
	case msg.Code == tea.KeyTab:
		m.viewModal.field = 1 - m.viewModal.field
		return m, nil
	case key.Matches(msg, m.keys.Up), key.Matches(msg, m.keys.Down):
		m.viewModal.field = 1 - m.viewModal.field
		return m, nil
	case key.Matches(msg, m.keys.Collapse):
		return m.cycleViewField(-1)
	case key.Matches(msg, m.keys.Expand):
		return m.cycleViewField(1)
	}
	return m, nil
}

// cycleViewField advances the focused field's axis by delta and previews.
func (m Model) cycleViewField(delta int) (tea.Model, tea.Cmd) {
	sel := m.viewSettings.normalized()
	if m.viewModal.field == 0 {
		sel.GroupBy = cycleChoice(sel.GroupBy, primaryAxisChoices(), delta)
	} else {
		sel.ThenBy = cycleChoice(sel.ThenBy, secondaryAxisChoices(sel.GroupBy), delta)
	}
	m.applyViewSettings(sel)
	return m, nil
}

// renderViewSettingsModal draws the two-axis chooser inside the shared
// bounded modal frame.
func renderViewSettingsModal(m Model) string {
	t := m.currentTheme()
	sel := m.viewSettings.normalized()

	field := func(label string, choices []groupingAxis, current groupingAxis, focused bool) string {
		var parts []string
		for _, c := range choices {
			text := axisLabel(c)
			if c == current {
				text = "[" + text + "]"
			} else {
				text = " " + text + " "
			}
			if focused && c == current {
				parts = append(parts, accentStyle(t).Bold(true).Render(text))
			} else if c == current {
				parts = append(parts, textStyle(t).Render(text))
			} else {
				parts = append(parts, dimTextStyle(t).Render(text))
			}
		}
		prefix := "  "
		if focused {
			prefix = "▸ "
		}
		return prefix + label + "  " + strings.Join(parts, " ")
	}

	body := []string{
		field("Group by:", primaryAxisChoices(), sel.GroupBy, m.viewModal.field == 0),
		field("Then by: ", secondaryAxisChoices(sel.GroupBy), sel.ThenBy, m.viewModal.field == 1),
	}
	return modalFrame(m, t.accent, titleStyle(t).Render("View settings"), body, []footerHint{
		{"tab/↑↓", "field"}, {"←→", "change"}, {"enter", "accept"}, {"esc", "cancel"},
	})
}

// ---- persistence ----

// viewPreferencesVersion is the only supported preference file version.
const viewPreferencesVersion = 1

type viewPreferencesFile struct {
	Version int          `json:"version"`
	View    viewSettings `json:"view"`
}

// viewPreferencesPath resolves $XDG_STATE_HOME/agent-marketplace/tui.json,
// falling back to ~/.local/state/agent-marketplace/tui.json.
func viewPreferencesPath(homeDir string) string {
	root := os.Getenv("XDG_STATE_HOME")
	if root == "" {
		root = filepath.Join(homeDir, ".local", "state")
	}
	return filepath.Join(root, "agent-marketplace", "tui.json")
}

// loadViewPreferences reads the versioned view preferences once. A missing
// file yields defaults silently; an unreadable, malformed, unsupported, or
// invalid file yields defaults plus a warning message. The file is never
// modified here.
func loadViewPreferences(homeDir string) (viewSettings, string) {
	defaults := viewSettings{GroupBy: axisStatus, ThenBy: axisNone}
	data, err := readViewPreferencesFile(homeDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return defaults, ""
		}
		return defaults, fmt.Sprintf("view preferences unreadable (%v); using defaults", err)
	}
	var f viewPreferencesFile
	if err := json.Unmarshal(data, &f); err != nil {
		return defaults, "view preferences file is malformed; using defaults"
	}
	if f.Version != viewPreferencesVersion {
		return defaults, fmt.Sprintf("view preferences version %d unsupported; using defaults", f.Version)
	}
	s := f.View
	primaryOK := s.GroupBy == axisStatus || s.GroupBy == axisSource || s.GroupBy == axisKind
	secondaryOK := s.ThenBy == axisNone || s.ThenBy == axisSource || s.ThenBy == axisKind
	if !primaryOK || !secondaryOK || s.ThenBy == s.GroupBy {
		return defaults, "view preferences contain invalid axes; using defaults"
	}
	return s.normalized(), ""
}

// readViewPreferencesFile opens the preference file through the fsboundary
// regular-file seam so a FIFO, device, or swapped leaf fails bounded instead
// of blocking the reader.
func readViewPreferencesFile(homeDir string) ([]byte, error) {
	path := viewPreferencesPath(homeDir)
	dir, err := fsboundary.OpenDir(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer func() { _ = dir.Close() }()
	file, _, err := dir.OpenFile(filepath.Base(path))
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	data, _, err := fsboundary.ReadFile(file, config.MaxEffectiveConfigSize)
	return data, err
}

// saveViewPreferences writes the confirmed settings atomically (private temp
// file plus rename). A failure preserves any previous file bytes.
func saveViewPreferences(homeDir string, s viewSettings) error {
	s = s.normalized()
	data, err := json.Marshal(viewPreferencesFile{Version: viewPreferencesVersion, View: s})
	if err != nil {
		return err
	}
	path := viewPreferencesPath(homeDir)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tui-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	removeTmp := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}
	if err := tmp.Chmod(0o600); err != nil {
		removeTmp()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		removeTmp()
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}
