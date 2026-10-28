package tui

import (
	"errors"
	"github.com/charmbracelet/x/ansi"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestEditConfigKeyOpensModal(t *testing.T) {
	repoDir, homeDir := t.TempDir(), t.TempDir()
	m := NewModel(repoDir, homeDir)
	m.pane = paneList

	mi, _ := m.Update(tea.KeyPressMsg{Code: 'E', Text: string('E')})
	m = mi.(Model)

	if m.pane != paneEditConfigModal {
		t.Fatalf("expected pane=paneEditConfigModal, got %v", m.pane)
	}
}

func TestEditConfigModalCancelReturnsToList(t *testing.T) {
	repoDir, homeDir := t.TempDir(), t.TempDir()
	m := NewModel(repoDir, homeDir)
	m.pane = paneEditConfigModal

	mi, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = mi.(Model)

	if m.pane != paneList {
		t.Fatalf("expected pane=paneList after esc, got %v", m.pane)
	}
}

func TestEditConfigModalScopeLaunchesEditor(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  rune
	}{
		{name: "global", key: 'g'},
		{name: "project", key: 'p'},
		{name: "local", key: 'l'},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repoDir, homeDir := t.TempDir(), t.TempDir()
			t.Setenv("HOME", homeDir)
			m := NewModel(repoDir, homeDir)
			m.pane = paneEditConfigModal

			mi, cmd := m.Update(tea.KeyPressMsg{Code: tc.key, Text: string(tc.key)})
			m = mi.(Model)

			if m.pane != paneList {
				t.Fatalf("expected pane=paneList after selecting scope, got %v", m.pane)
			}
			if cmd == nil {
				t.Fatal("expected a cmd that launches the editor")
			}
			if msg := cmd(); msg != nil {
				if toast, ok := msg.(toastMsg); ok {
					t.Fatalf("expected editor command, got toast %q", toast.text)
				}
			}
		})
	}
}

func TestRenderEditConfigModalContainsAllScopes(t *testing.T) {
	repoDir, homeDir := t.TempDir(), t.TempDir()
	m := NewModel(repoDir, homeDir)
	m.pane = paneEditConfigModal

	out := ansi.Strip(renderEditConfigModal(m))
	for _, want := range []string{"Edit Config", "[g]", "[p]", "[l]", "esc cancel"} {
		if !strings.Contains(out, want) {
			t.Fatalf("renderEditConfigModal missing %q", want)
		}
	}
}

// The message emitted when the editor process exits comes from Bubble Tea's
// exec callback, not from the command that started the editor, so it must
// carry the editor operation itself.
// spec: tui-rendering/editor-exit-reloads-and-frees-writes
func TestEditorExitReloadsAndFreesLaterWrites(t *testing.T) {
	t.Setenv("EDITOR", "true")
	var exited tea.ExecCallback
	restore := execEditor
	execEditor = func(_ tea.ExecCommand, fn tea.ExecCallback) tea.Cmd {
		exited = fn
		return func() tea.Msg { return nil }
	}
	t.Cleanup(func() { execEditor = restore })

	m := NewModel(t.TempDir(), t.TempDir())
	m.loading = false
	m.pane = paneList
	mi, _ := m.Update(keyPress('E'))
	m = mi.(Model)
	mi, cmd := m.Update(keyPress('p'))
	m = mi.(Model)
	if cmd == nil {
		t.Fatal("p in the config picker did not start the editor")
	}
	cmd()
	if exited == nil {
		t.Fatal("the editor command never reached the exec boundary")
	}
	before := m.requests.Active(requestSnapshot)

	mi, cmd = m.Update(exited(nil))
	m = mi.(Model)
	if cmd == nil || m.requests.Active(requestSnapshot) == before {
		t.Fatal("the editor exit did not reload the edited configuration")
	}
	if _, ok := m.beginOperation(operationMutation); !ok {
		t.Fatal("the editor operation is still active, so later config writes are refused")
	}
}

func TestEditConfigMsgReloadsSnapshot(t *testing.T) {
	repoDir, homeDir := t.TempDir(), t.TempDir()
	m := NewModel(repoDir, homeDir)
	m.pane = paneList
	operation, ok := m.beginOperation(operationEditor)
	if !ok {
		t.Fatal("editor operation was blocked")
	}

	mi, cmd := m.Update(editConfigPathMsg{operation: operation, path: "/repo/.agents/config.yaml"})
	m = mi.(Model)

	if cmd == nil {
		t.Fatal("expected loadSnapshot cmd after editConfigPathMsg")
	}
}

func TestEditConfigMsgErrorIsRecorded(t *testing.T) {
	repoDir, homeDir := t.TempDir(), t.TempDir()
	m := NewModel(repoDir, homeDir)
	m.pane = paneList
	priorErr := errors.New("prior")
	editorErr := errors.New("boom")
	m.errs = []error{priorErr}
	operation, ok := m.beginOperation(operationEditor)
	if !ok {
		t.Fatal("editor operation was blocked")
	}

	mi, cmd := m.Update(editConfigPathMsg{operation: operation, path: "/repo/.agents/config.yaml", err: editorErr})
	m = mi.(Model)

	// The command is a delayed toast expiry timer; do not invoke it here.
	if cmd == nil {
		t.Fatal("expected toast expiry command")
	}
	if !strings.Contains(m.toast, editorErr.Error()) {
		t.Fatalf("expected error toast, got %q", m.toast)
	}
	if len(m.errs) != 2 || !errors.Is(m.errs[0], priorErr) || !errors.Is(m.errs[1], editorErr) {
		t.Fatalf("expected editor error to be appended, got %v", m.errs)
	}
}

func TestOpenEditorCmdUsesEditor(t *testing.T) {
	t.Setenv("EDITOR", "cat")

	cmd := openEditorCmd("/some/path", 1)
	if cmd == nil {
		t.Fatal("expected a non-nil cmd")
	}
}

func TestOpenEditorCmdFallsBackToVi(t *testing.T) {
	t.Setenv("EDITOR", "")

	cmd := openEditorCmd("/some/path", 1)
	if cmd == nil {
		t.Fatal("expected a non-nil cmd")
	}
}

func TestOpenEditorCmdCreatesConfigDirectory(t *testing.T) {
	t.Setenv("EDITOR", "cat")
	path := filepath.Join(t.TempDir(), ".agents", "config.yaml")

	msg := openEditorCmd(path, 7)()

	if _, ok := msg.(editConfigPathMsg); ok {
		t.Fatalf("expected editor command, got %T", msg)
	}
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Fatalf("expected config directory to exist: %v", err)
	}
}

func TestOpenEditorCmdReportsDirectoryCreationError(t *testing.T) {
	t.Setenv("EDITOR", "cat")
	parent := filepath.Join(t.TempDir(), ".agents")
	if err := os.WriteFile(parent, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write parent file: %v", err)
	}
	path := filepath.Join(parent, "config.yaml")

	msg := openEditorCmd(path, 7)()

	result, ok := msg.(editConfigPathMsg)
	if !ok {
		t.Fatalf("expected editConfigPathMsg, got %T", msg)
	}
	if result.path != path || result.err == nil || result.operation != 7 {
		t.Fatalf("expected directory error for %q from operation 7, got path=%q err=%v operation=%d", path, result.path, result.err, result.operation)
	}
}

func TestSplitEditorCommand(t *testing.T) {
	for _, tc := range []struct {
		name string
		cmd  string
		want []string
	}{
		{name: "empty", want: []string{"vi"}},
		{name: "arguments", cmd: "vim -u none", want: []string{"vim", "-u", "none"}},
		{name: "double quoted", cmd: `vim -c "it's fine"`, want: []string{"vim", "-c", "it's fine"}},
		{name: "single quoted", cmd: `vim -c 'say "hello"'`, want: []string{"vim", "-c", `say "hello"`}},
		{name: "quoted executable", cmd: `"/Applications/Visual Studio Code.app/bin/code" --wait`, want: []string{"/Applications/Visual Studio Code.app/bin/code", "--wait"}},
		{name: "empty quoted argument", cmd: `vim -c ""`, want: []string{"vim", "-c", ""}},
		{name: "tab separated", cmd: "vim\t-u\tnone", want: []string{"vim", "-u", "none"}},
		{name: "adjacent quoted segment", cmd: `code --option="value with spaces"`, want: []string{"code", "--option=value with spaces"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := splitEditorCommand(tc.cmd); !slices.Equal(got, tc.want) {
				t.Fatalf("splitEditorCommand(%q) = %q, want %q", tc.cmd, got, tc.want)
			}
		})
	}
}

func TestEditConfigKeyBindingInKeyMap(t *testing.T) {
	k := defaultKeyMap
	if !k.EditConfig.Enabled() {
		t.Fatalf("EditConfig key binding should be enabled")
	}
	help := k.EditConfig.Help()
	if help.Key != "E" {
		t.Fatalf("expected EditConfig help key 'E', got %q", help.Key)
	}
}

func TestEditConfigModalBindingsEnabled(t *testing.T) {
	k := defaultKeyMap
	for _, b := range k.editConfigModalBindings() {
		if !b.Enabled() {
			t.Fatalf("edit config modal binding should be enabled: %v", b.Help())
		}
	}
}
