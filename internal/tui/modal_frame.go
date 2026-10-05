package tui

import (
	"image/color"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	modalMaxInnerWidth = 72
	// modalFrameRows is the rows every modal spends outside its body: the
	// top and bottom border and the hint row.
	modalFrameRows = 3
)

// modalInnerWidth is the content width available inside a modal frame: the
// terminal width minus the border, one column of padding on each side, and
// a margin so the frame never touches the terminal edge.
func modalInnerWidth(m Model) int {
	if m.width <= 0 {
		return modalMaxInnerWidth
	}
	return max(1, min(modalMaxInnerWidth, m.width-8))
}

// modalFrame renders every modal the same way: a rounded border in the
// given color, one column of padding, the title, the body lines, and a
// single hint row. Body text wraps and the title is truncated to the
// frame so the border stays complete; body rows that would push the hint
// row off-screen are dropped from the end.
func modalFrame(m Model, border color.Color, title string, body []string, hints []footerHint) string {
	inner := modalInnerWidth(m)
	lines := make([]string, 0, len(body)+2)
	lines = append(lines, ansi.Truncate(title, inner, "…"))
	for _, b := range body {
		lines = append(lines, strings.Split(ansi.Wrap(b, inner, ""), "\n")...)
	}
	maxBody := max(1, m.height-modalFrameRows)
	if m.height > 0 && len(lines) > maxBody {
		lines = lines[:maxBody]
	}
	// Separate the hints from the body when the terminal has a row to spare.
	if m.height <= 0 || len(lines) < maxBody {
		lines = append(lines, "")
	}
	items := make([]footerItem, len(hints))
	for i, h := range hints {
		items[i] = hintItem(h.key, h.desc, i, false)
	}
	lines = append(lines, layoutFooter(inner, items))
	width := 0
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, inner, "…")
		width = max(width, ansi.StringWidth(lines[i]))
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Padding(0, 1).
		Width(width + 4).
		Render(strings.Join(lines, "\n"))
}

// footerHint is a key and what it does, shown in a modal's hint row.
type footerHint struct{ key, desc string }

// shortenPath fits a path into width by replacing its middle with an
// ellipsis while keeping the file name visible; the home directory is
// shown as ~.
func shortenPath(path, home string, width int) string {
	if home != "" {
		if rel, err := filepath.Rel(home, path); err == nil && !strings.HasPrefix(rel, "..") {
			path = filepath.Join("~", rel)
		}
	}
	path = sanitizeTerminalData(path, terminalSingleLine)
	if ansi.StringWidth(path) <= width {
		return path
	}
	name := filepath.Base(path)
	if ansi.StringWidth(name)+2 >= width {
		return ansi.Truncate(name, width, "…")
	}
	head := width - ansi.StringWidth(name) - 2
	return ansi.Truncate(path, head, "") + "…/" + name
}
