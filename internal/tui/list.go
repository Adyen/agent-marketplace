package tui

import (
	"fmt"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/skill"
	"github.com/charmbracelet/lipgloss"
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

	if visibleHeight < 1 {
		visibleHeight = 1
	}

	start := m.listOffset
	end := start + visibleHeight
	if end > len(m.flatItems) {
		end = len(m.flatItems)
	}

	var b strings.Builder
	for i := start; i < end; i++ {
		line := formatFlatItem(m.flatItems[i])
		if i == m.cursor {
			b.WriteString(styleSelected.Render(line))
		} else {
			b.WriteString(stylizeFlatItem(m.flatItems[i], line))
		}
		b.WriteString("\n")
	}

	return b.String()
}

// formatFlatItem returns the unstyled text for a flat item, used for the
// selected (highlighted) row. Sections indent by SectionDepth, skill rows
// indent by Depth (which buildTree sets to SectionDepth+1 of the enclosing
// section).
func formatFlatItem(it flatItem) string {
	switch it.Kind {
	case flatSection:
		indent := strings.Repeat("  ", it.SectionDepth)
		glyph := "▼"
		if it.Collapsed {
			glyph = "▶"
		}
		return fmt.Sprintf("%s%s %s  (%d)", indent, glyph, it.SectionTitle, it.SectionCount)
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
func stylizeFlatItem(it flatItem, plain string) string {
	switch it.Kind {
	case flatSection:
		indent := strings.Repeat("  ", it.SectionDepth)
		glyph := "▼"
		if it.Collapsed {
			glyph = "▶"
		}
		title := styleSectionHeader.Render(fmt.Sprintf("%s %s", glyph, it.SectionTitle))
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
	return plain
}

func resourceLabel(r row) string {
	return skill.KindLabel(r.Artifact.Kind) + " " + r.Artifact.Name
}
