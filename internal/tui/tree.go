package tui

import (
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
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
	SectionPath  string // stable key for fold state, e.g. "scope:global:skills:security"
	SectionTitle string // display label
	SectionCount int    // number of skill rows under this section (pre-fold)
	SectionDepth int    // 0 = top-level, 1+ = sub-section
	Collapsed    bool   // true when the fold state hides this section's children

	// For skill rows.
	Row   row
	Depth int // indentation depth; matches SectionDepth+1 of the owning section
}

// buildTree groups rows into a tree based on the active tab, applies fold
// state, and returns a flat render-ready list. Top-level ordering of
// sections is stable (fixed per tab); sub-sections and skills are sorted
// alphabetically.
func buildTree(rows []row, activeTab tab, foldState map[string]bool) []flatItem {
	switch activeTab {
	case tabRegistry:
		return buildRegistryTree(rows, foldState)
	case tabStatus:
		return buildStatusTree(rows, foldState)
	default:
		return buildScopeTree(rows, foldState)
	}
}

// ---- Scope tab ----------------------------------------------------------

// scopeBucket is the most-specific scope a skill resolves to for grouping.
type scopeBucket int

const (
	bucketGlobal scopeBucket = iota
	bucketProject
	bucketLocal
	bucketAvailable
	bucketMalformed
)

func (b scopeBucket) path() string {
	switch b {
	case bucketGlobal:
		return "scope:global"
	case bucketProject:
		return "scope:project"
	case bucketLocal:
		return "scope:local"
	case bucketAvailable:
		return "scope:available"
	case bucketMalformed:
		return "scope:malformed"
	}
	return "scope:unknown"
}

func (b scopeBucket) title() string {
	switch b {
	case bucketGlobal:
		return "Global"
	case bucketProject:
		return "Project"
	case bucketLocal:
		return "Local"
	case bucketAvailable:
		return "Available"
	case bucketMalformed:
		return "Malformed"
	}
	return "?"
}

// classifyScope picks the most-specific scope for a row.
// Precedence: Malformed > Local(excluded) > Local(enabled) > Project > Global > Available.
// Locally-excluded skills go to Local regardless of project enablement — they
// should not be duplicated under Project.
func classifyScope(r row) scopeBucket {
	if r.IsMalformed {
		return bucketMalformed
	}
	if r.ExcludedLocally {
		return bucketLocal
	}
	if r.EnabledLocal {
		return bucketLocal
	}
	if r.EnabledProject {
		return bucketProject
	}
	if r.EnabledGlobal {
		return bucketGlobal
	}
	return bucketAvailable
}

func buildScopeTree(rows []row, foldState map[string]bool) []flatItem {
	// Bucket rows by most-specific scope.
	buckets := map[scopeBucket][]row{}
	for _, r := range rows {
		b := classifyScope(r)
		buckets[b] = append(buckets[b], r)
	}

	order := []scopeBucket{bucketGlobal, bucketProject, bucketLocal, bucketAvailable, bucketMalformed}
	var out []flatItem

	for _, b := range order {
		rowsIn := buckets[b]
		if len(rowsIn) == 0 {
			continue
		}
		topPath := b.path()
		topCollapsed := foldState[topPath]
		out = append(out, flatItem{
			Kind:         flatSection,
			SectionPath:  topPath,
			SectionTitle: b.title(),
			SectionCount: len(rowsIn),
			SectionDepth: 0,
			Collapsed:    topCollapsed,
		})
		if topCollapsed {
			continue
		}

		// Sub-group by resource kind, then registry, sorted.
		out = appendKindRegistrySections(out, topPath, rowsIn, foldState, 1)
	}

	return out
}

// ---- Registry tab -------------------------------------------------------

func buildRegistryTree(rows []row, foldState map[string]bool) []flatItem {
	return appendKindRegistrySections(nil, "registry", rows, foldState, 0)
}

func appendKindRegistrySections(out []flatItem, parentPath string, rows []row, foldState map[string]bool, kindDepth int) []flatItem {
	byKind := map[ir.ResourceKind][]row{}
	for _, r := range rows {
		byKind[r.Artifact.Kind] = append(byKind[r.Artifact.Kind], r)
	}

	for _, kind := range orderedResourceKinds(byKind) {
		rowsIn := byKind[kind]
		topPath := parentPath + ":" + resourceKindSectionPath(kind)
		topCollapsed := foldState[topPath]
		out = append(out, flatItem{
			Kind:         flatSection,
			SectionPath:  topPath,
			SectionTitle: resourceKindSectionTitle(kind),
			SectionCount: len(rowsIn),
			SectionDepth: kindDepth,
			Collapsed:    topCollapsed,
		})
		if topCollapsed {
			continue
		}

		byRegistry := map[string][]row{}
		for _, r := range rowsIn {
			byRegistry[registryDisplayLabel(r)] = append(byRegistry[registryDisplayLabel(r)], r)
		}
		for _, name := range keysSorted(byRegistry) {
			subPath := topPath + ":" + name
			subCollapsed := foldState[subPath]
			sub := byRegistry[name]
			sort.SliceStable(sub, func(i, j int) bool {
				return sub[i].Artifact.Name < sub[j].Artifact.Name
			})
			out = append(out, flatItem{
				Kind:         flatSection,
				SectionPath:  subPath,
				SectionTitle: name,
				SectionCount: len(sub),
				SectionDepth: kindDepth + 1,
				Collapsed:    subCollapsed,
			})
			if subCollapsed {
				continue
			}
			for _, r := range sub {
				out = append(out, flatItem{Kind: flatArtifact, Row: r, Depth: kindDepth + 2})
			}
		}
	}
	return out
}

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
		return unknown[i].String() < unknown[j].String()
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
		return kind.String()
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

// ---- Status tab ---------------------------------------------------------

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
		sort.SliceStable(rowsIn, func(i, j int) bool {
			return rowsIn[i].Artifact.Name < rowsIn[j].Artifact.Name
		})
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

// registryDisplayLabel returns the display name used for registry grouping.
// When the registry entry has a path subdirectory, the path's last segment is
// appended to the URL-derived name so entries from the same repo but different
// subdirectories are visually distinct (e.g. "awesome-ai / pm-data-analytics").
// Falls back to "(local)" for skills without a recorded registry URL so
// local-only skills still group predictably.
func registryDisplayLabel(r row) string {
	base := r.RegistryName
	if base == "" {
		base = registryDisplayName(r.RegistryURL)
	}
	if base == "" {
		base = "(local)"
	}
	if p := strings.Trim(r.RegistryPath, "/"); p != "" {
		// Keep full normalized path to avoid collisions in grouping keys
		// (e.g. foo/skills vs bar/skills would collapse if only the
		// basename were used).
		base = base + " / " + p
	}
	return base
}

func keysSorted(m map[string][]row) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
