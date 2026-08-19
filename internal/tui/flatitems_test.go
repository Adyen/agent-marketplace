package tui

import (
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestModelRebuildFlatItemsReflectsActiveTab(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.rows = []row{
		mkRow("audit", withGlobal, withRegistry("sec.git", "security")),
		mkRow("deploy", withProject, withRegistry("infra.git", "infra")),
	}
	m.filteredRows = m.rows
	m.rebuildFlatItems()

	paths := sectionPaths(m.flatItems)
	found := false
	for _, p := range paths {
		if p == "scope:global" {
			found = true
		}
	}
	if !found {
		t.Fatalf("scope tab should yield scope:global section, got %v", paths)
	}

	m.activeTab = tabRegistry
	m.rebuildFlatItems()
	paths = sectionPaths(m.flatItems)
	found = false
	for _, p := range paths {
		if p == "registry:skills:security" {
			found = true
		}
	}
	if !found {
		t.Fatalf("registry tab should yield registry:skills:security, got %v", paths)
	}
}

func TestToggleFoldCurrentSectionTogglesFoldState(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.rows = []row{mkRow("audit", withGlobal, withRegistry("sec.git", "security"))}
	m.filteredRows = m.rows
	m.rebuildFlatItems()

	// Move cursor to the top-level scope:global section.
	m.cursor = 0
	if m.flatItems[m.cursor].SectionPath != "scope:global" {
		t.Fatalf("expected cursor on scope:global, got %q", m.flatItems[m.cursor].SectionPath)
	}

	m.toggleFoldAtCursor()
	if !m.currentFoldState()["scope:global"] {
		t.Fatalf("toggleFoldAtCursor should collapse scope:global")
	}

	m.rebuildFlatItems()

	// When collapsed, child items must not appear.
	for _, it := range m.flatItems {
		if it.Kind == flatArtifact && it.Row.Artifact.Name == "audit" {
			t.Fatalf("audit should be hidden under collapsed scope:global")
		}
	}

	// Toggle again — expand.
	m.cursor = 0
	m.toggleFoldAtCursor()
	if m.currentFoldState()["scope:global"] {
		t.Fatalf("toggleFoldAtCursor should expand scope:global on second call")
	}
}

func TestCollapseAllAndExpandAllAffectAllSections(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.rows = []row{
		mkRow("a", withGlobal, withRegistry("sec.git", "security")),
		mkRow("b", withProject, withRegistry("infra.git", "infra")),
	}
	m.filteredRows = m.rows
	m.rebuildFlatItems()

	m.collapseAllSections()
	fs := m.currentFoldState()
	if !fs["scope:global"] || !fs["scope:project"] {
		t.Fatalf("collapseAllSections should collapse all top-level sections, got %v", fs)
	}

	m.expandAllSections()
	fs = m.currentFoldState()
	for path, collapsed := range fs {
		if collapsed {
			t.Fatalf("expandAllSections left %s collapsed", path)
		}
	}
}

func TestFoldStateIsPerTab(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.rows = []row{
		mkRow("audit", withGlobal, withRegistry("sec.git", "security")),
	}
	m.filteredRows = m.rows

	// Collapse scope:global on the Scope tab.
	m.rebuildFlatItems()
	m.cursor = 0
	m.toggleFoldAtCursor()

	// Switch to Registry tab — scope:global fold should not bleed over.
	m.switchTab(tabRegistry)
	m.rebuildFlatItems()
	if m.currentFoldState()["scope:global"] {
		t.Fatalf("Registry tab should not share Scope tab's fold state")
	}

	// Back to Scope — fold state restored.
	m.switchTab(tabScope)
	m.rebuildFlatItems()
	if !m.currentFoldState()["scope:global"] {
		t.Fatalf("Scope tab fold state should persist across tab switches")
	}
}

func TestCursorSkipsFromFoldedSectionToNextVisible(t *testing.T) {
	// With scope:global collapsed, pressing down from scope:global should
	// land on the next visible item (scope:project or subsequent section).
	m := NewModel("/repo", "/home")
	m.rows = []row{
		mkRow("a", withGlobal, withRegistry("sec.git", "security")),
		mkRow("b", withProject, withRegistry("infra.git", "infra")),
	}
	m.filteredRows = m.rows
	m.rebuildFlatItems()

	// cursor on scope:global, collapse it.
	m.cursor = 0
	m.toggleFoldAtCursor()
	m.rebuildFlatItems()

	// After collapse, scope:global is still at index 0; pressing down should
	// go to scope:project (next top-level section).
	m.cursor = 0
	if m.cursor+1 >= len(m.flatItems) {
		t.Fatalf("expected at least 2 flat items after collapse, got %d", len(m.flatItems))
	}
	next := m.flatItems[1]
	if next.Kind != flatSection || next.SectionPath != "scope:project" {
		t.Fatalf("expected scope:project at index 1 after collapse, got %+v", next)
	}
}

func TestSelectedRowReturnsUnderlyingRowForSkillItem(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.rows = []row{
		mkRow("audit", withGlobal, withRegistry("sec.git", "security")),
	}
	m.filteredRows = m.rows
	m.rebuildFlatItems()

	// Find the skill flatItem.
	skillIdx := -1
	for i, it := range m.flatItems {
		if it.Kind == flatArtifact {
			skillIdx = i
			break
		}
	}
	if skillIdx < 0 {
		t.Fatalf("no skill flatItem found in %+v", m.flatItems)
	}
	m.cursor = skillIdx
	r, ok := m.selectedRow()
	if !ok {
		t.Fatalf("selectedRow should return true for cursor on skill item")
	}
	if r.Artifact.Name != "audit" {
		t.Fatalf("selectedRow = %q, want audit", r.Artifact.Name)
	}
}

func TestSelectedRowReturnsFalseForSectionCursor(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.rows = []row{mkRow("audit", withGlobal, withRegistry("sec.git", "security"))}
	m.filteredRows = m.rows
	m.rebuildFlatItems()

	m.cursor = 0 // section header
	if _, ok := m.selectedRow(); ok {
		t.Fatalf("selectedRow should return false when cursor is on section header")
	}
}

func TestRenderListIndentsNestedSectionsUnderParent(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.loading = false
	m.rows = []row{mkRow("audit", withGlobal, withRegistry("sec.git", "security"))}
	m.filteredRows = m.rows
	m.rebuildFlatItems()

	out := renderList(m, 24)
	lines := splitByNewline(out)

	// Find the Global and security lines.
	var globalLine, securityLine string
	for _, l := range lines {
		if contains(l, "Global") {
			globalLine = l
		}
		if contains(l, "security") {
			securityLine = l
		}
	}
	if globalLine == "" || securityLine == "" {
		t.Fatalf("expected Global and security lines in:\n%s", out)
	}

	globalIndent := leadingSpaces(globalLine)
	securityIndent := leadingSpaces(securityLine)
	if securityIndent <= globalIndent {
		t.Fatalf("nested 'security' (indent=%d) should indent deeper than parent 'Global' (indent=%d)",
			securityIndent, globalIndent)
	}
}

func splitByNewline(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func leadingSpaces(s string) int {
	// Strip ANSI escape codes: find visible leading whitespace. We only
	// count runs of plain space characters before the first non-space,
	// ignoring escape sequences.
	count := 0
	i := 0
	for i < len(s) {
		if s[i] == 0x1b { // ESC
			// skip until 'm'
			for i < len(s) && s[i] != 'm' {
				i++
			}
			i++
			continue
		}
		if s[i] == ' ' {
			count++
			i++
			continue
		}
		break
	}
	return count
}

func TestRenderListShowsSectionHeadersAndSkillLines(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.loading = false
	m.rows = []row{
		mkRow("audit", withGlobal, withRegistry("sec.git", "security")),
	}
	m.filteredRows = m.rows
	m.rebuildFlatItems()

	out := renderList(m, 24)

	// Should contain the Global section header (▼ glyph + title) and the skill.
	for _, fragment := range []string{"Global", "audit", "▼"} {
		if !contains(out, fragment) {
			t.Fatalf("renderList output missing %q:\n%s", fragment, out)
		}
	}
}

func TestRenderListUsesCollapsedGlyphForFoldedSections(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.loading = false
	m.rows = []row{mkRow("audit", withGlobal, withRegistry("sec.git", "security"))}
	m.filteredRows = m.rows
	m.rebuildFlatItems()
	// Collapse scope:global
	m.cursor = 0
	m.toggleFoldAtCursor()
	m.rebuildFlatItems()

	out := renderList(m, 24)
	if !contains(out, "▶") {
		t.Fatalf("renderList should show ▶ for collapsed sections, got:\n%s", out)
	}
	if contains(out, "audit") {
		t.Fatalf("renderList should hide children of collapsed section, got:\n%s", out)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	if len(sub) == 0 {
		return 0
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// Ensure filteredRows is the source and filtering still works atop tree.
func TestRebuildFlatItemsHonorsFilteredRows(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.rows = []row{
		{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "audit"}}, EnabledGlobal: true, RegistryName: "security"},
		{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "extra"}}, EnabledGlobal: true, RegistryName: "security"},
	}
	// Only include "audit" in filteredRows.
	m.filteredRows = []row{m.rows[0]}
	m.rebuildFlatItems()

	// Only audit should appear in skills.
	var skills []string
	for _, it := range m.flatItems {
		if it.Kind == flatArtifact {
			skills = append(skills, it.Row.Artifact.Name)
		}
	}
	if len(skills) != 1 || skills[0] != "audit" {
		t.Fatalf("expected only 'audit' in filtered tree, got %v", skills)
	}
}

func TestRenderListHidesDefaultHarnessBadge(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.loading = false
	m.rows = []row{mkRow("audit", withProject, withHarnesses("factory", "cursor"), withRegistry("sec.git", "security"))}
	m.filteredRows = m.rows
	m.rebuildFlatItems()

	out := renderList(m, 24)
	if contains(out, "{factory,cursor}") {
		t.Fatalf("renderList should hide default harness badge, got:\n%s", out)
	}
}

func TestRenderListShowsRegistryOverrideHarnessBadge(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.loading = false
	m.rows = []row{mkRow("audit", withProject, withHarnesses("codex"), withHarnessSource("registry override"), withRegistry("sec.git", "security"))}
	m.filteredRows = m.rows
	m.rebuildFlatItems()

	out := renderList(m, 24)
	if !contains(out, "{Codex}") {
		t.Fatalf("renderList should include registry override harness badge, got:\n%s", out)
	}
}

func TestRenderListShowsResourceKindForEveryRow(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.loading = false
	m.rows = []row{
		mkRow("audit", withGlobal, withKind(ir.KindSkill), withRegistry("resources.git", "resources")),
		mkRow("continue", withGlobal, withKind(ir.KindCommand), withRegistry("resources.git", "resources")),
		mkRow("worker", withGlobal, withKind(ir.KindSubagent), withRegistry("resources.git", "resources")),
		mkRow("pre-tool-use", withGlobal, withKind(ir.KindHook), withRegistry("resources.git", "resources")),
		mkRow("memory", withGlobal, withKind(ir.KindMcpServer), withRegistry("resources.git", "resources")),
	}
	m.filteredRows = m.rows
	m.rebuildFlatItems()

	out := renderList(m, 24)
	for _, want := range []string{"skill audit", "command continue", "subagent worker", "hook pre-tool-use", "mcp memory"} {
		if !contains(out, want) {
			t.Fatalf("renderList output missing %q:\n%s", want, out)
		}
	}
}
