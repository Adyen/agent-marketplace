package tui

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/charmbracelet/colorprofile"
)

// spec: tui-mouse-navigation/terminal-drag-transport-copies-selected-error-text
func TestErrorSelectionClipboardTransport(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("CODEX_HOME", t.TempDir())
	t.Cleanup(swapLoadOrgConfig(func() (*config.Config, error) {
		return nil, nil
	}))
	t.Cleanup(swapLoadGlobalConfig(func() (*config.Config, error) {
		return &config.Config{Harnesses: []string{"factory"}}, nil
	}))
	t.Cleanup(swapLoadRepoConfig(func() (*config.Config, error) {
		return nil, nil
	}))
	t.Cleanup(swapLoadLocalConfig(func() (*config.LocalConfig, error) {
		return nil, nil
	}))

	m := NewModel(t.TempDir(), t.TempDir())
	m.width, m.height, m.loading = 80, 24, false
	m.driftAssessment = driftReady

	out := &lockedBuffer{}
	inR, inW := io.Pipe()
	startupReady := make(chan struct{})
	var startupReadyOnce sync.Once
	program := newTUIProgram(m, tea.WithInput(inR), tea.WithOutput(out), tea.WithWindowSize(80, 24),
		tea.WithFilter(func(_ tea.Model, msg tea.Msg) tea.Msg {
			if snapshot, ok := msg.(snapshotLoadedMsg); ok && snapshot.stage == snapshotStageFinal {
				snapshot.errs = []error{errors.New("unicode-中文字-😀-payload-UNIQUE")}
				startupReadyOnce.Do(func() { close(startupReady) })
				return snapshot
			}
			return msg
		}),
		tea.WithColorProfile(colorprofile.TrueColor))
	done := make(chan error, 1)
	t.Cleanup(func() {
		program.Kill()
		_ = inW.Close()
		_ = inR.Close()
	})
	go func() {
		_, err := program.Run()
		done <- err
	}()

	waitFor := func(needle string) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if strings.Contains(out.String(), needle) {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %q in terminal output; output so far: %q", needle, out.String())
	}

	sgr := func(code, x, y int, final byte) string {
		return fmt.Sprintf("\x1b[<%d;%d;%d%c", code, x+1, y+1, final)
	}

	waitFor("\x1b[?1002h")
	select {
	case <-startupReady:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for final startup snapshot before mouse input; output so far: %q", out.String())
	}

	if _, err := inW.Write([]byte("e")); err != nil {
		t.Fatal(err)
	}
	waitFor("Errors (1)")
	waitFor("unicode-中文字-😀-payload-UNIQUE")

	if _, err := inW.Write([]byte(sgr(0, 4, 2, 'M') + sgr(0, 4, 2, 'm'))); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if strings.Contains(out.String(), "\x1b]52;") {
		t.Fatal("ignored gestures emitted an OSC52 clipboard write")
	}

	if _, err := inW.Write([]byte(sgr(0, 2, 2, 'M') + sgr(32, 33, 2, 'M') + sgr(0, 33, 2, 'm'))); err != nil {
		t.Fatal(err)
	}
	want := "unicode-中文字-😀-payload-UNIQUE"
	payload := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(want)) + "\x07"
	waitFor(payload)
	if strings.Count(out.String(), "\x1b]52;") != 1 {
		t.Fatalf("terminal emitted %d clipboard writes, want 1", strings.Count(out.String(), "\x1b]52;"))
	}
	highlightStyle := regexp.MustCompile(`\x1b\[[0-9;]*7[0-9;]*m`)
	highlightDeadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(highlightDeadline) && !highlightStyle.MatchString(out.String()) {
		time.Sleep(5 * time.Millisecond)
	}
	if !highlightStyle.MatchString(out.String()) {
		t.Fatalf("timed out waiting for the selection highlight style; output so far: %q", out.String())
	}

	if _, err := inW.Write([]byte{0x03}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("program exited with %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("program did not exit on ctrl+c")
	}
}
