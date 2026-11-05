package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	ltree "charm.land/lipgloss/v2/tree"
	"github.com/charmbracelet/x/ansi"
)

func renderBundleTree(m Model, width, height int) string {
	if width < 1 || height < 1 {
		return ""
	}
	if m.bundle == nil {
		return lipgloss.NewStyle().Width(width).Render(fitTerminalRows("bundle\nNo bundle tree available", width, height))
	}

	// The native tree renders the same depth-first order as m.bundle.entries():
	// line i+1 below the root corresponds to visible entry i, so the cursor,
	// the scroll offset and the per-entry styling all keep their meaning.
	tr := ltree.Root(bundleTreeRootLabel(m.bundle))
	tr.RootStyle(styleDetailLabel)
	expandedDirs := map[*ltree.Tree]bool{}
	var build func(parent *ltree.Tree, node *bundleNode)
	build = func(parent *ltree.Tree, node *bundleNode) {
		value := sanitizeTerminalData(node.name, terminalSingleLine)
		if node.err != nil {
			value += " [error]"
		}
		child := ltree.Root(value)
		parent.Child(child)
		if !node.isDir {
			return
		}
		expandedDirs[child] = node.expanded
		if !node.expanded {
			return
		}
		for _, grandchild := range node.children {
			build(child, grandchild)
		}
	}
	for _, child := range m.bundle.rootNode.children {
		build(tr, child)
	}
	tr.Enumerator(func(children ltree.Children, index int) string {
		if dir, ok := children.At(index).(*ltree.Tree); ok {
			if expandedDirs[dir] {
				return "▾ "
			}
			return "▸ "
		}
		return "• "
	})

	lines := strings.Split(tr.String(), "\n")
	out := make([]string, 0, height)
	// The root label stays on one row, as clicks map row 0 to it and row i+1
	// to visible entry i.
	out = append(out, ansi.Truncate(lines[0], width, "…"))
	if m.bundle.rootErr != nil {
		out = append(out, "  Error: "+sanitizeTerminalData(m.bundle.rootErr.Error(), terminalSingleLine))
	} else if len(m.bundle.entries()) == 0 {
		out = append(out, "  (empty bundle)")
	}
	entries := m.bundle.entries()
	start := m.bundleOffset
	if start > len(entries) {
		start = len(entries)
	}
	for i := start; i < len(entries) && len(out) < height && i+1 < len(lines); i++ {
		line := ansi.Truncate(lines[i+1], width, "")
		if i == m.bundle.cursor {
			line = styleSelected.Render(line)
		}
		out = append(out, line)
	}
	return lipgloss.NewStyle().Width(width).Height(height).Render(strings.Join(out, "\n"))
}
