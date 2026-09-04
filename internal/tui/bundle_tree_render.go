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
	if m.bundle == nil {
		return fitTerminalRows("bundle\nNo bundle tree available", width, height)
	}
	lines := make([]string, 0, height)
	lines = append(lines, styleDetailLabel.Render(bundleTreeRootLabel(m.bundle)))
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
		if i == m.bundle.cursor {
			line = styleSelected.Render(line)
		}
		lines = append(lines, line)
	}
	return lipgloss.NewStyle().Width(width).Height(height).Render(strings.Join(lines, "\n"))
}
