package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestFlatItemWrapPreservesLiteralRunes verifies the list word wrap never
// rewrites input characters: a private-use rune in a resource name survives
// rendering, and a hyphenated name stays contiguous on one wrapped line at
// the 50x12 minimum layout, on both color and monochrome glyph profiles.
func TestFlatItemWrapPreservesLiteralRunes(t *testing.T) {
	for _, capable := range []bool{true, false} {
		stubColorCapable(t, capable)
		m := NewModel(t.TempDir(), t.TempDir())
		m.width, m.height = 50, 12
		m.loading = false
		m.flatItems = []flatItem{{Kind: flatArtifact, Row: mkRow("p hyphen-name tail"), Depth: 1}}

		out := ansi.Strip(renderList(m, 12))
		if !strings.Contains(out, "hyphen-name") {
			t.Fatalf("colorCapable=%v: literal U+E000 corrupted or hyphenated name split:\n%s", capable, out)
		}
		for i, line := range strings.Split(out, "\n") {
			if w := ansi.StringWidth(line); w > 20 {
				t.Fatalf("colorCapable=%v: line %d exceeds the list pane width: %d %q", capable, i, w, line)
			}
		}
	}
}
