package tui

import (
	"image/color"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"
	"github.com/charmbracelet/colorprofile"
)

// palette holds the background-independent ANSI-16 colors shared by every
// model and by the package-level styles below. Mapping roles onto the
// terminal's ANSI-16 palette lets the TUI inherit the user's own colors.
var (
	paletteAccent    = lipgloss.ANSIColor(14)
	paletteOK        = lipgloss.ANSIColor(10)
	paletteWarn      = lipgloss.ANSIColor(11)
	paletteErr       = lipgloss.ANSIColor(9)
	paletteMuted     = lipgloss.ANSIColor(8)
	paletteSurface   = lipgloss.ANSIColor(8)
	paletteOnSurface = lipgloss.ANSIColor(15)
)

// theme owns every TUI color as semantic roles mapped onto the terminal's
// ANSI-16 palette, so the interface inherits the user's terminal colors.
// Only roles that depend on the background (mark, text) carry a
// light/dark pair; the rest are fixed ANSI-16 indexes.
type theme struct {
	dark    bool
	accent  color.Color // informational emphasis, prompts, borders
	ok      color.Color // synced / enabled / affirmative
	warn    color.Color // pending / stale / caution
	mark    color.Color // key-letter marks inside modal hints
	err     color.Color // broken / malformed / failures
	muted   color.Color // dim hints, secondary text
	text    color.Color // readable body text on the surface
	surface color.Color // raised chrome background (header, selection, overlays)
	onSurf  color.Color // readable text on the surface
	onAcc   color.Color // readable text on an accent fill (focused buttons)
}

// newTheme builds the semantic ANSI-16 theme for one terminal background.
// Each model keeps the theme matching the background it was told about via
// tea.BackgroundColorMsg, so differently-backgrounded models never share
// style state; tests use it directly for a deterministic theme.
func newTheme(isDark bool) theme {
	ld := lipgloss.LightDark(isDark)
	return theme{
		dark:    isDark,
		accent:  paletteAccent,
		ok:      paletteOK,
		warn:    paletteWarn,
		mark:    ld(lipgloss.ANSIColor(3), lipgloss.ANSIColor(10)),
		err:     paletteErr,
		muted:   paletteMuted,
		text:    ld(lipgloss.ANSIColor(0), lipgloss.ANSIColor(15)),
		surface: paletteSurface,
		onSurf:  paletteOnSurface,
		onAcc:   lipgloss.ANSIColor(0),
	}
}

// currentTheme returns the theme bound to this model's reported terminal
// background. Zero-value models fall back to the detected background so
// test scaffolding constructed without NewModel still renders.
func (m Model) currentTheme() theme {
	if m.theme.accent == nil {
		return newTheme(m.isDark || compat.HasDarkBackground)
	}
	return m.theme
}

// Style factories for the background-dependent roles. Every color decision
// lives here so renderers share the semantic theme instead of restating
// inline colors.
func textStyle(t theme) lipgloss.Style    { return lipgloss.NewStyle().Foreground(t.text) }
func accentStyle(t theme) lipgloss.Style  { return lipgloss.NewStyle().Foreground(t.accent) }
func titleStyle(t theme) lipgloss.Style   { return lipgloss.NewStyle().Bold(true).Foreground(t.accent) }
func markKeyStyle(t theme) lipgloss.Style { return lipgloss.NewStyle().Foreground(t.mark).Bold(true) }
func okStyle(t theme) lipgloss.Style      { return lipgloss.NewStyle().Foreground(t.ok) }
func okBoldStyle(t theme) lipgloss.Style  { return lipgloss.NewStyle().Bold(true).Foreground(t.ok) }
func errStyle(t theme) lipgloss.Style     { return lipgloss.NewStyle().Foreground(t.err) }
func dimTextStyle(t theme) lipgloss.Style { return lipgloss.NewStyle().Foreground(t.muted) }
func warnBoldStyle(t theme) lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(t.warn)
}
func fullKeyStyle(t theme) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(t.mark).Width(13)
}

// huhTheme adapts the model's semantic ANSI-16 theme onto Huh form chrome so
// modal forms inherit the same roles as the rest of the TUI instead of the
// generic base palette.
func huhTheme(t theme) *huh.Styles {
	s := huh.ThemeBase(t.dark)
	button := lipgloss.NewStyle().Padding(0, 2).MarginRight(1)

	// The modal frame owns the border; a second focus bar inside it would
	// duplicate the option cursor.
	s.Focused.Base = lipgloss.NewStyle()
	s.Focused.Card = s.Focused.Base
	s.Focused.Title = s.Focused.Title.Foreground(t.accent).Bold(true)
	s.Focused.NoteTitle = s.Focused.NoteTitle.Foreground(t.accent).Bold(true)
	s.Focused.Description = s.Focused.Description.Foreground(t.muted)
	s.Focused.ErrorIndicator = s.Focused.ErrorIndicator.Foreground(t.err)
	s.Focused.ErrorMessage = s.Focused.ErrorMessage.Foreground(t.err)
	s.Focused.SelectSelector = s.Focused.SelectSelector.Foreground(t.accent)
	s.Focused.NextIndicator = s.Focused.NextIndicator.Foreground(t.accent)
	s.Focused.PrevIndicator = s.Focused.PrevIndicator.Foreground(t.accent)
	s.Focused.Option = s.Focused.Option.Foreground(t.text)
	s.Focused.MultiSelectSelector = s.Focused.MultiSelectSelector.Foreground(t.accent)
	s.Focused.SelectedOption = s.Focused.SelectedOption.Foreground(t.ok)
	s.Focused.SelectedPrefix = s.Focused.SelectedPrefix.Foreground(t.ok)
	s.Focused.UnselectedOption = s.Focused.UnselectedOption.Foreground(t.text)
	s.Focused.UnselectedPrefix = s.Focused.UnselectedPrefix.Foreground(t.muted)
	s.Focused.FocusedButton = button.Foreground(t.onAcc).Background(t.accent).Bold(true)
	s.Focused.Next = s.Focused.FocusedButton
	s.Focused.BlurredButton = button.Foreground(t.text).Background(t.surface)
	s.Focused.TextInput.Cursor = s.Focused.TextInput.Cursor.Foreground(t.ok)
	s.Focused.TextInput.Placeholder = s.Focused.TextInput.Placeholder.Foreground(t.muted)
	s.Focused.TextInput.Prompt = s.Focused.TextInput.Prompt.Foreground(t.accent)
	s.Focused.TextInput.Text = s.Focused.TextInput.Text.Foreground(t.text)

	s.Blurred = s.Focused
	s.Blurred.Card = s.Blurred.Base
	s.Blurred.MultiSelectSelector = lipgloss.NewStyle().SetString("  ")
	s.Blurred.NextIndicator = lipgloss.NewStyle()
	s.Blurred.PrevIndicator = lipgloss.NewStyle()

	s.Group.Title = s.Focused.Title
	s.Group.Description = s.Focused.Description
	return s
}

// colorCapable reports whether the detected terminal profile can render the
// semantic colors; no-color profiles keep the readable ASCII status glyphs.
// It is a package seam so tests can pin a fixed profile.
var colorCapable = func() bool {
	return lipgloss.Writer.Profile > colorprofile.Ascii
}

var (
	styleHeader = lipgloss.NewStyle().
			Bold(true).
			Foreground(paletteOnSurface).
			Background(paletteSurface).
			Padding(0, 1)

	styleGlyphSynced   = lipgloss.NewStyle().Foreground(paletteOK)
	styleGlyphPending  = lipgloss.NewStyle().Foreground(paletteWarn)
	styleGlyphStale    = lipgloss.NewStyle().Foreground(paletteErr)
	styleGlyphBroken   = lipgloss.NewStyle().Foreground(paletteErr)
	styleGlyphRequired = lipgloss.NewStyle().Foreground(paletteAccent)
	styleGlyphOrphaned = lipgloss.NewStyle().Foreground(paletteMuted)
	styleGlyphDefault  = lipgloss.NewStyle().Foreground(paletteMuted)

	styleSelected = lipgloss.NewStyle().
			Foreground(paletteOnSurface).
			Background(paletteSurface)

	styleDetailLabel = lipgloss.NewStyle().
				Bold(true).
				Foreground(paletteAccent)

	styleToast = lipgloss.NewStyle().
			Foreground(paletteWarn).
			Padding(0, 1)

	styleDriftOK      = lipgloss.NewStyle().Foreground(paletteOK)
	styleDriftPending = lipgloss.NewStyle().Foreground(paletteWarn)

	styleHelpOverlay = lipgloss.NewStyle().
				Foreground(paletteOnSurface).
				Background(paletteSurface).
				Padding(1, 2)

	styleSearchPrompt = lipgloss.NewStyle().
				Foreground(paletteAccent)

	styleHelpSection = lipgloss.NewStyle().
				Bold(true).
				Foreground(paletteAccent)

	styleErrorTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(paletteErr)

	styleWarningTitle = lipgloss.NewStyle().
				Bold(true).
				Foreground(paletteWarn)

	styleWarningMsg = lipgloss.NewStyle().
			Foreground(paletteMuted)

	styleConfirmTitle = lipgloss.NewStyle().
				Bold(true).
				Foreground(paletteWarn)

	styleFooterPending    = lipgloss.NewStyle().Foreground(paletteWarn)
	styleFooterRefreshing = lipgloss.NewStyle().Foreground(paletteAccent)
	styleFooterError      = lipgloss.NewStyle().Foreground(paletteErr)
	styleFooterWarning    = lipgloss.NewStyle().Foreground(paletteWarn)

	shortcutKeyStyle  = lipgloss.NewStyle().Foreground(paletteMuted)
	shortcutDescStyle = lipgloss.NewStyle().Foreground(paletteMuted)
	shortcutSepStyle  = lipgloss.NewStyle().Foreground(paletteMuted)

	helpFullSepStyle = lipgloss.NewStyle().
				Foreground(paletteMuted)

	styleDim = lipgloss.NewStyle().
			Foreground(paletteMuted)

	styleListEmpty = lipgloss.NewStyle().Foreground(paletteMuted)

	styleSectionHeader = lipgloss.NewStyle().
				Bold(true).
				Foreground(paletteAccent)

	styleSectionCount = lipgloss.NewStyle().
				Foreground(paletteMuted)

	styleSearchMatch = lipgloss.NewStyle().
				Bold(true).
				Underline(true)

	styleBadgeWarn  = lipgloss.NewStyle().Foreground(paletteWarn)
	styleBadgeMulti = lipgloss.NewStyle().Bold(true).Foreground(paletteAccent)

	styleSpinner = lipgloss.NewStyle().Foreground(paletteWarn)
)

func glyphStyle(glyph string) lipgloss.Style {
	switch glyph {
	case "[x]", "●":
		return styleGlyphSynced
	case "[*]", "◐":
		return styleGlyphPending
	case "[~]":
		return styleGlyphStale
	case "[-]", "◌":
		return styleGlyphOrphaned
	case "[!]", "✗":
		return styleGlyphBroken
	case "[R]", "■":
		return styleGlyphRequired
	default:
		return styleGlyphDefault
	}
}

// statusSymbol renders the row's authoritative status as a styled symbol on
// color-capable profiles and its ASCII glyph on monochrome terminals.
func statusSymbol(r row) string {
	st := r.status()
	if !colorCapable() {
		return st.glyph
	}
	return st.symbol
}
