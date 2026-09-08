package tui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestBundlePagerHelper(t *testing.T) {
	if os.Getenv("TUI_PAGER_HELPER") != "1" {
		return
	}
	if os.Getenv("LESSSECURE") != "1" {
		os.Exit(2)
	}
	if _, err := io.Copy(os.Stdout, os.Stdin); err != nil {
		os.Exit(3)
	}
	os.Exit(0)
}

func TestBundlePagerStreamsWholeSourceWithoutChangingIt(t *testing.T) {
	t.Setenv("TUI_PAGER_HELPER", "1")
	t.Setenv("LESSSECURE", "0")
	root := t.TempDir()
	name := "-source ; file.py"
	content := strings.Repeat("def example():\n\treturn 'value'\n\n", maxDetailSourceBytes/16)
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	process := newBundlePagerProcess(root, name, fmt.Sprintf("%q -test.run=^TestBundlePagerHelper$", os.Args[0]))
	var output bytes.Buffer
	process.SetStdout(&output)
	process.SetStderr(io.Discard)
	process.SetStdin(strings.NewReader("not the source"))
	if err := process.Run(); err != nil {
		t.Fatal(err)
	}
	if output.String() != content {
		t.Fatalf("pager received %d bytes, want the complete %d-byte source", output.Len(), len(content))
	}
	if got := process.cmd.Args; len(got) != 2 || got[1] != "-test.run=^TestBundlePagerHelper$" {
		t.Fatalf("source path leaked into pager arguments: %q", got)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != content {
		t.Fatalf("source changed: %v", err)
	}
	if _, err := process.cmd.Stdin.(*os.File).Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("source descriptor not closed: %v", err)
	}
}

func TestBundlePagerCommandSelection(t *testing.T) {
	for _, tc := range []struct {
		pager string
		want  []string
	}{
		{"", []string{"less"}},
		{"  ", []string{"less"}},
		{`less -S`, []string{"less", "-S"}},
		{`"/path with spaces/pager" --label "source view"`, []string{"/path with spaces/pager", "--label", "source view"}},
	} {
		t.Run(tc.pager, func(t *testing.T) {
			process := newBundlePagerProcess("/unused", "file.py", tc.pager)
			if !reflect.DeepEqual(process.cmd.Args, tc.want) {
				t.Fatalf("args = %q, want %q", process.cmd.Args, tc.want)
			}
		})
	}
}

func TestBundlePagerRejectsUnsafeSourcesBeforeLaunching(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("outside data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "directory"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(root, "fifo"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../outside", outside, "link", "directory", "fifo", "missing"} {
		t.Run(name, func(t *testing.T) {
			process := newBundlePagerProcess(root, name, os.Args[0])
			if err := process.Run(); err == nil {
				t.Fatal("unsafe source was accepted")
			}
			if process.cmd.Process != nil {
				t.Fatal("pager launched for unsafe source")
			}
		})
	}
}

func TestBundlePagerMissingExecutableClosesSource(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "source.py"), []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	process := newBundlePagerProcess(root, "source.py", filepath.Join(root, "missing-pager"))
	if err := process.Run(); err == nil {
		t.Fatal("missing pager did not fail")
	}
	if _, err := process.cmd.Stdin.(*os.File).Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("source descriptor not closed: %v", err)
	}
}

func bundlePagerTestModel(t testing.TB) Model {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "source.py")
	if err := os.WriteFile(path, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := mkRow("source", withProject)
	r.Artifact.Path = path
	m := modelWithSelectedRow(t, r)
	cmd := m.syncInlineBundle()
	updated, _ := m.Update(cmd())
	m = updated.(Model)
	m.enterDetailPane()
	m.detailRenderWidth = m.previewWidth()
	m.detailViewport.SetContent(strings.Repeat("preview\n", 100))
	m.detailViewport.SetYOffset(7)
	return m
}

func TestBundlePagerKeyAndReturnPreserveBrowser(t *testing.T) {
	m := bundlePagerTestModel(t)
	t.Setenv("PAGER", "missing-pager")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if cmd == nil || m.operations.active == nil || m.operations.active.kind != operationPager {
		t.Fatal("Enter did not schedule a pager operation")
	}
	operation := m.operations.active.id
	for _, err := range []error{nil, errors.New("missing pager\x1b[2J")} {
		next, _ := m.Update(bundlePagerClosedMsg{operation: operation, err: err})
		got := next.(Model)
		if got.pane != paneDetail || got.detailSelectedPath != "source.py" || got.bundle.cursor != m.bundle.cursor || got.detailViewport.YOffset != 7 {
			t.Fatal("return from pager changed browser state")
		}
		if got.operations.active != nil {
			t.Fatal("pager operation not released")
		}
		if err != nil {
			view := ansi.Strip(got.View())
			if !strings.Contains(view, "pager error:") || strings.Contains(view, "\x1b[2J") {
				t.Fatalf("pager failure missing or unsafe in detail view: %q", view)
			}
		}
	}
	_, blocked := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if _, ok := blocked().(toastMsg); !ok {
		t.Fatal("overlapping pager operation was not blocked")
	}
	stale, _ := m.Update(bundlePagerClosedMsg{operation: operation + 1})
	if stale.(Model).operations.active == nil {
		t.Fatal("stale pager completion cleared current operation")
	}
	for _, width := range []int{50, 80, 120} {
		m.width = width
		view := ansi.Strip(m.View())
		if !strings.Contains(view, "enter open") {
			t.Fatalf("pager key missing at width %d: %q", width, view)
		}
		assertTerminalBounds(t, m.View(), width, m.height)
	}
	if !strings.Contains(ansi.Strip(renderHelp(m)), "$PAGER") {
		t.Fatal("pager action missing from help")
	}
}

type pagerProgramModel struct{ Model }

func (m pagerProgramModel) Init() tea.Cmd { return nil }

func (m pagerProgramModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	updated, cmd := m.Model.Update(msg)
	if _, ok := msg.(bundlePagerClosedMsg); ok {
		return updated, tea.Quit
	}
	return pagerProgramModel{updated.(Model)}, cmd
}

func TestBundlePagerProgramResumesAfterProcessExit(t *testing.T) {
	t.Setenv("TUI_PAGER_HELPER", "1")
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			pager := fmt.Sprintf("%q -test.run=^TestBundlePagerHelper$", os.Args[0])
			if fail {
				pager = filepath.Join(t.TempDir(), "missing-pager")
			}
			t.Setenv("PAGER", pager)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			program := tea.NewProgram(pagerProgramModel{bundlePagerTestModel(t)}, tea.WithContext(ctx), tea.WithInput(strings.NewReader("")), tea.WithOutput(io.Discard), tea.WithoutRenderer(), tea.WithoutSignalHandler())
			go program.Send(tea.KeyMsg{Type: tea.KeyEnter})
			result, err := program.Run()
			if err != nil {
				t.Fatal(err)
			}
			m := result.(Model)
			if m.pane != paneDetail || m.detailSelectedPath != "source.py" || m.detailViewport.YOffset != 7 || m.operations.active != nil {
				t.Fatal("pager did not resume the original browser")
			}
			if strings.Contains(m.toast, "pager error:") != fail {
				t.Fatalf("pager completion error = %q, want failure=%v", m.toast, fail)
			}
		})
	}
}

func TestBundlePagerWithoutSelectedFileDoesNotLaunch(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	m.pane = paneDetail
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("pager launched without a selected file")
	}
	if err := os.Mkdir(filepath.Join(m.repoRoot, "directory"), 0o755); err != nil {
		t.Fatal(err)
	}
	m.bundle = newBundleTree(m.repoRoot, "")
	if entry, ok := m.bundle.selectedEntry(); !ok || !entry.IsDir {
		t.Fatal("expected a highlighted directory")
	}
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
		t.Fatal("pager launched for a directory")
	}
}
