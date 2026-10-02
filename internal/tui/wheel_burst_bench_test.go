package tui

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
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
