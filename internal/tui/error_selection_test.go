package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func selectionErrorsModel(t testing.TB, width, height int, errs ...error) Model {
	t.Helper()
	m := NewModel(t.TempDir(), t.TempDir())
	m.width, m.height, m.loading = width, height, false
	m.driftAssessment = driftReady
	m.errs = errs
	next, _ := m.Update(keyPress('e'))
	m = next.(Model)
	if m.pane != paneErrors {
		t.Fatal("e did not open the errors screen")
	}
	return m
}

// spec: tui-mouse-navigation/forward-and-backward-drags-copy-wrapped-unicode-error-text
func TestErrorSelectionCopiesWrappedUnicode(t *testing.T) {
	unicodeErr := errors.New("alpha-中文字-😀-omega unicode wrap segment one segment two segment three tailend")
	for _, direction := range []string{"forward", "backward"} {
		t.Run(direction, func(t *testing.T) {
			m := selectionErrorsModel(t, 60, 24, unicodeErr)
			snap := selSnap(m)
			x0, y0, x1, y1 := 2, 2, 59, 3
			if direction == "backward" {
				x0, y0, x1, y1 = 20, 3, 2, 2
			}
			got, cmd := selDrag(t, m, snap, x0, y0, x1, y1)
			want := "alpha-中文字-😀-omega unicode wrap segment one segment two\nsegment three tailend"
			text := clipboardText(t, cmd)
			if text != want {
				t.Fatalf("copied %q, want %q", text, want)
			}
			if strings.Contains(text, "\x1b") || strings.ContainsRune(text, '\x00') {
				t.Fatalf("copied text contains control content %q", text)
			}
			if !got.previewSel.copied {
				t.Fatal("release did not leave a copied selection highlight")
			}
			lo, hi := highlightCols(t, got.View().Content, 2)
			if lo != 2 || hi != 60 {
				t.Fatalf("highlight covers columns [%d,%d), want [2,60)", lo, hi)
			}
		})
	}

	t.Run("out-of-body drag clips to the errors body", func(t *testing.T) {
		m := selectionErrorsModel(t, 60, 24, errors.New("first-clip-line"), errors.New("second-clip-line"))
		snap := selSnap(m)
		_, cmd := selDrag(t, m, snap, 0, 0, 90, 40)
		want := "Errors (2)\n\n" +
			"• first-clip-line\n" +
			"  → Run `agent-marketplace sync --check` in this workspace\n" +
			"for details. Fix the reported issue, then press `r` in the\n" +
			"resource list to reload.\n" +
			"• second-clip-line\n" +
			"  → Run `agent-marketplace sync --check` in this workspace\n" +
			"for details. Fix the reported issue, then press `r` in the\n" +
			"resource list to reload.\n\n" +
			"Press esc to close"
		if got := clipboardText(t, cmd); got != want {
			t.Fatalf("clipped drag copied %q, want %q", got, want)
		}
	})
}

// spec: tui-mouse-navigation/error-screen-gestures-outside-displayed-text-do-not-copy
func TestErrorSelectionIgnoresOutsideGestures(t *testing.T) {
	newErrors := func(t *testing.T) Model {
		return selectionErrorsModel(t, 80, 24, errors.New("err-payload-one"), errors.New("err-payload-two"))
	}

	cases := []struct {
		name string
		seq  []tea.MouseMsg
	}{
		{name: "click without drag", seq: []tea.MouseMsg{selClick(4, 2), selRelease(4, 2)}},
		{name: "release without active selection", seq: []tea.MouseMsg{selRelease(4, 2)}},
		{name: "footer start", seq: []tea.MouseMsg{selClick(4, 23), selMotion(10, 23), selRelease(10, 23)}},
		{name: "blank row start", seq: []tea.MouseMsg{selClick(4, 1), selMotion(10, 1), selRelease(10, 1)}},
		{name: "right padding start", seq: []tea.MouseMsg{selClick(40, 2), selMotion(50, 2), selRelease(50, 2)}},
		{name: "shift modified drag", seq: []tea.MouseMsg{
			tea.MouseClickMsg{Button: tea.MouseLeft, Mod: tea.ModShift, X: 4, Y: 2},
			tea.MouseMotionMsg{Button: tea.MouseLeft, Mod: tea.ModShift, X: 10, Y: 2},
			tea.MouseReleaseMsg{Button: tea.MouseLeft, X: 10, Y: 2}}},
		{name: "right button drag", seq: []tea.MouseMsg{
			tea.MouseClickMsg{Button: tea.MouseRight, X: 4, Y: 2},
			tea.MouseMotionMsg{Button: tea.MouseRight, X: 10, Y: 2},
			tea.MouseReleaseMsg{Button: tea.MouseRight, X: 10, Y: 2}}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			m := newErrors(t)
			snap := selSnap(m)
			for _, ev := range tt.seq {
				var cmd tea.Cmd
				m, cmd = selSend(t, m, snap, ev)
				if cmd != nil {
					t.Fatalf("event %v scheduled clipboard command %v", ev, cmd)
				}
			}
			if m.previewSel.copied || m.previewSel.active {
				t.Fatalf("ignored gesture left selection state %+v", m.previewSel)
			}
			if strings.Contains(m.View().Content, "\x1b[7m") {
				t.Fatal("ignored gesture produced a highlight")
			}
		})
	}

	t.Run("body drag is a positive control", func(t *testing.T) {
		m := newErrors(t)
		_, cmd := selDrag(t, m, selSnap(m), 2, 2, 16, 2)
		if clipboardText(t, cmd) != "err-payload-one" {
			t.Fatalf("control drag copied %q", clipboardText(t, cmd))
		}
	})
}

// spec: tui-mouse-navigation/scrolling-and-view-changes-cancel-error-screen-selection-and-preserve-exits
func TestErrorSelectionClearsOnViewChanges(t *testing.T) {
	overflowErrs := func() []error {
		errs := make([]error, 40)
		for i := range errs {
			errs[i] = fmt.Errorf("overflow-error-%02d", i)
		}
		return errs
	}
	newOverflow := func(t *testing.T) Model { return selectionErrorsModel(t, 80, 24, overflowErrs()...) }

	drag := func(t *testing.T, m Model) Model {
		t.Helper()
		m, cmd := selDrag(t, m, selSnap(m), 2, 2, 12, 2)
		if clipboardText(t, cmd) == "" {
			t.Fatal("setup drag did not copy")
		}
		if !m.previewSel.active {
			t.Fatal("setup drag left no selection")
		}
		return m
	}
	assertCleared := func(t *testing.T, m Model, what string) Model {
		t.Helper()
		if m.previewSel.active || m.previewSel.copied {
			t.Fatalf("%s left stale selection %+v", what, m.previewSel)
		}
		if strings.Contains(m.View().Content, "\x1b[7m") {
			t.Fatalf("%s left a stale highlight", what)
		}
		return m
	}

	scrolls := []struct {
		name    string
		prepare func(t *testing.T, m Model) Model
		msg     tea.Msg
		check   func(t *testing.T, m Model)
	}{
		{name: "wheel down", msg: tea.MouseWheelMsg{Button: tea.MouseWheelDown, X: 40, Y: 5},
			check: func(t *testing.T, m Model) {
				if m.errorsViewport.YOffset() != 1 {
					t.Fatalf("wheel down offset = %d, want 1", m.errorsViewport.YOffset())
				}
			}},
		{name: "wheel up", msg: tea.MouseWheelMsg{Button: tea.MouseWheelUp, X: 40, Y: 5},
			prepare: func(t *testing.T, m Model) Model {
				next, _ := m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown, X: 40, Y: 5})
				return next.(Model)
			},
			check: func(t *testing.T, m Model) {
				if m.errorsViewport.YOffset() != 0 {
					t.Fatalf("wheel up offset = %d, want 0", m.errorsViewport.YOffset())
				}
			}},
		{name: "line scroll", msg: keyPress('j'),
			check: func(t *testing.T, m Model) {
				if m.errorsViewport.YOffset() != 1 {
					t.Fatalf("line scroll offset = %d, want 1", m.errorsViewport.YOffset())
				}
			}},
		{name: "page scroll", msg: tea.KeyPressMsg{Code: tea.KeyPgDown},
			check: func(t *testing.T, m Model) {
				if m.errorsViewport.YOffset() == 0 {
					t.Fatal("page scroll did not move the viewport")
				}
			}},
		{name: "jump bottom", msg: keyPress('G'),
			check: func(t *testing.T, m Model) {
				if !m.errorsViewport.AtBottom() {
					t.Fatal("G did not reach the bottom")
				}
			}},
		{name: "jump top", msg: keyPress('g'),
			prepare: func(t *testing.T, m Model) Model {
				next, _ := m.Update(keyPress('G'))
				return next.(Model)
			},
			check: func(t *testing.T, m Model) {
				if m.errorsViewport.YOffset() != 0 {
					t.Fatalf("jump top offset = %d, want 0", m.errorsViewport.YOffset())
				}
			}},
	}
	for _, tt := range scrolls {
		t.Run(tt.name, func(t *testing.T) {
			m := newOverflow(t)
			if tt.prepare != nil {
				m = tt.prepare(t, m)
			}
			m = drag(t, m)
			next, _ := m.Update(tt.msg)
			m = assertCleared(t, next.(Model), tt.name)
			tt.check(t, m)
		})
	}

	t.Run("resize clears", func(t *testing.T) {
		m := drag(t, newOverflow(t))
		next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
		assertCleared(t, next.(Model), "resize")
	})

	t.Run("e closes and clears", func(t *testing.T) {
		m := drag(t, newOverflow(t))
		next, _ := m.Update(keyPress('e'))
		m = next.(Model)
		if m.pane != paneList {
			t.Fatalf("e left pane %d, want list", m.pane)
		}
		if m.previewSel.active || m.previewSel.copied {
			t.Fatalf("e close left stale selection %+v", m.previewSel)
		}
	})

	t.Run("escape clears then closes", func(t *testing.T) {
		m := drag(t, newOverflow(t))
		next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		m = next.(Model)
		if m.previewSel.active {
			t.Fatal("escape left the selection active")
		}
		if m.pane != paneErrors {
			t.Fatalf("first escape closed the errors screen: pane %d", m.pane)
		}
		next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		m = next.(Model)
		if m.pane != paneList {
			t.Fatalf("second escape did not close the errors screen: pane %d", m.pane)
		}
	})

	t.Run("ctrlc retains hard exit", func(t *testing.T) {
		m := drag(t, newOverflow(t))
		_, cmd := m.Update(ctrlCKey())
		if cmd == nil {
			t.Fatal("ctrl+c did not quit")
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatalf("ctrl+c produced %T, want tea.QuitMsg", cmd())
		}
	})
}

// spec: tui-mouse-navigation/scrolling-and-view-changes-cancel-error-screen-selection-and-preserve-exits
func TestErrorSelectionDropsOnReplacedDiagnostics(t *testing.T) {
	m := selectionErrorsModel(t, 80, 24, errors.New("original-diagnostic"))
	m.errorsViewport.SetContent(renderErrors(m))
	model := newFramePacedModel(m)
	model.View()
	var cmd tea.Cmd
	for _, ev := range []tea.Msg{selClick(2, 2), selMotion(14, 2), selRelease(14, 2)} {
		model, cmd = updateFramePaced(t, model, ev)
	}
	if cmd == nil {
		t.Fatal("errors drag did not copy")
	}
	if inner := model.inner.(Model); !inner.previewSel.copied || !inner.previewSel.active {
		t.Fatalf("drag left selection %+v", inner.previewSel)
	}
	inner := model.inner.(Model)
	inner.errs = []error{errors.New("replacement-diagnostic")}
	model.inner = inner
	model.refresh(time.Now())
	model, cmd = updateFramePaced(t, model, selRelease(14, 2))
	if cmd != nil {
		t.Fatalf("release after replacement scheduled %v", cmd)
	}
	if inner := model.inner.(Model); inner.previewSel.active {
		t.Fatal("selection survived diagnostic replacement")
	}
	if strings.Contains(model.View().Content, "\x1b[7m") {
		t.Fatal("stale highlight remains after diagnostic replacement")
	}
	if !strings.Contains(ansi.Strip(model.View().Content), "replacement-diagnostic") {
		t.Fatal("replacement diagnostics did not render")
	}
}
