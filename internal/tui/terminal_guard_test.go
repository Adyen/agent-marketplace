package tui

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestTerminalGuardRestoreEmitsTrustedSequencesOnce(t *testing.T) {
	var buf bytes.Buffer
	guard := acquireTerminalGuard(terminalIO{sequenceOut: &buf})
	guard.restore()

	want := strings.Join([]string{
		"\x1b[?1002l", // mouse button-motion off
		"\x1b[?1003l", // mouse any-motion off
		"\x1b[?1006l", // mouse SGR mode off
		"\x1b[?2004l", // bracketed paste off
		"\x1b[?25h",   // cursor visible
		"\x1b[?1049l", // leave alternate screen (last: return to main buffer)
	}, "")
	if buf.String() != want {
		t.Fatalf("guard restore wrote %q, want %q", buf.String(), want)
	}

	guard.restore()
	if buf.String() != want {
		t.Fatalf("guard restore is not idempotent, wrote %q", buf.String())
	}
}

func TestTerminalGuardSkipsNonTerminalTermios(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close(); _ = w.Close() }()

	var buf bytes.Buffer
	guard := acquireTerminalGuard(terminalIO{sequenceOut: &buf, termios: []*os.File{r, w}})
	if len(guard.termios) != 0 {
		t.Fatalf("non-terminal fds must not be captured for termios restore: %d", len(guard.termios))
	}
	guard.restore()
	if buf.Len() == 0 {
		t.Fatal("guard restore did not write sequences for piped output")
	}
}

type guardProbeModel struct{}

func (m guardProbeModel) Init() tea.Cmd                       { return tea.Quit }
func (m guardProbeModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }
func (m guardProbeModel) View() tea.View {
	v := tea.NewView("guard probe")
	v.AltScreen = true
	return v
}

// spec: tui-rendering/error-after-entering-alternate-screen
func TestRunWithTerminalGuardRestoresAroundEveryRunOutcome(t *testing.T) {
	sentinel := errors.New("cancel reader initialization failed")
	for _, tc := range []struct {
		name    string
		run     func(t *testing.T, renderBuf *bytes.Buffer) (tea.Model, error)
		wantErr error
	}{
		{
			name: "clean quit",
			run: func(t *testing.T, renderBuf *bytes.Buffer) (tea.Model, error) {
				return tea.NewProgram(guardProbeModel{}, tea.WithInput(nil), tea.WithOutput(renderBuf)).Run()
			},
		},
		{
			name: "startup failure",
			run: func(t *testing.T, renderBuf *bytes.Buffer) (tea.Model, error) {
				return nil, sentinel
			},
			wantErr: sentinel,
		},
		{
			name: "interrupted",
			run: func(t *testing.T, renderBuf *bytes.Buffer) (tea.Model, error) {
				return nil, tea.ErrInterrupted
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var sequenceOut, renderBuf bytes.Buffer
			err := runWithTerminalGuard(
				terminalIO{sequenceOut: &sequenceOut},
				func() (tea.Model, error) { return tc.run(t, &renderBuf) },
			)
			if tc.wantErr == nil && err != nil {
				t.Fatalf("run returned %v, want nil", err)
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("run returned %v, want identity %v", err, tc.wantErr)
			}
			if !strings.Contains(sequenceOut.String(), "\x1b[?1049l") || !strings.Contains(sequenceOut.String(), "\x1b[?25h") {
				t.Fatalf("guard did not restore cursor/alternate screen after run: %q", sequenceOut.String())
			}
		})
	}
}
