package tui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
)

const (
	minimumTerminalWidth  = 50
	minimumTerminalHeight = 12
)

func (m Model) belowMinimumTerminal() bool {
	return m.width < minimumTerminalWidth || m.height < minimumTerminalHeight
}

func compactTerminalLayout(m Model) bool {
	return m.width < 60 || m.height < 20
}

func compactModalStyle(style lipgloss.Style, m Model) lipgloss.Style {
	if compactTerminalLayout(m) {
		return style.Padding(0, 1)
	}
	return style
}

func fitTerminalRows(content string, width, height int) string {
	if width < 1 || height < 1 {
		return ""
	}
	lines := strings.Split(content, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], width, "")
	}
	return strings.Join(lines, "\n")
}

func fitTerminalWithFooter(content, footer string, width, height int) string {
	if width < 1 || height < 1 {
		return ""
	}
	footer = fitFooterGroups(footer, " · ", width)
	if height == 1 {
		return footer
	}
	body := fitTerminalRows(content, width, height-1)
	if body == "" {
		return footer
	}
	return body + "\n" + footer
}

func renderResizeRequired(m Model) string {
	body := strings.Join([]string{
		"Resize required",
		fmt.Sprintf("Minimum terminal: %dx%d", minimumTerminalWidth, minimumTerminalHeight),
		fmt.Sprintf("Current terminal: %dx%d", max(0, m.width), max(0, m.height)),
		"Resize to continue the current workflow.",
	}, "\n")
	return fitTerminalWithFooter(body, "q quit · Ctrl+C quit", m.width, m.height)
}

// probeTerminalSize reports the terminal's current dimensions. ok is false
// for non-terminal outputs (pipes, files) and failed or degenerate probes.
// It is a package-level seam so tests can substitute a deterministic size.
var probeTerminalSize = func(out *os.File) (width, height int, ok bool) {
	if out == nil || !term.IsTerminal(out.Fd()) {
		return 0, 0, false
	}
	w, h, err := term.GetSize(out.Fd())
	if err != nil || w < 1 || h < 1 {
		return 0, 0, false
	}
	return w, h, true
}

// seedTerminalSize initializes the model with the real terminal size when it
// can be determined before the first frame. Without it the model starts at
// the unknown size (below minimum) and the first frame renders the bounded
// resize-required state until Bubble Tea delivers the initial WindowSizeMsg.
func seedTerminalSize(m *Model, out *os.File) {
	m.width, m.height = 0, 0
	if w, h, ok := probeTerminalSize(out); ok {
		m.width, m.height = w, h
	}
}
