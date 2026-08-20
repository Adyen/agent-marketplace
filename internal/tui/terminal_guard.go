package tui

import (
	"io"
	"os"
	"strconv"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/term"
)

// This file owns the terminal boundary around the whole TUI run.
//
// Two cooperating mechanisms live here:
//
//  1. Environment-only terminal style selection. Neither the TUI nor the
//     glamour/lipgloss renderers may actively probe the terminal (OSC 10/11
//     status reports): once Bubble Tea owns the terminal the probe response
//     is swallowed by the input reader, which stalls startup for the full
//     query timeout and leaks query bytes into the frame. The background is
//     therefore derived from COLORFGBG only, defaulting to dark, and the
//     result is pushed into the lipgloss background cache so adaptive colors
//     never trigger a lazy probe either.
//  2. An outer terminal guard that snapshots termios state before the
//     program starts and restores it — together with the alternate screen,
//     cursor visibility, mouse modes, and bracketed paste — on every exit:
//     clean quit, renderer/cancel-reader initialization failure, returned
//     error, or a handled SIGINT/SIGTERM. Bubble Tea restores the terminal
//     on its own handled exits; the guard is the last-resort layer for the
//     startup paths where Bubble Tea returns before its shutdown runs.

// trustedTerminalRestoreSequence resets every terminal mode the TUI enables,
// in an order that keeps all resets inside the alternate buffer until the
// final sequence returns to the main screen.
const trustedTerminalRestoreSequence = "" +
	"\x1b[?1002l" + // mouse button-event motion off
	"\x1b[?1003l" + // mouse any-event motion off
	"\x1b[?1006l" + // mouse SGR extended mode off
	"\x1b[?2004l" + // bracketed paste off
	"\x1b[?25h" + // cursor visible
	"\x1b[?1049l" // leave alternate screen (must stay last)

// terminalIO names the terminal resources the guard captures and restores.
// sequenceOut receives the trusted restore sequences; termios lists the fds
// whose termios state is snapshotted on acquire and restored on restore.
type terminalIO struct {
	sequenceOut io.Writer
	termios     []*os.File
}

// terminalGuard is an idempotent, last-resort terminal restoration scope.
type terminalGuard struct {
	io      terminalIO
	termios []capturedTermios
	once    sync.Once
}

type capturedTermios struct {
	fd    uintptr
	state *term.State
}

// acquireTerminalGuard snapshots the termios state of every real terminal fd
// before the TUI takes over. Non-terminal fds (pipes, files, /dev/null) carry
// no termios and are skipped.
func acquireTerminalGuard(io terminalIO) *terminalGuard {
	guard := &terminalGuard{io: io}
	seen := map[uintptr]bool{}
	for _, f := range io.termios {
		if f == nil {
			continue
		}
		fd := f.Fd()
		if seen[fd] || !term.IsTerminal(fd) {
			continue
		}
		seen[fd] = true
		state, err := term.GetState(fd)
		if err != nil {
			continue
		}
		guard.termios = append(guard.termios, capturedTermios{fd: fd, state: state})
	}
	return guard
}

// restore returns the terminal to the acquired state: it emits the trusted
// mode-reset sequences and restores the captured termios. Safe to call more
// than once; only the first call acts.
func (g *terminalGuard) restore() {
	g.once.Do(func() {
		if g.io.sequenceOut != nil {
			_, _ = io.WriteString(g.io.sequenceOut, trustedTerminalRestoreSequence)
		}
		for _, captured := range g.termios {
			_ = term.Restore(captured.fd, captured.state)
		}
	})
}

// runWithTerminalGuard wraps a full Bubble Tea run in the terminal guard so
// every outcome — success, startup/initialization failure, returned error,
// or handled signal — restores the terminal before the error reaches Cobra.
func runWithTerminalGuard(io terminalIO, run func() (tea.Model, error)) error {
	guard := acquireTerminalGuard(io)
	defer guard.restore()
	finalModel, err := run()
	return finishTUIRun(finalModel, err)
}

// defaultTerminalIO captures the process terminal: restore sequences go to
// stdout (the Bubble Tea output), termios is captured from stdin (raw-mode
// input) and stdout.
func defaultTerminalIO() terminalIO {
	return terminalIO{sequenceOut: os.Stdout, termios: []*os.File{os.Stdin, os.Stdout}}
}

// detectDarkBackgroundFromEnv resolves the terminal background using only
// process environment data. It never queries the terminal.
//
// COLORFGBG (xterm convention) is "fg;bg" — some terminals insert a middle
// "default" field — where the background indices 7 and 15 are light and all
// other values are dark. Unknown or unparsable values default to dark, the
// far more common terminal background.
func detectDarkBackgroundFromEnv() bool {
	return colorFGBGHasDarkBackground(os.Getenv("COLORFGBG"))
}

func colorFGBGHasDarkBackground(value string) bool {
	fields := strings.Split(value, ";")
	background, err := strconv.Atoi(strings.TrimSpace(fields[len(fields)-1]))
	if err != nil {
		return true
	}
	return background != 7 && background != 15
}

// glamourStyleForBackground maps the environment-derived background to an
// explicit, trusted glamour style. This replaces glamour's WithAutoStyle,
// which actively probes the terminal with an OSC 11 query.
func glamourStyleForBackground(dark bool) string {
	if dark {
		return "dark"
	}
	return "light"
}

// applyTerminalBackground resolves the terminal background from the
// environment only and seeds the lipgloss background cache. Once the cache
// is set explicitly, lipgloss adaptive colors never trigger a lazy OSC 11
// probe — which would otherwise fire on the first styled render, after
// Bubble Tea has taken over the terminal, and stall for the query timeout.
// Returns the resolved background for callers that also pick a style.
func applyTerminalBackground() (dark bool) {
	dark = detectDarkBackgroundFromEnv()
	lipgloss.SetHasDarkBackground(dark)
	return dark
}
