package tui

import (
	"sort"

	"github.com/Adyen/agent-marketplace/internal/materialize"
)

// flatItemKind distinguishes section headers from skill rows in the rendered
// flat list.
type flatItemKind int

const (
	flatSection flatItemKind = iota
	flatArtifact
)

// flatItem is one displayable line in the left pane: either a section header
// (foldable) or a skill row. The flat list is what renderList and the cursor
// index into; sections with Collapsed=true suppress their children here.
type flatItem struct {
	Kind flatItemKind

	// For section headers.
	SectionPath  string // stable key for fold state, e.g. "status:enabled"
	SectionTitle string // display label
	SectionCount int    // number of skill rows under this section (pre-fold)
	SectionDepth int    // 0 = top-level, 1+ = sub-section
	Collapsed    bool   // true when the fold state hides this section's children

	// For skill rows.
	Row   row
	Depth int // indentation depth; matches SectionDepth+1 of the owning section
}

// statusBucket groups skills by their sync/lifecycle status.
type statusBucket int

const (
	statusRequired statusBucket = iota
	statusEnabled
	statusPending
	statusStale
	statusBroken
	statusAvailable
	statusMalformed
)

func (s statusBucket) path() string {
	switch s {
	case statusRequired:
		return "status:required"
	case statusEnabled:
		return "status:enabled"
	case statusPending:
		return "status:pending"
	case statusStale:
		return "status:stale"
	case statusBroken:
		return "status:broken"
	case statusAvailable:
		return "status:available"
	case statusMalformed:
		return "status:malformed"
	}
	return "status:unknown"
}

func (s statusBucket) title() string {
	switch s {
	case statusRequired:
		return "Required"
	case statusEnabled:
		return "Enabled"
	case statusPending:
		return "Pending sync"
	case statusStale:
		return "Stale"
	case statusBroken:
		return "Broken"
	case statusAvailable:
		return "Available"
	case statusMalformed:
		return "Malformed"
	}
	return "?"
}

// classifyStatus assigns a row to a status bucket. Required wins over sync
// status. Drift reasons map to pending/stale/broken.
func classifyStatus(r row) statusBucket {
	if r.IsMalformed {
		return statusMalformed
	}
	if r.UnavailableReason != "" {
		return statusBroken
	}
	if r.Artifact.Required {
		return statusRequired
	}
	if r.IsAvailable {
		return statusAvailable
	}
	if r.DriftItem != nil {
		switch r.DriftItem.Reason {
		case materialize.DriftMissing:
			return statusPending
		case materialize.DriftOrphaned:
			return statusBroken
		case materialize.DriftBroken:
			return statusBroken
		}
	}
	return statusEnabled
}

func buildStatusTree(rows []row, foldState map[string]bool) []flatItem {
	buckets := map[statusBucket][]row{}
	for _, r := range rows {
		s := classifyStatus(r)
		buckets[s] = append(buckets[s], r)
	}
	order := []statusBucket{statusRequired, statusEnabled, statusPending, statusStale, statusBroken, statusAvailable, statusMalformed}

	var out []flatItem
	for _, s := range order {
		rowsIn := buckets[s]
		if len(rowsIn) == 0 {
			continue
		}
		sortLeafRows(rowsIn)
		path := s.path()
		collapsed := foldState[path]
		out = append(out, flatItem{
			Kind:         flatSection,
			SectionPath:  path,
			SectionTitle: s.title(),
			SectionCount: len(rowsIn),
			SectionDepth: 0,
			Collapsed:    collapsed,
		})
		if collapsed {
			continue
		}
		for _, r := range rowsIn {
			out = append(out, flatItem{Kind: flatArtifact, Row: r, Depth: 1})
		}
	}
	return out
}

// sortLeafRows orders rows inside one leaf section: filter matches by
// search rank (name matches first, then descending score), then name, then
// stable identity; unfiltered rows stay alphabetical. Section order is
// untouched.
func sortLeafRows(rows []row) {
	sort.SliceStable(rows, func(i, j int) bool {
		ri, rj := rows[i], rows[j]
		if ri.searchRanked || rj.searchRanked {
			if searchRankLess(ri, rj) || searchRankLess(rj, ri) {
				return searchRankLess(ri, rj)
			}
			if ri.Artifact.Name != rj.Artifact.Name {
				return ri.Artifact.Name < rj.Artifact.Name
			}
			return rowKey(ri) < rowKey(rj)
		}
		return ri.Artifact.Name < rj.Artifact.Name
	})
}
