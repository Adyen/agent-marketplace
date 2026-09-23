package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func inlineBundleRow(t testing.TB, name string) row {
	t.Helper()
	root := t.TempDir()
	for _, file := range []string{"SKILL.md", "scripts/check.py", "z-last.txt"} {
		path := filepath.Join(root, file)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(name+"_CONTENT\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r := mkRow(name, withProject)
	r.Artifact.Path = root
	r.Artifact.Description = "Description of " + name
	return r
}

func applyBundleCommand(t testing.TB, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for n := 0; cmd != nil; n++ {
		if n > 10 {
			t.Fatal("bundle commands did not settle")
		}
		msg := cmd()
		switch msg.(type) {
		case bundleLoadedMsg, detailContentMsg:
		default:
			t.Fatalf("unexpected bundle command result %T", msg)
		}
		updated, next := m.Update(msg)
		m, cmd = updated.(Model), next
	}
	return m
}

func TestBundleArrowKeysSwitchPaneFocus(t *testing.T) {
	m := modelWithSelectedRow(t, inlineBundleRow(t, "alpha"))
	updated, load := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = applyBundleCommand(t, updated.(Model), load)
	if m.pane != paneDetail || m.bundle == nil || m.detailSelectedPath != "SKILL.md" {
		t.Fatal("right arrow did not focus and load the file pane")
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	if cmd != nil || updated.(Model).operations.active != nil || updated.(Model).pane != paneDetail {
		t.Fatal("right arrow on a file should retain focus without opening the pager")
	}
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyHome})
	m = applyBundleCommand(t, updated.(Model), cmd)
	updated, cmd = m.Update(keyPress('l'))
	m = applyBundleCommand(t, updated.(Model), cmd)
	entry, ok := m.bundle.selectedEntry()
	if !ok || !entry.IsDir || !entry.Expanded {
		t.Fatal("l did not expand the selected directory")
	}
	tree, cursor := m.bundle, m.bundle.cursor
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = updated.(Model)
	entry, ok = m.bundle.selectedEntry()
	if cmd != nil || m.pane != paneList || m.bundle != tree || m.bundle.cursor != cursor || !ok || !entry.Expanded {
		t.Fatal("left arrow should return focus without collapsing or discarding the tree")
	}
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = applyBundleCommand(t, updated.(Model), cmd)
	if m.pane != paneDetail || m.bundle != tree || m.bundle.cursor != cursor {
		t.Fatal("right arrow did not restore file focus and selection")
	}
	updated, cmd = m.Update(keyPress('h'))
	m = applyBundleCommand(t, updated.(Model), cmd)
	entry, _ = m.bundle.selectedEntry()
	if m.pane != paneDetail || entry.Expanded {
		t.Fatal("h should still collapse directories without moving pane focus")
	}
}

func TestBundleRightArrowExpandsSectionsWithoutToggling(t *testing.T) {
	m := modelWithSelectedRow(t, inlineBundleRow(t, "alpha"))
	m.cursor = 0
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = updated.(Model)
	if len(m.flatItems) != 1 {
		t.Fatal("left arrow did not collapse the section")
	}
	for i := 0; i < 2; i++ {
		updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRight})
		m = updated.(Model)
		if cmd != nil || m.pane != paneList || len(m.flatItems) <= 1 {
			t.Fatal("right arrow should expand a section and leave it expanded")
		}
	}
}

func TestBundleLivesInMainView(t *testing.T) {
	m := bundlePagerTestModel(t)
	m.pane = paneList
	view := ansi.Strip(m.View())
	for _, want := range []string{"agent-marketplace", "source.py", "enter browse"} {
		if !strings.Contains(view, want) {
			t.Fatalf("main view missing %q: %s", want, view)
		}
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	view = ansi.Strip(m.View())
	for _, want := range []string{"agent-marketplace", "source.py", "enter open", "esc resources"} {
		if !strings.Contains(view, want) {
			t.Fatalf("focused view missing %q: %s", want, view)
		}
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if got := updated.(Model); got.pane != paneList || got.bundle == nil || got.detailSelectedPath != "source.py" {
		t.Fatal("returning focus discarded the file tree or selection")
	}
}

func TestBundleEnterOpensFileWithoutOShortcut(t *testing.T) {
	m := bundlePagerTestModel(t)
	t.Setenv("PAGER", "missing-pager")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || updated.(Model).operations.active == nil || updated.(Model).operations.active.kind != operationPager {
		t.Fatal("Enter on a file did not schedule the pager")
	}
	updated, cmd = m.Update(keyPress('o'))
	if cmd != nil || updated.(Model).operations.active != nil {
		t.Fatal("obsolete o shortcut still launches the pager")
	}
}

func TestBundleLoadsOnResourceSelectionAndEnterWhileLoading(t *testing.T) {
	r := inlineBundleRow(t, "alpha")
	m := modelWithSelectedRow(t, r)
	m.cursor--
	updated, load := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	if load == nil || m.bundle != nil || m.pane != paneList || !strings.Contains(ansi.Strip(m.View()), "Description of alpha") {
		t.Fatal("selecting a resource did not schedule its tree without changing focus")
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if cmd != nil || m.pane != paneDetail {
		t.Fatal("Enter while loading should focus files without a duplicate load")
	}
	m = applyBundleCommand(t, m, load)
	if m.detailSelectedPath != "SKILL.md" || !strings.Contains(ansi.Strip(m.View()), "alpha_CONTENT") {
		t.Fatalf("loaded focused bundle missing initial preview: %s", m.View())
	}
}

func TestBundleRejectsStaleTreeAndPreviewAcrossResources(t *testing.T) {
	alpha, beta := inlineBundleRow(t, "alpha"), inlineBundleRow(t, "beta")
	m := modelWithSelectedRow(t, alpha)
	m.rows = append(m.rows, beta)
	m.recomputeView()
	firstLoad := m.syncInlineBundle()
	first := firstLoad().(bundleLoadedMsg)
	updated, secondLoad := m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	m = updated.(Model)
	if secondLoad == nil {
		t.Fatal("moving to beta did not load its bundle")
	}
	updated, cmd := m.Update(first)
	m = updated.(Model)
	if cmd != nil || m.bundle != nil || m.detailKey != rowKey(beta) {
		t.Fatal("stale alpha tree changed beta selection")
	}
	second := secondLoad().(bundleLoadedMsg)
	m = applyBundleCommand(t, m, func() tea.Msg { return second })
	betaRoot, err := filepath.EvalSymlinks(beta.Artifact.Path)
	if err != nil {
		t.Fatal(err)
	}
	if m.bundle == nil || m.bundle.root != betaRoot {
		t.Fatal("current beta tree was not accepted")
	}
	updated, cmd = m.Update(second)
	if cmd != nil || updated.(Model).bundle != m.bundle {
		t.Fatal("duplicate tree completion changed state")
	}
	updated, oldPreview := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if oldPreview == nil {
		t.Fatal("focus did not request beta preview")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	updated, load := updated.(Model).Update(tea.KeyMsg{Type: tea.KeyUp})
	m = applyBundleCommand(t, updated.(Model), load)
	updated, preview := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	updated, cmd = m.Update(oldPreview())
	m = updated.(Model)
	if cmd != nil || strings.Contains(ansi.Strip(m.View()), "beta_CONTENT") {
		t.Fatal("stale preview from same relative path replaced alpha")
	}
	m = applyBundleCommand(t, m, preview)
	if !strings.Contains(ansi.Strip(m.View()), "alpha_CONTENT") {
		t.Fatal("current alpha preview was not accepted")
	}
}

func TestBundleDirectoryNavigationClearsPreviewAndDoesNotLaunchPager(t *testing.T) {
	m := modelWithSelectedRow(t, inlineBundleRow(t, "alpha"))
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = applyBundleCommand(t, updated.(Model), cmd)
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyHome})
	m = updated.(Model)
	if cmd != nil || m.detailSelectedPath != "" || strings.Contains(ansi.Strip(m.View()), "alpha_CONTENT") {
		t.Fatal("directory selection retained a stale file preview")
	}
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if cmd != nil || m.operations.active != nil || !strings.Contains(ansi.Strip(m.View()), "check.py") {
		t.Fatal("Enter on directory did not expand without launching pager")
	}
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = applyBundleCommand(t, updated.(Model), cmd)
	if m.detailSelectedPath != "scripts/check.py" {
		t.Fatal("nested file selection did not update preview")
	}
	updated, cmd = m.Update(keyPress('h'))
	m = updated.(Model)
	if cmd != nil || m.detailSelectedPath != "" {
		t.Fatal("moving back to parent kept file preview active")
	}
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	m = applyBundleCommand(t, updated.(Model), cmd)
	if m.detailSelectedPath != "z-last.txt" {
		t.Fatal("End did not preview the last file")
	}
}

func TestBundleCursorRemainsVisibleAfterResize(t *testing.T) {
	r := inlineBundleRow(t, "alpha")
	for i := 0; i < 30; i++ {
		if err := os.WriteFile(filepath.Join(r.Artifact.Path, fmt.Sprintf("file-%02d.txt", i)), []byte("content"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := modelWithSelectedRow(t, r)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = applyBundleCommand(t, updated.(Model), cmd)
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	m = applyBundleCommand(t, updated.(Model), cmd)
	for _, size := range []tea.WindowSizeMsg{{Width: 120, Height: 40}, {Width: 50, Height: 12}, {Width: 80, Height: 24}} {
		updated, cmd = m.Update(size)
		m = applyBundleCommand(t, updated.(Model), cmd)
		_, treeHeight, _ := m.bundlePaneHeights()
		if view := ansi.Strip(renderBundleTree(m, 29, treeHeight)); !strings.Contains(view, "z-last.txt") {
			t.Fatalf("last selected file scrolled out of tree after resize: %s", view)
		}
		assertTerminalBounds(t, m.View(), size.Width, size.Height)
	}
}

func TestBundleHiddenPreviewDoesNotRenderUntilSpaceReturns(t *testing.T) {
	m := modelWithSelectedRow(t, inlineBundleRow(t, "alpha"))
	m.width, m.height = 50, 12
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = applyBundleCommand(t, updated.(Model), cmd)
	if m.requests.active(requestDetail) != 0 || m.detailRenderWidth != 0 {
		t.Fatal("hidden preview performed rendering")
	}
	updated, cmd = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if cmd == nil {
		t.Fatal("growing the terminal did not render the selected file")
	}
	m = applyBundleCommand(t, updated.(Model), cmd)
	if !strings.Contains(ansi.Strip(m.View()), "alpha_CONTENT") {
		t.Fatal("preview was not displayed after growing")
	}
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyHome})
	m = applyBundleCommand(t, updated.(Model), cmd)
	updated, cmd = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if cmd != nil || updated.(Model).detailSelectedPath != "" {
		t.Fatal("resizing a selected directory rendered unrelated resource content")
	}
}

func TestBundlePreviewResumesAfterHelpInterruptsLoading(t *testing.T) {
	m := modelWithSelectedRow(t, inlineBundleRow(t, "alpha"))
	load := m.syncInlineBundle()
	m = applyBundleCommand(t, m, load)
	updated, preview := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if preview == nil {
		t.Fatal("no pending preview to interrupt")
	}
	updated, _ = m.Update(keyPress('?'))
	m = updated.(Model)
	if m.pane != paneHelp || m.requests.active(requestDetail) != 0 {
		t.Fatal("help did not cancel the pending preview")
	}
	updated, _ = m.Update(preview())
	updated, _ = updated.(Model).Update(tea.KeyMsg{Type: tea.KeyEscape})
	updated, preview = updated.(Model).Update(tea.KeyMsg{Type: tea.KeyEnter})
	if preview == nil {
		t.Fatal("returning from help left the preview stuck loading")
	}
	m = applyBundleCommand(t, updated.(Model), preview)
	if !strings.Contains(ansi.Strip(m.View()), "alpha_CONTENT") {
		t.Fatal("preview did not recover after help")
	}
}

func TestBundleLoadErrorAndSectionSelection(t *testing.T) {
	r := mkRow("missing", withProject)
	r.Artifact.Path = filepath.Join(t.TempDir(), "not-present")
	m := modelWithSelectedRow(t, r)
	m.cursor--
	updated, load := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = applyBundleCommand(t, updated.(Model), load)
	if view := ansi.Strip(m.View()); !strings.Contains(view, "Error reading resource") {
		t.Fatalf("missing bundle error is not visible: %s", view)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(Model)
	if m.bundle != nil || m.bundleKey != "" || m.detailSelectedPath != "" || strings.Contains(ansi.Strip(m.View()), "Error reading resource") {
		t.Fatal("section selection leaked previous resource state")
	}
}

func TestBundleAdaptivePreview(t *testing.T) {
	m := bundlePagerTestModel(t)
	for _, tc := range []struct {
		width, height int
		preview       bool
	}{{120, 40, true}, {80, 24, true}, {50, 12, false}} {
		updated, _ := m.Update(tea.WindowSizeMsg{Width: tc.width, Height: tc.height})
		m = updated.(Model)
		m.detailViewport.SetContent("UNIQUE_PREVIEW_CONTENT")
		view := ansi.Strip(m.View())
		if strings.Contains(view, "UNIQUE_PREVIEW_CONTENT") != tc.preview {
			t.Fatalf("preview visibility at %dx%d: %s", tc.width, tc.height, view)
		}
		if !strings.Contains(view, "source.py") || !strings.Contains(view, "enter open") {
			t.Fatalf("preview crowded out tree or actions: %s", view)
		}
		assertTerminalBounds(t, m.View(), tc.width, tc.height)
	}
}
