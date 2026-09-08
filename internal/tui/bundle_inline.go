package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type bundleLoadedMsg struct {
	generation requestGeneration
	key        string
	tree       *bundleTree
	err        error
}

func (m *Model) syncInlineBundle() tea.Cmd {
	r, ok := m.selectedRow()
	key := ""
	if ok {
		key = rowKey(r) + "\x00" + r.Artifact.Path
	}
	if key == m.bundleKey && !m.bundleDirty {
		return nil
	}
	selected := ""
	if key == m.bundleKey && m.bundle != nil {
		if entry, exists := m.bundle.selectedEntry(); exists {
			selected = entry.RelPath
		}
	}
	m.bundleDirty = false
	m.cancelRequest(requestBundle)
	m.cancelRequest(requestDetail)
	m.bundleKey = key
	m.bundle = nil
	m.bundleError = ""
	m.bundleOffset = 0
	m.detailSelectedPath = ""
	m.detailRenderWidth = 0
	m.detailLegacyRender = false
	m.detailViewport.SetContent("")
	m.detailViewport.GotoTop()
	if !ok {
		m.detailKey = ""
		if m.pane == paneDetail {
			m.pane = paneList
		}
		return nil
	}
	m.detailKey = rowKey(r)
	if r.Artifact.Path == "" {
		m.bundleError = "No source available"
		return nil
	}
	generation := m.beginRequest(requestBundle)
	return func() tea.Msg {
		tree, err := bundleTreeForRow(r)
		if err == nil && selected != "" {
			tree.restoreSelection(selected)
		}
		return bundleLoadedMsg{generation: generation, key: key, tree: tree, err: err}
	}
}

func (m Model) bundlePaneHeights() (summary, tree, preview int) {
	height := screenLayoutFor(m.height, m.filterActive).mainHeight
	summary = min(7, max(1, height/3))
	tree = max(1, height-summary)
	if m.pane == paneDetail && tree >= 12 {
		treeRows := tree - tree/2
		if m.bundle != nil {
			treeRows = min(treeRows, max(4, len(m.bundle.entries())+1))
		}
		preview = tree - treeRows
		tree = treeRows
	}
	return
}

func (m *Model) sizeBundlePreview() {
	_, _, height := m.bundlePaneHeights()
	m.detailViewport.Width = m.previewWidth()
	m.detailViewport.Height = max(1, height-1)
	m.ensureBundleCursorVisible()
}

func (m Model) renderBundlePane(width, height int) string {
	if _, ok := m.selectedRow(); !ok {
		return fitTerminalRows(renderDetail(m), width, height)
	}
	summaryHeight, treeHeight, previewHeight := m.bundlePaneHeights()
	summary := strings.Split(strings.TrimRight(renderDetail(m), "\n"), "\n")
	if len(summary) > summaryHeight {
		summary = summary[:summaryHeight]
		summary[len(summary)-1] = ansi.Truncate(summary[len(summary)-1], max(1, width-1), "") + "…"
	}
	header := fitTerminalRows(strings.Join(summary, "\n"), width, summaryHeight)
	tree := renderBundleTree(m, width, treeHeight)
	parts := []string{header, tree}
	if previewHeight > 0 {
		preview := m.detailViewport
		preview.Width = width
		preview.Height = previewHeight - 1
		label := "Preview"
		if entry, ok := m.bundle.selectedEntry(); ok && !entry.IsDir {
			label += " · " + sanitizeTerminalData(entry.RelPath, terminalSingleLine)
		} else {
			preview.SetContent("Select a file to preview")
		}
		parts = append(parts, ansi.Truncate(styleDetailLabel.Render(label), width, ""), preview.View())
	}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}
