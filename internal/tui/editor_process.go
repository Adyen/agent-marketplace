package tui

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"time"
)

// editorProcess is a tea.ExecCommand that owns the full lifecycle of the
// external config editor: start, signal forwarding, bounded wait, forced
// kill, and reap.
//
// tea.ExecProcess alone forwards nothing: while the editor runs, the Bubble
// Tea event loop is blocked inside Exec, so an external SIGINT/SIGTERM
// (kill(1), tmux kill-pane, system shutdown) is queued by Bubble Tea's
// signal handler but cannot be processed until the editor exits. An editor
// parked at a prompt — or one that ignores the signal — would hang the TUI
// forever and swallow the interrupt entirely.
//
// Here the signal is forwarded to the editor immediately, the editor gets a
// bounded grace period to exit on its own, and a straggler is killed and
// reaped. Once Run returns, Bubble Tea delivers the queued
// interrupt/quit message, so the program still performs its hard exit with
// the terminal restored.
//
// The editor deliberately stays in the TUI's terminal process group: a
// child moved into its own (background) group would be stopped by SIGTTIN
// the first time it read the terminal. Keeping the group also preserves the
// editor's native Ctrl+C handling for terminal-generated interrupts; the
// bounded forwarding path governs the shutdown either way.
type editorProcess struct {
	cmd   *exec.Cmd
	grace time.Duration
	// waiting is drawn on the alternate screen while the editor runs, so
	// the terminal never falls back to the pre-launch primary screen while
	// Bubble Tea has released it. Empty draws nothing.
	waiting string
}

const enterAltScreenAndClear = "\x1b[?1049h\x1b[2J\x1b[H"

// withWaitingScreen sets the screen shown while the editor runs: the file
// being edited and the editor that must exit before the TUI resumes.
func (e *editorProcess) withWaitingScreen(path, editor string) *editorProcess {
	e.waiting = fmt.Sprintf("agent-marketplace\r\n\r\nEditing %s\r\nWaiting for %s to exit…\r\n",
		sanitizeTerminalData(path, terminalSingleLine), sanitizeTerminalData(editor, terminalSingleLine))
	return e
}

// defaultEditorSignalGrace bounds how long the editor may keep the TUI
// suspended after an interrupt/termination signal before it is killed.
var defaultEditorSignalGrace = 2 * time.Second

func newEditorProcess(cmd *exec.Cmd) *editorProcess {
	return &editorProcess{cmd: cmd, grace: defaultEditorSignalGrace}
}

// SetStdin mirrors bubbletea's osExecCommand: the editor inherits the
// program input unless the caller overrode it.
func (e *editorProcess) SetStdin(r io.Reader) {
	if e.cmd.Stdin == nil {
		e.cmd.Stdin = r
	}
}

func (e *editorProcess) SetStdout(w io.Writer) {
	if e.cmd.Stdout == nil {
		e.cmd.Stdout = w
	}
}

func (e *editorProcess) SetStderr(w io.Writer) {
	if e.cmd.Stderr == nil {
		e.cmd.Stderr = w
	}
}

// Run starts the editor and blocks until it exits, an external signal
// terminates it within the grace period, or the grace period expires and
// the editor is killed. The child is always reaped before Run returns, so
// no zombie survives any outcome.
func (e *editorProcess) Run() error {
	// Register before Start so a signal arriving immediately after spawn
	// can never slip past unobserved.
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, editorForwardedSignals()...)
	defer signal.Stop(signals)

	if e.waiting != "" && e.cmd.Stdout != nil {
		// A full-screen editor leaves the alternate screen on exit, which
		// would reveal the primary screen until Bubble Tea redraws; enter
		// and clear it again so the TUI resumes without that flash.
		_, _ = io.WriteString(e.cmd.Stdout, enterAltScreenAndClear+e.waiting)
		defer func() { _, _ = io.WriteString(e.cmd.Stdout, enterAltScreenAndClear) }()
	}

	if err := e.cmd.Start(); err != nil {
		return err
	}
	wait := make(chan error, 1)
	go func() { wait <- e.cmd.Wait() }()

	for {
		select {
		case err := <-wait:
			return err
		case sig := <-signals:
			// Forward to the editor so it can shut down on its own; the
			// process may already be gone, which is fine.
			_ = e.cmd.Process.Signal(sig)
			select {
			case err := <-wait:
				return err
			case <-time.After(e.grace):
				_ = e.cmd.Process.Kill()
				err := <-wait // reap the killed editor
				return fmt.Errorf("config editor did not exit within %s of %s; killed and reaped: %w", e.grace, sig, err)
			}
		}
	}
}
