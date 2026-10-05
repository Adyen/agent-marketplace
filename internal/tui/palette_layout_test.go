package tui

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestPaletteLayoutShowsShortcutColumnCountAndHints(t *testing.T) {
	for _, size := range [][2]int{{50, 12}, {80, 24}, {120, 34}} {
		m := openPaletteFor(t, readySizedPaletteModel(size[0], size[1]))
		lines := strings.Split(ansi.Strip(m.viewContent()), "\n")
		assertTerminalBounds(t, m.viewContent(), size[0], size[1])
		if !strings.Contains(lines[0], "agent-marketplace") {
			t.Fatalf("%dx%d: the palette must not cover the header: %q", size[0], size[1], lines[0])
		}
		if !strings.Contains(lines[len(lines)-1], "q quit") {
			t.Fatalf("%dx%d: the palette must not cover the footer: %q", size[0], size[1], lines[len(lines)-1])
		}
		view := strings.Join(lines, "\n")
		for _, want := range []string{"Command palette", "enter run", "esc close"} {
			if !strings.Contains(view, want) {
				t.Fatalf("%dx%d: palette is missing %q:\n%s", size[0], size[1], want, view)
			}
		}
		var searchRow string
		for _, l := range lines {
			if strings.Contains(l, "Search resources") {
				searchRow = l
			}
		}
		if !shortcutColumn.MatchString(searchRow) {
			t.Fatalf("%dx%d: the shortcut must sit in its own right-aligned column, got %q", size[0], size[1], searchRow)
		}
		count := len(m.palette.matches)
		if !strings.Contains(view, "1/"+strconv.Itoa(count)) {
			t.Fatalf("%dx%d: palette must show the cursor position out of %d matches:\n%s", size[0], size[1], count, view)
		}
	}
}

// shortcutColumn matches a palette row whose shortcut is separated from
// its label and aligned against the right border.
var shortcutColumn = regexp.MustCompile(`Search resources {2,}/ ?│`)

// spec: tui-feedback/clear-search-from-the-palette
func TestPaletteClearSearchActionFollowsFilterState(t *testing.T) {
	m := readySizedPaletteModel(80, 24)
	m = openPaletteFor(t, m)
	for _, match := range m.palette.matches {
		if match.action.id == actionClearSearch {
			t.Fatalf("clear search must be absent without an applied filter")
		}
	}
	m.closePalette()

	m.filter = "a"
	m.recomputeView()
	m = openPaletteFor(t, m)
	m = typePaletteQuery(t, m, "clear")
	m, _ = submitPalette(t, m)
	if m.filter != "" || m.filterActive {
		t.Fatalf("clear search from the palette must clear the filter like esc; filter=%q active=%v", m.filter, m.filterActive)
	}
}
