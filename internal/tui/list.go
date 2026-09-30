package tui

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/Adyen/agent-marketplace/internal/skill"
	"github.com/charmbracelet/x/ansi"
)

// renderList renders the left pane using the precomputed m.flatItems slice.
// Section headers get a ▼/▶ glyph plus count suffix; skill rows get the
// existing glyph + name rendering. The cursor highlights either a section
// header or a skill row depending on what it points to.
func renderList(m Model, visibleHeight int) string {
	if m.loading && len(m.flatItems) == 0 {
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
	compact := compactTerminalLayout(m)
	for i := m.listOffset; i < len(m.flatItems); i++ {
		var line string
		var itemHeight int
		if i == m.cursor {
			// Wrap before styling: a whole-line SGR run has no breakable
			// spaces, so wrapping after styling would split words.
			line, itemHeight = fitFlatItem(m.flatItems[i], formatFlatItem(m.flatItems[i], listWidth, compact), listWidth)
			wrapped := strings.Split(line, "\n")
			for j, l := range wrapped {
				wrapped[j] = styleSelected.Render(l)
			}
			line = strings.Join(wrapped, "\n")
		} else {
			line = stylizeFlatItem(m.flatItems[i], listWidth, compact)
			line, itemHeight = fitFlatItem(m.flatItems[i], line, listWidth)
		}
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

func flatItemHeight(item flatItem, width int, compact bool) int {
	_, height := fitFlatItem(item, formatFlatItem(item, width, compact), width)
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
	wrapped := wrapFlatItemLine(line, width)
	return wrapped, strings.Count(wrapped, "\n") + 1
}

// wordRunes matches the non-space segments wrapFlatItemLine treats as
// unbreakable words, keeping every separator byte verbatim.
var wordRunes = regexp.MustCompile(`\S+`)

// wrapFlatItemLine wraps a rendered row at spaces only, so hyphenated names
// (and badges like "P-") stay contiguous and legible. It never substitutes
// input characters; a word that still exceeds the width (e.g. a name longer
// than the whole pane) falls back to a hard wrap to keep the output bounded.
func wrapFlatItemLine(line string, width int) string {
	var lines []string
	var cur strings.Builder
	curWidth := 0
	flush := func() {
		if cur.Len() > 0 {
			lines = append(lines, cur.String())
			cur.Reset()
			curWidth = 0
		}
	}
	pos := 0
	for _, loc := range wordRunes.FindAllStringIndex(line, -1) {
		sep, word := line[pos:loc[0]], line[loc[0]:loc[1]]
		pos = loc[1]
		wordWidth := ansi.StringWidth(word)
		if wordWidth > width {
			flush()
			segments := strings.Split(ansi.Hardwrap(word, width, false), "\n")
			lines = append(lines, segments[:len(segments)-1]...)
			cur.WriteString(segments[len(segments)-1])
			curWidth = ansi.StringWidth(segments[len(segments)-1])
			continue
		}
		if sepWidth := ansi.StringWidth(sep); cur.Len() > 0 && curWidth+sepWidth+wordWidth > width {
			flush()
		} else {
			cur.WriteString(sep)
			curWidth += sepWidth
		}
		cur.WriteString(word)
		curWidth += wordWidth
	}
	flush()
	return strings.Join(lines, "\n")
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
func formatFlatItem(it flatItem, width int, compact bool) string {
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
		g := statusSymbol(r)
		name := resourceLabel(r)
		badgeSuffix := scopeBadgeSuffix(r, compact, width-ansi.StringWidth(indent+g+" "+name))
		harnessSuffix := ""
		if hb := harnessBadge(r); hb != "" {
			harnessSuffix = " " + hb
		}
		return fmt.Sprintf("%s%s %s%s%s", indent, g, name, badgeSuffix, harnessSuffix)
	}
	return ""
}

// stylizeFlatItem applies per-element colors for unselected rows.
func stylizeFlatItem(it flatItem, width int, compact bool) string {
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
		g := statusSymbol(r)
		name := resourceLabel(r)
		if len(r.searchMatchRunes) > 0 {
			name = highlightNameMatches(r, name)
		}
		glyphRendered := glyphStyle(g).Render(g)
		badgeSuffix := scopeBadgeSuffix(r, compact, width-ansi.StringWidth(indent+g+" "+name))
		if badgeSuffix != "" {
			badgeSuffix = " " + badgeStyle(originBadge(r)).Render(strings.TrimPrefix(badgeSuffix, " "))
		}
		harnessSuffix := ""
		if hb := harnessBadge(r); hb != "" {
			harnessSuffix = " " + harnessBadgeStyle().Render(hb)
		}
		return fmt.Sprintf("%s%s %s%s%s", indent, glyphRendered, name, badgeSuffix, harnessSuffix)
	}
	return formatFlatItem(it, width, compact)
}

func resourceLabel(r row) string {
	return sanitizeTerminalData(skill.KindLabel(r.Artifact.Kind)+" "+r.Artifact.Name, terminalSingleLine)
}

// highlightNameMatches styles the matched name runes inside the sanitized
// label, shifting the name-relative indexes by the kind prefix width.
func highlightNameMatches(r row, label string) string {
	nameWidth := utf8.RuneCountInString(sanitizeTerminalData(r.Artifact.Name, terminalSingleLine))
	offset := utf8.RuneCountInString(label) - nameWidth
	if offset < 0 {
		return label
	}
	idx := make([]int, 0, len(r.searchMatchRunes))
	for _, ri := range r.searchMatchRunes {
		idx = append(idx, ri+offset)
	}
	return lipgloss.StyleRunes(label, idx, styleSearchMatch, lipgloss.NewStyle())
}
