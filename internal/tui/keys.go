package tui

import (
	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
)

// keyMap defines all keyboard shortcuts for the TUI. It is the single source
// of truth for key bindings and their help text; both the footer hint line
// and the ? help overlay are driven from these bindings via help.Model.
type keyMap struct {
	// Navigation
	Up           key.Binding
	Down         key.Binding
	HalfPageDown key.Binding
	HalfPageUp   key.Binding
	PageDown     key.Binding
	PageUp       key.Binding
	Top          key.Binding
	Bottom       key.Binding
	NextTab      key.Binding
	PrevTab      key.Binding

	// Tab switching
	Tab1 key.Binding
	Tab2 key.Binding
	Tab3 key.Binding

	// Actions
	Search     key.Binding
	Clear      key.Binding // esc: clear search / close overlay
	Enter      key.Binding
	Space      key.Binding
	Collapse   key.Binding
	Expand     key.Binding
	Reload     key.Binding
	Sync       key.Binding
	Update     key.Binding
	Harness    key.Binding
	EditConfig key.Binding
	Palette    key.Binding

	// Fold (vim-style z-prefix)
	FoldPrefix key.Binding
	FoldToggle key.Binding // za
	FoldAll    key.Binding // zM
	ExpandAll  key.Binding // zR

	// Overlays
	Help   key.Binding
	Errors key.Binding

	// Quit
	Quit key.Binding

	// Toggle modal
	ToggleGlobal  key.Binding
	ToggleProject key.Binding
	ToggleLocal   key.Binding
	ToggleExclude key.Binding
	ToggleCancel  key.Binding

	HarnessGlobal  key.Binding
	HarnessProject key.Binding
	HarnessSave    key.Binding
	HarnessCancel  key.Binding

	MigrationRun   key.Binding
	MigrationLeave key.Binding

	// Edit config modal
	EditConfigGlobal  key.Binding
	EditConfigProject key.Binding
	EditConfigLocal   key.Binding
	EditConfigCancel  key.Binding

	// Detail pane
	DetailBack key.Binding

	// Search mode
	SearchConfirm   key.Binding
	SearchBackspace key.Binding

	// Confirm dialog
	ConfirmYes    key.Binding
	ConfirmNo     key.Binding
	ConfirmCancel key.Binding
}

// defaultKeyMap is the default set of key bindings.
var defaultKeyMap = keyMap{
	// Navigation
	Up:           key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("↑/k", "move up")),
	Down:         key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("↓/j", "move down")),
	HalfPageDown: key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", "half-page down")),
	HalfPageUp:   key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("ctrl+u", "half-page up")),
	PageDown:     key.NewBinding(key.WithKeys("f", "ctrl+f", "pgdown"), key.WithHelp("f/ctrl+f/pgdn", "page down")),
	PageUp:       key.NewBinding(key.WithKeys("b", "ctrl+b", "pgup"), key.WithHelp("b/ctrl+b/pgup", "page up")),
	Top:          key.NewBinding(key.WithKeys("g", "home"), key.WithHelp("g/home", "jump to top")),
	Bottom:       key.NewBinding(key.WithKeys("G", "end"), key.WithHelp("G/end", "jump to bottom")),
	NextTab:      key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next tab")),
	PrevTab:      key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("S-tab", "previous tab")),

	// Tab switching
	Tab1: key.NewBinding(key.WithKeys("1"), key.WithHelp("1", "Scope tab")),
	Tab2: key.NewBinding(key.WithKeys("2"), key.WithHelp("2", "Registry tab")),
	Tab3: key.NewBinding(key.WithKeys("3"), key.WithHelp("3", "Status tab")),

	// Actions
	Search:     key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "fuzzy search")),
	Clear:      key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "clear search / close")),
	Enter:      key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "view details / toggle fold")),
	Space:      key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "toggle modal / fold section")),
	Collapse:   key.NewBinding(key.WithKeys("h", "left"), key.WithHelp("h/←", "collapse section")),
	Expand:     key.NewBinding(key.WithKeys("l", "right"), key.WithHelp("l/→", "expand section")),
	Reload:     key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "reload from disk")),
	Sync:       key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sync pending")),
	Update:     key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "update caches")),
	Harness:    key.NewBinding(key.WithKeys("H"), key.WithHelp("H", "harness defaults")),
	EditConfig: key.NewBinding(key.WithKeys("E"), key.WithHelp("E", "edit config")),
	Palette:    key.NewBinding(key.WithKeys("ctrl+k", ":"), key.WithHelp("ctrl+k/:", "command palette")),

	// Fold (vim-style z-prefix)
	FoldPrefix: key.NewBinding(key.WithKeys("z"), key.WithHelp("z", "fold prefix")),
	FoldToggle: key.NewBinding(key.WithKeys("a"), key.WithHelp("za", "toggle current fold")),
	FoldAll:    key.NewBinding(key.WithKeys("M"), key.WithHelp("zM", "collapse all")),
	ExpandAll:  key.NewBinding(key.WithKeys("R"), key.WithHelp("zR", "expand all")),

	// Overlays
	Help:   key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "this help")),
	Errors: key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "view errors")),

	// Quit
	Quit: key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),

	// Toggle modal
	ToggleGlobal:  key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "toggle global")),
	ToggleProject: key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "toggle project")),
	ToggleLocal:   key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "toggle local")),
	ToggleExclude: key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "toggle local exclude")),
	ToggleCancel:  key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),

	HarnessGlobal:  key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "edit global")),
	HarnessProject: key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "edit project")),
	HarnessSave:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "save")),
	HarnessCancel:  key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),

	MigrationRun:   key.NewBinding(key.WithKeys("m", "enter"), key.WithHelp("m/enter", "run migration")),
	MigrationLeave: key.NewBinding(key.WithKeys("l", "esc"), key.WithHelp("l/esc", "leave config")),

	// Edit config modal
	EditConfigGlobal:  key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "edit global")),
	EditConfigProject: key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "edit project")),
	EditConfigLocal:   key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "edit local")),
	EditConfigCancel:  key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),

	// Detail pane
	DetailBack: key.NewBinding(key.WithKeys("esc", "q"), key.WithHelp("esc/q", "back to list")),

	// Search mode
	SearchConfirm:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm search")),
	SearchBackspace: key.NewBinding(key.WithKeys("backspace"), key.WithHelp("⌫", "delete char")),

	// Confirm dialog
	ConfirmYes:    key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "sync + quit")),
	ConfirmNo:     key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "quit without sync")),
	ConfirmCancel: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
}

// ShortHelp returns key bindings to show in the footer hint line.
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		k.Help, k.Search, k.Tab1, k.Tab2, k.Tab3, k.Harness, k.EditConfig, k.Sync, k.Quit,
	}
}

// FullHelp returns key bindings grouped into sections for the help overlay.
// Each group is rendered as a vertical column by help.Model.FullHelpView.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		k.navigationBindings(),
		k.actionBindings(),
		k.toggleModalBindings(),
		k.harnessModalBindings(),
		k.editConfigModalBindings(),
		k.otherBindings(),
	}
}

// navigationBindings returns the navigation-related key bindings.
func (k keyMap) navigationBindings() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.HalfPageDown, k.HalfPageUp, k.PageDown, k.PageUp, k.Top, k.Bottom, k.Tab1, k.Tab2, k.Tab3, k.NextTab, k.PrevTab}
}

// actionBindings returns the action-related key bindings.
func (k keyMap) actionBindings() []key.Binding {
	return []key.Binding{k.Search, k.Clear, k.Enter, k.Space, k.Collapse, k.Expand, k.FoldToggle, k.FoldAll, k.ExpandAll, k.Harness, k.EditConfig, k.Sync, k.Update, k.Palette}
}

// toggleModalBindings returns the toggle-modal key bindings.
func (k keyMap) toggleModalBindings() []key.Binding {
	return []key.Binding{k.ToggleGlobal, k.ToggleProject, k.ToggleLocal, k.ToggleExclude, k.ToggleCancel}
}

func (k keyMap) harnessModalBindings() []key.Binding {
	return []key.Binding{k.HarnessGlobal, k.HarnessProject, k.Space, k.HarnessSave, k.HarnessCancel}
}

func (k keyMap) editConfigModalBindings() []key.Binding {
	return []key.Binding{k.EditConfigGlobal, k.EditConfigProject, k.EditConfigLocal, k.EditConfigCancel}
}

// otherBindings returns miscellaneous key bindings.
func (k keyMap) otherBindings() []key.Binding {
	return []key.Binding{k.Reload, k.Errors, k.Help, k.Quit}
}

// helpStyles builds the help.Model styles for one model theme. The full-key
// and full-desc styles follow the model's reported background; the rest use
// background-independent roles.
func helpStyles(t theme) help.Styles {
	return help.Styles{
		ShortKey:       shortcutKeyStyle,
		ShortDesc:      shortcutDescStyle,
		ShortSeparator: shortcutSepStyle,
		Ellipsis:       shortcutSepStyle,
		FullKey:        fullKeyStyle(t),
		FullDesc:       textStyle(t),
		FullSeparator:  helpFullSepStyle,
	}
}

// newHelpModel creates a help.Model styled to match the model theme.
func newHelpModel(t theme) help.Model {
	h := help.New()
	h.ShortSeparator = " · "
	h.Styles = helpStyles(t)
	return h
}
