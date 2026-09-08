package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func renderBundleTree(m Model, width, height int) string {
	if width < 1 || height < 1 {
		return ""
	}
	title := "Files"
	if m.pane == paneDetail {
		title = "> Files"
	}
	if m.bundle == nil {
		message := m.bundleError
		if message == "" {
			message = "Loading files…"
		}
		return fitTerminalRows(title+"\n"+message, width, height)
	}
	lines := make([]string, 0, height)
	lines = append(lines, ansi.Truncate(styleDetailLabel.Render(title+" · "+bundleTreeRootLabel(m.bundle)), width, ""))
	if m.bundle.rootErr != nil {
		lines = append(lines, "  Error: "+sanitizeTerminalData(m.bundle.rootErr.Error(), terminalSingleLine))
	} else if len(m.bundle.entries()) == 0 {
		lines = append(lines, "  (empty bundle)")
	}
	entries := m.bundle.entries()
	start := m.bundleOffset
	if start > len(entries) {
		start = len(entries)
	}
	for i := start; i < len(entries) && len(lines) < height; i++ {
		entry := entries[i]
		indent := strings.Repeat("  ", entry.Depth)
		glyph := "• "
		if entry.IsDir {
			glyph = "▸ "
			if entry.Expanded {
				glyph = "▾ "
			}
		}
		name := sanitizeTerminalData(entry.Name, terminalSingleLine)
		line := fmt.Sprintf("%s%s%s", indent, glyph, name)
		if entry.Err != nil {
			line += " [error]"
		}
		line = ansi.Truncate(line, width, "")
		if i == m.bundle.cursor && m.pane == paneDetail {
			line = styleSelected.Render(line)
		}
		lines = append(lines, line)
	}
	return lipgloss.NewStyle().Width(width).Height(height).Render(fitTerminalRows(strings.Join(lines, "\n"), width, height))
}
