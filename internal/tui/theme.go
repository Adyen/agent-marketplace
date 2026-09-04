package tui

import (
	"github.com/charmbracelet/lipgloss"
)

// AdaptiveColor picks Dark or Light depending on the terminal background.
var (
	styleHeader = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#FFFFFF"}).
			Background(lipgloss.AdaptiveColor{Light: "#4A4A4A", Dark: "#4A4A4A"}).
			Padding(0, 1)

	styleGlyphSynced   = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#5C8A1E", Dark: "#A6E22E"})
	styleGlyphPending  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#C47000", Dark: "#FD971F"})
	styleGlyphStale    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#D41B5C", Dark: "#F92672"})
	styleGlyphBroken   = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#D41B5C", Dark: "#F92672"})
	styleGlyphRequired = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#1A8AAF", Dark: "#66D9EF"})
	styleGlyphOrphaned = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#666666", Dark: "#888888"})
	styleGlyphDefault  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#666666", Dark: "#888888"})

	styleSelected = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#F8F8F2", Dark: "#F8F8F2"}).
			Background(lipgloss.AdaptiveColor{Light: "#4A4A4A", Dark: "#4A4A4A"})

	styleDetailLabel = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.AdaptiveColor{Light: "#1A8AAF", Dark: "#66D9EF"})

	styleDetailValue = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "#1A1A1A", Dark: "#F8F8F2"})

	styleToast = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#C47000", Dark: "#FD971F"}).
			Padding(0, 1)

	styleDriftOK      = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#5C8A1E", Dark: "#A6E22E"})
	styleDriftPending = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#C47000", Dark: "#FD971F"})

	styleHelpOverlay = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "#F8F8F2", Dark: "#F8F8F2"}).
				Background(lipgloss.AdaptiveColor{Light: "#333333", Dark: "#333333"}).
				Padding(1, 2)

	styleSearchPrompt = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "#1A8AAF", Dark: "#66D9EF"})

	// Help overlay section title
	styleHelpSection = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.AdaptiveColor{Light: "#1A8AAF", Dark: "#66D9EF"})

	styleErrorTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.AdaptiveColor{Light: "#D41B5C", Dark: "#F92672"})

	styleErrorMsg = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#1A1A1A", Dark: "#F8F8F2"})

	styleWarningTitle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.AdaptiveColor{Light: "#C47000", Dark: "#FD971F"})

	styleWarningMsg = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#666666", Dark: "#A6A68A"})

	styleConfirmTitle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.AdaptiveColor{Light: "#C47000", Dark: "#FD971F"})

	// Footer styles
	styleFooterPending    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#C47000", Dark: "#FD971F"})
	styleFooterRefreshing = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#1A8AAF", Dark: "#66D9EF"})
	styleFooterError      = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#D41B5C", Dark: "#F92672"})
	styleFooterWarning    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#C47000", Dark: "#FD971F"})

	// Footer shortcut styles (driven by help.Model)
	shortcutKeyStyle  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#888888", Dark: "#888888"})
	shortcutDescStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#888888", Dark: "#888888"})
	shortcutSepStyle  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#888888", Dark: "#888888"})

	// Full help overlay styles (driven by help.Model)
	helpFullKeyStyle = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "#A6A000", Dark: "#A6E22E"}).
				Width(13)
	helpFullDescStyle = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "#1A1A1A", Dark: "#F8F8F2"})
	helpFullSepStyle = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "#888888", Dark: "#888888"})

	// Dim text for hints like "Press esc or ? to close"
	styleDim = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#666666", Dark: "#888888"})

	// List styles
	styleListEmpty = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#666666", Dark: "#888888"})
)

func glyphStyle(glyph string) lipgloss.Style {
	switch glyph {
	case "[x]":
		return styleGlyphSynced
	case "[*]":
		return styleGlyphPending
	case "[~]":
		return styleGlyphStale
	case "[-]":
		return styleGlyphOrphaned
	case "[!]":
		return styleGlyphBroken
	case "[R]":
		return styleGlyphRequired
	default:
		return styleGlyphDefault
	}
}
