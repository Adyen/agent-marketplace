//go:build darwin || linux || freebsd || netbsd || openbsd || dragonfly || solaris || aix

package tui

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Adyen/agent-marketplace/internal/config"
)

type signalContractModel struct {
	marker string
}

func (m signalContractModel) Init() tea.Cmd {
	return func() tea.Msg {
		if err := os.MkdirAll(filepath.Dir(m.marker), 0o755); err != nil {
			return err
		}
		tmp := m.marker + ".tmp"
		if err := os.WriteFile(tmp, []byte("apiVersion: agent-marketplace/v4\nharnesses: ["), 0o644); err != nil {
			return err
		}
		if err := os.Rename(tmp, m.marker); err != nil {
			return err
		}
		select {}
	}
}

func (m signalContractModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }

func (m signalContractModel) View() tea.View {
	v := tea.NewView("active filesystem work")
	v.AltScreen = true
	return v
}

func TestTUISignalHelper(t *testing.T) {
	if os.Getenv("TUI_SIGNAL_HELPER") != "1" {
		return
	}
	marker := os.Getenv("TUI_SIGNAL_MARKER")
	if err := runTUIModel(signalContractModel{marker: marker}, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout)); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}

func TestHandledSignalsRestoreTerminalAndExposePartialFilesystemState(t *testing.T) {
	if raceEnabled {
		t.Skip("Bubble Tea closes its cancel reader concurrently during signal shutdown under the race runtime")
	}
	for _, tc := range []struct {
		name   string
		signal os.Signal
	}{
		{name: "SIGINT", signal: syscall.SIGINT},
		{name: "SIGTERM", signal: syscall.SIGTERM},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			marker := filepath.Join(home, ".agents", "config.yaml")
			cmd := exec.Command(os.Args[0], "-test.run=^TestTUISignalHelper$")
			cmd.Env = append(os.Environ(), "HOME="+home, "TUI_SIGNAL_HELPER=1", "TUI_SIGNAL_MARKER="+marker)
			stdin, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(5 * time.Second)
			for {
				if _, err := os.Stat(marker); err == nil {
					break
				}
				if time.Now().After(deadline) {
					_ = cmd.Process.Kill()
					t.Fatalf("active operation did not start; stderr: %s", stderr.String())
				}
				time.Sleep(10 * time.Millisecond)
			}
			// The alternate screen is entered with the first rendered
			// frame, so wait for that frame before delivering the signal.
			frameDeadline := time.Now().Add(5 * time.Second)
			for !strings.Contains(stdout.String(), "\x1b[?1049h") {
				if time.Now().After(frameDeadline) {
					_ = cmd.Process.Kill()
					t.Fatalf("%s never rendered its first alt-screen frame; stdout: %q stderr: %s", tc.name, stdout.String(), stderr.String())
				}
				time.Sleep(10 * time.Millisecond)
			}
			if err := cmd.Process.Signal(tc.signal); err != nil {
				t.Fatal(err)
			}
			if err := cmd.Wait(); err != nil {
				t.Fatalf("handled %s exited with error: %v\nstderr: %s", tc.name, err, stderr.String())
			}
			_ = stdin.Close()

			output := stdout.String()
			if !strings.Contains(output, "\x1b[?1049h") || !strings.Contains(output, "\x1b[?1049l") || !strings.Contains(output, "\x1b[?25h") {
				t.Fatalf("%s did not restore alternate screen and cursor: %q", tc.name, output)
			}
			// The outer terminal guard is the last terminal writer: its
			// trusted restore tail covers mouse modes and bracketed paste
			// and ends with the alternate-screen exit, regardless of how
			// the run terminated. (The helper's test framework appends its
			// own PASS line afterwards, so match containment + ordering.)
			guardTail := strings.LastIndex(output, trustedTerminalRestoreSequence)
			bubbleTeaExit := strings.Index(output, "\x1b[?1049l")
			if guardTail < 0 || guardTail < bubbleTeaExit {
				t.Fatalf("%s did not restore the full terminal mode set last: %q", tc.name, output)
			}
			data, err := os.ReadFile(marker)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != "apiVersion: agent-marketplace/v4\nharnesses: [" {
				t.Fatalf("hard exit hid or rewrote partial filesystem state: %q", data)
			}
			t.Setenv("HOME", home)
			if _, err := config.LoadGlobalConfig(); err == nil {
				t.Fatal("subsequent config inspection did not report partial mutation")
			}
		})
	}
}
