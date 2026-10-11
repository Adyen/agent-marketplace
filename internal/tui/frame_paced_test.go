package tui

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	tea "charm.land/bubbletea/v2"
)

type framePacedClock struct{ now time.Time }

func (c *framePacedClock) Now() time.Time { return c.now }

func newFramePacedListModel(t *testing.T, names []string) (*framePacedModel, *framePacedClock) {
	t.Helper()
	m := NewModel(t.TempDir(), t.TempDir())
	m.width, m.height, m.loading, m.pane = 80, 24, false, paneList
	m.driftAssessment = driftReady
	for _, name := range names {
		r := mkRow(name, withGlobal)
		r.Artifact.Description = "selected detail " + name
		m.flatItems = append(m.flatItems, flatItem{Kind: flatArtifact, Row: r, Depth: 1})
	}
	m.clampListOffset()
	clock := &framePacedClock{now: time.Unix(100, 0)}
	model := newFramePacedModel(m)
	model.clock = clock.Now
	return model, clock
}

func updateFramePaced(t *testing.T, model *framePacedModel, msg tea.Msg) (*framePacedModel, tea.Cmd) {
	t.Helper()
	next, cmd := model.Update(msg)
	wrapped, ok := next.(*framePacedModel)
	if !ok {
		t.Fatalf("updated model has type %T, want *framePacedModel", next)
	}
	return wrapped, cmd
}

func innerListModel(t *testing.T, model *framePacedModel) Model {
	t.Helper()
	inner, ok := model.inner.(Model)
	if !ok {
		t.Fatalf("wrapped inner model has type %T, want Model", model.inner)
	}
	return inner
}

// spec: tui-mouse-navigation/rapid-wheel-reversals-preserve-selection
func TestFramePacedListWheelOrder(t *testing.T) {
	names := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf("wrapped-resource-name-%02d-with-a-deliberately-long-label", i)
		}
		return out
	}
	down := tea.MouseWheelMsg{Button: tea.MouseWheelDown, X: 2, Y: 3}
	up := tea.MouseWheelMsg{Button: tea.MouseWheelUp, X: 2, Y: 3}

	t.Run("first boundary reversal", func(t *testing.T) {
		model, _ := newFramePacedListModel(t, names(12))
		model.View()
		for i := 0; i < 20; i++ {
			model, _ = updateFramePaced(t, model, up)
		}
		model, _ = updateFramePaced(t, model, down)
		if got := innerListModel(t, model).cursor; got != 1 {
			t.Fatalf("20 wheel-ups then one wheel-down at the first entry selected %d, want 1", got)
		}
	})

	t.Run("last boundary reversal", func(t *testing.T) {
		model, _ := newFramePacedListModel(t, names(12))
		model.View()
		for i := 0; i < 20; i++ {
			model, _ = updateFramePaced(t, model, down)
		}
		model, _ = updateFramePaced(t, model, up)
		if got := innerListModel(t, model).cursor; got != 10 {
			t.Fatalf("20 wheel-downs then one wheel-up at the last entry selected %d, want 10", got)
		}
	})

	t.Run("wrapped rows and alternating events", func(t *testing.T) {
		model, _ := newFramePacedListModel(t, names(30))
		model.View()
		sequence := []tea.MouseWheelMsg{
			down, down, up, down, down, down, up, up, down, down,
			down, up, down, down, down, down, up, down, down, up,
		}
		want := 0
		for _, msg := range sequence {
			if msg.Button == tea.MouseWheelDown {
				want++
			} else {
				want--
			}
			model, _ = updateFramePaced(t, model, msg)
		}
		if got := innerListModel(t, model).cursor; got != want {
			t.Fatalf("alternating wheel burst selected %d, want independently computed %d", got, want)
		}
	})

	t.Run("shift wheel and ignored controls keep routing", func(t *testing.T) {
		model, _ := newFramePacedListModel(t, names(12))
		model.View()
		model, _ = updateFramePaced(t, model, tea.MouseWheelMsg{Button: tea.MouseWheelDown, Mod: tea.ModShift, X: 2, Y: 3})
		if got := innerListModel(t, model).cursor; got != 1 {
			t.Fatalf("Shift+wheel-down selected %d, want 1", got)
		}
		for _, msg := range []tea.Msg{
			tea.MouseClickMsg{Button: tea.MouseLeft, X: 2, Y: 0},
			tea.MouseReleaseMsg{Button: tea.MouseLeft, X: 2, Y: 0},
			tea.MouseMotionMsg{Button: tea.MouseLeft, X: 4, Y: 4},
			tea.MouseWheelMsg{Button: tea.MouseWheelRight, X: 2, Y: 3},
		} {
			var cmd tea.Cmd
			model, cmd = updateFramePaced(t, model, msg)
			if cmd != nil {
				t.Fatalf("ignored mouse event %T scheduled a command", msg)
			}
			if model.framePending {
				t.Fatalf("ignored mouse event %T scheduled a paced frame", msg)
			}
		}
		inner := innerListModel(t, model)
		if inner.cursor != 1 {
			t.Fatalf("ignored mouse events moved the selection to %d, want 1", inner.cursor)
		}
	})
}

// spec: tui-mouse-navigation/final-selection-appears-after-a-wheel-burst
func TestFramePacedListTrailingView(t *testing.T) {
	model, clock := newFramePacedListModel(t, []string{"first-resource", "second-resource"})

	initial := model.View()
	if !strings.Contains(initial.Content, "selected detail first-resource") || strings.Contains(initial.Content, "selected detail second-resource") {
		t.Fatalf("initial frame does not describe the first resource:\n%s", initial.Content)
	}

	var scheduled int
	for i := 0; i < 3; i++ {
		var cmd tea.Cmd
		model, cmd = updateFramePaced(t, model, tea.MouseWheelMsg{Button: tea.MouseWheelDown, X: 2, Y: 3})
		if cmd != nil {
			scheduled++
		}
	}
	inner := innerListModel(t, model)
	if inner.cursor != 1 {
		t.Fatalf("inner cursor = %d after 3 wheel-downs, want 1 (clamped at last entry)", inner.cursor)
	}
	if scheduled != 1 || !model.framePending {
		t.Fatalf("scheduled frame commands = %d, pending = %t; want exactly one pending frame", scheduled, model.framePending)
	}
	if got := model.View(); got.Content != initial.Content {
		t.Fatal("cached frame changed inside one interval")
	}

	clock.now = clock.now.Add(time.Second / 60)
	model, cmd := updateFramePaced(t, model, framePacedTickMsg{generation: model.frameGeneration})
	if cmd != nil {
		t.Fatal("trailing frame tick returned a command")
	}
	if model.framePending {
		t.Fatal("trailing frame tick left a pending frame")
	}
	view := model.View()
	if strings.Contains(view.Content, "selected detail first-resource") || !strings.Contains(view.Content, "selected detail second-resource") {
		t.Fatalf("trailing frame does not show the newest selected resource:\n%s", view.Content)
	}
	if !view.AltScreen || view.MouseMode != tea.MouseModeCellMotion {
		t.Fatalf("trailing frame lost view metadata: alt-screen=%v mouse-mode=%v", view.AltScreen, view.MouseMode)
	}
}

// spec: tui-mouse-navigation/final-selection-appears-after-a-wheel-burst
func TestFramePacedListTickCommand(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		first := mkRow("tick-first", withGlobal)
		first.Artifact.Description = "selected detail tick-first"
		second := mkRow("tick-second", withGlobal)
		second.Artifact.Description = "selected detail tick-second"
		m := NewModel(t.TempDir(), t.TempDir())
		m.width, m.height, m.loading, m.pane = 80, 24, false, paneList
		m.driftAssessment = driftReady
		m.flatItems = []flatItem{
			{Kind: flatArtifact, Row: first, Depth: 1},
			{Kind: flatArtifact, Row: second, Depth: 1},
		}

		model := newFramePacedModel(m)
		model.clock = time.Now
		model.View()

		time.Sleep(time.Millisecond)
		next, cmd := model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown, X: 2, Y: 3})
		wrapped, ok := next.(*framePacedModel)
		if !ok {
			t.Fatalf("updated model has type %T, want *framePacedModel", next)
		}
		model = wrapped
		if cmd == nil {
			t.Fatal("paced wheel did not return the frame command")
		}

		start := time.Now()
		msg := cmd()
		if elapsed := time.Since(start); elapsed != time.Second/60-time.Millisecond {
			t.Fatalf("frame command waited %v, want %v", elapsed, time.Second/60-time.Millisecond)
		}
		tick, ok := msg.(framePacedTickMsg)
		if !ok {
			t.Fatalf("frame command produced %T, want framePacedTickMsg", msg)
		}
		if tick.generation != model.frameGeneration {
			t.Fatalf("tick generation = %d, want %d", tick.generation, model.frameGeneration)
		}

		next, cmd = model.Update(tick)
		model = next.(*framePacedModel)
		if cmd != nil {
			t.Fatal("frame tick scheduled a recurring command")
		}
		if model.framePending {
			t.Fatal("frame tick left a pending frame")
		}
		if got := model.View().Content; !strings.Contains(got, "selected detail tick-second") {
			t.Fatalf("repaint does not show the newest selection:\n%s", got)
		}
	})
}

// spec: tui-mouse-navigation/non-wheel-updates-supersede-pending-frames
func TestFramePacedListImmediateUpdates(t *testing.T) {
	build := func(t *testing.T) (*framePacedModel, *framePacedClock) {
		return newFramePacedListModel(t, []string{"entry-0", "entry-1", "entry-2", "entry-3", "entry-4"})
	}

	for _, tc := range []struct {
		name string
		msg  func(t *testing.T, model *framePacedModel) tea.Msg
		want func(t *testing.T, model *framePacedModel)
	}{
		{
			name: "key action",
			msg:  func(t *testing.T, model *framePacedModel) tea.Msg { return keyPress('G') },
			want: func(t *testing.T, model *framePacedModel) {
				inner := innerListModel(t, model)
				if inner.cursor != 4 {
					t.Fatalf("G selected %d, want the last entry 4", inner.cursor)
				}
				content := model.View().Content
				if !strings.Contains(content, "selected detail entry-4") {
					t.Fatalf("post-key view does not show the target selection:\n%s", content)
				}
				if strings.Contains(content, "selected detail entry-0") {
					t.Fatalf("post-key view still shows the stale selection:\n%s", content)
				}
			},
		},
		{
			name: "resize",
			msg: func(t *testing.T, model *framePacedModel) tea.Msg {
				return tea.WindowSizeMsg{Width: 120, Height: 30}
			},
			want: func(t *testing.T, model *framePacedModel) {
				inner := innerListModel(t, model)
				if inner.width != 120 || inner.height != 30 {
					t.Fatalf("resize applied width=%d height=%d, want 120x30", inner.width, inner.height)
				}
				if got, fresh := model.View().Content, model.inner.View().Content; got != fresh {
					t.Fatal("view did not refresh to the resized state immediately")
				}
			},
		},
		{
			name: "accepted snapshot",
			msg: func(t *testing.T, model *framePacedModel) tea.Msg {
				inner := innerListModel(t, model)
				generation := inner.beginRequest(requestSnapshot)
				model.inner = inner
				r := mkRow("snapshot-resource", withGlobal)
				r.Artifact.Description = "independent snapshot detail"
				return snapshotLoadedMsg{
					generation:      generation,
					rows:            []row{r},
					driftAssessment: driftReady,
				}
			},
			want: func(t *testing.T, model *framePacedModel) {
				inner := innerListModel(t, model)
				if len(inner.rows) != 1 || inner.rows[0].Artifact.Name != "snapshot-resource" {
					t.Fatalf("accepted snapshot rows = %v, want snapshot-resource", inner.rows)
				}
				if got := model.View().Content; !strings.Contains(got, "independent snapshot detail") {
					t.Fatalf("view did not refresh to the accepted snapshot immediately:\n%s", got)
				}
			},
		},
		{
			name: "pane change",
			msg:  func(t *testing.T, model *framePacedModel) tea.Msg { return keyPress('?') },
			want: func(t *testing.T, model *framePacedModel) {
				inner := innerListModel(t, model)
				if inner.pane != paneHelp {
					t.Fatalf("? opened pane %d, want paneHelp", inner.pane)
				}
				if got, fresh := model.View().Content, model.inner.View().Content; got != fresh {
					t.Fatal("view did not refresh to the help pane immediately")
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model, clock := build(t)
			initial := model.View()
			if !strings.Contains(initial.Content, "selected detail entry-0") {
				t.Fatalf("initial frame does not describe entry-0:\n%s", initial.Content)
			}
			clock.now = clock.now.Add(time.Millisecond)
			model, _ = updateFramePaced(t, model, tea.MouseWheelMsg{Button: tea.MouseWheelDown, X: 2, Y: 3})
			if !model.framePending {
				t.Fatal("fixture did not leave a pending paced frame")
			}
			if got := model.View().Content; !strings.Contains(got, "selected detail entry-0") {
				t.Fatalf("paced frame left the pre-wheel selection early:\n%s", got)
			}
			staleTick := framePacedTickMsg{generation: model.frameGeneration}

			model, _ = updateFramePaced(t, model, tc.msg(t, model))
			if model.framePending {
				t.Fatal("non-wheel update left the stale frame pending")
			}
			tc.want(t, model)

			before := model.View().Content
			model, _ = updateFramePaced(t, model, staleTick)
			if got := model.View().Content; got != before {
				t.Fatal("stale tick reverted or replaced the current view")
			}
		})
	}

	t.Run("non-list inner command stays executable", func(t *testing.T) {
		probe := &framePacedProbeInner{}
		clock := &framePacedClock{now: time.Unix(100, 0)}
		model := newFramePacedModel(probe)
		model.clock = clock.Now
		model.View()

		model, cmd := updateFramePaced(t, model, tea.MouseWheelMsg{Button: tea.MouseWheelDown, X: 2, Y: 3})
		if model.framePending {
			t.Fatal("wheel on a non-list inner model was paced")
		}
		if cmd != nil {
			t.Fatal("probe wheel returned an unexpected command")
		}
		_, cmd = updateFramePaced(t, model, framePacedProbeRunMsg{})
		if cmd == nil {
			t.Fatal("inner command was dropped by the wrapper")
		}
		if msg := cmd(); msg != (framePacedProbeRanMsg{}) {
			t.Fatalf("inner command executed to %v, want the probe result", msg)
		}
		if !probe.ran {
			t.Fatal("inner command was not executable")
		}
	})
}

type framePacedProbeRunMsg struct{}
type framePacedProbeRanMsg struct{}

type framePacedProbeInner struct {
	wheels int
	ran    bool
}

func (m *framePacedProbeInner) Init() tea.Cmd { return nil }
func (m *framePacedProbeInner) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg.(type) {
	case tea.MouseWheelMsg:
		m.wheels++
	case framePacedProbeRunMsg:
		return m, func() tea.Msg {
			m.ran = true
			return framePacedProbeRanMsg{}
		}
	}
	return m, nil
}
func (m *framePacedProbeInner) View() tea.View {
	return tea.NewView(fmt.Sprintf("probe wheels=%d", m.wheels))
}

// spec: tui-rendering/pacing-retains-terminal-and-error-ownership
func TestFramePacedNonListWheelImmediate(t *testing.T) {
	wheel := tea.MouseWheelMsg{Button: tea.MouseWheelDown, X: 2, Y: 3}

	newWrapped := func(t *testing.T, inner Model) *framePacedModel {
		t.Helper()
		clock := &framePacedClock{now: time.Unix(100, 0)}
		model := newFramePacedModel(inner)
		model.clock = clock.Now
		model.View()
		return model
	}

	assertImmediate := func(t *testing.T, model *framePacedModel, msg tea.Msg) *framePacedModel {
		t.Helper()
		model, _ = updateFramePaced(t, model, msg)
		if model.framePending {
			t.Fatalf("%v scheduled a paced frame on a non-list path", msg)
		}
		if got, fresh := model.View().Content, model.inner.View().Content; got != fresh {
			t.Fatalf("view was not refreshed immediately for %v", msg)
		}
		return model
	}

	t.Run("detail preview", func(t *testing.T) {
		m := NewModel(t.TempDir(), t.TempDir())
		m.width, m.height = 80, 24
		m.pane = paneDetail
		m.detailViewport = newTUIViewport(80, 3)
		m.detailViewport.SetContent(strings.Repeat("preview line\n", 20))
		model := assertImmediate(t, newWrapped(t, m), wheel)
		inner := innerListModel(t, model)
		if got := inner.detailViewport.YOffset(); got != 1 {
			t.Fatalf("preview offset = %d, want one line", got)
		}
	})

	t.Run("help overlay", func(t *testing.T) {
		m := NewModel(t.TempDir(), t.TempDir())
		m.width, m.height = 80, 24
		m.pane = paneHelp
		m.helpViewport.SetWidth(m.width)
		m.helpViewport.SetHeight(m.height - 1)
		m.helpViewport.SetContent(renderHelp(m))
		model := assertImmediate(t, newWrapped(t, m), wheel)
		inner := innerListModel(t, model)
		if got := inner.helpViewport.YOffset(); got != 1 {
			t.Fatalf("help offset = %d, want one line", got)
		}
	})

	t.Run("errors overlay", func(t *testing.T) {
		m := NewModel(t.TempDir(), t.TempDir())
		m.width, m.height = 80, 12
		m.pane = paneErrors
		m.errorsViewport.SetHeight(m.height - 1)
		for i := 0; i < 20; i++ {
			m.errs = append(m.errs, fmt.Errorf("diagnostic %d", i))
		}
		m.errorsViewport.SetContent(renderErrors(m))
		model := assertImmediate(t, newWrapped(t, m), wheel)
		inner := innerListModel(t, model)
		if got := inner.errorsViewport.YOffset(); got != 1 {
			t.Fatalf("errors offset = %d, want one line", got)
		}
	})

	t.Run("marketplace consent", func(t *testing.T) {
		m := NewModel(t.TempDir(), t.TempDir())
		m.width, m.height = 80, 12
		m.pane = paneMarketplaceConsent
		m.setMarketplaceConsentPreview(strings.Repeat("verified resource\n", 30))
		model := assertImmediate(t, newWrapped(t, m), wheel)
		inner := innerListModel(t, model)
		if got := inner.marketplaceConsentViewport.YOffset(); got != 1 {
			t.Fatalf("consent offset = %d, want one line", got)
		}
	})

	t.Run("palette open keeps list wheel immediate", func(t *testing.T) {
		m := NewModel(t.TempDir(), t.TempDir())
		m.width, m.height, m.loading, m.pane = 80, 24, false, paneList
		m.flatItems = []flatItem{{Kind: flatArtifact, Row: mkRow("entry", withGlobal)}}
		m.palette.open = true
		assertImmediate(t, newWrapped(t, m), wheel)
	})

	t.Run("undersized terminal", func(t *testing.T) {
		m := NewModel(t.TempDir(), t.TempDir())
		m.width, m.height, m.loading, m.pane = 49, 12, false, paneList
		m.flatItems = []flatItem{{Kind: flatArtifact, Row: mkRow("entry", withGlobal)}}
		model, cmd := updateFramePaced(t, newWrapped(t, m), wheel)
		if cmd != nil || model.framePending {
			t.Fatal("wheel below the minimum size scheduled work")
		}
	})

	t.Run("horizontal wheel on the list", func(t *testing.T) {
		m := NewModel(t.TempDir(), t.TempDir())
		m.width, m.height, m.loading, m.pane = 80, 24, false, paneList
		m.flatItems = []flatItem{{Kind: flatArtifact, Row: mkRow("entry", withGlobal)}}
		assertImmediate(t, newWrapped(t, m), tea.MouseWheelMsg{Button: tea.MouseWheelRight, X: 2, Y: 3})
	})

	t.Run("arbitrary probe model", func(t *testing.T) {
		probe := &framePacedProbeInner{}
		clock := &framePacedClock{now: time.Unix(100, 0)}
		model := newFramePacedModel(probe)
		model.clock = clock.Now
		model.View()
		model, _ = updateFramePaced(t, model, wheel)
		if model.framePending {
			t.Fatal("wheel on a probe model was paced")
		}
		if !strings.Contains(model.View().Content, "wheels=1") {
			t.Fatal("probe view was not refreshed immediately")
		}
	})

	t.Run("pending list frame cannot affect another pane", func(t *testing.T) {
		model, clock := newFramePacedListModel(t, []string{"entry-0", "entry-1"})
		model.View()
		clock.now = clock.now.Add(time.Millisecond)
		model, _ = updateFramePaced(t, model, wheel)
		staleTick := framePacedTickMsg{generation: model.frameGeneration}

		model, _ = updateFramePaced(t, model, keyPress('?'))
		model, _ = updateFramePaced(t, model, wheel)
		inner := innerListModel(t, model)
		if inner.pane != paneHelp {
			t.Fatalf("pane = %d, want help", inner.pane)
		}
		if model.framePending {
			t.Fatal("wheel on the help pane was paced")
		}
		model, _ = updateFramePaced(t, model, staleTick)
		if got, fresh := model.View().Content, model.inner.View().Content; got != fresh {
			t.Fatal("stale list tick changed the help view")
		}
	})
}

// spec: tui-rendering/pacing-retains-terminal-and-error-ownership
// spec: tui-rendering/mouse-reporting-agrees-with-wheel-navigation
func TestFramePacedProgramConstruction(t *testing.T) {
	probe := &framePacedQuitProbe{}
	var output bytes.Buffer
	program := newTUIProgram(probe, tea.WithInput(nil), tea.WithOutput(&output))
	final, err := program.Run()
	if err != nil {
		t.Fatalf("program run: %v", err)
	}
	wrapped, ok := final.(*framePacedModel)
	if !ok {
		t.Fatalf("final model has type %T, want *framePacedModel", final)
	}
	if wrapped.inner != probe {
		t.Fatalf("wrapped inner = %p, want the original probe %p", wrapped.inner, probe)
	}
	got := output.String()
	if !strings.Contains(got, "\x1b[?1049h") {
		t.Fatalf("program did not enter the alternate screen: %q", got)
	}
	for _, sequence := range []string{"\x1b[?1002h", "\x1b[?1006h"} {
		if !strings.Contains(got, sequence) {
			t.Fatalf("program did not enable wheel reporting with %q: %q", sequence, got)
		}
	}
	if strings.Contains(got, "\x1b[?1003h") {
		t.Fatalf("program enabled all-motion tracking: %q", got)
	}
}

type framePacedQuitProbe struct{}

func (m *framePacedQuitProbe) Init() tea.Cmd { return tea.Quit }
func (m *framePacedQuitProbe) Update(tea.Msg) (tea.Model, tea.Cmd) {
	return m, nil
}
func (m *framePacedQuitProbe) View() tea.View {
	v := tea.NewView("quit probe")
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

// spec: tui-rendering/pacing-retains-terminal-and-error-ownership
func TestFramePacedFinishSyncQuitError(t *testing.T) {
	syncSentinel := errors.New("sync backend failed")
	m := Model{syncQuitErr: fmt.Errorf("sync /tmp/\x1b]52;c;c2VjcmV0=\x07/registry: %w", syncSentinel)}
	final := tea.Model(newFramePacedModel(m))

	var sequenceOut bytes.Buffer
	err := runWithTerminalGuard(
		terminalIO{sequenceOut: &sequenceOut},
		func() (tea.Model, error) { return final, nil },
	)
	if err == nil {
		t.Fatal("sync-and-quit error was swallowed by the wrapper")
	}
	if !errors.Is(err, syncSentinel) {
		t.Fatalf("sync-and-quit error lost Unwrap identity: %v", err)
	}
	if strings.ContainsRune(err.Error(), '\x1b') {
		t.Fatalf("sync-and-quit error carried raw terminal bytes: %q", err.Error())
	}
	if got := sequenceOut.String(); got != trustedTerminalRestoreSequence {
		t.Fatalf("terminal restoration did not precede the returned error: wrote %q", got)
	}

	var interrupt bytes.Buffer
	if err := runWithTerminalGuard(
		terminalIO{sequenceOut: &interrupt},
		func() (tea.Model, error) { return final, tea.ErrInterrupted },
	); err != nil {
		t.Fatalf("handled interrupt surfaced an error: %v", err)
	}
	if interrupt.String() != trustedTerminalRestoreSequence {
		t.Fatalf("interrupted run skipped terminal restoration: %q", interrupt.String())
	}
}
