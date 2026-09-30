package tui

import (
	"image/color"

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
// Only roles that depend on the background (mark, text, card) carry a
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
	card    color.Color // floating modal background
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
		card:    ld(lipgloss.ANSIColor(15), lipgloss.ANSIColor(0)),
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

// modalBoxStyle builds the bordered floating-box style for modal panes. The
// card and text roles follow the model's reported background so the box
// reads clearly on top of the main two-pane layout.
func modalBoxStyle(t theme, border lipgloss.Border, borderColor color.Color) lipgloss.Style {
	return lipgloss.NewStyle().
		Border(border).
		BorderForeground(borderColor).
		Background(t.card).
		Foreground(t.text).
		Padding(1, 2)
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

// statusSymbol renders the row status as a styled symbol on color-capable
// profiles and the original ASCII glyph on monochrome terminals.
func statusSymbol(r row) string {
	if !colorCapable() {
		return r.Glyph()
	}
	switch r.Glyph() {
	case "[x]":
		return "●"
	case "[ ]":
		return "○"
	case "[*]":
		return "◐"
	case "[-]":
		return "◌"
	case "[!]":
		return "✗"
	case "[R]":
		return "■"
	default:
		return r.Glyph()
	}
}
