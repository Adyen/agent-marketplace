package tui

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/Adyen/agent-marketplace/internal/config"
)

// editConfigPathMsg is emitted after the editor exits so the TUI reloads.
type editConfigPathMsg struct {
	operation operationID
	path      string
	err       error
}

func renderEditConfigModal(m Model) string {
	t := m.currentTheme()
	title := titleStyle(t).Render("Edit Config")
	hints := []footerHint{{"↑↓", "move"}, {"enter", "open"}, {"esc", "cancel"}}
	m.ensureEditConfigForm()

	var body []string
	if !compactTerminalLayout(m) {
		globalPath, err := config.GlobalConfigPath()
		if err != nil {
			globalPath = fmt.Sprintf("<global config path unavailable: %s>", err.Error())
		}
		width := modalInnerWidth(m) - len("Project  ")
		dim := dimTextStyle(t)
		body = append(body,
			"Global   "+dim.Render(shortenPath(globalPath, m.homeDir, width)),
			"Project  "+dim.Render(shortenPath(config.ProjectConfigPath(m.repoRoot), m.homeDir, width)),
			"Local    "+dim.Render(shortenPath(config.LocalConfigPath(m.workspace.LocalConfigDir()), m.homeDir, width)),
			"")
	}
	if m.editConfigForm != nil {
		body = append(body, m.editConfigForm.View())
	}
	return modalFrame(m, t.accent, title, body, hints)
}

func (m Model) handleEditConfigModalKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.ensureEditConfigForm()
	switch {
	case key.Matches(msg, m.keys.EditConfigCancel):
		m.pane = paneList
		return m, nil
	case key.Matches(msg, m.keys.EditConfigGlobal):
		return m.editConfigScope("g")
	case key.Matches(msg, m.keys.EditConfigProject):
		return m.editConfigScope("p")
	case key.Matches(msg, m.keys.EditConfigLocal):
		return m.editConfigScope("l")
	default:
		m.editConfigForm = updateModalForm(m.editConfigForm, msg)
		if m.editConfigForm != nil && m.editConfigForm.State == huh.StateCompleted && m.editConfigChoice != nil && *m.editConfigChoice != "" {
			choice := *m.editConfigChoice
			m.editConfigForm = nil
			return m.editConfigScope(choice)
		}
		return m, nil
	}
}

// editConfigScope opens the external editor for the config layer selected
// by its accelerator letter (g/p/l).
func (m Model) editConfigScope(letter string) (tea.Model, tea.Cmd) {
	switch letter {
	case "g":
		path, err := config.GlobalConfigPath()
		if err != nil {
			slog.Warn("config editor path resolution failed", "scope", "global", "err", err)
			m.pane = paneList
			return m, func() tea.Msg {
				return toastMsg{text: fmt.Sprintf("error: %v", err), duration: 3 * time.Second}
			}
		}
		return m.openConfigEditor("global", path)
	case "p":
		return m.openConfigEditor("project", config.ProjectConfigPath(m.repoRoot))
	case "l":
		return m.openConfigEditor("local", config.LocalConfigPath(m.workspace.LocalConfigDir()))
	}
	return m, nil
}

func (m Model) openConfigEditor(scope, path string) (tea.Model, tea.Cmd) {
	operation, ok := m.beginOperation(operationEditor)
	if !ok {
		return m, operationBlockedCmd()
	}
	slog.Debug("opening config editor", "scope", scope, "path", path)
	m.pane = paneList
	return m, correlateOperation(openEditorCmd(path), operation, false)
}

// openEditorCmd returns a tea.Cmd that suspends the TUI and launches $EDITOR
// (or vi as fallback) with the given file path. When the editor exits, the
// TUI resumes and reloads the snapshot.
func openEditorCmd(path string) tea.Cmd {
	editor := strings.TrimSpace(os.Getenv("EDITOR"))
	if editor == "" {
		editor = "vi"
	}

	// Split editor into program and arguments
	parts := splitEditorCommand(editor)

	return func() tea.Msg {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return editConfigPathMsg{path: path, err: fmt.Errorf("create config directory for %q: %w", path, err)}
		}
		c := exec.Command(parts[0], append(parts[1:], path)...)
		return tea.Exec(newEditorProcess(c).withWaitingScreen(path, parts[0]), func(err error) tea.Msg {
			slog.Debug("config editor process exited", "editor", editor, "path", path, "err", err)
			return editConfigPathMsg{path: path, err: err}
		})()
	}
}

// splitEditorCommand splits an editor command string into program and arguments.
// It handles simple quoted strings and space-separated arguments.
func splitEditorCommand(cmd string) []string {
	var parts []string
	var current strings.Builder
	var quoteChar rune
	tokenStarted := false
	flush := func() {
		if tokenStarted {
			parts = append(parts, current.String())
			current.Reset()
			tokenStarted = false
		}
	}

	for _, ch := range cmd {
		switch {
		case quoteChar != 0:
			if ch == quoteChar {
				quoteChar = 0
			} else {
				current.WriteRune(ch)
			}
		case ch == '"' || ch == '\'':
			quoteChar = ch
			tokenStarted = true
		case unicode.IsSpace(ch):
			flush()
		default:
			current.WriteRune(ch)
			tokenStarted = true
		}
	}
	flush()

	if len(parts) == 0 {
		return []string{"vi"}
	}

	return parts
}
