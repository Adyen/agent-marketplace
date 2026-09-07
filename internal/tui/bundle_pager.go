package tui

import (
	"log/slog"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type bundlePagerClosedMsg struct {
	operation operationID
	err       error
}

type bundlePagerProcess struct {
	*editorProcess
	root     string
	relative string
}

func newBundlePagerProcess(root, relative, pager string) *bundlePagerProcess {
	pager = strings.TrimSpace(pager)
	if pager == "" {
		pager = "less"
	}
	parts := splitEditorCommand(pager)
	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Env = append(os.Environ(), "LESSSECURE=1")
	return &bundlePagerProcess{editorProcess: newEditorProcess(cmd), root: root, relative: relative}
}

func (p *bundlePagerProcess) Run() error {
	file, err := openBundleFile(p.root, p.relative)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	p.cmd.Stdin = file
	return p.editorProcess.Run()
}

func (m Model) openBundlePager() (tea.Model, tea.Cmd) {
	if m.bundle == nil {
		return m, nil
	}
	entry, ok := m.bundle.selectedEntry()
	if !ok || entry.IsDir || entry.Err != nil {
		return m, nil
	}
	operation, ok := m.beginOperation(operationPager)
	if !ok {
		return m, operationBlockedCmd()
	}
	process := newBundlePagerProcess(m.bundle.root, entry.RelPath, os.Getenv("PAGER"))
	slog.Debug("opening bundle pager", "path", entry.RelPath)
	return m, tea.Exec(process, func(err error) tea.Msg {
		slog.Debug("bundle pager exited", "err", err)
		return bundlePagerClosedMsg{operation: operation, err: err}
	})
}
