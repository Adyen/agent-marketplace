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
	m.listHeightPrefix = m.listHeightPrefix[:0]
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

// rowKey is the stable identity of a list row across snapshot rebuilds:
// catalog rows key on kind/name/registry coordinates plus the source path,
// orphaned output on its drift identity. Never user-visible; the NUL
// separator cannot collide with untrusted field content because those
// fields are sanitized before display, not before identity use.
func rowKey(r row) string {
	if r.isOrphanOutput() && r.DriftItem != nil {
		return "orphan\x00" + r.DriftItem.Path + "\x00" + string(r.DriftItem.Target) + "\x00" + r.DriftItem.OutDir
	}
	malformed := "0"
	if r.IsMalformed {
		malformed = "1"
	}
	return r.Artifact.Kind.String() + "\x00" + r.Artifact.Name + "\x00" + r.RegistryURL + "\x00" + r.RegistryPath + "\x00" + r.Artifact.Path + "\x00" + malformed
}

// flatItemKey is the stable identity of one flat list entry: a section
// header keys on its section path, a resource row on its rowKey.
func flatItemKey(it flatItem) string {
	if it.Kind == flatSection {
		return "S\x00" + it.SectionPath
	}
	return "R\x00" + rowKey(it.Row)
}

// selectionKey captures the stable identity of the item under the cursor so
// a snapshot rebuild can restore the selection to the same resource or
// section instead of the same numeric index. Empty when the cursor is out
// of range or there is nothing to preserve.
func (m Model) selectionKey() string {
	if m.cursor < 0 || m.cursor >= len(m.flatItems) {
		return ""
	}
	return flatItemKey(m.flatItems[m.cursor])
}

// restoreSelectionKey moves the cursor back to the entry with the captured
// identity after a rebuild. Returns false when the identity no longer
// exists; the caller then falls back to numeric clamping.
func (m *Model) restoreSelectionKey(key string) bool {
	if key == "" {
		return false
	}
	for i, it := range m.flatItems {
		if flatItemKey(it) == key {
			m.cursor = i
			m.clampListOffset()
			return true
		}
	}
	return false
}

// selectionKeyForRebuild captures the identity that must survive an
// accepted snapshot rebuild: while the detail pane is open that is the open
// resource (the list cursor is frozen behind it), otherwise the item under
// the cursor.
func (m Model) selectionKeyForRebuild() string {
	if m.pane == paneDetail && m.detailKey != "" {
		return m.detailKey
	}
	return m.selectionKey()
}

// restoreSelectionAfterRebuild re-anchors the cursor to the captured
// identity after recomputeView. An active filter keeps its snap-to-first
// match behavior; identity restore applies only to the unfiltered view.
func (m *Model) restoreSelectionAfterRebuild(key string) {
	if m.filter != "" {
		return
	}
	m.restoreSelectionKey(key)
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
