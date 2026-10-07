package tui

import (
	"sort"

	tea "charm.land/bubbletea/v2"
)

// pendingClick is a left press over the resource list or bundle tree that
// acts only if it is released in the same cell, so drags stay inert.
type pendingClick struct {
	pane   pane
	x, y   int
	active bool
}

// routeNavigationClick consumes press, motion, and release events that belong
// to a navigation click. It reports handled=false for every other event so the
// right-hand text-selection path still receives them.
func (m *Model) routeNavigationClick(msg tea.Msg, snap previewSelectionView) (tea.Model, tea.Cmd, bool) {
	switch ev := msg.(type) {
	case tea.MouseClickMsg:
		m.pendingClick = pendingClick{}
		if !m.clickTargetsNavigation(ev.Mouse(), snap) {
			return *m, nil, false
		}
		m.pendingClick = pendingClick{pane: m.pane, x: ev.X, y: ev.Y, active: true}
		return *m, nil, true
	case tea.MouseMotionMsg:
		if !m.pendingClick.active {
			return *m, nil, false
		}
		if ev.X != m.pendingClick.x || ev.Y != m.pendingClick.y {
			m.pendingClick = pendingClick{}
		}
		return *m, nil, true
	case tea.MouseReleaseMsg:
		if !m.pendingClick.active {
			return *m, nil, false
		}
		press := m.pendingClick
		m.pendingClick = pendingClick{}
		// Release reports may omit the button, so the press alone proves it
		// was a left click.
		target := ev.Mouse()
		target.Button = tea.MouseLeft
		if ev.X != press.x || ev.Y != press.y || press.pane != m.pane || !m.clickTargetsNavigation(target, snap) {
			return *m, nil, true
		}
		switch m.pane {
		case paneList:
			next, cmd := m.clickResourceList(ev.Y)
			return next, cmd, true
		case paneDetail:
			next, cmd := m.clickBundleTree(ev.Y)
			return next, cmd, true
		}
		return *m, nil, true
	}
	return *m, nil, false
}

func (m Model) clickTargetsNavigation(ev tea.Mouse, snap previewSelectionView) bool {
	if ev.Button != tea.MouseLeft || ev.Mod != 0 {
		return false
	}
	if m.belowMinimumTerminal() || m.palette.open || m.filterActive {
		return false
	}
	listWidth, _ := paneWidths(m.width)
	if ev.X < 0 || ev.X >= listWidth {
		return false
	}
	switch m.pane {
	case paneList:
		top, height, ok := m.resourceListRows()
		return ok && ev.Y >= top && ev.Y < top+height && !snap.masked(ev.X, ev.Y)
	case paneDetail:
		return m.bundle != nil && ev.Y >= 1 && ev.Y < max(1, m.height-1)
	}
	return false
}

// resourceListRows returns the first screen row and height of the resource
// list, mirroring renderBaseScreen.
func (m Model) resourceListRows() (top, height int, ok bool) {
	layout := screenLayoutFor(m.height, m.searchRowVisible())
	if !layout.header || layout.mainHeight <= 0 {
		return 0, 0, false
	}
	top = 1
	if layout.search {
		top++
	}
	return top, layout.mainHeight, true
}

func (m Model) clickResourceList(y int) (tea.Model, tea.Cmd) {
	top, visible, ok := m.resourceListRows()
	if !ok || len(m.flatItems) == 0 {
		return m, nil
	}
	listWidth, _ := paneWidths(m.width)
	heights := m.listHeightOffsets(listWidth)
	start := min(max(0, m.listOffset), len(m.flatItems)-1)
	target := heights[start] + (y - top)
	if target >= heights[len(m.flatItems)] {
		return m, nil
	}
	i := sort.Search(len(m.flatItems), func(i int) bool { return heights[i+1] > target })
	// renderList stops before an item that would overflow the pane, so the
	// leftover rows show nothing and must not select an invisible item.
	if i != start && heights[i+1]-heights[start] > visible {
		return m, nil
	}
	m.previewSel = previewSelection{}
	if m.flatItems[i].Kind == flatSection || i == m.cursor {
		m.cursor = i
		return m.dispatchAction(actionDetail)
	}
	m.cursor = i
	m.clampListOffset()
	return m, nil
}

func (m Model) clickBundleTree(y int) (tea.Model, tea.Cmd) {
	entries := m.bundle.entries()
	if m.bundle.rootErr != nil || len(entries) == 0 {
		return m, nil
	}
	// Row 0 is the bundle root label; entries follow from the scroll offset.
	i := m.bundleOffset + y - 1
	if i < 0 || i >= len(entries) {
		return m, nil
	}
	m.previewSel = previewSelection{}
	before := m.bundle.selectedPath()
	m.bundle.move(i - m.bundle.cursor)
	if entries[i].IsDir {
		m.bundle.toggle()
	}
	m.ensureBundleCursorVisible()
	if selected := m.bundle.selectedPath(); selected != "" && selected != before {
		m.detailSelectedPath = selected
		return m.queueBundlePreview(false)
	}
	return m, nil
}
