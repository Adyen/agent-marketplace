package tui

import (
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestModelRebuildFlatItemsGroupsByStatus(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.rows = []row{
		mkRow("audit", withGlobal, withRegistry("sec.git", "security")),
		mkRow("deploy", withAvailable, withRegistry("infra.git", "infra")),
	}
	m.filteredRows = m.rows
	m.rebuildFlatItems()

	paths := sectionPaths(m.flatItems)
	if len(paths) != 2 || paths[0] != "status:enabled" || paths[1] != "status:available" {
		t.Fatalf("expected status sections [status:enabled status:available], got %v", paths)
	}
}

func TestToggleFoldCurrentSectionTogglesFoldState(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.rows = []row{mkRow("audit", withGlobal, withRegistry("sec.git", "security"))}
	m.filteredRows = m.rows
	m.rebuildFlatItems()

	// Move cursor to the status:enabled section.
	m.cursor = 0
	if m.flatItems[m.cursor].SectionPath != "status:enabled" {
		t.Fatalf("expected cursor on status:enabled, got %q", m.flatItems[m.cursor].SectionPath)
	}

	m.toggleFoldAtCursor()
	if !m.currentFoldState()["status:enabled"] {
		t.Fatalf("toggleFoldAtCursor should collapse status:enabled")
	}

	m.rebuildFlatItems()

	// When collapsed, child items must not appear.
	for _, it := range m.flatItems {
		if it.Kind == flatArtifact && it.Row.Artifact.Name == "audit" {
			t.Fatalf("audit should be hidden under collapsed status:enabled")
		}
	}

	// Toggle again — expand.
	m.cursor = 0
	m.toggleFoldAtCursor()
	if m.currentFoldState()["status:enabled"] {
		t.Fatalf("toggleFoldAtCursor should expand status:enabled on second call")
	}
}

func TestCollapseAllAndExpandAllAffectAllSections(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.rows = []row{
		mkRow("a", withGlobal, withRegistry("sec.git", "security")),
		mkRow("b", withAvailable, withRegistry("infra.git", "infra")),
	}
	m.filteredRows = m.rows
	m.rebuildFlatItems()

	m.collapseAllSections()
	fs := m.currentFoldState()
	if !fs["status:enabled"] || !fs["status:available"] {
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

func TestCursorSkipsFromFoldedSectionToNextVisible(t *testing.T) {
	// With status:enabled collapsed, pressing down from it should land on the
	// next visible item (status:available).
	m := NewModel("/repo", "/home")
	m.rows = []row{
		mkRow("a", withGlobal, withRegistry("sec.git", "security")),
		mkRow("b", withAvailable, withRegistry("infra.git", "infra")),
	}
	m.filteredRows = m.rows
	m.rebuildFlatItems()

	// cursor on status:enabled, collapse it.
	m.cursor = 0
	m.toggleFoldAtCursor()
	m.rebuildFlatItems()

	// After collapse, status:enabled is still at index 0; pressing down should
	// go to status:available (next top-level section).
	m.cursor = 0
	if m.cursor+1 >= len(m.flatItems) {
		t.Fatalf("expected at least 2 flat items after collapse, got %d", len(m.flatItems))
	}
	next := m.flatItems[1]
	if next.Kind != flatSection || next.SectionPath != "status:available" {
		t.Fatalf("expected status:available at index 1 after collapse, got %+v", next)
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

func TestRenderListIndentsRowsUnderSection(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.loading = false
	m.rows = []row{mkRow("audit", withGlobal, withRegistry("sec.git", "security"))}
	m.filteredRows = m.rows
	m.rebuildFlatItems()

	out := renderList(m, 24)
	lines := splitByNewline(out)

	// Find the Enabled header and the audit row.
	var sectionLine, rowLine string
	for _, l := range lines {
		if contains(l, "Enabled") {
			sectionLine = l
		}
		if contains(l, "audit") {
			rowLine = l
		}
	}
	if sectionLine == "" || rowLine == "" {
		t.Fatalf("expected Enabled and audit lines in:\n%s", out)
	}

	sectionIndent := leadingSpaces(sectionLine)
	rowIndent := leadingSpaces(rowLine)
	if rowIndent <= sectionIndent {
		t.Fatalf("row 'audit' (indent=%d) should indent deeper than section 'Enabled' (indent=%d)",
			rowIndent, sectionIndent)
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

	// Should contain the Enabled section header (▼ glyph + title) and the
	// registry-qualified skill label.
	for _, fragment := range []string{"Enabled", "security/audit", "▼"} {
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
	// Collapse status:enabled
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
	m.width = 120
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
	for _, want := range []string{"skill resources/audit", "command resources/continue", "subagent resources/worker", "hook resources/pre-tool-use", "mcp resources/memory"} {
		if !contains(out, want) {
			t.Fatalf("renderList output missing %q:\n%s", want, out)
		}
	}
}
