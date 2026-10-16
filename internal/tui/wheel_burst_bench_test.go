package tui

import (
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

type wheelBurstBenchmarkModel struct {
	inner         tea.Model
	width, height int
	ready         chan struct{}
	readyOnce     sync.Once
	count         int
}

func (m *wheelBurstBenchmarkModel) Init() tea.Cmd {
	return func() tea.Msg { return tea.WindowSizeMsg{Width: m.width, Height: m.height} }
}

func (m *wheelBurstBenchmarkModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	inner, cmd := m.inner.Update(msg)
	m.inner = inner
	if _, ok := msg.(tea.WindowSizeMsg); ok {
		m.readyOnce.Do(func() { close(m.ready) })
	}
	if _, ok := msg.(tea.MouseWheelMsg); ok {
		m.count++
	}
	if key, ok := msg.(tea.KeyPressMsg); ok && key.String() == "x" {
		return m, tea.Batch(cmd, tea.Quit)
	}
	return m, cmd
}

func (m *wheelBurstBenchmarkModel) View() tea.View { return m.inner.View() }

func BenchmarkWheelBurstPath(b *testing.B) {
	oldProfile := lipgloss.Writer.Profile
	lipgloss.Writer.Profile = colorprofile.ANSI256
	b.Cleanup(func() { lipgloss.Writer.Profile = oldProfile })
	payload := strings.Repeat("\x1b[<65;2;3M", 1500) + strings.Repeat("\x1b[<64;2;3M", 500) + "x"
	for _, size := range [][2]int{{120, 34}, {200, 60}} {
		for _, paced := range []bool{false, true} {
			mode := "unpaced"
			if paced {
				mode = "paced"
			}
			b.Run(fmt.Sprintf("%dx%d/%s", size[0], size[1], mode), func(b *testing.B) {
				base := NewModel(b.TempDir(), b.TempDir())
				base.width, base.height, base.loading, base.pane = size[0], size[1], false, paneList
				base.driftAssessment = driftReady
				base.rows = buildBenchRows(3000)
				base.flatItems = make([]flatItem, len(base.rows))
				for i := range base.rows {
					base.rows[i].ensureLabelMemo()
					base.flatItems[i] = flatItem{Kind: flatArtifact, Row: base.rows[i], Depth: 1}
				}
				base.clampListOffset()
				b.ReportAllocs()
				b.SetBytes(int64(len(payload)))
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					b.StopTimer()
					var inner tea.Model = base
					if paced {
						inner = newFramePacedModel(inner)
					}
					probe := &wheelBurstBenchmarkModel{inner: inner, width: size[0], height: size[1], ready: make(chan struct{})}
					reader, writer, err := os.Pipe()
					if err != nil {
						b.Fatal(err)
					}
					ctx, cancel := context.WithTimeout(b.Context(), 30*time.Second)
					program := tea.NewProgram(probe, tea.WithContext(ctx), tea.WithInput(reader), tea.WithOutput(io.Discard), tea.WithoutSignals(), tea.WithEnvironment([]string{"TERM=xterm-256color"}), tea.WithWindowSize(size[0], size[1]), tea.WithColorProfile(colorprofile.ANSI256))
					written := make(chan error, 1)
					go func() {
						select {
						case <-probe.ready:
						case <-ctx.Done():
							written <- ctx.Err()
							return
						}
						_, err := io.WriteString(writer, payload)
						written <- err
					}()
					b.StartTimer()
					_, runErr := program.Run()
					b.StopTimer()
					cancel()
					_ = writer.Close()
					_ = reader.Close()
					writeErr := <-written
					if runErr != nil {
						b.Fatal(runErr)
					}
					if writeErr != nil {
						b.Fatal(writeErr)
					}
					final := probe.inner
					if wrapper, ok := final.(*framePacedModel); ok {
						final = wrapper.inner
					}
					model, ok := final.(Model)
					if !ok || probe.count != 2000 || model.cursor != 1000 {
						b.Fatalf("final type=%T events=%d cursor=%d, want Model, 2000 and 1000", final, probe.count, model.cursor)
					}
					b.StartTimer()
				}
				b.ReportMetric(2000, "events/op")
			})
		}
	}
}

type wheelDeliveryTestModel struct {
	inner         tea.Model
	width, height int
	ready         chan struct{}
	readyOnce     sync.Once
	wheels        []tea.MouseWheelMsg
}

func (m *wheelDeliveryTestModel) Init() tea.Cmd {
	return func() tea.Msg { return tea.WindowSizeMsg{Width: m.width, Height: m.height} }
}

func (m *wheelDeliveryTestModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	inner, cmd := m.inner.Update(msg)
	m.inner = inner
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.readyOnce.Do(func() { close(m.ready) })
	case tea.MouseWheelMsg:
		m.wheels = append(m.wheels, msg)
	}
	return m, cmd
}

func (m *wheelDeliveryTestModel) View() tea.View { return m.inner.View() }

// spec: tui-mouse-navigation/terminal-wheel-burst-reaches-the-expected-resource
func TestEncodedWheelBurstDeliversEveryEventInOrder(t *testing.T) {
	base := NewModel(t.TempDir(), t.TempDir())
	base.width, base.height, base.loading, base.pane = 80, 24, false, paneList
	base.driftAssessment = driftReady
	base.rows = make([]row, 30)
	base.flatItems = make([]flatItem, len(base.rows))
	for i := range base.rows {
		name := fmt.Sprintf("entry%02d", i)
		r := mkRow(name, withProject, withRegistry("fixture", "fixture"))
		r.Artifact.Kind = ir.KindSkill
		r.Artifact.RegistryURL = "fixture"
		r.Artifact.Description = "wheel delivery fixture"
		base.rows[i] = r
		base.flatItems[i] = flatItem{Kind: flatArtifact, Row: r, Depth: 1}
	}
	base.authoritativeRows = slices.Clone(base.rows)
	base.filteredRows = slices.Clone(base.rows)

	payload := strings.Repeat("\x1b[<65;4;6M", 10) + strings.Repeat("\x1b[<64;4;6M", 3) + strings.Repeat("\x1b[<64;4;6M", 20) + strings.Repeat("\x1b[<65;4;6M", 4)
	expected := make([]tea.MouseWheelMsg, 0, 37)
	for range 10 {
		expected = append(expected, tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	}
	for range 3 {
		expected = append(expected, tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	}
	for range 20 {
		expected = append(expected, tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	}
	for range 4 {
		expected = append(expected, tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	}

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	defer func() { _ = writer.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	probe := &wheelDeliveryTestModel{inner: newFramePacedModel(base), width: 80, height: 24, ready: make(chan struct{})}
	program := tea.NewProgram(probe, tea.WithContext(ctx), tea.WithInput(reader), tea.WithOutput(io.Discard), tea.WithoutSignals(), tea.WithEnvironment([]string{"TERM=xterm-256color"}), tea.WithWindowSize(80, 24), tea.WithColorProfile(colorprofile.ANSI256))
	written := make(chan error, 1)
	go func() {
		select {
		case <-probe.ready:
		case <-ctx.Done():
			written <- ctx.Err()
			return
		}
		_, writeErr := io.WriteString(writer, payload+"q")
		if closeErr := writer.Close(); writeErr == nil {
			writeErr = closeErr
		}
		written <- writeErr
	}()
	final, runErr := program.Run()
	writeErr := <-written
	if runErr != nil {
		t.Fatalf("Bubble Tea input program failed: %v", runErr)
	}
	if writeErr != nil {
		t.Fatalf("write encoded wheel input: %v", writeErr)
	}
	if final != probe {
		t.Fatalf("program final model = %T, want wheel capture wrapper", final)
	}
	if len(probe.wheels) != len(expected) {
		t.Fatalf("Bubble Tea test boundary received %d parsed wheel events, want %d", len(probe.wheels), len(expected))
	}
	for i, want := range expected {
		if got := probe.wheels[i].Button; got != want.Button {
			t.Fatalf("parsed wheel event %d = %v, want %v", i, got, want.Button)
		}
	}
	paced, ok := probe.inner.(*framePacedModel)
	if !ok {
		t.Fatalf("captured inner model = %T, want *framePacedModel", probe.inner)
	}
	model, ok := paced.inner.(Model)
	if !ok {
		t.Fatalf("paced inner model = %T, want Model", paced.inner)
	}
	if model.cursor != 4 {
		t.Fatalf("final cursor = %d, want 4", model.cursor)
	}
	selected, ok := model.selectedRow()
	if !ok || selected.Artifact.Name != "entry04" {
		t.Fatalf("final selection = %+v, want entry04", selected)
	}
	view := strings.Join(strings.Fields(ansi.Strip(paced.View().Content)), " ")
	for _, want := range []string{"Kind: skill", "Name: entry04", "Registry: fixture", "wheel delivery fixture"} {
		if !strings.Contains(view, want) {
			t.Fatalf("final entry04 detail is missing %q:\n%s", want, view)
		}
	}
}
