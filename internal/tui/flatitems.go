package tui

import (
	"strings"

	"github.com/Adyen/agent-marketplace/internal/materialize"
)

// flat-items helpers on Model: the cursor indexes into m.flatItems, which is
// computed from filteredRows + the status-grouped foldState. These helpers
// keep the transformation and fold-state mutation in one place.

// currentFoldState returns the model's fold-state map, creating it lazily.
func (m *Model) currentFoldState() map[string]bool {
	key := m.viewSettings.normalized()
	if m.filter != "" {
		if m.queryFoldSections == nil || m.queryFoldQuery != m.filter {
			m.queryFoldSections = make(map[viewSettings]map[string]bool)
			m.queryFoldQuery = m.filter
		}
		if m.queryFoldSections[key] == nil {
			m.queryFoldSections[key] = make(map[string]bool)
		}
		return m.queryFoldSections[key]
	}
	m.queryFoldSections = nil
	m.queryFoldQuery = ""
	if m.foldedSections == nil {
		m.foldedSections = make(map[viewSettings]map[string]bool)
	}
	if m.foldedSections[key] == nil {
		m.foldedSections[key] = make(map[string]bool)
	}
	return m.foldedSections[key]
}

// rebuildFlatItems recomputes m.flatItems from m.filteredRows and the fold
// state. Search folds live in a per-query map, so the user's persisted fold
// state is untouched and restored as soon as the filter clears.
func (m *Model) rebuildFlatItems() {
	m.flatItems = buildGroupedTree(m.filteredRows, m.viewSettings, m.currentFoldState())
	m.listHeightPrefix = m.listHeightPrefix[:0]
}

func (m *Model) applyViewSettings(s viewSettings) {
	s = s.normalized()
	if s == m.viewSettings.normalized() {
		m.viewSettings = s
		return
	}
	anchor := m.selectionKey()
	m.viewSettings = s
	m.filteredRows = m.applyFilter()
	m.rebuildFlatItems()
	if anchor != "" && m.restoreSelectionKey(anchor) {
		return
	}
	if rowAnchor := strings.TrimPrefix(anchor, "row\x00"); rowAnchor != anchor {
		if sectionPath := m.containingSectionPath(rowAnchor); sectionPath != "" {
			if m.restoreSelectionKey("section\x00" + sectionPath) {
				return
			}
		}
	}
	m.cursor = 0
	m.listOffset = 0
}

func (m *Model) containingSectionPath(key string) string {
	unfolded := buildGroupedTree(m.filteredRows, m.viewSettings, nil)
	stack := []string{}
	ancestors := []string{}
	for _, it := range unfolded {
		if it.Kind == flatSection {
			for len(stack) > it.SectionDepth {
				stack = stack[:len(stack)-1]
			}
			if len(stack) == it.SectionDepth {
				stack = append(stack, it.SectionPath)
			} else {
				stack[it.SectionDepth] = it.SectionPath
			}
			continue
		}
		if rowKey(it.Row) == key {
			ancestors = append([]string(nil), stack...)
			break
		}
	}
	for i := len(ancestors) - 1; i >= 0; i-- {
		want := "section\x00" + ancestors[i]
		for _, it := range m.flatItems {
			if flatItemKey(it) == want {
				return ancestors[i]
			}
		}
	}
	return ""
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
// m.flatItems.
func (m *Model) collapseAllSections() {
	fs := m.currentFoldState()
	for _, it := range m.flatItems {
		if it.Kind == flatSection {
			fs[it.SectionPath] = true
		}
	}
}

// expandAllSections clears the fold-state map, expanding every section.
func (m *Model) expandAllSections() {
	clear(m.currentFoldState())
}

// rowKey is the stable identity of a resource row across snapshot rebuilds.
// Registry coordinates disambiguate same-name resources while the source path
// keeps malformed and otherwise unattributed rows distinct.
func rowKey(r row) string {
	if d := r.MarketplaceDiscovery; d != nil {
		return "marketplace-discovery\x00" + d.Endpoint.InstanceID + "\x00" + d.Item.RegistryID + "\x00" + d.Item.Kind + "\x00" + d.Item.Name + "\x00" + d.Item.Revision + "\x00" + d.Item.ContentDigest
	}
	if r.DriftItem != nil && r.DriftItem.Reason == materialize.DriftOrphaned {
		return "orphan\x00" + r.DriftItem.Name
	}
	malformed := "0"
	if r.IsMalformed {
		malformed = "1"
	}
	return r.Artifact.Kind.String() + "\x00" + r.Artifact.Name + "\x00" + r.Artifact.CatalogKey + "\x00" + r.RegistryURL + "\x00" + r.RegistryRef + "\x00" + r.RegistryPath + "\x00" + r.Artifact.Path + "\x00" + malformed
}

// rowSelectionIdentity names a resource independently of the catalog that
// loaded it. A reload can switch a Git row from the mutable cache to a locked
// snapshot, or to a newer locked commit, which changes its catalog key and
// cache path but not the resource the user selected.
func rowSelectionIdentity(r row) string {
	if r.MarketplaceDiscovery != nil || (r.DriftItem != nil && r.DriftItem.Reason == materialize.DriftOrphaned) {
		return rowKey(r)
	}
	malformed := "0"
	if r.IsMalformed {
		malformed = "1"
	}
	return r.Artifact.Kind.String() + "\x00" + r.Artifact.Name + "\x00" + registryIdentityKey(r.RegistryURL, r.RegistryRef, r.RegistryPath) + "\x00" + malformed
}

// uniqueRowByIdentity returns the only row with identity, so a rebuild never
// guesses between resources that share it.
func uniqueRowByIdentity(rows []row, identity string) (row, bool) {
	var match row
	found := false
	for _, r := range rows {
		if rowSelectionIdentity(r) != identity {
			continue
		}
		if found {
			return row{}, false
		}
		match, found = r, true
	}
	return match, found
}

// flatItemKey adds a namespace to row and section identities so a snapshot
// rebuild cannot confuse a section path with resource data.
func flatItemKey(item flatItem) string {
	if item.Kind == flatSection {
		return "section\x00" + item.SectionPath
	}
	return "row\x00" + rowKey(item.Row)
}

func (m Model) selectionKey() string {
	if m.cursor < 0 || m.cursor >= len(m.flatItems) {
		return ""
	}
	return flatItemKey(m.flatItems[m.cursor])
}

// nextRowSelection anchors the resource below the cursor, or above it at the
// end of the list, so a disable can select it after the rebuild.
func (m Model) nextRowSelection() rebuildAnchor {
	if m.cursor < 0 || m.cursor >= len(m.flatItems) {
		return rebuildAnchor{}
	}
	for i := m.cursor + 1; i < len(m.flatItems); i++ {
		if m.flatItems[i].Kind == flatArtifact {
			return rebuildAnchor{key: flatItemKey(m.flatItems[i]), identity: rowSelectionIdentity(m.flatItems[i].Row)}
		}
	}
	for i := m.cursor - 1; i >= 0; i-- {
		if m.flatItems[i].Kind == flatArtifact {
			return rebuildAnchor{key: flatItemKey(m.flatItems[i]), identity: rowSelectionIdentity(m.flatItems[i].Row)}
		}
	}
	return rebuildAnchor{}
}

func (m *Model) restoreSelectionKey(key string) bool {
	if key == "" {
		return false
	}
	for i, item := range m.flatItems {
		if flatItemKey(item) == key {
			m.cursor = i
			m.clampListOffset()
			return true
		}
	}
	return false
}

// selectionKeyForRebuild anchors an open detail pane to the resource being
// read; otherwise it captures the list item under the cursor.
func (m Model) selectionKeyForRebuild() string {
	if m.pane == paneDetail && m.detailKey != "" {
		return "row\x00" + m.detailKey
	}
	if m.cursor == 0 && m.filter == "" {
		return ""
	}
	return m.selectionKey()
}

// rebuildAnchor is the item under the cursor and its line within the
// viewport, so rows inserted above it by a rebuild do not shift it on screen.
// identity is the selected resource's catalog-independent identity, used when
// the rebuild changed its row key.
type rebuildAnchor struct {
	key      string
	identity string
	line     int
}

func (m Model) captureRebuildAnchor() rebuildAnchor {
	anchor := rebuildAnchor{key: m.selectionKeyForRebuild(), line: m.cursor - m.listOffset}
	if anchor.key == "" {
		return anchor
	}
	if m.pane == paneDetail && m.detailKey != "" {
		for _, r := range m.rows {
			if rowKey(r) == m.detailKey {
				anchor.identity = rowSelectionIdentity(r)
				break
			}
		}
	} else if item := m.flatItems[m.cursor]; item.Kind == flatArtifact {
		anchor.identity = rowSelectionIdentity(item.Row)
	}
	return anchor
}

// restoreSelectionIdentity selects the only artifact with identity.
func (m *Model) restoreSelectionIdentity(identity string) bool {
	if identity == "" {
		return false
	}
	match := -1
	for i, item := range m.flatItems {
		if item.Kind != flatArtifact || rowSelectionIdentity(item.Row) != identity {
			continue
		}
		if match >= 0 {
			return false
		}
		match = i
	}
	if match < 0 {
		return false
	}
	m.cursor = match
	m.clampListOffset()
	return true
}

// restoreAnchor selects the anchored item by key, or its resource by identity
// when the rebuild changed the key.
func (m *Model) restoreAnchor(anchor rebuildAnchor) bool {
	return m.restoreSelectionKey(anchor.key) || m.restoreSelectionIdentity(anchor.identity)
}

func (m *Model) restoreSelectionAfterRebuild(anchor rebuildAnchor) {
	if !m.restoreAnchor(anchor) {
		return
	}
	m.listOffset = m.cursor - anchor.line
	m.clampListOffset()
}

// recomputeView is called after any input change (rows/filter/fold). It
// reapplies the filter, rebuilds the flat item list, and adjusts the cursor.
// When a filter is active, the cursor snaps to the best-ranked match (the
// first one on ties) so arrow-keys don't start from a section header or a
// stale row.
func (m *Model) recomputeView() {
	m.filteredRows = m.applyFilter()
	m.rebuildFlatItems()

	if len(m.flatItems) == 0 {
		m.cursor = 0
		m.listOffset = 0
		return
	}

	if m.filter != "" {
		// Scroll from the top so the best match keeps its section headers
		// in view whenever it fits.
		m.cursor, m.listOffset = 0, 0
		best := -1
		for i, it := range m.flatItems {
			if it.Kind == flatArtifact && (best < 0 || searchRankLess(it.Row, m.flatItems[best].Row)) {
				best = i
			}
		}
		if best >= 0 {
			m.cursor = best
		}
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
