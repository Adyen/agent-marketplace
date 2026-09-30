package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestKeyMapNavigationBindingsMatch(t *testing.T) {
	k := defaultKeyMap

	tests := []struct {
		name    string
		msg     tea.KeyPressMsg
		binding key.Binding
		want    bool
	}{
		{"k matches Up", keyPress('k'), k.Up, true},
		{"up matches Up", keyNamed("up"), k.Up, true},
		{"j matches Down", keyPress('j'), k.Down, true},
		{"down matches Down", keyNamed("down"), k.Down, true},
		{"ctrl+d matches HalfPageDown", tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}, k.HalfPageDown, true},
		{"ctrl+u matches HalfPageUp", tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}, k.HalfPageUp, true},
		{"f matches PageDown", keyPress('f'), k.PageDown, true},
		{"ctrl+f matches PageDown", tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl}, k.PageDown, true},
		{"pgdown matches PageDown", tea.KeyPressMsg{Code: tea.KeyPgDown}, k.PageDown, true},
		{"b matches PageUp", keyPress('b'), k.PageUp, true},
		{"ctrl+b matches PageUp", tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl}, k.PageUp, true},
		{"pgup matches PageUp", tea.KeyPressMsg{Code: tea.KeyPgUp}, k.PageUp, true},
		{"g matches Top", keyPress('g'), k.Top, true},
		{"home matches Top", tea.KeyPressMsg{Code: tea.KeyHome}, k.Top, true},
		{"G matches Bottom", keyPress('G'), k.Bottom, true},
		{"end matches Bottom", tea.KeyPressMsg{Code: tea.KeyEnd}, k.Bottom, true},
		{"tab matches NextTab", keyNamed("tab"), k.NextTab, true},
		{"shift+tab matches PrevTab", keyNamed("shift+tab"), k.PrevTab, true},
		{"1 matches Tab1", keyPress('1'), k.Tab1, true},
		{"2 matches Tab2", keyPress('2'), k.Tab2, true},
		{"3 matches Tab3", keyPress('3'), k.Tab3, true},
		{"x does not match Up", keyPress('x'), k.Up, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := key.Matches(tt.msg, tt.binding)
			if got != tt.want {
				t.Errorf("key.Matches(%v, binding) = %v, want %v", tt.msg, got, tt.want)
			}
		})
	}
}

func TestKeyMapActionBindingsMatch(t *testing.T) {
	k := defaultKeyMap

	tests := []struct {
		name    string
		msg     tea.KeyPressMsg
		binding key.Binding
		want    bool
	}{
		{"/ matches Search", keyPress('/'), k.Search, true},
		{"esc matches Clear", keyNamed("esc"), k.Clear, true},
		{"enter matches Enter", keyNamed("enter"), k.Enter, true},
		{"space matches Space", keyNamed(" "), k.Space, true},
		{"h matches Collapse", keyPress('h'), k.Collapse, true},
		{"left matches Collapse", keyNamed("left"), k.Collapse, true},
		{"l matches Expand", keyPress('l'), k.Expand, true},
		{"right matches Expand", keyNamed("right"), k.Expand, true},
		{"r matches Reload", keyPress('r'), k.Reload, true},
		{"s matches Sync", keyPress('s'), k.Sync, true},
		{"u matches Update", keyPress('u'), k.Update, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := key.Matches(tt.msg, tt.binding)
			if got != tt.want {
				t.Errorf("key.Matches(%v, binding) = %v, want %v", tt.msg, got, tt.want)
			}
		})
	}
}

func TestKeyMapFoldBindingsMatch(t *testing.T) {
	k := defaultKeyMap

	tests := []struct {
		name    string
		msg     tea.KeyPressMsg
		binding key.Binding
		want    bool
	}{
		{"z matches FoldPrefix", keyPress('z'), k.FoldPrefix, true},
		{"a matches FoldToggle", keyPress('a'), k.FoldToggle, true},
		{"M matches FoldAll", keyPress('M'), k.FoldAll, true},
		{"R matches ExpandAll", keyPress('R'), k.ExpandAll, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := key.Matches(tt.msg, tt.binding)
			if got != tt.want {
				t.Errorf("key.Matches(%v, binding) = %v, want %v", tt.msg, got, tt.want)
			}
		})
	}
}

func TestKeyMapOverlayBindingsMatch(t *testing.T) {
	k := defaultKeyMap

	tests := []struct {
		name    string
		msg     tea.KeyPressMsg
		binding key.Binding
		want    bool
	}{
		{"? matches Help", keyPress('?'), k.Help, true},
		{"e matches Errors", keyPress('e'), k.Errors, true},
		{"q matches Quit", keyPress('q'), k.Quit, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := key.Matches(tt.msg, tt.binding)
			if got != tt.want {
				t.Errorf("key.Matches(%v, binding) = %v, want %v", tt.msg, got, tt.want)
			}
		})
	}
}

func TestKeyMapToggleModalBindingsMatch(t *testing.T) {
	k := defaultKeyMap

	tests := []struct {
		name    string
		msg     tea.KeyPressMsg
		binding key.Binding
		want    bool
	}{
		{"g matches ToggleGlobal", keyPress('g'), k.ToggleGlobal, true},
		{"p matches ToggleProject", keyPress('p'), k.ToggleProject, true},
		{"l matches ToggleLocal", keyPress('l'), k.ToggleLocal, true},
		{"e matches ToggleExclude", keyPress('e'), k.ToggleExclude, true},
		{"esc matches ToggleCancel", keyNamed("esc"), k.ToggleCancel, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := key.Matches(tt.msg, tt.binding)
			if got != tt.want {
				t.Errorf("key.Matches(%v, binding) = %v, want %v", tt.msg, got, tt.want)
			}
		})
	}
}

func TestKeyMapDetailBindingsMatch(t *testing.T) {
	k := defaultKeyMap

	tests := []struct {
		name    string
		msg     tea.KeyPressMsg
		binding key.Binding
		want    bool
	}{
		{"esc matches DetailBack", keyNamed("esc"), k.DetailBack, true},
		{"q matches DetailBack", keyPress('q'), k.DetailBack, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := key.Matches(tt.msg, tt.binding)
			if got != tt.want {
				t.Errorf("key.Matches(%v, binding) = %v, want %v", tt.msg, got, tt.want)
			}
		})
	}
}

func TestKeyMapSearchBindingsMatch(t *testing.T) {
	k := defaultKeyMap

	tests := []struct {
		name    string
		msg     tea.KeyPressMsg
		binding key.Binding
		want    bool
	}{
		{"enter matches SearchConfirm", keyNamed("enter"), k.SearchConfirm, true},
		{"backspace matches SearchBackspace", keyNamed("backspace"), k.SearchBackspace, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := key.Matches(tt.msg, tt.binding)
			if got != tt.want {
				t.Errorf("key.Matches(%v, binding) = %v, want %v", tt.msg, got, tt.want)
			}
		})
	}
}

func TestKeyMapConfirmBindingsMatch(t *testing.T) {
	k := defaultKeyMap

	tests := []struct {
		name    string
		msg     tea.KeyPressMsg
		binding key.Binding
		want    bool
	}{
		{"y matches ConfirmYes", keyPress('y'), k.ConfirmYes, true},
		{"n matches ConfirmNo", keyPress('n'), k.ConfirmNo, true},
		{"esc matches ConfirmCancel", keyNamed("esc"), k.ConfirmCancel, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := key.Matches(tt.msg, tt.binding)
			if got != tt.want {
				t.Errorf("key.Matches(%v, binding) = %v, want %v", tt.msg, got, tt.want)
			}
		})
	}
}

func TestKeyMapShortHelpReturnsBindings(t *testing.T) {
	k := defaultKeyMap
	bindings := k.ShortHelp()
	if len(bindings) == 0 {
		t.Fatal("ShortHelp should return at least one binding")
	}
	// Verify all returned bindings are enabled
	for _, b := range bindings {
		if !b.Enabled() {
			t.Errorf("ShortHelp binding %q should be enabled", b.Help().Key)
		}
	}
}

func TestKeyMapFullHelpReturnsGroups(t *testing.T) {
	k := defaultKeyMap
	groups := k.FullHelp()
	if len(groups) != 6 {
		t.Fatalf("FullHelp should return 6 groups (Navigation, Actions, Toggle, Harness, Edit Config, Other), got %d", len(groups))
	}
	// Verify each group has bindings
	names := []string{"Navigation", "Actions", "Toggle Modal", "Harness Modal", "Edit Config Modal", "Other"}
	for i, g := range groups {
		if len(g) == 0 {
			t.Fatalf("%s group should have at least one binding", names[i])
		}
	}
}

func TestHelpKeepsLongNavigationAliasesTogether(t *testing.T) {
	output := ansi.Strip(renderHelp(NewModel("/repo", "/home")))
	for _, want := range []string{
		"f/ctrl+f/pgdn page down",
		"b/ctrl+b/pgup page up",
	} {
		found := false
		for _, line := range strings.Split(output, "\n") {
			if strings.Contains(line, want) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("help entry %q was wrapped or missing:\n%s", want, output)
		}
	}
}

func TestKeyMapHelpTextConsistency(t *testing.T) {
	// Every binding should have non-empty help text.
	k := defaultKeyMap
	allBindings := []key.Binding{
		k.Up, k.Down, k.HalfPageDown, k.HalfPageUp, k.PageDown, k.PageUp, k.Top, k.Bottom, k.NextTab, k.PrevTab,
		k.Tab1, k.Tab2, k.Tab3,
		k.Search, k.Clear, k.Enter, k.Space, k.Collapse, k.Expand,
		k.Reload, k.Sync, k.Update, k.Harness, k.EditConfig,
		k.FoldPrefix, k.FoldToggle, k.FoldAll, k.ExpandAll,
		k.Help, k.Errors, k.Quit,
		k.ToggleGlobal, k.ToggleProject, k.ToggleLocal, k.ToggleExclude, k.ToggleCancel,
		k.HarnessGlobal, k.HarnessProject, k.HarnessSave, k.HarnessCancel,
		k.EditConfigGlobal, k.EditConfigProject, k.EditConfigLocal, k.EditConfigCancel,
		k.DetailBack,
		k.SearchConfirm, k.SearchBackspace,
		k.ConfirmYes, k.ConfirmNo, k.ConfirmCancel,
	}
	for _, b := range allBindings {
		help := b.Help()
		if help.Key == "" {
			t.Errorf("binding should have a non-empty help key, got empty for desc=%q", help.Desc)
		}
		if help.Desc == "" {
			t.Errorf("binding should have a non-empty help desc, got empty for key=%q", help.Key)
		}
	}
}

func TestKeyMapAllBindingsEnabled(t *testing.T) {
	k := defaultKeyMap
	allBindings := []key.Binding{
		k.Up, k.Down, k.HalfPageDown, k.HalfPageUp, k.PageDown, k.PageUp, k.Top, k.Bottom, k.NextTab, k.PrevTab,
		k.Tab1, k.Tab2, k.Tab3,
		k.Search, k.Clear, k.Enter, k.Space, k.Collapse, k.Expand,
		k.Reload, k.Sync, k.Update, k.Harness, k.EditConfig,
		k.FoldPrefix, k.FoldToggle, k.FoldAll, k.ExpandAll,
		k.Help, k.Errors, k.Quit,
		k.ToggleGlobal, k.ToggleProject, k.ToggleLocal, k.ToggleExclude, k.ToggleCancel,
		k.HarnessGlobal, k.HarnessProject, k.HarnessSave, k.HarnessCancel,
		k.EditConfigGlobal, k.EditConfigProject, k.EditConfigLocal, k.EditConfigCancel,
		k.DetailBack,
		k.SearchConfirm, k.SearchBackspace,
		k.ConfirmYes, k.ConfirmNo, k.ConfirmCancel,
	}
	for _, b := range allBindings {
		if !b.Enabled() {
			t.Errorf("binding %q should be enabled by default", b.Help().Key)
		}
	}
}
