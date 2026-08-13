package tui

// flat-items helpers on Model: the cursor indexes into m.flatItems, which is
// computed from filteredRows + activeTab + per-tab foldState. These helpers
// keep the transformation and fold-state mutation in one place.

// currentFoldState returns the fold-state map for the active tab, creating it
// lazily if the tabStates bucket is missing.
func (m *Model) currentFoldState() map[string]bool {
	if m.tabStates == nil {
		m.tabStates = newTabStates()
	}
	st, ok := m.tabStates[m.activeTab]
	if !ok || st == nil {
		st = &tabViewState{foldedSections: make(map[string]bool)}
		m.tabStates[m.activeTab] = st
	}
	if st.foldedSections == nil {
		st.foldedSections = make(map[string]bool)
	}
	return st.foldedSections
}

// rebuildFlatItems recomputes m.flatItems from m.filteredRows, the active
// tab, and the active tab's fold state. When a filter is active, fold state
// is overridden so that sections containing matches render expanded. The
// user's persisted fold state in tabStates is untouched and restored as
// soon as the filter clears.
func (m *Model) rebuildFlatItems() {
	fold := m.currentFoldState()
	if m.filter != "" {
		fold = map[string]bool{}
	}
	m.flatItems = buildTree(m.filteredRows, m.activeTab, fold)
}

// selectedRow returns the skill row the cursor currently points to, if any.
// Returns (row{}, false) when the cursor is on a section header or out of
// range.
func (m Model) selectedRow() (row, bool) {
	if m.cursor < 0 || m.cursor >= len(m.flatItems) {
		return row{}, false
	}
	it := m.flatItems[m.cursor]
	if it.Kind != flatArtifact {
		return row{}, false
	}
	return it.Row, true
}

// selectedSectionPath returns the section path under the cursor, if the
// cursor is on a section header.
func (m Model) selectedSectionPath() (string, bool) {
	if m.cursor < 0 || m.cursor >= len(m.flatItems) {
		return "", false
	}
	it := m.flatItems[m.cursor]
	if it.Kind != flatSection {
		return "", false
	}
	return it.SectionPath, true
}

// toggleFoldAtCursor flips the fold state of the section under the cursor.
// No-op if the cursor is not on a section header.
func (m *Model) toggleFoldAtCursor() {
	path, ok := m.selectedSectionPath()
	if !ok {
		return
	}
	fs := m.currentFoldState()
	fs[path] = !fs[path]
}

// setFoldAtCursor forces the fold state of the section under the cursor
// (or the parent section of a skill row) to `collapsed`. Used by h/l.
func (m *Model) setFoldAtCursor(collapsed bool) {
	if m.cursor < 0 || m.cursor >= len(m.flatItems) {
		return
	}
	it := m.flatItems[m.cursor]
	fs := m.currentFoldState()
	switch it.Kind {
	case flatSection:
		fs[it.SectionPath] = collapsed
	case flatArtifact:
		// Walk backward to find the nearest enclosing section header.
		for i := m.cursor - 1; i >= 0; i-- {
			prev := m.flatItems[i]
			if prev.Kind != flatSection {
				continue
			}
			if collapsed {
				// h on a skill row should collapse its direct parent section
				// (the deepest enclosing one) and then move the cursor to that
				// header so subsequent navigation is predictable.
				fs[prev.SectionPath] = true
				m.cursor = i
			}
			// l on a skill row is a no-op: the section is already expanded
			// since the skill itself is visible.
			return
		}
	}
}

// collapseAllSections collapses every section currently present in
// m.flatItems for the active tab.
func (m *Model) collapseAllSections() {
	fs := m.currentFoldState()
	for _, it := range m.flatItems {
		if it.Kind == flatSection {
			fs[it.SectionPath] = true
		}
	}
}

// expandAllSections clears the fold-state map for the active tab, expanding
// every section.
func (m *Model) expandAllSections() {
	if m.tabStates == nil {
		return
	}
	st, ok := m.tabStates[m.activeTab]
	if !ok || st == nil {
		return
	}
	st.foldedSections = make(map[string]bool)
}

// recomputeView is called after any input change (rows/filter/fold/tab). It
// reapplies the filter, rebuilds the flat item list, and adjusts the cursor.
// When a filter is active, the cursor snaps to the first matching skill so
// arrow-keys don't start from a section header or a stale row.
func (m *Model) recomputeView() {
	m.filteredRows = m.applyFilter()
	m.rebuildFlatItems()

	if len(m.flatItems) == 0 {
		m.cursor = 0
		m.listOffset = 0
		return
	}

	if m.filter != "" {
		// Snap cursor to the first skill row (preferred) or section header.
		for i, it := range m.flatItems {
			if it.Kind == flatArtifact {
				m.cursor = i
				m.clampListOffset()
				return
			}
		}
		m.cursor = 0
		m.clampListOffset()
		return
	}

	// No active filter: keep cursor within bounds.
	if m.cursor >= len(m.flatItems) {
		m.cursor = len(m.flatItems) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.clampListOffset()
}
