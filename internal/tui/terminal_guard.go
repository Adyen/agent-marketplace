package tui

import (
	"io"
	"os"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"
)

// This file owns the terminal boundary around the whole TUI run.
//
// Two cooperating mechanisms live here:
//
//  1. Bubble Tea's package initialization lets Lip Gloss and Termenv query
//     terminal capabilities before Bubble Tea acquires stdin. TUI styles use
//     those Charm-provided results rather than maintaining a separate
//     background detector.
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
