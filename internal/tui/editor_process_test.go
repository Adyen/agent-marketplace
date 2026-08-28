package tui

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestEditorLifecycleHelper is a fake $EDITOR used by the editor-process
// lifecycle tests. It runs only as a re-executed test binary child.
//
//   - TUI_EDITOR_HELPER_EXIT=0: exit immediately and successfully.
//   - TUI_EDITOR_HELPER_MARKER=<path>: write a readiness marker, then block
//     forever (a fake blocking editor).
//   - TUI_EDITOR_HELPER_IGNORE_SIGNALS=1: ignore interrupt/termination before
//     blocking (an editor stuck in a prompt).
func TestEditorLifecycleHelper(t *testing.T) {
	if os.Getenv("TUI_EDITOR_HELPER") != "1" {
		return
	}
	if os.Getenv("TUI_EDITOR_HELPER_EXIT") == "0" {
		return
	}
	if marker := os.Getenv("TUI_EDITOR_HELPER_MARKER"); marker != "" {
		if os.Getenv("TUI_EDITOR_HELPER_IGNORE_SIGNALS") == "1" {
			ignoreEditorHelperSignals()
		}
		if err := os.WriteFile(marker, editorHelperMarkerContent(), 0o644); err != nil {
			_, _ = os.Stderr.WriteString(err.Error())
			os.Exit(2)
		}
		// Block "forever" with a timer loop: a bare select{} would trip the
		// Go deadlock detector ("all goroutines are asleep") and exit the
		// helper on its own instead of staying blocked.
		for {
			time.Sleep(time.Hour)
		}
	}
}

func editorHelperCmd(t *testing.T, env ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestEditorLifecycleHelper$")
	cmd.Env = append(os.Environ(), append([]string{"TUI_EDITOR_HELPER=1"}, env...)...)
	t.Cleanup(func() {
		if cmd.Process != nil && cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
		}
	})
	return cmd
}

func waitForEditorMarker(t *testing.T, marker string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("fake editor did not signal readiness")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestEditorProcessNormalExitIsReaped(t *testing.T) {
	p := newEditorProcess(editorHelperCmd(t, "TUI_EDITOR_HELPER_EXIT=0"))
	if err := p.Run(); err != nil {
		t.Fatalf("clean editor exit returned %v", err)
	}
	if p.cmd.ProcessState == nil || !p.cmd.ProcessState.Exited() || !p.cmd.ProcessState.Success() {
		t.Fatalf("editor process was not reaped after clean exit: %+v", p.cmd.ProcessState)
	}
}

func TestEditorProcessNonzeroExitPropagates(t *testing.T) {
	falsePath, err := exec.LookPath("false")
	if err != nil {
		t.Skipf("false(1) not usable: %v", err)
	}
	p := newEditorProcess(exec.Command(falsePath))
	runErr := p.Run()
	var exitErr *exec.ExitError
	if !errors.As(runErr, &exitErr) {
		t.Fatalf("non-zero editor exit = %v (%T), want *exec.ExitError passthrough", runErr, runErr)
	}
	if p.cmd.ProcessState == nil || p.cmd.ProcessState.Success() {
		t.Fatalf("failing editor was not reaped: %+v", p.cmd.ProcessState)
	}
}

func TestEditorProcessStartFailureIsImmediate(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-editor")
	p := newEditorProcess(exec.Command(missing, "config.yaml"))
	started := time.Now()
	err := p.Run()
	if err == nil {
		t.Fatal("starting a missing editor did not fail")
	}
	if time.Since(started) > time.Second {
		t.Fatalf("editor start failure was not immediate: %s", time.Since(started))
	}
	if p.cmd.ProcessState != nil {
		t.Fatalf("editor that never started must not be waited on: %+v", p.cmd.ProcessState)
	}
	var notFound *exec.Error
	if !errors.As(err, &notFound) && !strings.Contains(err.Error(), "no such file") {
		t.Fatalf("start failure = %v, want exec start error", err)
	}
}
