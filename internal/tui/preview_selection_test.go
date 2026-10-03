package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func selectionDetailModel(t testing.TB, lines ...string) Model {
	t.Helper()
	m := NewModel(t.TempDir(), t.TempDir())
	m.width, m.height, m.loading, m.pane = 80, 24, false, paneDetail
	m.driftAssessment = driftReady
	m.detailViewport = newTUIViewport(80, 23)
	m.detailViewport.SetContent(strings.Join(lines, "\n"))
	return m
}

func selectionListModel(t testing.TB, r row) Model {
	t.Helper()
	m := modelWithSelectedRow(t, r)
	m.width, m.height = 80, 24
	m.driftAssessment = driftReady
	return m
}

func selSnap(m Model) previewSelectionView {
	return m.previewSelectionSnapshot(m.View().Content)
}

func selSend(t testing.TB, m Model, snap previewSelectionView, msg tea.MouseMsg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(previewSelectionMouseMsg{msg: msg, snap: snap})
	return next.(Model), cmd
}

func selClick(x, y int) tea.MouseMsg   { return tea.MouseClickMsg{Button: tea.MouseLeft, X: x, Y: y} }
func selMotion(x, y int) tea.MouseMsg  { return tea.MouseMotionMsg{Button: tea.MouseLeft, X: x, Y: y} }
func selRelease(x, y int) tea.MouseMsg { return tea.MouseReleaseMsg{Button: tea.MouseLeft, X: x, Y: y} }

func selDrag(t testing.TB, m Model, snap previewSelectionView, x0, y0, x1, y1 int) (Model, tea.Cmd) {
	t.Helper()
	m, cmd := selSend(t, m, snap, selClick(x0, y0))
	if cmd != nil {
		t.Fatalf("press at (%d,%d) scheduled %v", x0, y0, cmd)
	}
	m, cmd = selSend(t, m, snap, selMotion(x1, y1))
	if cmd != nil {
		t.Fatalf("motion scheduled %v", cmd)
	}
	return selSend(t, m, snap, selRelease(x1, y1))
}

func clipboardText(t testing.TB, cmd tea.Cmd) string {
	t.Helper()
	if cmd == nil {
		return ""
	}
	msg := cmd()
	return fmt.Sprintf("%s", msg)
}

func highlightCols(t testing.TB, view string, row int) (start, end int) {
	t.Helper()
	lines := strings.Split(view, "\n")
	if row < 0 || row >= len(lines) {
		t.Fatalf("row %d out of %d view lines", row, len(lines))
	}
	line := lines[row]
	open := strings.Index(line, "\x1b[7m")
	close := strings.LastIndex(line, "\x1b[27m")
	if open < 0 || close < 0 || close < open {
		t.Fatalf("row %d has no reverse-video highlight: %q", row, line)
	}
	start = ansi.StringWidth(ansi.Strip(line[:open]))
	end = ansi.StringWidth(ansi.Strip(line[:close]))
	return start, end
}

func cellOf(t testing.TB, view, sub string) (x, y int) {
	t.Helper()
	for i, line := range strings.Split(view, "\n") {
		if idx := strings.Index(ansi.Strip(line), sub); idx >= 0 {
			return ansi.StringWidth(ansi.Strip(line)[:idx]), i
		}
	}
	t.Fatalf("view does not contain %q:\n%s", sub, view)
	return 0, 0
}

// spec: tui-mouse-navigation/forward-and-backward-drags-copy-displayed-unicode-text
func TestPreviewSelectionCopiesDisplayedText(t *testing.T) {
	for _, tt := range []struct {
		name      string
		lines     []string
		x0, y0    int
		x1, y1    int
		want      string
		wantHLRow int
		wantHLLo  int
		wantHLHi  int
	}{
		{name: "forward single line", lines: []string{"alpha beta", "second line"}, x0: 32, y0: 0, x1: 36, y1: 0, want: "alpha", wantHLRow: 0, wantHLLo: 32, wantHLHi: 37},
		{name: "backward single line", lines: []string{"alpha beta", "second line"}, x0: 36, y0: 0, x1: 32, y1: 0, want: "alpha", wantHLRow: 0, wantHLLo: 32, wantHLHi: 37},
		{name: "forward multiline", lines: []string{"alpha", "beta", "omega"}, x0: 34, y0: 0, x1: 33, y1: 1, want: "pha\nbe", wantHLRow: 0, wantHLLo: 34, wantHLHi: 79},
		{name: "backward multiline", lines: []string{"alpha", "beta", "omega"}, x0: 33, y0: 1, x1: 34, y1: 0, want: "pha\nbe", wantHLRow: 0, wantHLLo: 34, wantHLHi: 79},
		{name: "cjk wide glyphs", lines: []string{"中文字x tail"}, x0: 32, y0: 0, x1: 35, y1: 0, want: "中文", wantHLRow: 0, wantHLLo: 32, wantHLHi: 36},
		{name: "combining accent", lines: []string{"e\u0301clair rest"}, x0: 32, y0: 0, x1: 34, y1: 0, want: "e\u0301cl", wantHLRow: 0, wantHLLo: 32, wantHLHi: 35},
		{name: "emoji", lines: []string{"a😀b tail"}, x0: 33, y0: 0, x1: 34, y1: 0, want: "😀", wantHLRow: 0, wantHLLo: 33, wantHLHi: 35},
		{name: "styled ansi contents", lines: []string{"\x1b[31mred text\x1b[0m plain"}, x0: 32, y0: 0, x1: 34, y1: 0, want: "red", wantHLRow: 0, wantHLLo: 32, wantHLHi: 35},
		{name: "trailing layout padding stripped", lines: []string{"pad   "}, x0: 32, y0: 0, x1: 78, y1: 0, want: "pad", wantHLRow: 0, wantHLLo: 32, wantHLHi: 79},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := selectionDetailModel(t, tt.lines...)
			snap := selSnap(m)
			got, cmd := selDrag(t, m, snap, tt.x0, tt.y0, tt.x1, tt.y1)
			if text := clipboardText(t, cmd); text != tt.want {
				t.Fatalf("copied %q, want %q", text, tt.want)
			}
			if !got.previewSel.copied {
				t.Fatal("release did not leave a copied selection highlight")
			}
			lo, hi := highlightCols(t, got.View().Content, tt.wantHLRow)
			if lo != tt.wantHLLo || hi != tt.wantHLHi {
				t.Fatalf("highlight covers columns [%d,%d), want [%d,%d)", lo, hi, tt.wantHLLo, tt.wantHLHi)
			}
		})
	}
}

// spec: tui-mouse-navigation/forward-and-backward-drags-copy-displayed-unicode-text
func TestPreviewSelectionCopiesOverviewDetail(t *testing.T) {
	r := mkRow("overview-skill", withGlobal)
	r.Artifact.Description = "UNIQUE-OVERVIEW-TEXT-9"
	m := selectionListModel(t, r)
	view := ansi.Strip(m.View().Content)
	x0, y0 := cellOf(t, view, "UNIQUE-OVERVIEW-TEXT-9")
	snap := selSnap(m)
	got, cmd := selDrag(t, m, snap, x0+8, y0, x0, y0)
	if text := clipboardText(t, cmd); text != "UNIQUE-OV" {
		t.Fatalf("copied %q, want %q", text, "UNIQUE-OV")
	}
	if !got.previewSel.copied {
		t.Fatal("overview release did not leave a copied selection")
	}
	if lo, hi := highlightCols(t, got.View().Content, y0); lo != x0 || hi != x0+9 {
		t.Fatalf("overview highlight covers [%d,%d), want [%d,%d)", lo, hi, x0, x0+9)
	}
}

// spec: tui-mouse-navigation/gestures-outside-selectable-text-do-not-copy
func TestPreviewSelectionIgnoresOutsideGestures(t *testing.T) {
	line := "preview payload line"

	newDetail := func(t *testing.T) Model { return selectionDetailModel(t, line) }

	cases := []struct {
		name      string
		base      func(t *testing.T) Model
		seq       func(m Model, snap previewSelectionView) []tea.MouseMsg
		checkNoHL bool
	}{
		{name: "list pane", base: func(t *testing.T) Model { return selectionListModel(t, mkRow("res", withGlobal)) },
			seq: func(m Model, s previewSelectionView) []tea.MouseMsg {
				return []tea.MouseMsg{selClick(5, 5), selMotion(8, 5), selRelease(8, 5)}
			}},
		{name: "tree margin", base: newDetail,
			seq: func(m Model, s previewSelectionView) []tea.MouseMsg {
				return []tea.MouseMsg{selClick(5, 5), selMotion(40, 5), selRelease(40, 5)}
			}},
		{name: "footer row", base: newDetail,
			seq: func(m Model, s previewSelectionView) []tea.MouseMsg {
				return []tea.MouseMsg{selClick(40, 23), selMotion(50, 23), selRelease(50, 23)}
			}},
		{name: "header row", base: func(t *testing.T) Model { return selectionListModel(t, mkRow("res", withGlobal)) },
			seq: func(m Model, s previewSelectionView) []tea.MouseMsg {
				return []tea.MouseMsg{selClick(40, 0), selMotion(50, 0), selRelease(50, 0)}
			}},
		{name: "search row", base: func(t *testing.T) Model {
			m := selectionListModel(t, mkRow("res", withGlobal))
			m.filter = "res"
			return m
		}, seq: func(m Model, s previewSelectionView) []tea.MouseMsg {
			return []tea.MouseMsg{selClick(40, 1), selMotion(50, 1), selRelease(50, 1)}
		}},
		{name: "palette open", checkNoHL: true, base: func(t *testing.T) Model {
			m := selectionListModel(t, mkRow("res", withGlobal))
			m.palette.open = true
			return m
		}, seq: func(m Model, s previewSelectionView) []tea.MouseMsg {
			return []tea.MouseMsg{selClick(40, 5), selMotion(50, 5), selRelease(50, 5)}
		}},
		{name: "modal pane", base: func(t *testing.T) Model {
			m := selectionListModel(t, mkRow("res", withGlobal))
			m.pane = paneToggleModal
			return m
		}, seq: func(m Model, s previewSelectionView) []tea.MouseMsg {
			return []tea.MouseMsg{selClick(40, 5), selMotion(50, 5), selRelease(50, 5)}
		}},
		{name: "help overlay", base: func(t *testing.T) Model {
			m := newDetail(t)
			m.pane = paneHelp
			return m
		}, seq: func(m Model, s previewSelectionView) []tea.MouseMsg {
			return []tea.MouseMsg{selClick(40, 5), selMotion(50, 5), selRelease(50, 5)}
		}},
		{name: "below minimum terminal", base: func(t *testing.T) Model {
			m := newDetail(t)
			m.width = 49
			return m
		}, seq: func(m Model, s previewSelectionView) []tea.MouseMsg {
			return []tea.MouseMsg{selClick(20, 5), selMotion(30, 5), selRelease(30, 5)}
		}},
		{name: "right button drag", base: newDetail,
			seq: func(m Model, s previewSelectionView) []tea.MouseMsg {
				return []tea.MouseMsg{tea.MouseClickMsg{Button: tea.MouseRight, X: 40, Y: 5}, tea.MouseMotionMsg{Button: tea.MouseRight, X: 50, Y: 5}, tea.MouseReleaseMsg{Button: tea.MouseRight, X: 50, Y: 5}}
			}},
		{name: "shift modified drag", base: newDetail,
			seq: func(m Model, s previewSelectionView) []tea.MouseMsg {
				return []tea.MouseMsg{tea.MouseClickMsg{Button: tea.MouseLeft, Mod: tea.ModShift, X: 40, Y: 5}, tea.MouseMotionMsg{Button: tea.MouseLeft, Mod: tea.ModShift, X: 50, Y: 5}, tea.MouseReleaseMsg{Button: tea.MouseLeft, X: 50, Y: 5}}
			}},
		{name: "click without drag", base: newDetail,
			seq: func(m Model, s previewSelectionView) []tea.MouseMsg {
				return []tea.MouseMsg{selClick(40, 5), selRelease(40, 5)}
			}},
		{name: "release without active selection", base: newDetail,
			seq: func(m Model, s previewSelectionView) []tea.MouseMsg { return []tea.MouseMsg{selRelease(40, 5)} }},
		{name: "toast covered text", base: func(t *testing.T) Model {
			m := selectionListModel(t, mkRow("res", withGlobal))
			m.pushToast("toast-cover-note-one", time.Minute)
			m.pushToast("toast-cover-note-two", time.Minute)
			return m
		}, seq: func(m Model, s previewSelectionView) []tea.MouseMsg {
			return []tea.MouseMsg{selClick(70, 1), selMotion(75, 1), selRelease(75, 1)}
		}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			m := tt.base(t)
			snap := selSnap(m)
			var cmd tea.Cmd
			for _, ev := range tt.seq(m, snap) {
				m, cmd = selSend(t, m, snap, ev)
				if cmd != nil {
					t.Fatalf("event %v scheduled clipboard command %v", ev, cmd)
				}
			}
			if m.previewSel.copied || m.previewSel.active {
				t.Fatalf("ignored gesture left selection state %+v", m.previewSel)
			}
			if !tt.checkNoHL && strings.Contains(m.View().Content, "\x1b[7m") {
				t.Fatal("ignored gesture produced a highlight")
			}
		})
	}

	t.Run("duplicate release does not copy twice", func(t *testing.T) {
		m := newDetail(t)
		snap := selSnap(m)
		m, cmd := selDrag(t, m, snap, 32, 0, 37, 0)
		if clipboardText(t, cmd) != "previe" {
			t.Fatalf("first release copied %q", clipboardText(t, cmd))
		}
		if _, cmd = selSend(t, m, selSnap(m), selRelease(37, 0)); cmd != nil {
			t.Fatalf("duplicate release scheduled %v", cmd)
		}
	})

	t.Run("overview region is a positive control", func(t *testing.T) {
		m := selectionListModel(t, mkRow("res", withGlobal))
		snap := selSnap(m)
		if _, cmd := selDrag(t, m, snap, 34, 2, 40, 2); cmd == nil {
			t.Fatal("in-region overview drag produced no clipboard command")
		}
	})
}

// spec: tui-mouse-navigation/gestures-outside-selectable-text-do-not-copy
func TestPreviewSelectionOutsidePressCancelsPendingGesture(t *testing.T) {
	for _, tt := range []struct {
		name string
		mod  tea.KeyMod
	}{
		{name: "unmodified", mod: tea.KeyMod(0)},
		{name: "shift", mod: tea.ModShift},
		{name: "ctrl", mod: tea.ModCtrl},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := selectionDetailModel(t, "alpha beta")
			s := selSnap(m)
			m, _ = selSend(t, m, s, selClick(33, 0))
			m, _ = selSend(t, m, s, tea.MouseClickMsg{Button: tea.MouseLeft, Mod: tt.mod, X: 4, Y: 0})
			m, _ = selSend(t, m, s, selMotion(5, 0))
			got, cmd := selSend(t, m, s, selRelease(5, 0))
			if cmd != nil {
				t.Fatalf("new list-side drag copied %q after earlier release was missed; want no clipboard write", clipboardText(t, cmd))
			}
			if got.previewSel.active {
				t.Fatal("ignored new gesture retained an unfinished preview anchor")
			}
		})
	}
}

// spec: tui-mouse-navigation/scrolling-clears-selection-without-changing-navigation
func TestPreviewSelectionClearsOnScroll(t *testing.T) {
	lines := make([]string, 30)
	for i := range lines {
		lines[i] = fmt.Sprintf("scroll-line-%02d", i)
	}
	m := selectionDetailModel(t, lines...)
	snap := selSnap(m)
	m, cmd := selDrag(t, m, snap, 33, 0, 40, 1)
	if clipboardText(t, cmd) == "" {
		t.Fatal("setup drag did not copy")
	}
	if !m.previewSel.copied {
		t.Fatal("setup drag left no selection")
	}

	next, _ := m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown, X: 60, Y: 5})
	m = next.(Model)
	if m.previewSel.active {
		t.Fatal("wheel left the selection active")
	}
	if m.detailViewport.YOffset() != 1 {
		t.Fatalf("wheel still scrolled to %d, want 1", m.detailViewport.YOffset())
	}
	if strings.Contains(m.View().Content, "\x1b[7m") {
		t.Fatal("view still renders a cleared highlight")
	}

	m, cmd = selDrag(t, m, selSnap(m), 33, 0, 40, 1)
	if clipboardText(t, cmd) == "" {
		t.Fatal("re-selection after scroll did not copy")
	}
	before := m.detailViewport.YOffset()
	next, _ = m.Update(keyPress('j'))
	m = next.(Model)
	if m.previewSel.active {
		t.Fatal("viewport scroll key left the selection active")
	}
	if m.detailViewport.YOffset() != before+1 {
		t.Fatalf("scroll key moved offset to %d, want %d", m.detailViewport.YOffset(), before+1)
	}

	m, cmd = selDrag(t, m, selSnap(m), 33, 0, 40, 1)
	if clipboardText(t, cmd) == "" {
		t.Fatal("re-selection for list-wheel control did not copy")
	}
	next, _ = m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp, X: 2, Y: 3})
	m = next.(Model)
	if m.previewSel.active {
		t.Fatal("list-side wheel left the selection active")
	}

	m, cmd = selDrag(t, m, selSnap(m), 33, 0, 40, 1)
	if clipboardText(t, cmd) == "" {
		t.Fatal("re-selection for horizontal-wheel control did not copy")
	}
	offset := m.detailViewport.YOffset()
	next, cmd = m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelRight, X: 60, Y: 5})
	if cmd != nil {
		t.Fatalf("horizontal wheel scheduled %v", cmd)
	}
	m = next.(Model)
	if !m.previewSel.active || !m.previewSel.copied {
		t.Fatal("ignored horizontal wheel cleared the selection")
	}
	if m.detailViewport.YOffset() != offset {
		t.Fatalf("ignored horizontal wheel scrolled to %d, want %d", m.detailViewport.YOffset(), offset)
	}
	if !strings.Contains(m.View().Content, "\x1b[7m") {
		t.Fatal("ignored horizontal wheel dropped the highlight")
	}
}

// spec: tui-mouse-navigation/selection-remains-safe-across-view-changes-and-exits
func TestPreviewSelectionSafetyAcrossChanges(t *testing.T) {
	detail := func(t *testing.T) Model {
		return selectionDetailModel(t, "first preview line", "second preview line")
	}

	t.Run("drag clamps to the pane", func(t *testing.T) {
		m := detail(t)
		_, cmd := selDrag(t, m, selSnap(m), 32, 0, 400, 400)
		want := "first preview line\nsecond preview line"
		if got := clipboardText(t, cmd); got != want {
			t.Fatalf("clamped drag copied %q, want %q", got, want)
		}
	})

	t.Run("resize clears", func(t *testing.T) {
		m := detail(t)
		m, cmd := selDrag(t, m, selSnap(m), 33, 0, 40, 0)
		if clipboardText(t, cmd) == "" {
			t.Fatal("setup drag did not copy")
		}
		next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
		if next.(Model).previewSel.active {
			t.Fatal("resize left the selection active")
		}
	})

	t.Run("resource change clears", func(t *testing.T) {
		m := detail(t)
		m.detailKey = "resource-a"
		snap := selSnap(m)
		m, cmd := selSend(t, m, snap, selClick(33, 0))
		if cmd != nil {
			t.Fatal("press scheduled a command")
		}
		m.detailKey = "resource-b"
		m, _ = selSend(t, m, snap, selMotion(40, 0))
		m, cmd = selSend(t, m, snap, selRelease(40, 0))
		if cmd != nil || m.previewSel.active {
			t.Fatal("stale identity drag copied or kept selection")
		}
	})

	t.Run("file change clears", func(t *testing.T) {
		m := detail(t)
		m.detailSelectedPath = "a.md"
		snap := selSnap(m)
		m, cmd := selSend(t, m, snap, selClick(33, 0))
		if cmd != nil {
			t.Fatal("press scheduled a command")
		}
		m.detailSelectedPath = "b.md"
		m, _ = selSend(t, m, snap, selMotion(40, 0))
		m, cmd = selSend(t, m, snap, selRelease(40, 0))
		if cmd != nil || m.previewSel.active {
			t.Fatal("file-change drag copied or kept selection")
		}
	})

	t.Run("accepted replacement content clears", func(t *testing.T) {
		m := detail(t)
		generation := m.beginRequest(requestDetail)
		m, cmd := selDrag(t, m, selSnap(m), 33, 0, 40, 0)
		if clipboardText(t, cmd) == "" {
			t.Fatal("setup drag did not copy")
		}
		next, _ := m.Update(detailContentMsg{generation: generation, content: "replacement", width: m.previewWidth()})
		m = next.(Model)
		if m.previewSel.active {
			t.Fatal("content replacement left the selection active")
		}
	})

	t.Run("escape clears before its normal action", func(t *testing.T) {
		m := detail(t)
		m, cmd := selDrag(t, m, selSnap(m), 33, 0, 40, 0)
		if clipboardText(t, cmd) == "" {
			t.Fatal("setup drag did not copy")
		}
		next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		m = next.(Model)
		if m.previewSel.active {
			t.Fatal("escape left the selection active")
		}
		if m.pane != paneDetail {
			t.Fatal("escape both cleared and exited the detail pane")
		}
		next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		m = next.(Model)
		if m.pane != paneList {
			t.Fatalf("second escape did not exit the detail pane: %d", m.pane)
		}
	})

	t.Run("ctrlc retains hard exit", func(t *testing.T) {
		m := detail(t)
		m, cmd := selDrag(t, m, selSnap(m), 33, 0, 40, 0)
		if clipboardText(t, cmd) == "" {
			t.Fatal("setup drag did not copy")
		}
		_, cmd = m.Update(ctrlCKey())
		if cmd == nil {
			t.Fatal("ctrl+c did not quit")
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatalf("ctrl+c produced %T, want tea.QuitMsg", cmd())
		}
	})
}

// spec: tui-mouse-navigation/selection-remains-safe-across-view-changes-and-exits
func TestFramePacedSelectionUsesPresentedFrame(t *testing.T) {
	model, clock := newFramePacedListModel(t, []string{"stale-frame-resource", "hidden-resource"})
	model.View()
	clock.now = clock.now.Add(time.Millisecond)
	model, _ = updateFramePaced(t, model, tea.MouseWheelMsg{Button: tea.MouseWheelDown, X: 2, Y: 3})
	if !model.framePending {
		t.Fatal("fixture did not leave a pending paced frame")
	}
	if inner := innerListModel(t, model); inner.cursor != 1 {
		t.Fatalf("inner cursor = %d, want 1", inner.cursor)
	}
	x, y := cellOf(t, ansi.Strip(model.View().Content), "selected detail stale-frame-resource")
	var cmd tea.Cmd
	for _, ev := range []tea.Msg{
		selClick(x+4, y), selMotion(x+10, y), selRelease(x+10, y),
	} {
		model, cmd = updateFramePaced(t, model, ev)
		if cmd != nil {
			t.Fatalf("stale-frame gesture scheduled %v", cmd)
		}
	}
	if inner := innerListModel(t, model); inner.previewSel.copied || inner.previewSel.active {
		t.Fatalf("hidden-resource press left selection state %+v", inner.previewSel)
	}
}

// spec: tui-mouse-navigation/selection-remains-safe-across-view-changes-and-exits
func TestFramePacedSelectionDropsOnStalePresentedFrame(t *testing.T) {
	drag := func(t *testing.T, model *framePacedModel, clock *framePacedClock) {
		t.Helper()
		model.View()
		var cmd tea.Cmd
		for _, ev := range []tea.Msg{selClick(60, 2), selMotion(75, 2), selRelease(75, 2)} {
			model, cmd = updateFramePaced(t, model, ev)
		}
		if cmd == nil {
			t.Fatal("overview drag did not copy")
		}
		if inner := innerListModel(t, model); !inner.previewSel.copied || !inner.previewSel.active {
			t.Fatalf("drag left selection %+v", inner.previewSel)
		}
	}
	release := func(t *testing.T, model *framePacedModel) {
		t.Helper()
		var cmd tea.Cmd
		model, cmd = updateFramePaced(t, model, selRelease(75, 2))
		if cmd != nil {
			t.Fatalf("release after invalidation scheduled %v", cmd)
		}
		if inner := innerListModel(t, model); inner.previewSel.active {
			t.Fatal("selection survived invalidation")
		}
		if strings.Contains(model.View().Content, "\x1b[7m") {
			t.Fatal("stale highlight remains after invalidation")
		}
	}

	t.Run("toast appearing after press drops the selection", func(t *testing.T) {
		model, clock := newFramePacedListModel(t, []string{"overlay-target"})
		drag(t, model, clock)
		inner := innerListModel(t, model)
		inner.pushToast("overlay-stale-toast", time.Minute)
		model.inner = inner
		clock.now = clock.now.Add(time.Millisecond)
		model.refresh(clock.now)
		release(t, model)
	})

	t.Run("same-identity visible content change drops the selection", func(t *testing.T) {
		model, clock := newFramePacedListModel(t, []string{"content-target"})
		drag(t, model, clock)
		inner := innerListModel(t, model)
		inner.flatItems[0].Row.Artifact.Description = "replaced visible overview text"
		model.inner = inner
		clock.now = clock.now.Add(time.Millisecond)
		model.refresh(clock.now)
		release(t, model)
	})
}
