package tui

import (
	"image/color"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// TestModelBackgroundThemesAreIsolated verifies each model keeps the theme
// implied by its own BackgroundColorMsg: two models reporting different
// backgrounds render background-dependent styles differently, and neither
// model's render changes when the other model's message arrives.
func TestModelBackgroundThemesAreIsolated(t *testing.T) {
	modelFor := func(dark bool) Model {
		m := NewModel(t.TempDir(), t.TempDir())
		bg := color.White
		if dark {
			bg = color.Black
		}
		updated, _ := m.Update(tea.BackgroundColorMsg{Color: bg})
		return updated.(Model)
	}
	keyHint := func(m Model) string {
		return m.help.Styles.FullKey.Render("g")
	}

	dark, light := modelFor(true), modelFor(false)
	for _, tc := range []struct {
		name  string
		model Model
		color lipgloss.ANSIColor
	}{
		{"dark", dark, lipgloss.ANSIColor(15)},
		{"light", light, lipgloss.ANSIColor(0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := tc.model.searchInput
			input.SetValue("query")
			input.Focus()
			want := lipgloss.NewStyle().Foreground(tc.color).Render("query")
			if got := input.View(); !strings.Contains(got, want) {
				t.Fatalf("search input = %q, missing readable text %q", got, want)
			}
		})
	}
	if keyHint(dark) == keyHint(light) {
		t.Fatalf("models with different backgrounds rendered the same key style: %q", keyHint(dark))
	}
	if want := lipgloss.NewStyle().Foreground(newTheme(true).mark).Width(13).Render("g"); keyHint(dark) != want {
		t.Fatalf("dark model key style = %q, want %q", keyHint(dark), want)
	}
	// A second model adopting the opposite background must not move the first.
	before := keyHint(dark)
	_ = modelFor(false)
	if keyHint(dark) != before {
		t.Fatalf("dark model key style changed after another model's message")
	}
	if after := keyHint(dark); after != lipgloss.NewStyle().Foreground(newTheme(true).mark).Width(13).Render("g") {
		t.Fatalf("dark model key style drifted: %q", after)
	}
}
