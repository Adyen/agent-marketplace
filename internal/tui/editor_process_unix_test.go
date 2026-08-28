//go:build darwin || linux || freebsd || netbsd || openbsd || dragonfly || solaris || aix

package tui

import (
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// ignoreEditorHelperSignals makes the fake editor child ignore the same
// signals a stuck editor would swallow at a prompt.
func ignoreEditorHelperSignals() {
	signal.Ignore(syscall.SIGINT, syscall.SIGTERM)
}

// editorHelperMarkerContent reports the helper's process group so the
// parent test can verify group placement without racing cmd.Start.
func editorHelperMarkerContent() []byte {
	return []byte(strconv.Itoa(syscall.Getpgrp()))
}

// signalSelf delivers sig to this test process. editorProcess.Run registers
// signal.Notify before starting the child, so the signal is intercepted
// (never the default terminate action) and forwarded to the editor.
//
// A second, test-level Notify registration stays installed for the whole
// signal window. It mirrors production, where Bubble Tea's own SIGINT/
// SIGTERM handler is registered for the entire program lifetime — the
// editor process is never the process's only handler. Without the net, a
// delivery racing outside the editor's registration window would kill the
// whole test binary instead of failing the test.
func signalSelf(t *testing.T, sig syscall.Signal) {
	t.Helper()
	if err := syscall.Kill(os.Getpid(), sig); err != nil {
		t.Fatalf("signal self: %v", err)
	}
}

func installSignalSafetyNet(t *testing.T) {
	t.Helper()
	net := make(chan os.Signal, 4)
	signal.Notify(net, syscall.SIGINT, syscall.SIGTERM)
	t.Cleanup(func() { signal.Stop(net) })
}

func TestBlockingEditorSignaledExternallyIsForwardedBoundedAndReaped(t *testing.T) {
	for _, tc := range []struct {
		name string
		sig  syscall.Signal
	}{
		{name: "SIGINT", sig: syscall.SIGINT},
		{name: "SIGTERM", sig: syscall.SIGTERM},
	} {
		t.Run(tc.name, func(t *testing.T) {
			installSignalSafetyNet(t)
			marker := filepath.Join(t.TempDir(), "editor-ready")
			p := newEditorProcess(editorHelperCmd(t, "TUI_EDITOR_HELPER_MARKER="+marker))
			p.grace = 300 * time.Millisecond

			runErr := make(chan error, 1)
			go func() { runErr <- p.Run() }()

			waitForEditorMarker(t, marker)
			started := time.Now()
			signalSelf(t, tc.sig)

			select {
			case err := <-runErr:
				// The fake editor dies from the forwarded signal; Run must
				// return its exit error promptly — not hang on the blocked
				// editor, not swallow the signal.
				if err == nil {
					t.Fatalf("%s: signaled editor returned nil error", tc.name)
				}
				if elapsed := time.Since(started); elapsed > 2*time.Second {
					t.Fatalf("%s: editor shutdown was not bounded: %s", tc.name, elapsed)
				}
				if elapsed := time.Since(started); elapsed >= p.grace {
					t.Fatalf("%s: cooperative editor did not exit on the forwarded signal within the grace period: %s", tc.name, elapsed)
				}
			case <-time.After(3 * time.Second):
				t.Fatalf("%s: Run hung after external signal", tc.name)
			}
			// A non-nil ProcessState proves Wait returned: the editor was
			// reaped, no zombie. The death must be the forwarded signal
			// itself, not the grace-period kill.
			state := p.cmd.ProcessState
			if state == nil {
				t.Fatalf("%s: signaled editor was not reaped", tc.name)
			}
			status, ok := state.Sys().(syscall.WaitStatus)
			if !ok || !status.Signaled() || status.Signal() != tc.sig {
				t.Fatalf("%s: editor died by %v, want the forwarded %s", tc.name, state.Sys(), tc.sig)
			}
		})
	}
}

func TestSignalIgnoringEditorIsKilledAfterGraceAndReaped(t *testing.T) {
	installSignalSafetyNet(t)
	marker := filepath.Join(t.TempDir(), "editor-ready")
	p := newEditorProcess(editorHelperCmd(t,
		"TUI_EDITOR_HELPER_MARKER="+marker,
		"TUI_EDITOR_HELPER_IGNORE_SIGNALS=1",
	))
	p.grace = 300 * time.Millisecond

	runErr := make(chan error, 1)
	started := time.Now()
	go func() { runErr <- p.Run() }()

	waitForEditorMarker(t, marker)
	signalSelf(t, syscall.SIGTERM)

	select {
	case err := <-runErr:
		elapsed := time.Since(started)
		if err == nil {
			t.Fatal("killed editor returned nil error")
		}
		if !strings.Contains(err.Error(), "killed") && !strings.Contains(err.Error(), "signal") {
			t.Fatalf("killed editor error does not report termination: %v", err)
		}
		if elapsed < p.grace {
			t.Fatalf("editor was killed before the forwarding grace period elapsed: %s", elapsed)
		}
		if elapsed > 3*time.Second {
			t.Fatalf("editor kill was not bounded: %s", elapsed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run hung on signal-ignoring editor")
	}

	// A non-nil ProcessState proves Wait returned: the editor was reaped.
	state := p.cmd.ProcessState
	if state == nil {
		t.Fatal("killed editor was not reaped")
	}
	if signaled, ok := state.Sys().(syscall.WaitStatus); !ok || !signaled.Signaled() || signaled.Signal() != syscall.SIGKILL {
		t.Fatalf("signal-ignoring editor was not SIGKILLed: %+v", state.Sys())
	}
}

func TestEditorProcessKeepsTerminalProcessGroup(t *testing.T) {
	installSignalSafetyNet(t)
	// The editor must stay in the TUI's terminal process group: moving it
	// into its own (background) group would suspend it with SIGTTIN the
	// first time it reads the terminal. Signals are therefore forwarded to
	// the editor process directly, and terminal-generated Ctrl+C keeps its
	// native editor meaning while the TUI bounds the shutdown.
	//
	// The helper writes its own pgid as the marker content: reading it
	// back avoids touching p.cmd.Process from the test goroutine while
	// Start is writing it (a data race the cross-process marker alone
	// cannot make visible to the race detector).
	marker := filepath.Join(t.TempDir(), "editor-ready")
	p := newEditorProcess(editorHelperCmd(t, "TUI_EDITOR_HELPER_MARKER="+marker))
	p.grace = 300 * time.Millisecond

	runErr := make(chan error, 1)
	go func() { runErr <- p.Run() }()

	editorPgid := waitForEditorMarkerPGID(t, marker)
	if editorPgid != syscall.Getpgrp() {
		t.Fatalf("editor pgid = %d, want the TUI terminal process group %d", editorPgid, syscall.Getpgrp())
	}

	signalSelf(t, syscall.SIGTERM)
	select {
	case <-runErr:
	case <-time.After(3 * time.Second):
		t.Fatal("Run hung while cleaning up editor")
	}
	if p.cmd.ProcessState == nil {
		t.Fatal("editor was not reaped")
	}
}

func waitForEditorMarkerPGID(t *testing.T, marker string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, err := os.ReadFile(marker)
		if err == nil {
			if pgid, parseErr := strconv.Atoi(strings.TrimSpace(string(data))); parseErr == nil {
				return pgid
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("fake editor did not report its process group")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
