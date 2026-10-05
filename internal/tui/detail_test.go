package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	ansi "github.com/charmbracelet/x/ansi"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// These tests pin down the right-pane rendering contract:
// the detail panel must describe the skill the cursor is actually on in
// the left-pane tree (m.flatItems[m.cursor]), NOT m.filteredRows[m.cursor].
// Those two indices diverge as soon as section headers sit above a skill.
// Further, when the cursor is on a section header, the detail panel must
// not pretend a skill is hovered.

func TestRenderDetailMatchesHoveredSkillAcrossSectionHeaders(t *testing.T) {
	// Two rows live in different top-level scope sections, so the
	// resulting flatItems layout is e.g.
	//   0: section "Global"
	//   1: section "security"   (sub-section)
	//   2: skill  "audit"
	//   3: section "Project"
	//   4: section "infra"      (sub-section)
	//   5: skill  "deploy"
	// Placing the cursor on index 5 (skill "deploy") must render details
	// for "deploy", not filteredRows[5] (which is out of range).
	m := NewModel("/repo", "/home")
	m.rows = []row{
		mkRow("audit", withGlobal, withRegistry("sec.git", "security")),
		mkRow("deploy", withProject, withRegistry("infra.git", "infra")),
	}
	m.filteredRows = m.rows
	m.rebuildFlatItems()

	// Locate the "deploy" skill flatItem.
	idx := -1
	for i, it := range m.flatItems {
		if it.Kind == flatArtifact && it.Row.Artifact.Name == "deploy" {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("could not find deploy skill in flatItems: %+v", m.flatItems)
	}
	m.cursor = idx

	out := renderDetail(m)
	if !contains(out, "deploy") {
		t.Fatalf("renderDetail should describe hovered skill 'deploy', got:\n%s", out)
	}
	// The mismatched row at filteredRows[idx] (out of range or wrong row)
	// must NOT leak through. In particular, 'audit' details should not show
	// when hovering 'deploy'.
	if contains(out, "audit") {
		t.Fatalf("renderDetail for cursor on 'deploy' must not show 'audit':\n%s", out)
	}
}

func TestRenderDetailShowsPlaceholderWhenCursorOnSectionHeader(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.rows = []row{
		mkRow("audit", withGlobal, withRegistry("sec.git", "security")),
	}
	m.filteredRows = m.rows
	m.rebuildFlatItems()

	// Cursor on the top-level "Global" section header.
	m.cursor = 0
	if m.flatItems[m.cursor].Kind != flatSection {
		t.Fatalf("precondition: cursor 0 should be a section header, got %+v", m.flatItems[m.cursor])
	}

	out := renderDetail(m)
	// Must not render skill metadata for the filteredRows[0] accident.
	if contains(out, "audit") {
		t.Fatalf("renderDetail must not show skill details when cursor is on a section header, got:\n%s", out)
	}
}

func TestRenderDetailShowsResourceKindAndName(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.rows = []row{
		mkRow("worker-generic", withGlobal, withKind(ir.KindSubagent), withRegistry("agents.git", "agents")),
	}
	m.filteredRows = m.rows
	m.rebuildFlatItems()
	for i, it := range m.flatItems {
		if it.Kind == flatArtifact {
			m.cursor = i
			break
		}
	}

	out := renderDetail(m)
	for _, want := range []string{"Kind:", "subagent", "Name:", "worker-generic"} {
		if !contains(out, want) {
			t.Fatalf("renderDetail output missing %q:\n%s", want, out)
		}
	}
	if contains(out, "Artifact:") {
		t.Fatalf("renderDetail should not label every resource as Artifact:\n%s", out)
	}
}

func TestRenderDetailFullShowsPlaceholderWhenCursorOnSectionHeader(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.rows = []row{
		mkRow("audit", withGlobal, withRegistry("sec.git", "security")),
	}
	m.filteredRows = m.rows
	m.rebuildFlatItems()

	m.cursor = 0 // section header

	out := renderDetailFull(m)
	// When the cursor is on a section, the full detail view should not
	// attempt to read a skill file or claim a skill is selected.
	if contains(out, "audit") {
		t.Fatalf("renderDetailFull must not show skill contents for a section header, got:\n%s", out)
	}
}

// --- Glamour rendering tests ---

func TestRenderMarkdownWithStyleProducesStyledOutput(t *testing.T) {
	input := "# Hello\n\nThis is **bold** text."
	out, err := renderMarkdownWithStyle(input, glamour.WithStylePath("dark"))
	if err != nil {
		t.Fatalf("renderMarkdownWithStyle: %v", err)
	}
	stripped := ansi.Strip(out)
	if !contains(stripped, "Hello") {
		t.Fatalf("styled output should contain 'Hello', got:\n%s", stripped)
	}
	if !contains(stripped, "bold") {
		t.Fatalf("styled output should contain 'bold', got:\n%s", stripped)
	}
	// Raw markdown syntax should be gone after rendering
	if contains(stripped, "**") {
		t.Fatalf("styled output should not contain raw '**' markdown, got:\n%s", stripped)
	}
}

func TestRenderMarkdownWithStyleFallsBackOnError(t *testing.T) {
	// Using an invalid style path should error, but the caller
	// (renderDetailFull) falls back to raw content.
	_, err := renderMarkdownWithStyle("# test", glamour.WithStylePath("/nonexistent/style.json"))
	if err == nil {
		t.Fatal("expected error from invalid style path")
	}
}

func TestRenderMarkdownAutoStyle(t *testing.T) {
	input := "## Section\n\nSome paragraph text."
	out, err := renderMarkdown(input)
	if err != nil {
		t.Fatalf("renderMarkdown: %v", err)
	}
	stripped := ansi.Strip(out)
	if !contains(stripped, "Section") {
		t.Fatalf("auto-styled output should contain 'Section', got:\n%s", stripped)
	}
}

func TestRenderDetailFullRendersWithGlamour(t *testing.T) {
	// Create a temp skill directory with SKILL.md
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "myskill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillContent := "# My Artifact\n\nA great **skill** for testing.\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewModel("/repo", "/home")
	m.rows = []row{{
		Artifact:      resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "myskill", Path: skillDir}},
		EnabledGlobal: true,
	}}
	m.rows[0].DetailSource = captureDetailSource(skillDir)
	m.filteredRows = m.rows
	m.rebuildFlatItems()

	// Find the skill flatItem
	idx := -1
	for i, it := range m.flatItems {
		if it.Kind == flatArtifact && it.Row.Artifact.Name == "myskill" {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("could not find myskill in flatItems")
	}
	m.cursor = idx

	out := renderDetailFull(m)
	stripped := ansi.Strip(out)
	if !contains(stripped, "My Artifact") {
		t.Fatalf("full view should contain heading 'My Artifact', got:\n%s", stripped)
	}
	if !contains(stripped, "skill") {
		t.Fatalf("full view should contain word 'skill', got:\n%s", stripped)
	}
}

func TestRenderDetailFullRendersCapturedNonSkillSource(t *testing.T) {
	dir := t.TempDir()
	commandPath := filepath.Join(dir, "continue.md")
	if err := os.WriteFile(commandPath, []byte("# Continue\n\nResume the work.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := row{
		Artifact:     resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindCommand, Name: "continue", Path: commandPath}},
		DetailSource: captureDetailSource(commandPath),
	}
	out := renderDetailFullContent(newTheme(true), r, 80)
	stripped := ansi.Strip(out)
	if !contains(stripped, "Continue") {
		t.Fatalf("full view should contain command source content, got:\n%s", stripped)
	}
}

func TestRenderDetailFullShowsAllSkillBundleFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# Review\n\nReview the change.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scripts", "check.sh"), []byte("#!/bin/sh\necho checked\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("# Notes\n\nKeep this nearby.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := renderDetailFullContent(newTheme(true), row{
		Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "review", Path: filepath.Join(dir, "SKILL.md")}},
	}, 80)
	stripped := ansi.Strip(out)
	for _, want := range []string{"SKILL.md", "Review the change.", "notes.md", "Keep this nearby.", "scripts/check.sh", "echo checked"} {
		if !contains(stripped, want) {
			t.Fatalf("full bundle view missing %q:\n%s", want, stripped)
		}
	}
	if strings.Index(stripped, "SKILL.md") > strings.Index(stripped, "notes.md") || strings.Index(stripped, "notes.md") > strings.Index(stripped, "scripts/check.sh") {
		t.Fatalf("bundle files are not rendered in deterministic order:\n%s", stripped)
	}
}

func TestDetailBundleSkipsSymlinkedFiles(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("must not be shown"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# Safe\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "secret.txt")); err != nil {
		t.Fatal(err)
	}

	out := renderDetailFullContent(newTheme(true), row{
		Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "safe", Path: dir}},
	}, 80)
	if contains(ansi.Strip(out), "must not be shown") {
		t.Fatalf("bundle view followed a symlink outside the resource:\n%s", ansi.Strip(out))
	}
}

// --- Viewport scroll tests ---

func TestDetailViewportScrollsDownWithJ(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.rows = []row{
		mkRow("audit", withGlobal, withRegistry("sec.git", "security")),
	}
	m.filteredRows = m.rows
	m.rebuildFlatItems()

	// Find the skill flatItem and enter detail pane.
	idx := -1
	for i, it := range m.flatItems {
		if it.Kind == flatArtifact {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("no skill flatItem found")
	}
	m.cursor = idx
	m.enterDetailPane()

	// Set content that exceeds the viewport height so scrolling is possible.
	longContent := strings.Repeat("line of content\n", 200)
	m.detailViewport.SetContent(longContent)

	// Scroll position should start at top.
	if m.detailViewport.AtTop() != true {
		t.Fatalf("viewport should start at top after entering detail pane")
	}

	// Simulate pressing "j" (down) — the viewport should handle it.
	updatedModel, _ := m.Update(tea.KeyPressMsg{Code: 'j', Text: string('j')})
	m2 := updatedModel.(Model)

	// After scrolling down, we should no longer be at the top.
	if m2.detailViewport.AtTop() {
		t.Fatalf("viewport should not be at top after pressing j")
	}
}

func TestDetailViewportScrollsUpWithK(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.rows = []row{
		mkRow("audit", withGlobal, withRegistry("sec.git", "security")),
	}
	m.filteredRows = m.rows
	m.rebuildFlatItems()

	idx := -1
	for i, it := range m.flatItems {
		if it.Kind == flatArtifact {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("no skill flatItem found")
	}
	m.cursor = idx
	m.enterDetailPane()

	longContent := strings.Repeat("line of content\n", 200)
	m.detailViewport.SetContent(longContent)

	// Scroll down several times, chaining model updates.
	model := tea.Model(&m)
	for i := 0; i < 5; i++ {
		updatedModel, _ := model.Update(tea.KeyPressMsg{Code: 'j', Text: string('j')})
		model = updatedModel
	}

	// Now scroll up.
	updatedModel, _ := model.Update(tea.KeyPressMsg{Code: 'k', Text: string('k')})
	m2 := updatedModel.(Model)

	// We scrolled down 5 and up 1, so we should not be at the top.
	if m2.detailViewport.AtTop() {
		t.Fatalf("viewport should not be at top after scrolling down 5 and up 1")
	}
}

func TestDetailViewportPageDownAndEnd(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.rows = []row{
		mkRow("audit", withGlobal, withRegistry("sec.git", "security")),
	}
	m.filteredRows = m.rows
	m.rebuildFlatItems()

	idx := -1
	for i, it := range m.flatItems {
		if it.Kind == flatArtifact {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("no skill flatItem found")
	}
	m.cursor = idx
	m.enterDetailPane()

	longContent := strings.Repeat("line of content\n", 200)
	m.detailViewport.SetContent(longContent)

	// pgdown should advance the scroll position.
	prevY := m.detailViewport.YOffset()
	updatedModel, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	m2 := updatedModel.(Model)
	if m2.detailViewport.YOffset() <= prevY {
		t.Fatalf("pgdown should advance scroll position: was %d, now %d", prevY, m2.detailViewport.YOffset())
	}

	// "G" (vim-style end) should jump to the bottom.
	updatedModel2, _ := m2.Update(tea.KeyPressMsg{Code: 'G', Text: string('G')})
	m3 := updatedModel2.(Model)
	if !m3.detailViewport.AtBottom() {
		t.Fatalf("G should scroll to bottom of content")
	}
}

func TestDetailViewportHomeJumpsToTop(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.rows = []row{
		mkRow("audit", withGlobal, withRegistry("sec.git", "security")),
	}
	m.filteredRows = m.rows
	m.rebuildFlatItems()

	idx := -1
	for i, it := range m.flatItems {
		if it.Kind == flatArtifact {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("no skill flatItem found")
	}
	m.cursor = idx
	m.enterDetailPane()

	longContent := strings.Repeat("line of content\n", 200)
	m.detailViewport.SetContent(longContent)

	// Scroll to the bottom first.
	m.detailViewport.GotoBottom()

	// "g" (home) should jump to the top.
	updatedModel, _ := m.Update(tea.KeyPressMsg{Code: 'g', Text: string('g')})
	m2 := updatedModel.(Model)
	if !m2.detailViewport.AtTop() {
		t.Fatalf("g should scroll to top of content")
	}
}

func TestDetailViewportEscReturnsToList(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.rows = []row{
		mkRow("audit", withGlobal, withRegistry("sec.git", "security")),
	}
	m.filteredRows = m.rows
	m.rebuildFlatItems()

	idx := -1
	for i, it := range m.flatItems {
		if it.Kind == flatArtifact {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("no skill flatItem found")
	}
	m.cursor = idx
	m.enterDetailPane()

	if m.pane != paneDetail {
		t.Fatalf("expected paneDetail after entering detail pane, got %v", m.pane)
	}

	// Esc should return to list pane.
	updatedModel, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m2 := updatedModel.(Model)
	if m2.pane != paneList {
		t.Fatalf("esc should return to list pane, got %v", m2.pane)
	}
}

func TestDetailViewportResizesOnWindowResize(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.rows = []row{
		mkRow("audit", withGlobal, withRegistry("sec.git", "security")),
	}
	m.filteredRows = m.rows
	m.rebuildFlatItems()

	idx := -1
	for i, it := range m.flatItems {
		if it.Kind == flatArtifact {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("no skill flatItem found")
	}
	m.cursor = idx
	m.enterDetailPane()

	// Resize the window.
	updatedModel, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m2 := updatedModel.(Model)

	if m2.detailViewport.Width() != 120 {
		t.Fatalf("viewport width should be 120 after resize, got %d", m2.detailViewport.Width())
	}
	if m2.detailViewport.Height() != 39 {
		t.Fatalf("viewport height should reserve the footer row: got %d, want 39", m2.detailViewport.Height())
	}
}

func TestRenderDetailShowsHarnessesForEnabledSkill(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.rows = []row{mkRow("audit", withProject, withHarnesses("codex"), withRegistry("sec.git", "security"))}
	m.filteredRows = m.rows
	m.rebuildFlatItems()
	for i, it := range m.flatItems {
		if it.Kind == flatArtifact {
			m.cursor = i
			break
		}
	}

	out := renderDetail(m)
	for _, want := range []string{"Targets:", "Codex"} {
		if !contains(out, want) {
			t.Fatalf("detail missing %q:\n%s", want, out)
		}
	}
}

func TestRenderDetailShowsWouldTargetForAvailableSkill(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.rows = []row{mkRow("audit", withAvailable, withHarnesses("factory"), withRegistry("sec.git", "security"))}
	m.filteredRows = m.rows
	m.rebuildFlatItems()
	for i, it := range m.flatItems {
		if it.Kind == flatArtifact {
			m.cursor = i
			break
		}
	}

	out := renderDetail(m)
	for _, want := range []string{"Would target:", "Factory Droid"} {
		if !contains(out, want) {
			t.Fatalf("detail missing %q:\n%s", want, out)
		}
	}
}

func TestDetailCaptureBudgetBoundsManyMaximumEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "maximum.md")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", maxDetailSourceBytes+1)), 0o644); err != nil {
		t.Fatal(err)
	}
	var budget detailCaptureBudget
	var snapshots []*detailSourceSnapshot
	for i := 0; i < maxDetailCaptureEntries+16; i++ {
		snapshots = append(snapshots, budget.capture(path))
	}
	if budget.bytes > maxDetailCaptureBytes || budget.entries > maxDetailCaptureEntries {
		t.Fatalf("aggregate capture exceeded budget: bytes=%d entries=%d", budget.bytes, budget.entries)
	}
	var truncated, omitted int
	for _, snapshot := range snapshots {
		if snapshot.truncationMarker != "" {
			truncated++
		}
		if snapshot.omitted {
			omitted++
			if got := renderDetailFullContent(newTheme(true), row{DetailSource: snapshot}, 80); got != detailCaptureOmittedMarker {
				t.Fatalf("omitted detail marker = %q, want %q", got, detailCaptureOmittedMarker)
			}
		}
	}
	if truncated == 0 || omitted == 0 {
		t.Fatalf("budget must visibly mark both truncated and omitted details: truncated=%d omitted=%d", truncated, omitted)
	}

	var entryBudget detailCaptureBudget
	small := filepath.Join(t.TempDir(), "small.md")
	if err := os.WriteFile(small, []byte("small"), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxDetailCaptureEntries; i++ {
		if snapshot := entryBudget.capture(small); snapshot.omitted {
			t.Fatalf("entry %d omitted before entry budget", i)
		}
	}
	if snapshot := entryBudget.capture(small); !snapshot.omitted {
		t.Fatal("entry beyond entry budget was captured")
	}
}

func TestRenderDetailShowsPathForMalformedSkill(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.rows = []row{
		mkRow("broken-skill", withMalformed("parse yaml: bad frontmatter")),
	}
	m.filteredRows = m.rows
	m.rebuildFlatItems()
	for i, it := range m.flatItems {
		if it.Kind == flatArtifact {
			m.cursor = i
			break
		}
	}

	out := renderDetail(m)

	// The error message must be shown.
	if !contains(out, "Error:") {
		t.Fatalf("malformed skill detail should contain Error: label:\n%s", out)
	}
	if !contains(out, "parse yaml: bad frontmatter") {
		t.Fatalf("malformed skill detail should contain error message:\n%s", out)
	}
	// The file path must also be shown (this is the fix — previously
	// the Source line was skipped for malformed skills).
	if !contains(out, "Source:") {
		t.Fatalf("malformed skill detail should contain Source: label:\n%s", out)
	}
	if !contains(out, "/cache/broken-skill") {
		t.Fatalf("malformed skill detail should contain the skill path:\n%s", out)
	}
}
