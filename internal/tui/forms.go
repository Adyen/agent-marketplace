package tui

import (
	"fmt"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/Adyen/agent-marketplace/internal/skill"
)

// drainFormCmd executes the commands a Huh form returns synchronously so
// a single Model update settles the form's next stable state. Huh
// completes fields through internal nextField/nextGroup message round
// trips; draining them here keeps modal dispatch (save, toggle, cancel)
// synchronous with the key press that caused it.
func drainFormCmd(form *huh.Form, cmd tea.Cmd) {
	pending := []tea.Cmd{cmd}
	for len(pending) > 0 {
		c := pending[0]
		pending = pending[1:]
		if c == nil {
			continue
		}
		switch msg := c().(type) {
		case nil:
		case tea.BatchMsg:
			pending = append(pending, msg...)
		default:
			_, next := form.Update(msg)
			pending = append(pending, next)
		}
	}
}

// updateModalForm forwards one message to a modal's Huh form, drains the
// resulting internal commands, and returns the settled form.
func updateModalForm(form *huh.Form, msg tea.Msg) *huh.Form {
	if form == nil {
		return nil
	}
	fm, cmd := form.Update(msg)
	if f, ok := fm.(*huh.Form); ok {
		form = f
	}
	drainFormCmd(form, cmd)
	return form
}

// modalSelectKeyMap adapts the default Huh keymap to the modal letter
// accelerators this TUI owns: 'g' switches harness scope or edits the
// global config, 'l' leaves a migration or edits the local config, so
// those letters never reach the form's own navigation bindings.
func modalFormKeyMap() *huh.KeyMap {
	km := huh.NewDefaultKeyMap()
	km.MultiSelect.Toggle = key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "toggle"))
	km.MultiSelect.GotoTop = key.NewBinding(key.WithKeys("home"), key.WithHelp("home", "go to start"), key.WithDisabled())
	km.MultiSelect.GotoBottom = key.NewBinding(key.WithKeys("end"), key.WithHelp("end", "go to end"), key.WithDisabled())
	km.Select.GotoTop = key.NewBinding(key.WithKeys("home"), key.WithHelp("home", "go to start"), key.WithDisabled())
	km.Select.GotoBottom = key.NewBinding(key.WithKeys("end"), key.WithHelp("end", "go to end"), key.WithDisabled())
	return km
}

// migrationFormKeyMap keeps 'm' as the migrate accelerator and 'l' as the
// leave accelerator by moving the confirm toggle off 'l'.
func migrationFormKeyMap() *huh.KeyMap {
	km := modalFormKeyMap()
	km.Confirm.Toggle = key.NewBinding(key.WithKeys("h", "left", "right"), key.WithHelp("←/→", "toggle"), key.WithDisabled())
	km.Confirm.Accept = key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "migrate"))
	km.Confirm.Reject = key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "leave"))
	return km
}

func newModalForm(field huh.Field, keymap *huh.KeyMap, t theme) *huh.Form {
	form := huh.NewForm(huh.NewGroup(field)).
		WithKeyMap(keymap).
		WithTheme(huh.ThemeFunc(func(bool) *huh.Styles { return huhTheme(t) })).
		WithShowHelp(false).
		WithShowErrors(true)
	drainFormCmd(form, form.Init())
	return form
}

// harnessOptionIDList mirrors the option order the harness/init forms
// present: known harnesses in UI order followed by unknown selected IDs.
func harnessOptionIDList(selected map[string]bool) []string {
	ids := make([]string, 0, len(knownHarnessOptions))
	for _, opt := range knownHarnessOptions {
		ids = append(ids, opt.ID)
	}
	return append(ids, unknownSelectedHarnesses(selected)...)
}

func newHarnessSelectForm(selected map[string]bool, t theme) (*huh.Form, *huh.MultiSelect[string], *[]string) {
	options := make([]huh.Option[string], 0, len(knownHarnessOptions))
	for _, opt := range knownHarnessOptions {
		options = append(options, huh.NewOption(fmt.Sprintf("%-14s %s", opt.Label, opt.ID), opt.ID))
	}
	for _, id := range unknownSelectedHarnesses(selected) {
		options = append(options, huh.NewOption(fmt.Sprintf("%-14s %s", "other", sanitizeTerminalData(id, terminalSingleLine)), id))
	}
	value := selectedHarnessIDs(selected)
	multi := huh.NewMultiSelect[string]().
		Options(options...).
		Value(&value).
		Height(len(options)).
		Filterable(false)
	return newModalForm(multi, modalFormKeyMap(), t), multi, &value
}

// reconcileMultiCursor drives a Huh multiselect cursor to the model's
// authoritative cursor index. Tests and scope switches may set the model
// cursor directly; Huh exposes no cursor setter, so the form is moved
// with synthetic key presses until its hovered option matches.
func reconcileMultiCursor(form *huh.Form, multi *huh.MultiSelect[string], optionIDs []string, want int) {
	if form == nil || multi == nil || len(optionIDs) == 0 {
		return
	}
	want = min(max(0, want), len(optionIDs)-1)
	for range len(optionIDs) {
		idx := -1
		if id, ok := multi.Hovered(); ok {
			for i, oid := range optionIDs {
				if oid == id {
					idx = i
					break
				}
			}
		}
		if idx < 0 || idx == want {
			return
		}
		code := tea.KeyDown
		if idx > want {
			code = tea.KeyUp
		}
		updateModalForm(form, tea.KeyPressMsg{Code: code})
	}
}

func syncMultiState(multi *huh.MultiSelect[string], value *[]string, optionIDs []string, selected map[string]bool) (map[string]bool, int) {
	cursor := -1
	if multi != nil {
		if id, ok := multi.Hovered(); ok {
			for i, oid := range optionIDs {
				if oid == id {
					cursor = i
					break
				}
			}
		}
	}
	if value != nil {
		selected = harnessSelection(*value)
	}
	return selected, cursor
}

// buildHarnessForm rebuilds the harness modal's multiselect form for the
// modal's current selection.
func (m *Model) buildHarnessForm() {
	m.harnessModal.form, m.harnessModal.multi, m.harnessModal.multiValue = newHarnessSelectForm(m.harnessModal.selected, m.currentTheme())
	m.harnessModal.optionIDs = harnessOptionIDList(m.harnessModal.selected)
	m.harnessModal.cursor = 0
}

func (m *Model) ensureHarnessForm() {
	if m.harnessModal.form == nil {
		m.buildHarnessForm()
		return
	}
	m.harnessModal.optionIDs = harnessOptionIDList(m.harnessModal.selected)
}

func (m *Model) ensureInitForm() {
	if m.initModal.form == nil {
		m.initModal.form, m.initModal.multi, m.initModal.multiValue = newHarnessSelectForm(m.initModal.selected, m.currentTheme())
	}
	m.initModal.optionIDs = harnessOptionIDList(m.initModal.selected)
}

// updateHarnessMulti delegates a message to a harness-style multiselect
// form and syncs the model's authoritative selection and cursor back.
func updateHarnessMulti(form *huh.Form, multi *huh.MultiSelect[string], value *[]string, optionIDs []string, cursor int, msg tea.Msg) (*huh.Form, map[string]bool, int) {
	if form == nil {
		return nil, nil, cursor
	}
	reconcileMultiCursor(form, multi, optionIDs, cursor)
	form = updateModalForm(form, msg)
	selected, synced := syncMultiState(multi, value, optionIDs, nil)
	if synced >= 0 {
		cursor = synced
	}
	return form, selected, cursor
}

// toggleModalActionOptions lists the per-scope actions the toggle form
// offers for a row, keyed by the same letters the modal accelerators use.
func toggleModalActionOptions(r row) []huh.Option[string] {
	kindLabel := skill.KindLabel(r.Artifact.Kind)
	if r.canOverrideMarketplace() {
		return []huh.Option[string]{huh.NewOption("[e]  toggle user override", "e")}
	}
	excludeLabel := fmt.Sprintf("toggle %s exclude", kindLabel)
	if r.Artifact.Source.IsMachineScope() || r.Artifact.IsMarketplace {
		hint := " (use [g] to toggle at user level)"
		if r.Artifact.IsMarketplace {
			hint = " (use installation scopes)"
		}
		excludeLabel += hint
	}
	return []huh.Option[string]{
		huh.NewOption("[g]  toggle global", "g"),
		huh.NewOption("[p]  toggle project", "p"),
		huh.NewOption("[l]  toggle local", "l"),
		huh.NewOption("[e]  "+excludeLabel, "e"),
	}
}

func (m *Model) buildToggleForm() {
	options := toggleModalActionOptions(m.modalSkill)
	choice := ""
	sel := huh.NewSelect[string]().
		Options(options...).
		Value(&choice).
		Height(len(options) + 1)
	m.toggleForm = newModalForm(sel, modalFormKeyMap(), m.currentTheme())
	m.toggleChoice = &choice
	m.toggleFormFor = rowKey(m.modalSkill)
}

func (m *Model) ensureToggleForm() {
	if m.toggleForm == nil || m.toggleFormFor != rowKey(m.modalSkill) {
		m.buildToggleForm()
	}
}

func (m *Model) buildMigrationForm() {
	value := true
	confirm := huh.NewConfirm().
		Key("migrate").
		Value(&value).
		Affirmative("migrate to v4").
		Negative("leave unchanged")
	m.migrationForm = newModalForm(confirm, migrationFormKeyMap(), m.currentTheme())
	m.migrationValue = &value
}

func (m *Model) ensureMigrationForm() {
	if m.migrationForm == nil {
		m.buildMigrationForm()
	}
}

func (m *Model) buildEditConfigForm() {
	choice := ""
	sel := huh.NewSelect[string]().
		Options(
			huh.NewOption("[g]  edit global config", "g"),
			huh.NewOption("[p]  edit project config", "p"),
			huh.NewOption("[l]  edit local config", "l"),
		).
		Value(&choice).
		Height(4)
	m.editConfigForm = newModalForm(sel, modalFormKeyMap(), m.currentTheme())
	m.editConfigChoice = &choice
}

func (m *Model) ensureEditConfigForm() {
	if m.editConfigForm == nil {
		m.buildEditConfigForm()
	}
}

func (m *Model) buildSyncQuitForm() {
	value := true
	confirm := huh.NewConfirm().
		Key("syncquit").
		Value(&value).
		Affirmative("sync+quit").
		Negative("quit")
	m.syncQuitForm = newModalForm(confirm, modalFormKeyMap(), m.currentTheme())
	m.syncQuitValue = &value
}

func (m *Model) ensureSyncQuitForm() {
	if m.syncQuitForm == nil {
		m.buildSyncQuitForm()
	}
}
