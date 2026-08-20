package tui

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestEditConfigKeyOpensModal(t *testing.T) {
	repoDir, homeDir := t.TempDir(), t.TempDir()
	m := NewModel(repoDir, homeDir)
	seedTestTerminalSize(&m)
	m.pane = paneList

	mi, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'E'}})
	m = mi.(Model)

	if m.pane != paneEditConfigModal {
		t.Fatalf("expected pane=paneEditConfigModal, got %v", m.pane)
	}
}

func TestEditConfigModalCancelReturnsToList(t *testing.T) {
	repoDir, homeDir := t.TempDir(), t.TempDir()
	m := NewModel(repoDir, homeDir)
	seedTestTerminalSize(&m)
	m.pane = paneEditConfigModal

	mi, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
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
			seedTestTerminalSize(&m)
			m.pane = paneEditConfigModal

			mi, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{tc.key}})
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
	seedTestTerminalSize(&m)
	m.pane = paneEditConfigModal

	out := renderEditConfigModal(m)
	for _, want := range []string{"Edit Config", "[g]", "[p]", "[l]", "[esc]"} {
		if !strings.Contains(out, want) {
			t.Fatalf("renderEditConfigModal missing %q", want)
		}
	}
}

func TestEditConfigMsgReloadsSnapshot(t *testing.T) {
	repoDir, homeDir := t.TempDir(), t.TempDir()
	m := NewModel(repoDir, homeDir)
	seedTestTerminalSize(&m)
	m.pane = paneList
	id, ok := m.beginOperation(operationEditor, nil)
	if !ok {
		t.Fatal("editor operation did not start")
	}

	mi, cmd := m.Update(editConfigPathMsg{operation: id, path: "/repo/.agents/config.yaml"})
	m = mi.(Model)

	if cmd == nil {
		t.Fatal("expected loadSnapshot cmd after editConfigPathMsg")
	}
}

func TestEditConfigMsgErrorIsRecorded(t *testing.T) {
	repoDir, homeDir := t.TempDir(), t.TempDir()
	m := NewModel(repoDir, homeDir)
	seedTestTerminalSize(&m)
	m.pane = paneList
	priorErr := errors.New("prior")
	editorErr := errors.New("boom")
	m.diagnostics.recordOperationFailure(operationSync, diagnosticSync, priorErr)
	id, ok := m.beginOperation(operationEditor, nil)
	if !ok {
		t.Fatal("editor operation did not start")
	}

	mi, cmd := m.Update(editConfigPathMsg{operation: id, path: "/repo/.agents/config.yaml", err: editorErr})
	m = mi.(Model)

	// The command batches an authoritative reload with a delayed toast
	// expiry timer; do not invoke it here.
	if cmd == nil {
		t.Fatal("expected reload + toast expiry command")
	}
	if !strings.Contains(m.toast, editorErr.Error()) {
		t.Fatalf("expected error toast, got %q", m.toast)
	}
	diagnostics := m.diagnostics.bySeverity(diagnosticError)
	if len(diagnostics) != 2 || !errors.Is(diagnostics[0].err, priorErr) || !errors.Is(diagnostics[1].err, editorErr) {
		t.Fatalf("expected editor error to be appended, got %v", diagnostics)
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

	msg := openEditorCmd(path, 1)()

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
	if result.path != path || result.err == nil {
		t.Fatalf("expected directory error for %q, got path=%q err=%v", path, result.path, result.err)
	}
	if result.operation != 7 {
		t.Fatalf("editor result did not carry the started operation id, got %d", result.operation)
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
