package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// overlayAt composites the top string on top of base at position (col, row),
// line-by-line. Both arguments may contain ANSI escape codes; ansi-aware
// slicing is used so styling on each side is preserved.
//
// col is measured in visual cells from the left; row is measured in lines
// from the top. Lines of top that fall outside the base are silently
// clipped. If a base line is shorter than col, it is right-padded with
// spaces up to col before splicing.
func overlayAt(base, top string, col, row int) string {
	if top == "" {
		return base
	}
	baseLines := strings.Split(base, "\n")
	topLines := strings.Split(top, "\n")

	for i, tl := range topLines {
		r := row + i
		if r < 0 || r >= len(baseLines) {
			continue
		}
		bl := baseLines[r]
		tw := ansi.StringWidth(tl)

		// Build left prefix of the base line up to `col` visual cells.
		left := ansi.Truncate(bl, col, "")
		leftW := ansi.StringWidth(left)
		if leftW < col {
			left += strings.Repeat(" ", col-leftW)
		}

		// Everything to the right of the overlay.
		right := ansi.TruncateLeft(bl, col+tw, "")

		baseLines[r] = left + tl + right
	}
	return strings.Join(baseLines, "\n")
}

// centerOverlay places top centered horizontally and vertically within a box
// of width x height cells, overlaid on base. Useful for modal dialogs.
func centerOverlay(base, top string, width, height int) string {
	topW := 0
	topH := 0
	for _, line := range strings.Split(top, "\n") {
		topH++
		if w := ansi.StringWidth(line); w > topW {
			topW = w
		}
	}

	col := 0
	if width > topW {
		col = (width - topW) / 2
	}
	row := 0
	if height > topH {
		row = (height - topH) / 2
	}
	return overlayAt(base, top, col, row)
}
