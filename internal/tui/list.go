package tui

import (
	"fmt"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/skill"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/cellbuf"
)

var (
	styleSectionHeader = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.AdaptiveColor{Light: "#1A8AAF", Dark: "#66D9EF"})

	styleSectionCount = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "#888888", Dark: "#888888"})
)

// renderList renders the left pane using the precomputed m.flatItems slice.
// Section headers get a ▼/▶ glyph plus count suffix; skill rows get the
// existing glyph + name rendering. The cursor highlights either a section
// header or a skill row depending on what it points to.
func renderList(m Model, visibleHeight int) string {
	if m.loading {
		return styleListEmpty.Render("Loading resources...")
	}

	if len(m.flatItems) == 0 {
		if m.filterActive && m.filter != "" {
			return styleListEmpty.Render("No resources match your search")
		}
		return styleListEmpty.Render("No resources found")
	}

	listWidth, _ := paneWidths(m.width)
	if visibleHeight < 1 || listWidth < 1 {
		return ""
	}
	var b strings.Builder
	renderedHeight := 0
	for i := m.listOffset; i < len(m.flatItems); i++ {
		line := formatFlatItem(m.flatItems[i], listWidth)
		if i == m.cursor {
			selectedStyle := styleSelected
			if m.pane == paneDetail {
				selectedStyle = selectedStyle.UnsetBackground().UnsetForeground().Faint(true)
			}
			line = selectedStyle.Render(line)
		} else {
			line = stylizeFlatItem(m.flatItems[i], listWidth)
		}
		line, itemHeight := fitFlatItem(m.flatItems[i], line, listWidth)
		if renderedHeight > 0 && renderedHeight+itemHeight > visibleHeight {
			break
		}
		b.WriteString(line)
		b.WriteString("\n")
		renderedHeight += itemHeight
		if renderedHeight >= visibleHeight {
			break
		}
	}

	return b.String()
}

func flatItemHeight(item flatItem, width int) int {
	_, height := fitFlatItem(item, formatFlatItem(item, width), width)
	return height
}

// fitFlatItem bounds one rendered flat item to the pane width. Section
// headers are truncated to a single line (fitSectionTitle already reserved
// room for the fold glyph and count) so the count never lands on a stray
// wrapped line; resource rows word-wrap so long names stay fully readable.
func fitFlatItem(it flatItem, line string, width int) (string, int) {
	if width < 1 {
		return "", 0
	}
	if it.Kind == flatSection {
		return ansi.Truncate(line, width, ""), 1
	}
	if ansi.StringWidth(line) <= width {
		return line, 1
	}
	wrapped := cellbuf.Wrap(line, width, "")
	return wrapped, strings.Count(wrapped, "\n") + 1
}

// fitSectionTitle truncates a section title so indent + fold glyph + title
// + "  (count)" fit on one line, keeping the count attached to its header.
func fitSectionTitle(title string, depth, count, width int) string {
	title = sanitizeTerminalData(title, terminalSingleLine)
	fixed := depth*2 + 2 + ansi.StringWidth(fmt.Sprintf("  (%d)", count))
	budget := width - fixed
	if budget < 1 {
		return ""
	}
	return ansi.Truncate(title, budget, "…")
}

// formatFlatItem returns the unstyled text for a flat item, used for the
// selected (highlighted) row. Sections indent by SectionDepth, skill rows
// indent by Depth (which buildTree sets to SectionDepth+1 of the enclosing
// section).
func formatFlatItem(it flatItem, width int) string {
	switch it.Kind {
	case flatSection:
		indent := strings.Repeat("  ", it.SectionDepth)
		glyph := "▼"
		if it.Collapsed {
			glyph = "▶"
		}
		return fmt.Sprintf("%s%s %s  (%d)", indent, glyph, fitSectionTitle(it.SectionTitle, it.SectionDepth, it.SectionCount, width), it.SectionCount)
	case flatArtifact:
		indent := strings.Repeat("  ", it.Depth)
		r := it.Row
		g := r.Glyph()
		name := resourceLabel(r)
		badge := originBadge(r)
		badgeSuffix := ""
		if badge != "" {
			badgeSuffix = " " + badge
		}
		harnessSuffix := ""
		if hb := harnessBadge(r); hb != "" {
			harnessSuffix = " " + hb
		}
		return fmt.Sprintf("%s%s %s%s%s", indent, g, name, badgeSuffix, harnessSuffix)
	}
	return ""
}

// stylizeFlatItem applies per-element colors for unselected rows.
func stylizeFlatItem(it flatItem, width int) string {
	switch it.Kind {
	case flatSection:
		indent := strings.Repeat("  ", it.SectionDepth)
		glyph := "▼"
		if it.Collapsed {
			glyph = "▶"
		}
		title := styleSectionHeader.Render(fmt.Sprintf("%s %s", glyph, fitSectionTitle(it.SectionTitle, it.SectionDepth, it.SectionCount, width)))
		count := styleSectionCount.Render(fmt.Sprintf("  (%d)", it.SectionCount))
		return indent + title + count
	case flatArtifact:
		indent := strings.Repeat("  ", it.Depth)
		r := it.Row
		g := r.Glyph()
		name := resourceLabel(r)
		glyphRendered := glyphStyle(g).Render(g)
		badge := originBadge(r)
		badgeSuffix := ""
		if badge != "" {
			badgeSuffix = " " + badgeStyle(badge).Render(badge)
		}
		harnessSuffix := ""
		if hb := harnessBadge(r); hb != "" {
			harnessSuffix = " " + harnessBadgeStyle().Render(hb)
		}
		return fmt.Sprintf("%s%s %s%s%s", indent, glyphRendered, name, badgeSuffix, harnessSuffix)
	}
	return formatFlatItem(it, width)
}

func resourceLabel(r row) string {
	return sanitizeTerminalData(skill.KindLabel(r.Artifact.Kind)+" "+r.Artifact.Name, terminalSingleLine)
}
