package tui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type previewSelectionView struct {
	identity string
	x0, y0   int
	w, h     int
	lines    []string
	maskX    int
	maskY1   int
}

type previewSelectionMouseMsg struct {
	msg  tea.Msg
	snap previewSelectionView
}

type selectionCell struct{ x, y int }

type previewSelection struct {
	snap    previewSelectionView
	anchor  selectionCell
	end     selectionCell
	active  bool
	dragged bool
	copied  bool
}

func (m Model) previewSelectionRegion() (x0, y0, w, h int, ok bool) {
	if m.belowMinimumTerminal() || m.palette.open {
		return 0, 0, 0, 0, false
	}
	listWidth, rightWidth := paneWidths(m.width)
	switch m.pane {
	case paneList:
		layout := screenLayoutFor(m.height, m.searchRowVisible())
		if !layout.header || layout.mainHeight <= 0 || rightWidth < 2 {
			return 0, 0, 0, 0, false
		}
		y0 := 1
		if layout.search {
			y0++
		}
		return listWidth + 1, y0, rightWidth - 1, layout.mainHeight, true
	case paneDetail:
		return listWidth, 0, rightWidth - 1, max(1, m.height-1), true
	case paneErrors:
		return 0, 0, m.width, m.height - 1, true
	}
	return 0, 0, 0, 0, false
}

func (m Model) previewSelectionIdentity() string {
	if _, _, _, _, ok := m.previewSelectionRegion(); !ok {
		return ""
	}
	switch m.pane {
	case paneList:
		key := ""
		if r, ok := m.selectedRow(); ok {
			key = rowKey(r)
		}
		return "list\x00" + strconv.Itoa(m.width) + "\x00" + strconv.Itoa(m.height) + "\x00" + key
	case paneDetail:
		return "detail\x00" + strconv.Itoa(m.width) + "\x00" + strconv.Itoa(m.height) + "\x00" +
			m.detailKey + "\x00" + m.detailSelectedPath + "\x00" + strconv.Itoa(m.detailViewport.YOffset())
	case paneErrors:
		return "errors\x00" + strconv.Itoa(m.width) + "\x00" + strconv.Itoa(m.height) + "\x00" +
			strconv.Itoa(m.errorsViewport.YOffset())
	}
	return ""
}

func (m Model) previewSelectionSnapshot(frame string) previewSelectionView {
	snap := previewSelectionView{identity: m.previewSelectionIdentity(), maskX: -1}
	if snap.identity == "" {
		return previewSelectionView{maskX: -1}
	}
	x0, y0, w, h, _ := m.previewSelectionRegion()
	snap.x0, snap.y0, snap.w, snap.h = x0, y0, w, h
	rows := strings.Split(frame, "\n")
	for y := y0; y < y0+h && y < len(rows); y++ {
		snap.lines = append(snap.lines, ansi.Cut(rows[y], x0, x0+w))
	}
	if toasts := m.layeredToasts(); len(toasts) > 0 && m.pane == paneList {
		stack := toastStackLayer(toasts)
		snap.maskX = max(0, m.width-lipgloss.Width(stack))
		snap.maskY1 = 1 + lipgloss.Height(stack)
	}
	return snap
}

func (s previewSelectionView) contains(x, y int) bool {
	return s.identity != "" && x >= s.x0 && x < s.x0+s.w && y >= s.y0 && y < s.y0+s.h
}

func (s previewSelectionView) masked(x, y int) bool {
	return s.maskX >= 0 && x >= s.maskX && y >= 1 && y < s.maskY1
}

func (s previewSelectionView) onErrorsText(x, y int) bool {
	local := y - s.y0
	if local < 0 || local >= len(s.lines) {
		return false
	}
	text := strings.TrimRight(ansi.Strip(s.lines[local]), " ")
	return x >= s.x0 && x-s.x0 < ansi.StringWidth(text)
}

func (s previewSelectionView) clamp(x, y int) selectionCell {
	return selectionCell{
		x: min(max(x, s.x0), s.x0+s.w-1),
		y: min(max(y, s.y0), s.y0+s.h-1),
	}
}

func (s previewSelectionView) equivalent(o previewSelectionView) bool {
	if s.identity != o.identity || s.x0 != o.x0 || s.y0 != o.y0 ||
		s.w != o.w || s.h != o.h || s.maskX != o.maskX || s.maskY1 != o.maskY1 ||
		len(s.lines) != len(o.lines) {
		return false
	}
	for i := range s.lines {
		if ansi.Strip(s.lines[i]) != ansi.Strip(o.lines[i]) {
			return false
		}
	}
	return true
}

type displayCell struct {
	col   int
	width int
	text  string
	raw   string
}

func splitDisplayCells(line string) (cells []displayCell, tail string) {
	var pending strings.Builder
	var state byte
	col := 0
	rest := line
	for len(rest) > 0 {
		seq, w, n, next := ansi.DecodeSequence(rest, state, nil)
		if n <= 0 {
			break
		}
		state = next
		rest = rest[n:]
		if w <= 0 && ansi.StringWidth(seq) == 0 {
			pending.WriteString(seq)
			continue
		}
		raw := pending.String() + seq
		pending.Reset()
		cells = append(cells, displayCell{col: col, width: w, text: ansi.Strip(raw), raw: raw})
		col += w
	}
	return cells, pending.String()
}

func (s previewSelectionView) selectedRange(anchor, end selectionCell) (first, last int, cols func(y int) (int, int)) {
	ay, by := anchor.y, end.y
	ax, bx := anchor.x, end.x
	if ay > by || (ay == by && ax > bx) {
		ay, by, ax, bx = by, ay, bx, ax
	}
	cols = func(y int) (int, int) {
		lo, hi := s.x0, s.x0+s.w-1
		if y == ay {
			lo = max(lo, ax)
		}
		if y == by {
			hi = min(hi, bx)
		}
		return lo, hi
	}
	return ay, by, cols
}

func (s previewSelectionView) extract(anchor, end selectionCell) string {
	first, last, cols := s.selectedRange(anchor, end)
	var b strings.Builder
	for y := first; y <= last; y++ {
		local := y - s.y0
		var line strings.Builder
		if local >= 0 && local < len(s.lines) {
			lo, hi := cols(y)
			cells, _ := splitDisplayCells(s.lines[local])
			for _, c := range cells {
				gx := s.x0 + c.col
				if c.col+c.width-1 < lo-s.x0 || c.col > hi-s.x0 || s.masked(gx, y) {
					continue
				}
				line.WriteString(c.text)
			}
		}
		b.WriteString(strings.TrimRight(line.String(), " "))
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

func reassertReverse(raw string) string {
	raw = strings.ReplaceAll(raw, "\x1b[0m", "\x1b[0m\x1b[7m")
	return strings.ReplaceAll(raw, "\x1b[m", "\x1b[m\x1b[7m")
}

func (m Model) applyPreviewSelection(content string) string {
	sel := m.previewSel
	if !sel.active || (!sel.dragged && !sel.copied) {
		return content
	}
	if sel.snap.identity == "" || sel.snap.identity != m.previewSelectionIdentity() {
		return content
	}
	snap := sel.snap
	first, last, cols := snap.selectedRange(sel.anchor, sel.end)
	lines := strings.Split(content, "\n")
	for y := first; y <= last && y < len(lines); y++ {
		local := y - snap.y0
		if local < 0 || local >= snap.h {
			continue
		}
		lo, hi := cols(y)
		cells, tail := splitDisplayCells(lines[y])
		var b strings.Builder
		for _, c := range cells {
			if c.col+c.width-1 < lo || c.col > hi || snap.masked(c.col, y) {
				b.WriteString(c.raw)
				continue
			}
			b.WriteString("\x1b[7m")
			b.WriteString(reassertReverse(c.raw))
			b.WriteString("\x1b[27m")
		}
		b.WriteString(tail)
		lines[y] = b.String()
	}
	return strings.Join(lines, "\n")
}

func (m *Model) invalidateStaleSelection() {
	if m.previewSel.active && m.previewSel.snap.identity != m.previewSelectionIdentity() {
		m.previewSel = previewSelection{}
	}
}

func (m Model) handlePreviewSelection(msg previewSelectionMouseMsg) (tea.Model, tea.Cmd) {
	snap := msg.snap
	if snap.identity == "" || snap.identity != m.previewSelectionIdentity() {
		m.previewSel = previewSelection{}
		return m, nil
	}
	if ev, ok := msg.msg.(tea.MouseMsg); ok && ev.Mouse().Button != tea.MouseLeft {
		if !m.previewSel.copied {
			m.previewSel = previewSelection{}
		}
		return m, nil
	}
	switch ev := msg.msg.(type) {
	case tea.MouseClickMsg:
		if !m.previewSel.copied {
			m.previewSel = previewSelection{}
		}
		if ev.Mod != 0 || !snap.contains(ev.X, ev.Y) || snap.masked(ev.X, ev.Y) {
			return m, nil
		}
		if m.pane == paneErrors && !snap.onErrorsText(ev.X, ev.Y) {
			return m, nil
		}
		m.previewSel = previewSelection{snap: snap, anchor: selectionCell{ev.X, ev.Y}, end: selectionCell{ev.X, ev.Y}, active: true}
		return m, nil
	case tea.MouseMotionMsg:
		if !m.previewSel.active || m.previewSel.copied {
			return m, nil
		}
		m.previewSel.end = snap.clamp(ev.X, ev.Y)
		if m.previewSel.end != m.previewSel.anchor {
			m.previewSel.dragged = true
		}
		return m, nil
	case tea.MouseReleaseMsg:
		sel := m.previewSel
		if !sel.active {
			return m, nil
		}
		if !sel.dragged {
			m.previewSel = previewSelection{}
			return m, nil
		}
		m.previewSel.dragged = false
		m.previewSel.copied = true
		return m, tea.SetClipboard(snap.extract(sel.anchor, sel.end))
	}
	return m, nil
}
