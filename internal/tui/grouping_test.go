package tui

import (
	"encoding/base64"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// groupingTestRows returns a fixture covering several kinds, registry
// identities (including two sources whose display labels collide but whose
// refs differ, and two URL spellings that normalize to the same identity),
// an unknown kind, a local row, and every attention status.
func groupingTestRows() []row {
	return []row{
		mkRow("alpha-skill", withProject, withKind(ir.KindSkill), withRegistry("repo.git", "repo"), withRegistryMeta("main", "frontend")),
		mkRow("beta-command", withProject, withKind(ir.KindCommand), withRegistry("repo.git", "repo"), withRegistryMeta("dev", "frontend")),
		mkRow("gamma-mcp", withGlobal, withKind(ir.KindMcpServer), withRegistry("sec.git", "security")),
		mkRow("delta-skill", withProject, withKind(ir.KindSkill), withRegistry("repo", "other-name")),
		mkRow("epsilon-unknown", withProject, withKind(ir.ResourceKind(99)), withRegistry("sec.git", "security")),
		mkRow("zeta-local", withLocal, withKind(ir.KindSkill)),
		mkRow("pending-one", withProject, withDrift(materialize.DriftMissing), withRegistry("sec.git", "security")),
		mkRow("broken-one", withProject, withDrift(materialize.DriftBroken), withRegistry("repo.git", "repo"), withRegistryMeta("main", "frontend")),
		mkRow("malformed-one", withMalformed("bad yaml"), withRegistry("repo.git", "repo"), withRegistryMeta("main", "frontend")),
		mkRow("required-one", withRequired, withProject, withRegistry("sec.git", "security")),
		mkRow("available-one", withAvailable, withRegistry("repo.git", "repo"), withRegistryMeta("dev", "frontend")),
	}
}

// artifactNames returns the resource names in item order.
func groupedRowNames(items []flatItem) []string {
	var names []string
	for _, it := range items {
		if it.Kind == flatArtifact {
			names = append(names, it.Row.Artifact.Name)
		}
	}
	return names
}

// artifactCount counts how often a resource name appears in the flattened
// tree; grouping must never duplicate or drop a row.
func artifactCount(items []flatItem, name string) int {
	n := 0
	for _, it := range items {
		if it.Kind == flatArtifact && it.Row.Artifact.Name == name {
			n++
		}
	}
	return n
}

// spec: tui-navigation/all-grouping-combinations-retain-distinct-resources
func TestBuildGroupedTreeAllCombinations(t *testing.T) {
	rows := groupingTestRows()
	combos := []viewSettings{
		{GroupBy: axisStatus, ThenBy: axisNone},
		{GroupBy: axisStatus, ThenBy: axisKind},
		{GroupBy: axisStatus, ThenBy: axisSource},
		{GroupBy: axisSource, ThenBy: axisNone},
		{GroupBy: axisSource, ThenBy: axisKind},
		{GroupBy: axisKind, ThenBy: axisNone},
		{GroupBy: axisKind, ThenBy: axisSource},
	}
	for _, combo := range combos {
		items := buildGroupedTree(rows, combo, nil)
		// Every row appears exactly once.
		for _, r := range rows {
			if got := artifactCount(items, r.Artifact.Name); got != 1 {
				t.Fatalf("%v: %q appears %d times", combo, r.Artifact.Name, got)
			}
		}
		// Section nesting: every artifact sits under a section, depth follows
		// the axis count.
		wantDepth := 1
		if combo.ThenBy != axisNone {
			wantDepth = 2
		}
		inAttention := false
		for _, it := range items {
			if it.Kind == flatSection {
				inAttention = it.SectionPath == "attention"
			}
			if it.Kind == flatArtifact && !inAttention && it.Depth != wantDepth {
				t.Fatalf("%v: %q depth = %d, want %d", combo, it.Row.Artifact.Name, it.Depth, wantDepth)
			}
			if it.Kind == flatSection && it.SectionCount == 0 {
				t.Fatalf("%v: empty section %q emitted", combo, it.SectionPath)
			}
		}
	}
}

// spec: tui-navigation/all-grouping-combinations-retain-distinct-resources
func TestBuildGroupedTreeAllCombinationsLiteralOrder(t *testing.T) {
	rows := []row{
		mkRow("z-skill", withProject, withKind(ir.KindSkill), withRegistry("z.git", "z")),
		mkRow("a-command", withProject, withKind(ir.KindCommand), withRegistry("a.git", "a")),
		mkRow("b-skill", withProject, withKind(ir.KindSkill), withRegistry("a.git", "a")),
		mkRow("y-command", withProject, withKind(ir.KindCommand), withRegistry("z.git", "z")),
	}
	flat := func(items []flatItem) []string {
		var out []string
		for _, it := range items {
			if it.Kind == flatSection {
				out = append(out, fmt.Sprintf("S%d %s(%d)", it.SectionDepth, it.SectionTitle, it.SectionCount))
			} else {
				out = append(out, fmt.Sprintf("R%d %s", it.Depth, it.Row.Artifact.Name))
			}
		}
		return out
	}
	for _, tc := range []struct {
		name     string
		settings viewSettings
		want     []string
	}{
		{"status/none", viewSettings{GroupBy: axisStatus, ThenBy: axisNone},
			[]string{"S0 Enabled(4)", "R1 a-command", "R1 b-skill", "R1 y-command", "R1 z-skill"}},
		{"status/kind", viewSettings{GroupBy: axisStatus, ThenBy: axisKind},
			[]string{"S0 Enabled(4)", "S1 Skills(2)", "R2 b-skill", "R2 z-skill", "S1 Commands(2)", "R2 a-command", "R2 y-command"}},
		{"status/source", viewSettings{GroupBy: axisStatus, ThenBy: axisSource},
			[]string{"S0 Enabled(4)", "S1 a(2)", "R2 a-command", "R2 b-skill", "S1 z(2)", "R2 y-command", "R2 z-skill"}},
		{"source/none", viewSettings{GroupBy: axisSource, ThenBy: axisNone},
			[]string{"S0 a(2)", "R1 a-command", "R1 b-skill", "S0 z(2)", "R1 y-command", "R1 z-skill"}},
		{"source/kind", viewSettings{GroupBy: axisSource, ThenBy: axisKind},
			[]string{"S0 a(2)", "S1 Skills(1)", "R2 b-skill", "S1 Commands(1)", "R2 a-command", "S0 z(2)", "S1 Skills(1)", "R2 z-skill", "S1 Commands(1)", "R2 y-command"}},
		{"kind/none", viewSettings{GroupBy: axisKind, ThenBy: axisNone},
			[]string{"S0 Skills(2)", "R1 b-skill", "R1 z-skill", "S0 Commands(2)", "R1 a-command", "R1 y-command"}},
		{"kind/source", viewSettings{GroupBy: axisKind, ThenBy: axisSource},
			[]string{"S0 Skills(2)", "S1 a(1)", "R2 b-skill", "S1 z(1)", "R2 z-skill", "S0 Commands(2)", "S1 a(1)", "R2 a-command", "S1 z(1)", "R2 y-command"}},
	} {
		got := flat(buildGroupedTree(rows, tc.settings, nil))
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s:\n got %v\nwant %v", tc.name, got, tc.want)
		}
	}
}

// spec: tui-navigation/all-grouping-combinations-retain-distinct-resources
func TestBuildGroupedTreeSourceIdentityCollisions(t *testing.T) {
	// Two sources with the same display label but different refs must stay
	// distinct sections; normalized-equal spellings merge elsewhere.
	rows := []row{
		mkRow("main-row", withProject, withRegistry("repo.git", "repo"), withRegistryMeta("main", "")),
		mkRow("dev-row", withProject, withRegistry("repo.git", "repo"), withRegistryMeta("dev", "")),
	}
	items := buildGroupedTree(rows, viewSettings{GroupBy: axisSource, ThenBy: axisNone}, nil)
	var sections int
	var memberOf = map[string]string{}
	lastTop := ""
	for _, it := range items {
		if it.Kind == flatSection && it.SectionDepth == 0 {
			sections++
			lastTop = it.SectionPath
			continue
		}
		if it.Kind == flatArtifact {
			memberOf[it.Row.Artifact.Name] = lastTop
		}
	}
	if sections != 2 {
		t.Fatalf("same label different refs must stay distinct, got %d sections", sections)
	}
	if memberOf["main-row"] == "" || memberOf["main-row"] == memberOf["dev-row"] {
		t.Fatalf("rows must sit in different source sections: %v", memberOf)
	}
}

// spec: tui-navigation/all-grouping-combinations-retain-distinct-resources
func TestBuildGroupedTreeKindOrder(t *testing.T) {
	rows := []row{
		mkRow("rule", withProject, withKind(ir.KindRule)),
		mkRow("skill", withProject, withKind(ir.KindSkill)),
		mkRow("command", withProject, withKind(ir.KindCommand)),
		mkRow("subagent", withProject, withKind(ir.KindSubagent)),
		mkRow("hook", withProject, withKind(ir.KindHook)),
		mkRow("mcp", withProject, withKind(ir.KindMcpServer)),
		mkRow("collection", withProject, withKind(ir.KindCollection)),
		mkRow("unknown-high", withProject, withKind(ir.ResourceKind(200))),
		mkRow("unknown-low", withProject, withKind(ir.ResourceKind(150))),
	}
	items := buildGroupedTree(rows, viewSettings{GroupBy: axisKind, ThenBy: axisNone}, nil)
	wantTitles := []string{"Skills", "Commands", "Subagents", "Hooks", "MCP servers", "Collections", "Rules", "unknown", "unknown"}
	wantPaths := []string{"kind:skills", "kind:commands", "kind:subagents", "kind:hooks", "kind:mcp-servers", "kind:collections", "kind:rules", "kind:kind-150", "kind:kind-200"}
	wantMembers := []string{"skill", "command", "subagent", "hook", "mcp", "collection", "rule", "unknown-low", "unknown-high"}
	var titles, paths []string
	var sections []flatItem
	for _, it := range items {
		if it.Kind == flatSection {
			titles = append(titles, it.SectionTitle)
			paths = append(paths, it.SectionPath)
			sections = append(sections, it)
		}
	}
	if strings.Join(titles, "|") != strings.Join(wantTitles, "|") {
		t.Fatalf("kind sections = %v, want %v", titles, wantTitles)
	}
	if strings.Join(paths, "|") != strings.Join(wantPaths, "|") {
		t.Fatalf("kind section paths = %v, want %v", paths, wantPaths)
	}
	for i, sec := range sections {
		names := skillNamesUnder(items, sec.SectionPath)
		if len(names) != 1 || names[0] != wantMembers[i] {
			t.Fatalf("section %q members = %v, want [%s]", sec.SectionPath, names, wantMembers[i])
		}
	}
}

// spec: tui-navigation/all-grouping-combinations-retain-distinct-resources
func TestBuildGroupedTreeSourcePathsNeverAliasNestedSections(t *testing.T) {
	// One registry and ref, two subpaths — the second embedding a nested
	// "kind:" segment that raw concatenation would alias.
	rows := []row{
		mkRow("outer", withProject, withKind(ir.KindSkill), withRegistry("repo.git", "repo"), withRegistryMeta("main", "frontend")),
		mkRow("aliaser", withProject, withKind(ir.KindSkill), withRegistry("repo.git", "repo"), withRegistryMeta("main", "frontend:kind:skills")),
	}
	items := buildGroupedTree(rows, viewSettings{GroupBy: axisSource, ThenBy: axisKind}, nil)
	seen := map[string]int{}
	var topPaths []string
	for _, it := range items {
		if it.Kind != flatSection {
			continue
		}
		seen[it.SectionPath]++
		if it.SectionDepth != 0 {
			continue
		}
		topPaths = append(topPaths, it.SectionPath)
		if !strings.HasPrefix(it.SectionPath, "src:") {
			t.Fatalf("top-level source section path %q is not identity-encoded", it.SectionPath)
		}
		seg := strings.TrimPrefix(it.SectionPath, "src:")
		if _, err := base64.RawURLEncoding.DecodeString(seg); err != nil {
			t.Fatalf("top-level source segment %q is not encoded identity: %v", seg, err)
		}
	}
	for path, n := range seen {
		if n != 1 {
			t.Fatalf("section path %q emitted %d times", path, n)
		}
	}
	if len(topPaths) != 2 {
		t.Fatalf("expected two distinct top-level source sections, got %v", topPaths)
	}
	// The second top-level source must yield only its own member.
	var members []string
	inSecond := false
	for _, it := range items {
		if it.Kind == flatSection && it.SectionDepth == 0 {
			inSecond = it.SectionPath == topPaths[1]
			continue
		}
		if it.Kind == flatArtifact && inSecond {
			members = append(members, it.Row.Artifact.Name)
		}
	}
	if len(members) != 1 || members[0] != "aliaser" {
		t.Fatalf("second top-level source members = %v, want [aliaser]", members)
	}
}

// spec: tui-navigation/all-grouping-combinations-retain-distinct-resources
func TestBuildGroupedTreeEqualIdentityDeterministicLabel(t *testing.T) {
	// Two spellings normalizing to one identity ("repo.git" == "repo") carry
	// different display labels; the section label must not depend on input
	// order.
	for _, swapped := range []bool{false, true} {
		rows := []row{
			mkRow("a", withProject, withRegistry("repo.git", "zzz-name"), withRegistryMeta("main", "")),
			mkRow("b", withProject, withRegistry("repo", "aaa-name"), withRegistryMeta("main", "")),
		}
		if swapped {
			rows[0], rows[1] = rows[1], rows[0]
		}
		items := buildGroupedTree(rows, viewSettings{GroupBy: axisSource, ThenBy: axisNone}, nil)
		var titles []string
		for _, it := range items {
			if it.Kind == flatSection {
				titles = append(titles, it.SectionTitle)
			}
		}
		if len(titles) != 1 || titles[0] != "aaa-name" {
			t.Fatalf("swapped=%v: merged section title = %v, want [aaa-name]", swapped, titles)
		}
	}
}

// spec: tui-navigation/all-grouping-combinations-retain-distinct-resources
func TestBuildGroupedTreeEqualLabelsOrderByRawIdentity(t *testing.T) {
	// Same display label, raw identities ordered opposite to their base64
	// encodings: section order must follow the raw identity.
	mkSrc := func(name, url string) row {
		return mkRow(name, withProject, withRegistry(url, "same"), withRegistryMeta("main", ""))
	}
	r1 := mkSrc("from-a", "repo.example/a")
	r2 := mkSrc("from-m", "repo.example/m")
	id1, id2 := sourceIdentity(r1), sourceIdentity(r2)
	if id1 >= id2 {
		t.Fatalf("fixture must keep raw identities ordered, got %q vs %q", id1, id2)
	}
	e1 := base64.RawURLEncoding.EncodeToString([]byte(id1))
	e2 := base64.RawURLEncoding.EncodeToString([]byte(id2))
	if e1 <= e2 {
		t.Fatalf("fixture lost discriminating power: encoded order matches raw order")
	}
	for _, swapped := range []bool{false, true} {
		rows := []row{r1, r2}
		if swapped {
			rows[0], rows[1] = rows[1], rows[0]
		}
		var titles []string
		for _, sec := range groupByAxis(rows, axisSource, "") {
			if len(sec.members) != 1 {
				t.Fatalf("equal-label sources must not merge: %v", sec.members)
			}
			titles = append(titles, sec.members[0].Artifact.Name)
		}
		if !reflect.DeepEqual(titles, []string{"from-a", "from-m"}) {
			t.Fatalf("swapped=%v: equal-label order = %v, want raw-identity order [from-a from-m]", swapped, titles)
		}
	}
}

// spec: tui-navigation/all-grouping-combinations-retain-distinct-resources
func TestBuildGroupedTreeNormalizedSubpathsMerge(t *testing.T) {
	// Spellings that normalize to one configured subpath share a section.
	rows := []row{
		mkRow("one", withProject, withRegistry("repo.git", "repo"), withRegistryMeta("main", "frontend")),
		mkRow("two", withProject, withRegistry("repo.git", "repo"), withRegistryMeta("main", "./frontend/")),
	}
	items := buildGroupedTree(rows, viewSettings{GroupBy: axisSource, ThenBy: axisNone}, nil)
	var sections int
	for _, it := range items {
		if it.Kind == flatSection {
			sections++
		}
	}
	if sections != 1 {
		t.Fatalf("equivalent normalized subpaths must share one section, got %d", sections)
	}
	if artifactCount(items, "one") != 1 || artifactCount(items, "two") != 1 {
		t.Fatalf("rows lost under normalized subpath merge")
	}
}

// spec: tui-navigation/attention-rows-move-without-duplication
func TestBuildGroupedTreeNeedsAttention(t *testing.T) {
	rows := groupingTestRows()
	items := buildGroupedTree(rows, viewSettings{GroupBy: axisSource, ThenBy: axisNone}, nil)

	// Needs attention must be the first section, in status order then name.
	if len(items) == 0 || items[0].Kind != flatSection || items[0].SectionPath != "attention" {
		t.Fatalf("first item should be the Needs attention section, got %+v", items[0])
	}
	if items[0].SectionTitle != "Needs attention" {
		t.Fatalf("attention title = %q", items[0].SectionTitle)
	}
	attention := skillNamesUnder(items, "attention")
	wantOrder := []string{"pending-one", "broken-one", "malformed-one"} // status order, then name
	if len(attention) != len(wantOrder) {
		t.Fatalf("attention = %v, want %v", attention, wantOrder)
	}
	for i := range wantOrder {
		if attention[i] != wantOrder[i] {
			t.Fatalf("attention = %v, want %v", attention, wantOrder)
		}
	}
	// No duplication anywhere.
	for _, r := range rows {
		if got := artifactCount(items, r.Artifact.Name); got != 1 {
			t.Fatalf("%q appears %d times", r.Artifact.Name, got)
		}
	}
	// Required and Available stay in their normal groups.
	if names := skillNamesUnder(items, "attention"); len(names) != 3 {
		t.Fatalf("attention section grew: %v", names)
	}
}

// spec: tui-navigation/attention-rows-move-without-duplication
func TestBuildGroupedTreeEmptyAttentionOmitted(t *testing.T) {
	rows := []row{
		mkRow("a", withProject, withRegistry("sec.git", "security")),
		mkRow("b", withAvailable),
	}
	items := buildGroupedTree(rows, viewSettings{GroupBy: axisKind, ThenBy: axisNone}, nil)
	for _, it := range items {
		if it.Kind == flatSection && it.SectionPath == "attention" {
			t.Fatalf("empty Needs attention section must be omitted")
		}
	}
	// Status primary never emits the attention section.
	items = buildGroupedTree(rows, viewSettings{GroupBy: axisStatus, ThenBy: axisNone}, nil)
	for _, it := range items {
		if it.Kind == flatSection && it.SectionPath == "attention" {
			t.Fatalf("status grouping must not emit Needs attention")
		}
	}
}

// spec: tui-navigation/kind-and-source-searches-work-across-groupings
func TestGroupedSearchMatchesKindAndSource(t *testing.T) {
	for _, combo := range []viewSettings{
		{GroupBy: axisStatus, ThenBy: axisNone},
		{GroupBy: axisSource, ThenBy: axisKind},
		{GroupBy: axisKind, ThenBy: axisSource},
	} {
		// Kind-only match: "mcpserver" matches the MCP server's kind metadata
		// (a term deliberately absent from its name) under every grouping.
		m := NewModel("/repo", "/home")
		m.loading = false
		m.rows = groupingTestRows()
		m.applyViewSettings(combo)
		m.filter = "mcpserver"
		m.recomputeView()
		names := groupedRowNames(m.flatItems)
		if len(names) != 1 || names[0] != "gamma-mcp" {
			t.Fatalf("%v: kind search found %v, want [gamma-mcp]", combo, names)
		}
		r, _ := m.selectedRow()
		if r.SearchNameMatch {
			t.Fatalf("%v: kind match must be metadata-only (no name match)", combo)
		}
		if len(r.SearchMatchRunes) != 0 {
			t.Fatalf("%v: metadata-only match must not mark name runes", combo)
		}
		// Sections without matches are hidden.
		for _, it := range m.flatItems {
			if it.Kind == flatSection && it.SectionPath != "" {
				if len(skillNamesUnder(m.flatItems, it.SectionPath)) == 0 {
					t.Fatalf("%v: filter left empty section %q visible", combo, it.SectionPath)
				}
			}
		}

		// Source-label match: "frontend" finds only frontend rows.
		m = NewModel("/repo", "/home")
		m.loading = false
		m.rows = groupingTestRows()
		m.applyViewSettings(combo)
		m.filter = "frontend"
		m.recomputeView()
		names = groupedRowNames(m.flatItems)
		if len(names) == 0 {
			t.Fatalf("%v: source-label search found nothing", combo)
		}
		for _, n := range names {
			if n != "alpha-skill" && n != "beta-command" && n != "broken-one" && n != "malformed-one" && n != "available-one" {
				t.Fatalf("%v: source-label search leaked %q", combo, n)
			}
		}
		// "frontend" is metadata-only for all of them.
		for _, it := range m.flatItems {
			if it.Kind == flatArtifact && it.Row.SearchNameMatch {
				t.Fatalf("%v: source-label match marked name match on %q", combo, it.Row.Artifact.Name)
			}
		}
	}
}

// spec: tui-navigation/kind-and-source-searches-work-across-groupings
func TestGroupedSearchNoMatchesLeavesValidCursor(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.loading = false
	m.rows = groupingTestRows()
	m.applyViewSettings(viewSettings{GroupBy: axisSource, ThenBy: axisKind})
	m.filter = "zzzzz-no-match"
	m.recomputeView()
	if len(m.flatItems) != 0 {
		t.Fatalf("no-match search should empty the list, got %d items", len(m.flatItems))
	}
	if m.cursor != 0 || m.listOffset != 0 {
		t.Fatalf("no-match search left invalid cursor %d offset %d", m.cursor, m.listOffset)
	}
	if _, ok := m.selectedRow(); ok {
		t.Fatalf("no-match search must not expose a selected row")
	}
}

// spec: tui-navigation/kind-and-source-searches-work-across-groupings
func TestGroupedSearchNameMatchOutranksMetadata(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.loading = false
	m.rows = append(groupingTestRows(),
		// Sorts ahead of alpha-skill by name but only matches on source
		// metadata; a metadata match must not outrank the name match.
		mkRow("aaa-metadata-only", withKind(ir.KindSkill), withRegistry("alpha-org.git", "alpha-org"), withRegistryMeta("main", "")),
	)
	m.applyViewSettings(viewSettings{GroupBy: axisKind, ThenBy: axisNone})
	// "alpha" is a name prefix for alpha-skill; also ensure a row whose
	// metadata contains the term does not outrank it.
	m.filter = "alpha"
	m.recomputeView()
	r, ok := m.selectedRow()
	if !ok || r.Artifact.Name != "alpha-skill" {
		t.Fatalf("name match should select alpha-skill, got %+v", r.Artifact.Name)
	}
	if !r.SearchNameMatch {
		t.Fatalf("alpha-skill should be a name match")
	}
	var metadataRow *row
	for i := range m.filteredRows {
		if m.filteredRows[i].Artifact.Name == "aaa-metadata-only" {
			metadataRow = &m.filteredRows[i]
		}
	}
	if metadataRow == nil || metadataRow.SearchNameMatch {
		t.Fatalf("competing row must be present as a metadata-only match")
	}
}

// spec: tui-navigation/grouping-changes-preserve-folds-and-resource-selection
func TestGroupingChangePreservesFoldsAndSelection(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.loading = false
	m.rows = groupingTestRows()
	m.recomputeView()

	// Fold a source section under source grouping.
	m.applyViewSettings(viewSettings{GroupBy: axisSource, ThenBy: axisKind})
	sourceFoldPath := ""
	for _, it := range m.flatItems {
		if it.Kind == flatSection && it.SectionDepth == 0 && it.SectionPath != "attention" {
			sourceFoldPath = it.SectionPath
			break
		}
	}
	if sourceFoldPath == "" {
		t.Fatalf("no source section found")
	}
	m.currentFoldState()[sourceFoldPath] = true
	m.rebuildFlatItems()

	// Fold a kind section under kind grouping.
	m.applyViewSettings(viewSettings{GroupBy: axisKind, ThenBy: axisSource})
	kindFoldPath := ""
	for _, it := range m.flatItems {
		if it.Kind == flatSection && it.SectionDepth == 0 && it.SectionPath != "attention" {
			kindFoldPath = it.SectionPath
			break
		}
	}
	if kindFoldPath == "" {
		t.Fatalf("no kind section found")
	}
	m.currentFoldState()[kindFoldPath] = true
	m.rebuildFlatItems()

	// Back to source: its fold survived; kind fold did not leak across.
	m.applyViewSettings(viewSettings{GroupBy: axisSource, ThenBy: axisKind})
	if !m.currentFoldState()[sourceFoldPath] {
		t.Fatalf("source fold %q lost after grouping round-trip", sourceFoldPath)
	}
	// Applying and clearing a search must not destroy session folds.
	m.filter = "alpha"
	m.recomputeView()
	m.filter = ""
	m.recomputeView()
	if !m.currentFoldState()[sourceFoldPath] {
		t.Fatalf("search cleared the saved fold %q", sourceFoldPath)
	}
	// The fold state under kind grouping is still isolated.
	m.applyViewSettings(viewSettings{GroupBy: axisKind, ThenBy: axisSource})
	if !m.currentFoldState()[kindFoldPath] {
		t.Fatalf("kind fold %q lost", kindFoldPath)
	}
	if m.currentFoldState()[sourceFoldPath] {
		t.Fatalf("fold state leaked across grouping combinations")
	}
}

// spec: tui-navigation/grouping-changes-preserve-folds-and-resource-selection
func TestGroupingChangeSelectionFollowsRowOrSection(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.loading = false
	m.rows = groupingTestRows()
	m.recomputeView()
	m.applyViewSettings(viewSettings{GroupBy: axisKind, ThenBy: axisNone})

	// Select a real row by key.
	target := "gamma-mcp"
	for i, it := range m.flatItems {
		if it.Kind == flatArtifact && it.Row.Artifact.Name == target {
			m.cursor = i
			break
		}
	}
	// Switch grouping where the row is still visible: selection stays on it.
	m.applyViewSettings(viewSettings{GroupBy: axisSource, ThenBy: axisNone})
	r, ok := m.selectedRow()
	if !ok || r.Artifact.Name != target {
		t.Fatalf("selection should follow %q, got %+v", target, r.Artifact.Name)
	}

	// Fold the section that contains it, then regroup: selection falls back
	// to the visible enclosing section.
	m = NewModel("/repo", "/home")
	m.loading = false
	m.rows = groupingTestRows()
	m.applyViewSettings(viewSettings{GroupBy: axisKind, ThenBy: axisNone})
	for i, it := range m.flatItems {
		if it.Kind == flatArtifact && it.Row.Artifact.Name == target {
			m.cursor = i
			break
		}
	}
	// Fold the kind section containing gamma-mcp under a fresh grouping
	// where it stays hidden: mark the target's new parent folded in the
	// destination combination's fold map first.
	dest := viewSettings{GroupBy: axisSource, ThenBy: axisKind}
	m.foldedSections[dest] = map[string]bool{}
	// Collapse the specific source section in the destination map by
	// pre-computing the unfolded destination tree.
	destItems := buildGroupedTree(m.rows, dest, nil)
	for i, it := range destItems {
		if it.Kind == flatArtifact && it.Row.Artifact.Name == target {
			for j := i - 1; j >= 0; j-- {
				if destItems[j].Kind == flatSection {
					m.foldedSections[dest][destItems[j].SectionPath] = true
					j = -1
				}
			}
			break
		}
	}
	m.applyViewSettings(dest)
	if _, ok := m.selectedRow(); ok {
		t.Fatalf("folded target should not stay selected")
	}
	if _, ok := m.selectedSectionPath(); !ok {
		t.Fatalf("selection should fall back to the containing section, cursor=%d", m.cursor)
	}
}
