package tui

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestColorFGBGBackgroundSelectionIsEnvironmentOnly(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		unset bool
		want  bool // true = dark background
	}{
		{name: "unset defaults dark", unset: true, want: true},
		{name: "empty defaults dark", value: "", want: true},
		{name: "black background", value: "15;0", want: true},
		{name: "dark blue background", value: "15;4", want: true},
		{name: "bright black background", value: "7;8", want: true},
		{name: "white background", value: "0;15", want: false},
		{name: "light gray background", value: "0;7", want: false},
		{name: "three field light background", value: "0;default;15", want: false},
		{name: "garbage defaults dark", value: "not-a-number", want: true},
		{name: "trailing garbage defaults dark", value: "15;x", want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.unset {
				t.Setenv("COLORFGBG", "")
				_ = os.Unsetenv("COLORFGBG")
			} else {
				t.Setenv("COLORFGBG", tc.value)
			}
			if got := detectDarkBackgroundFromEnv(); got != tc.want {
				t.Fatalf("detectDarkBackgroundFromEnv(COLORFGBG=%q) = %t, want %t", tc.value, got, tc.want)
			}
		})
	}
}

func TestTerminalBackgroundSelectionSetsLipglossCacheWithoutProbing(t *testing.T) {
	previous := lipgloss.HasDarkBackground()
	t.Cleanup(func() { lipgloss.SetHasDarkBackground(previous) })
	// COLORFGBG declares a LIGHT background. Environment-only selection must
	// seed the lipgloss background cache with light, so no renderer ever
	// issues an OSC 11 query (which would race or hang once Bubble Tea owns
	// the terminal). A probing implementation would instead answer from the
	// real terminal and disagree with the environment override.
	t.Setenv("COLORFGBG", "0;15")
	applyTerminalBackground()
	if lipgloss.HasDarkBackground() {
		t.Fatal("lipgloss background cache was not seeded from COLORFGBG (light)")
	}

	t.Setenv("COLORFGBG", "15;0")
	applyTerminalBackground()
	if !lipgloss.HasDarkBackground() {
		t.Fatal("lipgloss background cache was not seeded from COLORFGBG (dark)")
	}
}

func TestExplicitGlamourStyleSelection(t *testing.T) {
	if got := glamourStyleForBackground(true); got != "dark" {
		t.Fatalf("dark background style = %q, want dark", got)
	}
	if got := glamourStyleForBackground(false); got != "light" {
		t.Fatalf("light background style = %q, want light", got)
	}
}

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
func (m guardProbeModel) View() string                        { return "" }

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
