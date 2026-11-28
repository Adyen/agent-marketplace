package tui

import (
	"cmp"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/session"
	compilepkg "github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
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
	Row              row
	Depth            int // indentation depth; matches SectionDepth+1 of the owning section
	PluginParent     bool
	CollectionParent bool
	CollectionMember bool
}

// statusBucket groups skills by their sync/lifecycle status.
type statusBucket int

const (
	statusRequired  statusBucket = statusBucket(session.GroupRequired)
	statusEnabled   statusBucket = statusBucket(session.GroupEnabled)
	statusPending   statusBucket = statusBucket(session.GroupPending)
	statusStale     statusBucket = statusBucket(session.GroupStale)
	statusBroken    statusBucket = statusBucket(session.GroupBroken)
	statusAvailable statusBucket = statusBucket(session.GroupAvailable)
	statusMalformed statusBucket = statusBucket(session.GroupMalformed)
)

var statusBucketPaths = func() [7]string {
	var paths [7]string
	for _, group := range session.StatusGroups() {
		paths[group] = "status:" + group.ID()
	}
	return paths
}()

func (s statusBucket) path() string {
	if s < 0 || s >= statusBucket(len(statusBucketPaths)) {
		return "status:unknown"
	}
	return statusBucketPaths[s]
}

func (s statusBucket) title() string {
	return session.StatusGroup(s).Title()
}

// classifyStatus assigns a row to a status bucket. Required wins over sync
// status. Drift reasons map to pending/stale/broken.
func classifyStatus(r row) statusBucket {
	return statusBucket(r.StatusGroup())
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
		var out []groupedSection
		for _, group := range session.StatusGroups() {
			b := statusBucket(group)
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
func emitSection(out []flatItem, sec groupedSection, depth int, foldState map[string]bool, knownRows []row) []flatItem {
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
		out = emitResourceRow(out, r, depth+1, foldState, knownRows)
	}
	return out
}

func emitResourceRow(out []flatItem, resource row, depth int, foldState map[string]bool, knownRows []row) []flatItem {
	plugin, ok := resource.Artifact.Resource.(*ir.Plugin)
	if !ok {
		collection, ok := resource.Artifact.Resource.(*ir.Collection)
		if !ok || len(collectionEntries(collection)) == 0 {
			return append(out, flatItem{Kind: flatArtifact, Row: resource, Depth: depth})
		}
		key := "collection:" + rowKey(resource)
		collapsed := foldState[key]
		out = append(out, flatItem{
			Kind:             flatArtifact,
			Row:              resource,
			Depth:            depth,
			CollectionParent: true,
			SectionPath:      key,
			Collapsed:        collapsed,
		})
		if !collapsed {
			for _, member := range collectionMemberRows(resource, knownRows) {
				out = append(out, flatItem{Kind: flatArtifact, Row: member, Depth: depth + 1, CollectionMember: true})
			}
		}
		return out
	}
	key := "plugin:" + rowKey(resource)
	collapsed := foldState[key]
	out = append(out, flatItem{
		Kind:         flatArtifact,
		Row:          resource,
		Depth:        depth,
		PluginParent: true,
		SectionPath:  key,
		Collapsed:    collapsed,
	})
	if !collapsed {
		for _, member := range pluginMemberRows(resource, plugin) {
			out = append(out, flatItem{Kind: flatArtifact, Row: member, Depth: depth + 1})
		}
	}
	return out
}

func pluginMemberRows(parent row, plugin *ir.Plugin) []row {
	if plugin == nil {
		return nil
	}
	var exclusions []string
	if parent.Artifact.Selection != nil {
		exclusions = parent.Artifact.Selection.Exclusions
	}
	children := make([]row, 0, len(plugin.Members))
	for _, member := range plugin.Members {
		if member.Resource == nil {
			continue
		}
		key := member.Key()
		excluded := slices.Contains(exclusions, key)
		child := parent
		child.Artifact.Kind = member.Resource.Kind()
		child.Artifact.Name = member.Resource.Name()
		child.Artifact.Description = ""
		child.Artifact.Resource = member.Resource
		memberPath := compilepkg.SourceOf(member.Resource).File
		if parent.Artifact.PackageRoot != "" {
			memberPath = filepath.Join(parent.Artifact.PackageRoot, filepath.FromSlash(memberPath))
		}
		child.Artifact.Path = memberPath
		child.Artifact.Selection = nil
		child.Artifact.Plugin = nil
		child.Artifact.PluginPackage = &resolver.PluginPackage{
			Name:       plugin.Name(),
			Package:    plugin,
			Exclusions: append([]string(nil), exclusions...),
		}
		child.EnabledGlobal = parent.EnabledGlobal && !excluded
		child.EnabledProject = parent.EnabledProject && !excluded
		child.EnabledLocal = parent.EnabledLocal && !excluded
		child.PluginSelectedGlobal = parent.EnabledGlobal
		child.PluginSelectedProject = parent.EnabledProject
		child.PluginSelectedLocal = parent.EnabledLocal
		child.IsAvailable = parent.IsAvailable || excluded
		child.IsExcluded = excluded
		child.ExcludedLocally = false
		child.PluginMemberExcluded = excluded
		child.SearchReady = false
		child.SearchWords = nil
		child.LabelMemo = nil
		children = append(children, child)
	}
	return children
}

func collectionEntries(collection *ir.Collection) []ir.CollectionMember {
	if collection == nil {
		return nil
	}
	entries := slices.Clone(collection.Members)
	for _, name := range collection.Includes {
		entries = append(entries, ir.CollectionMember{Kind: ir.KindCollection, Name: name})
	}
	seen := make(map[string]bool, len(entries))
	out := entries[:0]
	for _, entry := range entries {
		if entry.Kind == 0 {
			entry.Kind = ir.KindSkill
		}
		key := entry.Kind.String() + "\x00" + entry.Name
		if entry.Name == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, entry)
	}
	return out
}

func collectionMemberRows(parent row, knownRows []row) []row {
	collection, ok := parent.Artifact.Resource.(*ir.Collection)
	if !ok {
		return nil
	}
	members := collectionEntries(collection)
	children := make([]row, 0, len(members))
	for _, member := range members {
		child, found := collectionMemberRow(parent, member, knownRows)
		if !found {
			child = row{
				Artifact: resolver.ResolvedArtifact{
					Entry: catalog.Entry{Kind: member.Kind, Name: member.Name},
				},
				PendingVerification: true,
			}
			if parent.Artifact.IsMarketplace {
				child.Artifact.IsMarketplace = true
				child.IsAvailable = false
				child.PendingVerification = true
				child.MarketplaceCollectionOnly = !parent.IsAvailable
				if discovery := parent.MarketplaceDiscovery; discovery != nil {
					child.Artifact.MarketplaceURL = discovery.Endpoint.URL
					child.Artifact.MarketplaceInstanceID = discovery.Endpoint.InstanceID
					child.Artifact.MarketplaceAlias = discovery.Endpoint.Alias
					child.Artifact.MarketplaceRegistryID = member.Registry
					child.Artifact.MarketplaceResourceName = member.Name
					child.Artifact.MarketplaceDigest = member.Ref
					child.RegistryURL = discovery.Endpoint.URL
					child.Artifact.RegistryURL = discovery.Endpoint.URL
					if member.Registry != "" && member.Ref != "" && parent.IsAvailable {
						child.IsAvailable = true
						child.PendingVerification = false
						child.MarketplaceDiscovery = &session.MarketplaceDiscovery{
							Endpoint: config.MarketplaceEntry{
								URL: discovery.Endpoint.URL, InstanceID: discovery.Endpoint.InstanceID, Alias: discovery.Endpoint.Alias,
							},
							Item: mpapi.ResourceListItem{
								RegistryID: member.Registry, Kind: member.Kind.String(), Name: member.Name,
								Revision: member.Ref, ContentDigest: member.Ref, Availability: mpapi.AvailabilityAvailable,
							},
						}
					}
				}
			}
		}
		children = append(children, child)
	}
	return children
}

func collectionMemberRow(parent row, member ir.CollectionMember, knownRows []row) (row, bool) {
	var chosen row
	var chosenLocation string
	found, ambiguous := false, false
	registry, ref := strings.TrimSpace(member.Registry), strings.TrimSpace(member.Ref)
	memberPath, pathPinned := config.NormalizeRegistryPath(member.Path), strings.TrimSpace(member.Path) != ""
	var parentLocation string
	if registry == "" && parent.Artifact.CatalogKey == "" {
		parentLocation = sourceIdentity(parent)
	}
	for _, candidate := range knownRows {
		if parent.Artifact.IsMarketplace {
			if member.Registry == "" || member.Ref == "" {
				continue
			}
			endpointURL := parent.Artifact.MarketplaceURL
			instanceID := parent.Artifact.MarketplaceInstanceID
			if parent.MarketplaceDiscovery != nil {
				endpointURL = parent.MarketplaceDiscovery.Endpoint.URL
				instanceID = parent.MarketplaceDiscovery.Endpoint.InstanceID
			}
			if candidate.MarketplaceDiscovery != nil {
				discovery := candidate.MarketplaceDiscovery
				if discovery.Endpoint.URL == endpointURL && discovery.Endpoint.InstanceID == instanceID &&
					discovery.Item.RegistryID == member.Registry &&
					discovery.Item.Kind == member.Kind.String() && discovery.Item.Name == member.Name &&
					discovery.Item.Revision == member.Ref && discovery.Item.ContentDigest == member.Ref {
					return candidate, true
				}
			}
			if candidate.Artifact.MarketplaceURL == endpointURL &&
				candidate.Artifact.Kind == member.Kind &&
				candidate.Artifact.MarketplaceInstanceID == instanceID &&
				candidate.Artifact.MarketplaceRegistryID == member.Registry &&
				candidate.Artifact.MarketplaceDigest == member.Ref &&
				cmp.Or(candidate.Artifact.MarketplaceResourceName, candidate.Artifact.Name) == member.Name {
				return candidate, true
			}
			continue
		}
		if candidate.Artifact.Kind != member.Kind || candidate.Artifact.Name != member.Name ||
			candidate.Artifact.IsMarketplace || candidate.IsMalformed || candidate.Artifact.Unresolved ||
			candidate.UnavailableReason != "" || candidate.PendingVerification || candidate.IsExcluded ||
			candidate.ExcludedLocally {
			continue
		}
		if registry != "" {
			if config.NormalizeGitURL(strings.TrimSpace(candidate.RegistryURL)) != config.NormalizeGitURL(registry) ||
				(ref != "" && strings.TrimSpace(candidate.RegistryRef) != "" && strings.TrimSpace(candidate.RegistryRef) != ref) ||
				(pathPinned && config.NormalizeRegistryPath(candidate.RegistryPath) != memberPath) {
				continue
			}
		} else if parent.Artifact.CatalogKey != "" {
			if candidate.Artifact.CatalogKey != parent.Artifact.CatalogKey {
				continue
			}
		} else if sourceIdentity(candidate) != parentLocation {
			continue
		}
		location := sourceIdentity(candidate)
		if !found {
			chosen, chosenLocation, found = candidate, location, true
			continue
		}
		if location != chosenLocation {
			ambiguous = true
		}
		if candidate.Artifact.Required && !chosen.Artifact.Required {
			chosen = candidate
		} else if candidate.Artifact.Required == chosen.Artifact.Required &&
			config.CompareEntryPrecedence(candidate.Artifact.Source, candidate.Artifact.FilePrecedence, chosen.Artifact.Source, chosen.Artifact.FilePrecedence) > 0 {
			chosen = candidate
		}
	}
	if !found || (ambiguous && (!pathPinned || ref == "")) {
		return row{}, false
	}
	return chosen, true
}

func emitAxisSections(out []flatItem, rows []row, settings viewSettings, axis groupingAxis, prefix string, depth int, foldState map[string]bool, knownRows []row) []flatItem {
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
				out = emitAxisSections(out, sec.members, settings, settings.ThenBy, sec.path, depth+1, foldState, knownRows)
			}
			continue
		}
		out = emitSection(out, sec, depth, foldState, knownRows)
	}
	return out
}

// buildGroupedTree builds the flattened list for any valid primary/secondary
// grouping combination. When the primary axis is not status, attention rows
// move into one pinned "Needs attention" section at the top.
func buildGroupedTree(rows []row, settings viewSettings, foldState map[string]bool) []flatItem {
	return buildGroupedTreeWithRows(rows, rows, settings, foldState)
}

func buildGroupedTreeWithRows(rows, knownRows []row, settings viewSettings, foldState map[string]bool) []flatItem {
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
					out = emitResourceRow(out, r, 1, foldState, knownRows)
				}
			}
		}
		remaining = rest
	}
	out = emitAxisSections(out, remaining, settings, settings.GroupBy, "", 0, foldState, knownRows)
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
		if ri.SearchRanked || rj.SearchRanked {
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
