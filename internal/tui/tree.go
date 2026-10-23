package tui

import (
	"encoding/base64"
	"fmt"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// groupingAxis names one list grouping dimension. The zero/unknown values
// normalize to status (primary) or none (secondary).
type groupingAxis string

const (
	axisStatus groupingAxis = "status"
	axisSource groupingAxis = "source"
	axisKind   groupingAxis = "kind"
	axisNone   groupingAxis = "none"
)

// viewSettings is the selectable grouping configuration persisted per user.
// It doubles as the fold-state map key, so it must stay comparable.
type viewSettings struct {
	GroupBy groupingAxis `json:"groupBy"`
	ThenBy  groupingAxis `json:"thenBy"`
}

// normalized enforces the valid axis set: primary defaults to status, the
// secondary defaults to none and may never be status or repeat the primary.
func (s viewSettings) normalized() viewSettings {
	switch s.GroupBy {
	case axisStatus, axisSource, axisKind:
	default:
		s.GroupBy = axisStatus
	}
	switch s.ThenBy {
	case axisSource, axisKind:
	default:
		s.ThenBy = axisNone
	}
	if s.ThenBy == s.GroupBy {
		s.ThenBy = axisNone
	}
	return s
}

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
		case materialize.DriftMissing, materialize.DriftOrphaned:
			return statusPending
		case materialize.DriftBroken:
			return statusBroken
		}
	}
	return statusEnabled
}

// attentionBuckets is the pinned Needs attention membership: actionable
// statuses lifted to one top section when the primary axis is not status.
func attentionBuckets() map[statusBucket]bool {
	return map[statusBucket]bool{
		statusPending:   true,
		statusStale:     true,
		statusBroken:    true,
		statusMalformed: true,
	}
}

// sourceIdentity is the unambiguous source key: normalized URL, ref, and
// normalized configured registry subpath. Empty coordinates still produce a
// distinct (local) identity.
func sourceIdentity(r row) string {
	if strings.TrimSpace(r.RegistryURL) == "" {
		return "local"
	}
	return registryIdentityKey(r.RegistryURL, r.RegistryRef, r.RegistryPath)
}

// sourceLabel is the display label for a source section: the registry display
// name (or URL-derived repository name) with the full normalized subpath, or
// "(local)" without registry metadata.
func sourceLabel(r row) string {
	base := strings.TrimSpace(r.RegistryName)
	if base == "" {
		base = registryDisplayName(r.RegistryURL)
	}
	if base == "" {
		base = "(local)"
	}
	if p := config.NormalizeRegistryPath(r.RegistryPath); p != "" {
		base = base + " / " + p
	}
	return base
}

// groupedSection is one section produced by grouping rows along one axis.
type groupedSection struct {
	path    string // full section path (parent path + this axis segment)
	title   string
	members []row
}

// groupByAxis buckets rows into ordered sections along one axis. prefix is
// the enclosing section path ("" at the top level).
func groupByAxis(rows []row, axis groupingAxis, prefix string) []groupedSection {
	switch axis {
	case axisStatus:
		byBucket := map[statusBucket][]row{}
		for _, r := range rows {
			b := classifyStatus(r)
			byBucket[b] = append(byBucket[b], r)
		}
		order := []statusBucket{statusRequired, statusEnabled, statusPending, statusStale, statusBroken, statusAvailable, statusMalformed}
		var out []groupedSection
		for _, b := range order {
			members := byBucket[b]
			if len(members) == 0 {
				continue
			}
			out = append(out, groupedSection{
				path:    strings.TrimPrefix(b.path(), "status:"),
				title:   b.title(),
				members: members,
			})
		}
		for i := range out {
			out[i].path = joinSectionPath(prefix, "status:"+out[i].path)
		}
		return out
	case axisKind:
		byKind := map[ir.ResourceKind][]row{}
		for _, r := range rows {
			byKind[r.Artifact.Kind] = append(byKind[r.Artifact.Kind], r)
		}
		kinds := orderedResourceKinds(byKind)
		out := make([]groupedSection, 0, len(kinds))
		for _, k := range kinds {
			out = append(out, groupedSection{
				path:    joinSectionPath(prefix, "kind:"+resourceKindSectionPath(k)),
				title:   resourceKindSectionTitle(k),
				members: byKind[k],
			})
		}
		return out
	case axisSource:
		byID := map[string][]row{}
		var ids []string
		for _, r := range rows {
			id := sourceIdentity(r)
			if _, ok := byID[id]; !ok {
				ids = append(ids, id)
			}
			byID[id] = append(byID[id], r)
		}
		// Equal display labels tie-break on the raw normalized identity, not
		// its encoded path segment, so order stays deterministic.
		sort.Strings(ids)
		out := make([]groupedSection, 0, len(ids))
		for _, id := range ids {
			members := byID[id]
			// Rows sharing one normalized identity can still carry different
			// display labels; pick the smallest raw label so the section
			// title never depends on input order.
			title := sourceLabel(members[0])
			for _, r := range members[1:] {
				if l := sourceLabel(r); l < title {
					title = l
				}
			}
			out = append(out, groupedSection{
				path:    joinSectionPath(prefix, "src:"+base64.RawURLEncoding.EncodeToString([]byte(id))),
				title:   title,
				members: members,
			})
		}
		sort.SliceStable(out, func(i, j int) bool {
			return out[i].title < out[j].title
		})
		return out
	}
	return nil
}

func joinSectionPath(parent, segment string) string {
	if parent == "" {
		return segment
	}
	return parent + ":" + segment
}

// orderedResourceKinds lists the section order for resource kinds: the known
// Day-1 kinds in UI order followed by unknown kinds alphabetically.
func orderedResourceKinds(buckets map[ir.ResourceKind][]row) []ir.ResourceKind {
	known := []ir.ResourceKind{ir.KindSkill, ir.KindCommand, ir.KindSubagent, ir.KindHook, ir.KindMcpServer, ir.KindCollection, ir.KindRule}
	out := make([]ir.ResourceKind, 0, len(buckets))
	seen := make(map[ir.ResourceKind]struct{}, len(known))
	for _, kind := range known {
		seen[kind] = struct{}{}
		if len(buckets[kind]) > 0 {
			out = append(out, kind)
		}
	}
	var unknown []ir.ResourceKind
	for kind := range buckets {
		if _, ok := seen[kind]; !ok {
			unknown = append(unknown, kind)
		}
	}
	sort.Slice(unknown, func(i, j int) bool {
		if unknown[i].String() != unknown[j].String() {
			return unknown[i].String() < unknown[j].String()
		}
		return unknown[i] < unknown[j]
	})
	return append(out, unknown...)
}

func resourceKindSectionPath(kind ir.ResourceKind) string {
	switch kind {
	case ir.KindSkill:
		return "skills"
	case ir.KindCommand:
		return "commands"
	case ir.KindSubagent:
		return "subagents"
	case ir.KindHook:
		return "hooks"
	case ir.KindMcpServer:
		return "mcp-servers"
	case ir.KindCollection:
		return "collections"
	case ir.KindRule:
		return "rules"
	default:
		// Unknown kinds can share one String() label; the numeric value keeps
		// their section paths distinct and deterministic.
		return fmt.Sprintf("kind-%d", int(kind))
	}
}

func resourceKindSectionTitle(kind ir.ResourceKind) string {
	switch kind {
	case ir.KindSkill:
		return "Skills"
	case ir.KindCommand:
		return "Commands"
	case ir.KindSubagent:
		return "Subagents"
	case ir.KindHook:
		return "Hooks"
	case ir.KindMcpServer:
		return "MCP servers"
	case ir.KindCollection:
		return "Collections"
	case ir.KindRule:
		return "Rules"
	default:
		return kind.String()
	}
}

// emitSection appends a section header and, unless folded, its children —
// nested sections for a secondary axis or sorted leaf rows.
func emitSection(out []flatItem, sec groupedSection, depth int, foldState map[string]bool) []flatItem {
	sortLeafRows(sec.members)
	collapsed := foldState[sec.path]
	out = append(out, flatItem{
		Kind:         flatSection,
		SectionPath:  sec.path,
		SectionTitle: sec.title,
		SectionCount: len(sec.members),
		SectionDepth: depth,
		Collapsed:    collapsed,
	})
	if collapsed {
		return out
	}
	for _, r := range sec.members {
		out = append(out, flatItem{Kind: flatArtifact, Row: r, Depth: depth + 1})
	}
	return out
}

func emitAxisSections(out []flatItem, rows []row, settings viewSettings, axis groupingAxis, prefix string, depth int, foldState map[string]bool) []flatItem {
	for _, sec := range groupByAxis(rows, axis, prefix) {
		if depth == 0 && settings.ThenBy != axisNone {
			collapsed := foldState[sec.path]
			out = append(out, flatItem{
				Kind:         flatSection,
				SectionPath:  sec.path,
				SectionTitle: sec.title,
				SectionCount: len(sec.members),
				SectionDepth: depth,
				Collapsed:    collapsed,
			})
			if !collapsed {
				out = emitAxisSections(out, sec.members, settings, settings.ThenBy, sec.path, depth+1, foldState)
			}
			continue
		}
		out = emitSection(out, sec, depth, foldState)
	}
	return out
}

// buildGroupedTree builds the flattened list for any valid primary/secondary
// grouping combination. When the primary axis is not status, attention rows
// move into one pinned "Needs attention" section at the top.
func buildGroupedTree(rows []row, settings viewSettings, foldState map[string]bool) []flatItem {
	settings = settings.normalized()
	if foldState == nil {
		foldState = map[string]bool{}
	}
	var out []flatItem
	remaining := rows
	if settings.GroupBy != axisStatus {
		var attention []row
		var rest []row
		attn := attentionBuckets()
		for _, r := range rows {
			if attn[classifyStatus(r)] {
				attention = append(attention, r)
			} else {
				rest = append(rest, r)
			}
		}
		if len(attention) > 0 {
			// Status-bucket order, then name; emitSection's leaf sort would
			// destroy the status order, so emit the header and rows directly.
			sort.SliceStable(attention, func(i, j int) bool {
				bi, bj := classifyStatus(attention[i]), classifyStatus(attention[j])
				if bi != bj {
					return bi < bj
				}
				return attention[i].Artifact.Name < attention[j].Artifact.Name
			})
			collapsed := foldState["attention"]
			out = append(out, flatItem{
				Kind:         flatSection,
				SectionPath:  "attention",
				SectionTitle: "Needs attention",
				SectionCount: len(attention),
				SectionDepth: 0,
				Collapsed:    collapsed,
			})
			if !collapsed {
				for _, r := range attention {
					out = append(out, flatItem{Kind: flatArtifact, Row: r, Depth: 1})
				}
			}
		}
		remaining = rest
	}
	out = emitAxisSections(out, remaining, settings, settings.GroupBy, "", 0, foldState)
	return out
}

func buildStatusTree(rows []row, foldState map[string]bool) []flatItem {
	return buildGroupedTree(rows, viewSettings{GroupBy: axisStatus, ThenBy: axisNone}, foldState)
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
